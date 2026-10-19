package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	authpkg "github.com/echoface/ozddns/internal/auth"
)

var authClient = ClientInfo{IP: "127.0.0.1", UserAgent: "auth-test"}

func enableTestAuth(t *testing.T, a *App, username, password string) (secret string, recovery []string) {
	t.Helper()
	ctx := context.Background()
	info, err := a.StartSetup(ctx, username, password, authClient)
	if err != nil {
		t.Fatalf("StartSetup: %v", err)
	}
	code, err := authpkg.TOTPCode(info.Secret, time.Now())
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}
	recovery, _, err = a.VerifySetup(ctx, code, authClient)
	if err != nil {
		t.Fatalf("VerifySetup: %v", err)
	}
	return info.Secret, recovery
}

func nextTOTP(t *testing.T, secret string) string {
	t.Helper()
	code, err := authpkg.TOTPCode(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}
	return code
}

func TestSetupCreatesSingleAdministratorAndTenRecoveryCodes(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	a.ListenAddr = "127.0.0.1:9876"

	state, err := a.AuthState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Enabled || state.Exposed || state.SetupPending {
		t.Fatalf("fresh state = %+v", state)
	}

	if _, err := a.StartSetup(ctx, "admin", "short", authClient); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short password error = %v", err)
	}
	info, err := a.StartSetup(ctx, "admin", "correct horse battery", authClient)
	if err != nil {
		t.Fatalf("StartSetup: %v", err)
	}
	if len(info.Secret) < 20 || info.OTPAuthURL == "" ||
		!strings.HasPrefix(info.QRCodeDataURL, "data:image/png;base64,") || info.ExpiresAt.IsZero() {
		t.Fatalf("setup info = %+v", info)
	}
	state, _ = a.AuthState(ctx)
	if !state.SetupPending || state.Enabled {
		t.Fatalf("pending state = %+v", state)
	}
	if _, _, err := a.VerifySetup(ctx, "000000", authClient); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("invalid setup code error = %v", err)
	}

	code, err := authpkg.TOTPCode(info.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, user, err := a.VerifySetup(ctx, code, authClient)
	if err != nil {
		t.Fatalf("VerifySetup: %v", err)
	}
	if user.Username != "admin" || len(codes) != RecoveryCodeCount {
		t.Fatalf("user=%+v codes=%d", user, len(codes))
	}
	seen := map[string]bool{}
	for _, value := range codes {
		if seen[value] {
			t.Fatalf("duplicate recovery code %q", value)
		}
		seen[value] = true
	}
	state, _ = a.AuthState(ctx)
	if !state.Enabled || state.SetupPending || state.RecoveryCodesRemaining != RecoveryCodeCount {
		t.Fatalf("enabled state = %+v", state)
	}
	if _, err := a.StartSetup(ctx, "other", "another password", authClient); !errors.Is(err, ErrAuthEnabled) {
		t.Fatalf("second setup error = %v", err)
	}
}

func TestConcurrentSetupOnlyCreatesOneAccount(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	info, err := a.StartSetup(ctx, "admin", "correct horse battery", authClient)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := authpkg.TOTPCode(info.Secret, time.Now())

	type result struct {
		err   error
		codes int
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			codes, _, err := a.VerifySetup(ctx, code, authClient)
			results <- result{err: err, codes: len(codes)}
		}()
	}
	close(start)
	okCount := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			okCount++
			if result.codes != RecoveryCodeCount {
				t.Fatalf("winner codes = %d", result.codes)
			}
		} else if !errors.Is(result.err, ErrAuthEnabled) {
			t.Fatalf("loser error = %v", result.err)
		}
	}
	if okCount != 1 {
		t.Fatalf("successful setups = %d, want 1", okCount)
	}
	codes, err := a.Store.Auth().ListRecoveryCodes(ctx)
	if err != nil || len(codes) != RecoveryCodeCount {
		t.Fatalf("stored recovery codes = %d, err=%v", len(codes), err)
	}
}

func TestLoginRequiresMFAAndRejectsTOTPReplay(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	secret, _ := enableTestAuth(t, a, "admin", "correct horse battery")

	if _, err := a.Login(ctx, "admin", "wrong password", authClient); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v", err)
	}
	challenge, err := a.Login(ctx, "admin", "correct horse battery", authClient)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if challenge == "" {
		t.Fatal("empty challenge")
	}
	if _, _, err := a.VerifyMFA(ctx, challenge, "000000", authClient); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("invalid MFA error = %v", err)
	}

	code := nextTOTP(t, secret)
	token, user, err := a.VerifyMFA(ctx, challenge, code, authClient)
	if err != nil {
		t.Fatalf("VerifyMFA: %v", err)
	}
	if token == "" || user.Username != "admin" {
		t.Fatalf("token=%q user=%+v", token, user)
	}
	if got, err := a.AuthenticateSession(ctx, token); err != nil || got.Username != "admin" {
		t.Fatalf("AuthenticateSession: user=%+v err=%v", got, err)
	}

	// A second challenge cannot reuse the same TOTP step.
	challenge, err = a.Login(ctx, "admin", "correct horse battery", authClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.VerifyMFA(ctx, challenge, code, authClient); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("TOTP replay error = %v", err)
	}
}

func TestRecoveryCodeIsSingleUseAndSessionCanBeRevoked(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	_, recovery := enableTestAuth(t, a, "admin", "correct horse battery")

	challenge, err := a.Login(ctx, "admin", "correct horse battery", authClient)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := a.VerifyMFA(ctx, challenge, recovery[0], authClient)
	if err != nil {
		t.Fatalf("recovery login: %v", err)
	}

	challenge, err = a.Login(ctx, "admin", "correct horse battery", authClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.VerifyMFA(ctx, challenge, recovery[0], authClient); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("recovery replay error = %v", err)
	}

	if err := a.Logout(ctx, token, authClient); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AuthenticateSession(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("session after logout error = %v", err)
	}
	state, _ := a.AuthState(ctx)
	if state.RecoveryCodesRemaining != RecoveryCodeCount-1 {
		t.Fatalf("recovery remaining = %d", state.RecoveryCodesRemaining)
	}
}

func TestDisableDeletesSessionsAndReenableUsesNewSecret(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	firstSecret, recovery := enableTestAuth(t, a, "admin", "correct horse battery")

	token, err := a.CreateSession(ctx, authClient)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DisableAuth(ctx, "wrong", recovery[0], authClient); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong disable password error = %v", err)
	}
	if err := a.DisableAuth(ctx, "correct horse battery", recovery[0], authClient); err != nil {
		t.Fatalf("DisableAuth: %v", err)
	}
	if _, err := a.AuthenticateSession(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("session after disable error = %v", err)
	}
	state, _ := a.AuthState(ctx)
	if state.Enabled || state.Sessions != 0 || state.RecoveryCodesRemaining != 0 {
		t.Fatalf("state after disable = %+v", state)
	}

	secondSecret, _ := enableTestAuth(t, a, "admin", "correct horse battery")
	if secondSecret == firstSecret {
		t.Fatal("re-enabling reused the previous TOTP secret")
	}
}

func TestRegenerateRecoveryCodesReplacesOldSet(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	_, oldCodes := enableTestAuth(t, a, "admin", "correct horse battery")

	newCodes, err := a.RegenerateRecoveryCodes(ctx, "correct horse battery", oldCodes[0], authClient)
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes: %v", err)
	}
	if len(newCodes) != RecoveryCodeCount {
		t.Fatalf("new code count = %d", len(newCodes))
	}
	challenge, err := a.Login(ctx, "admin", "correct horse battery", authClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.VerifyMFA(ctx, challenge, oldCodes[1], authClient); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("old recovery code error = %v", err)
	}
	challenge, _ = a.Login(ctx, "admin", "correct horse battery", authClient)
	if _, _, err := a.VerifyMFA(ctx, challenge, newCodes[0], authClient); err != nil {
		t.Fatalf("new recovery code: %v", err)
	}
}

func TestAuthStateReportsActualNetworkExposure(t *testing.T) {
	a := openApp(t)
	a.ListenAddr = "0.0.0.0:9876"
	state, err := a.AuthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exposed {
		t.Fatal("wildcard listener must be reported as exposed")
	}
}

func TestTrustedNetworkSuppressesExposure(t *testing.T) {
	a := openApp(t)
	// An overlay address (Tailscale CGNAT) is beyond loopback, so it is exposed
	// by default and only the explicit assertion clears it.
	a.ListenAddr = "100.64.0.10:9876"
	state, err := a.AuthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exposed {
		t.Fatal("an overlay listener must be reported as exposed by default")
	}

	a.TrustedNetwork = true
	state, err = a.AuthState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Exposed {
		t.Fatal("trusted network must suppress the exposure flag")
	}

	// Loopback is never exposed, with or without the flag.
	a.ListenAddr = "127.0.0.1:9876"
	a.TrustedNetwork = false
	if state, _ = a.AuthState(context.Background()); state.Exposed {
		t.Fatal("loopback must never be reported as exposed")
	}
}

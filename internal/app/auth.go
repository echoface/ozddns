package app

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/echoface/ozddns/internal/auth"
	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ids"
	"github.com/echoface/ozddns/internal/storage"
)

var (
	ErrAuthEnabled        = errors.New("authorization is already enabled")
	ErrAuthDisabled       = errors.New("authorization is not enabled")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidCode        = errors.New("invalid authentication code")
	ErrSetupExpired       = errors.New("authorization setup expired; start again")
	ErrTooManyAttempts    = errors.New("too many authentication attempts")
	ErrUnauthorized       = errors.New("session required")
	ErrInvalidInput       = errors.New("invalid input")
)

const (
	SessionIdleTTL       = 30 * time.Minute
	SessionAbsoluteTTL   = 12 * time.Hour
	SetupTTL             = 15 * time.Minute
	ChallengeTTL         = 5 * time.Minute
	MaxMFAAttempts       = 5
	RecoveryCodeCount    = 10
	sessionTouchInterval = 30 * time.Second
)

// ClientInfo is the request metadata recorded with sessions and audit events.
type ClientInfo struct {
	IP        string
	UserAgent string
}

// AuthState is the public description of the authorization mode.
type AuthState struct {
	Enabled                bool   `json:"enabled"`
	Authenticated          bool   `json:"authenticated"`
	SetupPending           bool   `json:"setupPending"`
	Username               string `json:"username,omitempty"`
	RecoveryCodesRemaining int    `json:"recoveryCodesRemaining,omitempty"`
	Sessions               int    `json:"sessions,omitempty"`
	Listen                 string `json:"listen"`
	Exposed                bool   `json:"exposed"`
}

// SetupInfo contains the one-time enrollment material shown to the operator.
type SetupInfo struct {
	Secret        string    `json:"secret"`
	OTPAuthURL    string    `json:"otpauthUrl"`
	QRCodeDataURL string    `json:"qrCodeDataUrl"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// AuthEnabled reports whether the single administrator account exists.
func (a *App) AuthEnabled(ctx context.Context) (bool, error) {
	_, err := a.Store.Auth().User(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// AuthState summarizes the mode for the public /auth/state endpoint.
func (a *App) AuthState(ctx context.Context) (AuthState, error) {
	now := time.Now().UTC()
	state := AuthState{Listen: a.ListenAddr, Exposed: a.listenExposed()}

	user, err := a.Store.Auth().User(ctx)
	switch {
	case err == nil:
		state.Enabled = true
		state.Username = user.Username
		codes, _ := a.Store.Auth().ListRecoveryCodes(ctx)
		for _, code := range codes {
			if code.UsedAt == nil {
				state.RecoveryCodesRemaining++
			}
		}
	case errors.Is(err, storage.ErrNotFound):
	default:
		return AuthState{}, err
	}

	pending, err := a.Store.Auth().Pending(ctx)
	if err == nil {
		if now.Before(pending.ExpiresAt) {
			state.SetupPending = true
		} else {
			_ = a.Store.Auth().DeletePending(ctx)
		}
	}

	_ = a.Store.Auth().DeleteExpiredSessions(ctx, now)
	_ = a.Store.Auth().DeleteExpiredChallenges(ctx, now)
	state.Sessions, _ = a.Store.Auth().SessionCount(ctx)
	return state, nil
}

// StartSetup validates the first factors and returns a fresh TOTP secret. The
// account is not created until VerifySetup sees a valid code.
func (a *App) StartSetup(ctx context.Context, username, password string, client ClientInfo) (SetupInfo, error) {
	if enabled, err := a.AuthEnabled(ctx); err != nil {
		return SetupInfo{}, err
	} else if enabled {
		return SetupInfo{}, ErrAuthEnabled
	}
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 {
		return SetupInfo{}, fmt.Errorf("%w: username must be between 1 and 64 characters", ErrInvalidInput)
	}
	if strings.IndexFunc(username, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return SetupInfo{}, fmt.Errorf("%w: username must not contain control characters", ErrInvalidInput)
	}
	if len(password) < auth.MinPasswordLen {
		return SetupInfo{}, fmt.Errorf("%w: password must be at least 10 characters", ErrInvalidInput)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return SetupInfo{}, err
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return SetupInfo{}, err
	}
	now := time.Now().UTC()
	pending := domain.AuthPending{
		Username: username, PasswordHash: hash, TOTPSecret: secret,
		CreatedAt: now, ExpiresAt: now.Add(SetupTTL),
	}
	if err := a.Store.Auth().SavePending(ctx, pending); err != nil {
		return SetupInfo{}, err
	}
	_ = a.Store.Auth().AddEvent(ctx, domain.AuthEvent{
		At: now, Event: "setup_started", Username: username,
		IP: client.IP, UserAgent: client.UserAgent,
	})
	otpauthURL := auth.TOTPURL("OzDDNS", username, secret)
	qrPNG, err := qrcode.Encode(otpauthURL, qrcode.Medium, 240)
	if err != nil {
		return SetupInfo{}, fmt.Errorf("generate TOTP QR code: %w", err)
	}
	return SetupInfo{
		Secret: secret, OTPAuthURL: otpauthURL,
		QRCodeDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrPNG),
		ExpiresAt:     pending.ExpiresAt,
	}, nil
}

// VerifySetup completes enrollment and returns the recovery codes exactly once.
func (a *App) VerifySetup(ctx context.Context, code string, client ClientInfo) ([]string, domain.AuthUser, error) {
	if enabled, err := a.AuthEnabled(ctx); err != nil {
		return nil, domain.AuthUser{}, err
	} else if enabled {
		return nil, domain.AuthUser{}, ErrAuthEnabled
	}
	pending, err := a.Store.Auth().Pending(ctx)
	if err != nil {
		return nil, domain.AuthUser{}, ErrSetupExpired
	}
	now := time.Now().UTC()
	if !now.Before(pending.ExpiresAt) {
		_ = a.Store.Auth().DeletePending(ctx)
		return nil, domain.AuthUser{}, ErrSetupExpired
	}
	step, ok := auth.ValidateTOTP(pending.TOTPSecret, code, now)
	if !ok {
		return nil, domain.AuthUser{}, ErrInvalidCode
	}
	user := domain.AuthUser{
		Username: pending.Username, PasswordHash: pending.PasswordHash,
		TOTPSecret: pending.TOTPSecret, TOTPLastStep: step, CreatedAt: now,
	}
	codes, err := auth.GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, domain.AuthUser{}, err
	}
	records := make([]domain.RecoveryCode, 0, len(codes))
	for _, code := range codes {
		records = append(records, domain.RecoveryCode{
			ID: ids.New("rc"), CodeHash: auth.HashRecoveryCode(code),
		})
	}
	if err := a.Store.Auth().CompleteSetup(ctx, user, records); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return nil, domain.AuthUser{}, ErrAuthEnabled
		}
		return nil, domain.AuthUser{}, err
	}
	_ = a.Store.Auth().AddEvent(ctx, domain.AuthEvent{
		At: now, Event: "authorization_enabled", Username: user.Username,
		IP: client.IP, UserAgent: client.UserAgent,
	})
	return codes, user, nil
}

// CancelSetup discards an in-progress enrollment.
func (a *App) CancelSetup(ctx context.Context) error {
	return a.Store.Auth().DeletePending(ctx)
}

// Login verifies the password and returns a short-lived challenge. A session is
// only created after the TOTP/recovery step.
func (a *App) Login(ctx context.Context, username, password string, client ClientInfo) (string, error) {
	now := time.Now().UTC()
	user, err := a.Store.Auth().User(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		verifyDummyPassword(password)
		a.audit(ctx, "login_failed", username, "authorization disabled", client)
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(user.Username)) == 1
	passOK := auth.VerifyPassword(user.PasswordHash, password)
	if !userOK || !passOK {
		a.audit(ctx, "login_failed", username, "invalid credentials", client)
		return "", ErrInvalidCredentials
	}
	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	_ = a.Store.Auth().DeleteExpiredChallenges(ctx, now)
	challenge := domain.AuthChallenge{
		TokenHash: auth.HashToken(token), CreatedAt: now, ExpiresAt: now.Add(ChallengeTTL),
	}
	if err := a.Store.Auth().SaveChallenge(ctx, challenge); err != nil {
		return "", err
	}
	return token, nil
}

// VerifyMFA accepts either a TOTP code or a one-time recovery code, then issues
// a server-side session token.
func (a *App) VerifyMFA(ctx context.Context, challengeToken, code string, client ClientInfo) (string, domain.AuthUser, error) {
	now := time.Now().UTC()
	hash := auth.HashToken(strings.TrimSpace(challengeToken))
	challenge, err := a.Store.Auth().Challenge(ctx, hash)
	if err != nil || !now.Before(challenge.ExpiresAt) {
		_ = a.Store.Auth().DeleteChallenge(ctx, hash)
		return "", domain.AuthUser{}, ErrInvalidCode
	}
	if challenge.Attempts >= MaxMFAAttempts {
		_ = a.Store.Auth().DeleteChallenge(ctx, hash)
		return "", domain.AuthUser{}, ErrTooManyAttempts
	}
	user, err := a.Store.Auth().User(ctx)
	if err != nil {
		_ = a.Store.Auth().DeleteChallenge(ctx, hash)
		return "", domain.AuthUser{}, ErrInvalidCredentials
	}
	usedRecovery, ok, err := a.verifySecondFactor(ctx, &user, code, now)
	if err != nil {
		return "", domain.AuthUser{}, err
	}
	if !ok {
		attempts, _ := a.Store.Auth().IncrementChallengeAttempts(ctx, hash)
		a.audit(ctx, "mfa_failed", user.Username, "invalid code", client)
		if attempts >= MaxMFAAttempts {
			_ = a.Store.Auth().DeleteChallenge(ctx, hash)
			return "", domain.AuthUser{}, ErrTooManyAttempts
		}
		return "", domain.AuthUser{}, ErrInvalidCode
	}
	consumed, err := a.Store.Auth().ConsumeChallenge(ctx, hash)
	if err != nil {
		return "", domain.AuthUser{}, err
	}
	if !consumed {
		return "", domain.AuthUser{}, ErrInvalidCode
	}
	token, session, err := newSession(now, client)
	if err != nil {
		return "", domain.AuthUser{}, err
	}
	if err := a.Store.Auth().InsertSession(ctx, session); err != nil {
		return "", domain.AuthUser{}, err
	}
	detail := ""
	if usedRecovery {
		detail = "recovery code used"
	}
	a.audit(ctx, "login_ok", user.Username, detail, client)
	return token, user, nil
}

// CreateSession issues a session for the current user (used after enrollment).
func (a *App) CreateSession(ctx context.Context, client ClientInfo) (string, error) {
	now := time.Now().UTC()
	token, session, err := newSession(now, client)
	if err != nil {
		return "", err
	}
	if err := a.Store.Auth().InsertSession(ctx, session); err != nil {
		return "", err
	}
	return token, nil
}

func newSession(now time.Time, client ClientInfo) (string, domain.AuthSession, error) {
	token, err := auth.NewToken()
	if err != nil {
		return "", domain.AuthSession{}, err
	}
	return token, domain.AuthSession{
		TokenHash: auth.HashToken(token), CreatedAt: now, LastSeenAt: now,
		IdleExpiresAt: now.Add(SessionIdleTTL), AbsoluteExpiresAt: now.Add(SessionAbsoluteTTL),
		UserAgent: client.UserAgent, IP: client.IP,
	}, nil
}

// AuthenticateSession resolves a cookie token and slides its idle expiry.
func (a *App) AuthenticateSession(ctx context.Context, token string) (domain.AuthUser, error) {
	if strings.TrimSpace(token) == "" {
		return domain.AuthUser{}, ErrUnauthorized
	}
	now := time.Now().UTC()
	hash := auth.HashToken(token)
	session, err := a.Store.Auth().Session(ctx, hash)
	if err != nil || !now.Before(session.IdleExpiresAt) || !now.Before(session.AbsoluteExpiresAt) {
		_ = a.Store.Auth().DeleteSession(ctx, hash)
		return domain.AuthUser{}, ErrUnauthorized
	}
	user, err := a.Store.Auth().User(ctx)
	if err != nil {
		_ = a.Store.Auth().DeleteSession(ctx, hash)
		return domain.AuthUser{}, ErrUnauthorized
	}
	if now.Sub(session.LastSeenAt) >= sessionTouchInterval {
		idle := now.Add(SessionIdleTTL)
		if idle.After(session.AbsoluteExpiresAt) {
			idle = session.AbsoluteExpiresAt
		}
		_ = a.Store.Auth().TouchSession(ctx, hash, now, idle)
	}
	return user, nil
}

// Logout revokes one session.
func (a *App) Logout(ctx context.Context, token string, client ClientInfo) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	hash := auth.HashToken(token)
	session, _ := a.Store.Auth().Session(ctx, hash)
	if err := a.Store.Auth().DeleteSession(ctx, hash); err != nil {
		return err
	}
	a.audit(ctx, "logout", "", "", ClientInfo{IP: firstNonEmpty(session.IP, client.IP), UserAgent: firstNonEmpty(session.UserAgent, client.UserAgent)})
	return nil
}

// DisableAuth verifies both factors and then deletes the account, every
// session, every challenge and every recovery code. Re-enabling therefore
// always starts from a brand-new TOTP secret.
func (a *App) DisableAuth(ctx context.Context, password, code string, client ClientInfo) error {
	now := time.Now().UTC()
	user, err := a.Store.Auth().User(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrAuthDisabled
	}
	if err != nil {
		return err
	}
	if !auth.VerifyPassword(user.PasswordHash, password) {
		a.audit(ctx, "disable_failed", user.Username, "invalid password", client)
		return ErrInvalidCredentials
	}
	_, ok, err := a.verifySecondFactor(ctx, &user, code, now)
	if err != nil {
		return err
	}
	if !ok {
		a.audit(ctx, "disable_failed", user.Username, "invalid code", client)
		return ErrInvalidCode
	}
	if err := a.Store.Auth().ClearAuth(ctx); err != nil {
		return err
	}
	a.audit(ctx, "authorization_disabled", user.Username, "", client)
	return nil
}

// ResetAuth is the local break-glass path used by `ozddns auth disable`. It
// deliberately requires filesystem access to the database rather than an
// unauthenticated HTTP reset link.
func (a *App) ResetAuth(ctx context.Context) error {
	if err := a.Store.Auth().ClearAuth(ctx); err != nil {
		return err
	}
	_ = a.Store.Auth().AddEvent(ctx, domain.AuthEvent{
		At: time.Now().UTC(), Event: "authorization_disabled", Detail: "local CLI reset",
	})
	return nil
}

// RegenerateRecoveryCodes invalidates the old set and returns a fresh one.
func (a *App) RegenerateRecoveryCodes(ctx context.Context, password, code string, client ClientInfo) ([]string, error) {
	now := time.Now().UTC()
	user, err := a.Store.Auth().User(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrAuthDisabled
	}
	if err != nil {
		return nil, err
	}
	if !auth.VerifyPassword(user.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	if _, ok, err := a.verifySecondFactor(ctx, &user, code, now); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrInvalidCode
	}
	codes, err := auth.GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, err
	}
	records := make([]domain.RecoveryCode, 0, len(codes))
	for _, code := range codes {
		records = append(records, domain.RecoveryCode{ID: ids.New("rc"), CodeHash: auth.HashRecoveryCode(code)})
	}
	if err := a.Store.Auth().ReplaceRecoveryCodes(ctx, records); err != nil {
		return nil, err
	}
	a.audit(ctx, "recovery_codes_regenerated", user.Username, "", client)
	return codes, nil
}

// AuthEvents returns the most recent security audit entries.
func (a *App) AuthEvents(ctx context.Context, limit int) ([]domain.AuthEvent, error) {
	return a.Store.Auth().ListEvents(ctx, limit)
}

// verifySecondFactor accepts a TOTP code or a recovery code. TOTP steps are
// single-use; a recovery code is consumed atomically.
func (a *App) verifySecondFactor(ctx context.Context, user *domain.AuthUser, code string, now time.Time) (bool, bool, error) {
	if step, ok := auth.ValidateTOTP(user.TOTPSecret, code, now); ok {
		consumed, err := a.Store.Auth().UseTOTPStep(ctx, step)
		if err != nil {
			return false, false, err
		}
		if !consumed {
			return false, false, nil
		}
		user.TOTPLastStep = step
		return false, true, nil
	}
	normalized := auth.NormalizeRecoveryCode(code)
	if len(normalized) != 10 {
		return false, false, nil
	}
	hash := auth.HashRecoveryCode(code)
	codes, err := a.Store.Auth().ListRecoveryCodes(ctx)
	if err != nil {
		return false, false, err
	}
	for _, candidate := range codes {
		if candidate.UsedAt == nil && subtle.ConstantTimeCompare([]byte(candidate.CodeHash), []byte(hash)) == 1 {
			if err := a.Store.Auth().UseRecoveryCode(ctx, candidate.ID, now); err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					return false, false, nil
				}
				return false, false, err
			}
			return true, true, nil
		}
	}
	return false, false, nil
}

func (a *App) audit(ctx context.Context, event, username, detail string, client ClientInfo) {
	_ = a.Store.Auth().AddEvent(ctx, domain.AuthEvent{
		At: time.Now().UTC(), Event: event, Username: username, Detail: detail,
		IP: client.IP, UserAgent: client.UserAgent,
	})
}

// listenExposed reports whether an open (no-administrator) panel bound here may
// be reachable by clients beyond this host and outside a network the operator
// declared trusted. A trusted network (--trusted-network) is an explicit
// assertion — a LAN or an overlay such as Tailscale — so the exposure warning is
// suppressed; it grants no access control on its own.
func (a *App) listenExposed() bool {
	return !a.listenIsLoopback() && !a.TrustedNetwork
}

func (a *App) listenIsLoopback() bool {
	addr := strings.TrimSpace(a.ListenAddr)
	if addr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

func verifyDummyPassword(password string) {
	dummyHashOnce.Do(func() {
		dummyHash, _ = auth.HashPassword("ozddns-dummy-password")
	})
	_ = auth.VerifyPassword(dummyHash, password)
}

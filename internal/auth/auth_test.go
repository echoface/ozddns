package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	first, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes must use independent random salts")
	}
	if !VerifyPassword(first, "correct horse battery") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(first, "wrong") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("", "anything") {
		t.Fatal("empty stored hash accepted")
	}
}

func TestTOTPKnownVectors(t *testing.T) {
	// RFC 6238 uses an ASCII secret of "12345678901234567890" for SHA-1.
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	vectors := []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, vector := range vectors {
		got, err := TOTPCode(secret, time.Unix(vector.unix, 0))
		if err != nil {
			t.Fatalf("TOTPCode(%d): %v", vector.unix, err)
		}
		if got != vector.code {
			t.Fatalf("TOTPCode(%d) = %s, want %s", vector.unix, got, vector.code)
		}
	}
}

func TestTOTPAcceptsOneStepOfClockSkewAndRejectsMalformedCodes(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	for _, delta := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		code, err := TOTPCode(secret, now.Add(delta))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ValidateTOTP(secret, code, now); !ok {
			t.Fatalf("code at offset %s rejected", delta)
		}
	}
	for _, code := range []string{"", "12345", "1234567", "abcdef", "12 456"} {
		if _, ok := ValidateTOTP(secret, code, now); ok {
			t.Fatalf("malformed code %q accepted", code)
		}
	}
}

func TestRecoveryCodesAreNormalizedAndHashed(t *testing.T) {
	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("codes = %d", len(codes))
	}
	seen := map[string]bool{}
	for _, code := range codes {
		if len(code) != 11 || code[5] != '-' {
			t.Fatalf("unexpected recovery code format %q", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code %q", code)
		}
		seen[code] = true
		if HashRecoveryCode(code) != HashRecoveryCode(strings.ToLower(strings.ReplaceAll(code, "-", " "))) {
			t.Fatalf("normalization changed code %q", code)
		}
	}
	if NormalizeRecoveryCode(" abcd-efghj ") != "ABCDEFGHJ" {
		t.Fatalf("normalize = %q", NormalizeRecoveryCode(" abcd-efghj "))
	}
}

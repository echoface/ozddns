// Package auth holds the cryptographic primitives used by the optional
// authorization mode: password hashing, session/challenge tokens, TOTP and
// recovery codes. It is deliberately independent of HTTP and storage so it can
// be tested in isolation.
package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// MinPasswordLen is the minimum accepted password length. Authorization is an
// opt-in mode for a panel that may be exposed beyond loopback, so the floor is
// higher than the old local-only Basic password.
const MinPasswordLen = 10

const (
	pbkdf2Iterations = 210_000
	pbkdf2KeyLength  = 32
	passwordSaltLen  = 16
)

// HashPassword returns a salted PBKDF2-HMAC-SHA256 hash in the documented
// "pbkdf2-sha256$iterations$salt$hash" format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating password salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return "", fmt.Errorf("deriving password hash: %w", err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches the stored PBKDF2 hash.
func VerifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	var iterations int
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewToken returns a 256-bit URL-safe random token. It is used for sessions and
// pending login challenges; only its hash is persisted.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the stable hex digest stored in the database.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateTOTPSecret returns a 160-bit base32 secret without padding.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// TOTPCode computes the six-digit code for the given secret and time.
func TOTPCode(secret string, at time.Time) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	return hotp(key, uint64(at.Unix()/30)), nil
}

// ValidateTOTP checks a code against the current 30-second step and its two
// neighbours. It returns the matched step so the caller can reject replays.
func ValidateTOTP(secret, code string, at time.Time) (int64, bool) {
	key, err := decodeSecret(secret)
	if err != nil {
		return 0, false
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	current := at.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		step := current + offset
		if step < 0 {
			continue
		}
		want := hotp(key, uint64(step))
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// TOTPURL builds the standard otpauth:// URI accepted by authenticator apps.
func TOTPURL(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", "6")
	query.Set("period", "30")
	return "otpauth://totp/" + label + "?" + query.Encode()
}

const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateRecoveryCodes returns n human-readable one-time codes. Each code is
// ten characters from an unambiguous alphabet, grouped as XXXXX-XXXXX.
func GenerateRecoveryCodes(n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	seen := make(map[string]bool, n)
	codes := make([]string, 0, n)
	for len(codes) < n {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		var b strings.Builder
		for i, v := range raw {
			if i == 5 {
				b.WriteByte('-')
			}
			b.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
		}
		code := b.String()
		if seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	return codes, nil
}

// NormalizeRecoveryCode uppercases a code and removes separators so users can
// type it with or without the dash.
func NormalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// HashRecoveryCode hashes the normalized code. Recovery codes carry 50 bits of
// entropy, so a fast digest is sufficient; they are also single-use.
func HashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(NormalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

func decodeSecret(secret string) ([]byte, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
}

func hotp(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}

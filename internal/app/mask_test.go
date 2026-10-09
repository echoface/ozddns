package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/echoface/ozddns/internal/providers"
)

// PRV-10: secrets are masked (last 4 kept) and non-secrets pass through.
func TestMaskConfig(t *testing.T) {
	reg := providers.NewRegistry()
	cfg := map[string]any{"api_token": "supersecrettoken1234", "email": "ops@example.com", "ttl": 300}
	masked := MaskConfig(reg, "cloudflare", cfg)
	tok, _ := masked["api_token"].(string)
	if tok == "supersecrettoken1234" {
		t.Fatal("secret not masked")
	}
	if tok[len(tok)-4:] != "1234" {
		t.Fatalf("expected last 4 chars kept, got %q", tok)
	}
	if masked["email"] != "ops@example.com" || masked["ttl"] != 300 {
		t.Fatalf("non-secret fields altered: %v", masked)
	}

	short := MaskConfig(reg, "cloudflare", map[string]any{"api_token": "abc"})
	if short["api_token"] != "••••" {
		t.Fatalf("short secret = %v", short["api_token"])
	}
}

// Regression (PRV-07): saving a form whose secret is displayed masked must keep
// the stored credential instead of persisting the mask.
func TestMergeSecretsKeepsStoredCredential(t *testing.T) {
	reg := providers.NewRegistry()
	existing := map[string]any{"api_token": "realtoken1234", "email": "me@example.com", "ttl": 300}
	// The panel submits the masked token back, plus an edited non-secret field.
	incoming := map[string]any{"api_token": "••••1234", "email": "new@example.com", "ttl": 600}
	merged := MergeSecrets(reg, "cloudflare", incoming, existing)
	if merged["api_token"] != "realtoken1234" {
		t.Fatalf("credential was overwritten by its mask: %v", merged["api_token"])
	}
	if merged["email"] != "new@example.com" || merged["ttl"] != 600 {
		t.Fatalf("non-secret edits lost: %v", merged)
	}

	// A genuinely new secret is kept as-is.
	fresh := MergeSecrets(reg, "cloudflare", map[string]any{"api_token": "brandnew9999"}, existing)
	if fresh["api_token"] != "brandnew9999" {
		t.Fatalf("new secret not stored: %v", fresh["api_token"])
	}

	// A mask with nothing stored is dropped rather than saved.
	dropped := MergeSecrets(reg, "cloudflare", map[string]any{"api_token": "••••1234"}, map[string]any{})
	if _, ok := dropped["api_token"]; ok {
		t.Fatalf("mask should not be persisted: %v", dropped)
	}

	// A blank or omitted secret keeps the stored credential (the panel renders
	// stored secrets as an empty input).
	blank := MergeSecrets(reg, "cloudflare", map[string]any{"api_token": "", "email": "x@y.z"}, existing)
	if blank["api_token"] != "realtoken1234" {
		t.Fatalf("blank secret should keep stored value: %v", blank["api_token"])
	}
	omitted := MergeSecrets(reg, "cloudflare", map[string]any{"email": "x@y.z"}, existing)
	if omitted["api_token"] != "realtoken1234" {
		t.Fatalf("omitted secret should keep stored value: %v", omitted["api_token"])
	}
	// But a blank secret with nothing stored stays absent.
	if _, ok := MergeSecrets(reg, "cloudflare", map[string]any{"api_token": ""}, map[string]any{})["api_token"]; ok {
		t.Fatal("blank secret with no stored value should not be persisted")
	}
	// Whitespace-only counts as blank too.
	ws := MergeSecrets(reg, "cloudflare", map[string]any{"api_token": "   "}, existing)
	if ws["api_token"] != "realtoken1234" {
		t.Fatalf("whitespace secret should keep stored value: %v", ws["api_token"])
	}
	if got := MaskedSecretFields(reg, "cloudflare", map[string]any{"api_token": "••••1234"}); len(got) != 1 {
		t.Fatalf("MaskedSecretFields = %v", got)
	}
}

// End-to-end guard for the exact flow the panel performs: it renders a stored
// secret as an empty input, so saving the form untouched must keep the
// credential, while a typed value still replaces it.
func TestUpdateProviderKeepsUntouchedSecret(t *testing.T) {
	ctx := context.Background()
	a, closer, err := Open(ctx, Options{DBPath: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer closer()

	p, err := a.CreateProvider(ctx, ProviderInput{Name: "Hook", Type: "custom",
		Config: map[string]any{"url": "https://example.com/hook", "auth": "Bearer super-secret-token"}})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	// The panel only ever sees the mask.
	v, err := a.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if !IsMaskedSecret(v.Config["auth"]) {
		t.Fatalf("expected masked auth in the view, got %v", v.Config["auth"])
	}

	// Save with the secret left blank (what the panel now submits).
	if _, err := a.UpdateProvider(ctx, p.ID, ProviderInput{Config: map[string]any{
		"url": "https://example.com/hook2", "auth": "", "method": "POST"}}); err != nil {
		t.Fatalf("UpdateProvider(blank): %v", err)
	}
	got, _ := a.Store.Providers().Get(ctx, p.ID)
	if got.Config["auth"] != "Bearer super-secret-token" {
		t.Fatalf("credential lost on blank save: %v", got.Config["auth"])
	}
	if got.Config["url"] != "https://example.com/hook2" {
		t.Fatalf("non-secret edit lost: %v", got.Config["url"])
	}

	// A typed value still replaces it.
	if _, err := a.UpdateProvider(ctx, p.ID, ProviderInput{Config: map[string]any{
		"url": "https://example.com/hook2", "auth": "Bearer rotated-token"}}); err != nil {
		t.Fatalf("UpdateProvider(new): %v", err)
	}
	got2, _ := a.Store.Providers().Get(ctx, p.ID)
	if got2.Config["auth"] != "Bearer rotated-token" {
		t.Fatalf("new credential not stored: %v", got2.Config["auth"])
	}
}

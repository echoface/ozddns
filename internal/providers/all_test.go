package providers

import (
	"context"
	"testing"

	"github.com/echoface/ozddns/internal/domain"
)

// PRV-01: every compiled-in adapter is registered exactly once.
func TestRegistryHasAllProviders(t *testing.T) {
	r := NewRegistry()
	types := r.Types()
	want := []string{"alidns", "cloudflare", "custom", "desec", "dnspod", "duckdns", "godaddy", "namecheap", "vercel"}
	if len(types) != len(want) {
		t.Fatalf("types = %v", types)
	}
	for _, w := range want {
		if _, ok := r.Get(w); !ok {
			t.Errorf("provider %q not registered", w)
		}
	}
	if metadatas := r.Metas(); len(metadatas) != len(want) {
		t.Fatalf("metas = %d", len(metadatas))
	}
}

// Each adapter builds and validates with a minimal config.
func TestBuildAndValidateConfig(t *testing.T) {
	r := NewRegistry()
	configs := map[string]map[string]any{
		"cloudflare": {"api_token": "t"},
		"alidns":     {"access_key_id": "a", "access_key_secret": "s"},
		"dnspod":     {"login_token_id": "1", "login_token": "t"},
		"duckdns":    {"token": "t", "subdomain": "s"},
		"namecheap":  {"password": "p", "domain": "home.example.com"},
		"godaddy":    {"key": "k", "secret": "s"},
		"desec":      {"token": "t"},
		"vercel":     {"token": "t"},
		"custom":     {"url": "https://example.com/hook"},
	}
	for typ, cfg := range configs {
		f, ok := r.Get(typ)
		if !ok {
			t.Fatalf("%s missing", typ)
		}
		if err := f.ValidateConfig(cfg); err != nil {
			t.Errorf("%s ValidateConfig: %v", typ, err)
		}
		p, err := r.Build(domain.Provider{Type: typ, Config: cfg})
		if err != nil {
			t.Errorf("%s Build: %v", typ, err)
			continue
		}
		if p == nil {
			t.Errorf("%s built nil", typ)
		}
	}
}

// A missing required field is rejected.
func TestValidateConfigRejectsMissing(t *testing.T) {
	r := NewRegistry()
	f, _ := r.Get("cloudflare")
	if err := f.ValidateConfig(map[string]any{}); err == nil {
		t.Fatal("cloudflare accepted an empty config")
	}
	if _, err := r.MustGet("does-not-exist"); err == nil {
		t.Fatal("unknown type should error")
	}
}

// PRV-10: at least one secret field per credential-based provider.
func TestSecretFields(t *testing.T) {
	r := NewRegistry()
	for _, m := range r.Metas() {
		hasSecret := false
		for _, f := range m.Fields {
			if f.Secret {
				hasSecret = true
			}
		}
		if m.Type != "custom" && !hasSecret {
			t.Errorf("%s exposes no secret field", m.Type)
		}
	}
	_ = context.Background()
}

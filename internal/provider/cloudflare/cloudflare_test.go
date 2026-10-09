package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/echoface/ozddns/internal/domain"
)

// newTestProvider points the adapter at a stub CF API and returns a provider.
func newTestProvider(t *testing.T, h http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })
	p, err := Factory{}.New(map[string]any{"api_token": "tok"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p.(*Provider)
}

func cfJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// PRV-01/PRV-02: a healthy token validates and zones list.
func TestValidateAndListZones(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user/tokens/verify"):
			cfJSON(w, 200, map[string]any{"success": true, "result": map[string]string{"status": "active"}})
		case strings.HasSuffix(r.URL.Path, "/zones"):
			cfJSON(w, 200, map[string]any{"success": true, "result_info": map[string]int{"page": 1, "total_pages": 1},
				"result": []map[string]string{{"id": "z1", "name": "example.com", "status": "active"}}})
		default:
			http.NotFound(w, r)
		}
	})
	if err := p.Validate(context.Background()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	zones, err := p.ListZones(context.Background())
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 1 || zones[0].Name != "example.com" {
		t.Fatalf("zones = %+v", zones)
	}
}

// PRV-03: a rejected token surfaces an error.
func TestValidateRejectsBadToken(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user/tokens/verify") {
			cfJSON(w, 401, map[string]any{"success": false, "messages": []string{"Invalid API Token"}})
			return
		}
		cfJSON(w, 401, map[string]any{"success": false, "messages": []string{"Invalid API Token"}})
	})
	if err := p.Validate(context.Background()); err == nil {
		t.Fatal("expected validation error for a bad token")
	}
}

// REC-01/02/03/05: ListRecords + create/update/delete via Apply.
func TestListRecordsAndApply(t *testing.T) {
	var wrote map[string]any
	var method, path string
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/zones"):
			cfJSON(w, 200, map[string]any{"success": true, "result": []map[string]string{{"id": "z1", "name": "example.com", "status": "active"}}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/dns_records"):
			cfJSON(w, 200, map[string]any{"success": true, "result": []map[string]any{
				{"id": "rec1", "name": "home.example.com", "type": "A", "content": "203.0.113.9", "ttl": 300, "proxied": false},
			}})
		default:
			method, path = r.Method, r.URL.Path
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &wrote)
			cfJSON(w, 200, map[string]any{"success": true, "result": map[string]string{"id": "rec1"}})
		}
	})

	recs, err := p.ListRecords(context.Background(), "home.example.com", domain.RecordFilter{Type: domain.RecordA})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(recs) != 1 || recs[0].Content != "203.0.113.9" || recs[0].Proxied {
		t.Fatalf("records = %+v", recs)
	}

	// Update (PUT to the record id).
	err = p.Apply(context.Background(), domain.Change{Kind: domain.ChangeUpdate, Record: domain.Record{
		ID: "rec1", Name: "home.example.com", Type: domain.RecordA, Content: "203.0.113.42", TTL: 300, Proxied: true}})
	if err != nil {
		t.Fatalf("Apply update: %v", err)
	}
	if method != http.MethodPut || !strings.HasSuffix(path, "/dns_records/rec1") {
		t.Fatalf("update used %s %s", method, path)
	}
	if wrote["content"] != "203.0.113.42" || wrote["proxied"] != true {
		t.Fatalf("payload = %+v", wrote)
	}

	// Create (POST to the zone's dns_records) when no record id is known.
	err = p.Apply(context.Background(), domain.Change{Kind: domain.ChangeCreate, Record: domain.Record{
		Name: "new.example.com", Type: domain.RecordA, Content: "192.0.2.1", TTL: 120}})
	if err != nil {
		t.Fatalf("Apply create: %v", err)
	}
	if method != http.MethodPost {
		t.Fatalf("create used %s", method)
	}

	// Delete.
	err = p.Apply(context.Background(), domain.Change{Kind: domain.ChangeDelete, Record: domain.Record{ID: "rec1", Name: "home.example.com"}})
	if err != nil {
		t.Fatalf("Apply delete: %v", err)
	}
	if method != http.MethodDelete {
		t.Fatalf("delete used %s", method)
	}
}

// PRV-10: secret fields are declared as secret so the app can mask them.
func TestMetaMarksSecrets(t *testing.T) {
	m := Factory{}.Meta()
	secrets := map[string]bool{}
	for _, f := range m.Fields {
		secrets[f.Key] = f.Secret
	}
	if !secrets["api_token"] || !secrets["api_key"] {
		t.Fatalf("api_token/api_key should be secret: %+v", m.Fields)
	}
	if !m.SupportsProxied {
		t.Fatal("cloudflare should support proxied records")
	}
}

// Regression: a Global API Key pasted into the API Token field must be caught
// at config time, not turned into a confusing "Invalid format for Authorization
// header" 400 from the API.
func TestValidateConfigCatchesGlobalKeyInTokenField(t *testing.T) {
	globalKey := "da67746d10727818930406f56525ec37a2481" // 37 hex chars
	if len(globalKey) != 37 || !looksLikeGlobalAPIKey(globalKey) {
		t.Fatal("test fixture is not a 37-char hex key")
	}
	err := Factory{}.ValidateConfig(map[string]any{"api_token": globalKey, "email": "me@example.com"})
	if err == nil {
		t.Fatal("expected an error for a Global API Key in the token field")
	}
	if !strings.Contains(err.Error(), "Global API key") {
		t.Fatalf("error should name the right field: %v", err)
	}
	if _, err := (Factory{}).New(map[string]any{"api_token": globalKey, "email": "me@example.com"}); err == nil {
		t.Fatal("New should reject the same config")
	}
}

// The correct legacy pairing (email + Global API key) is accepted and sent as
// X-Auth-Email / X-Auth-Key, never as a Bearer token.
func TestGlobalKeyAuthUsesLegacyHeaders(t *testing.T) {
	var gotEmail, gotKey, gotAuth string
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotEmail, gotKey, gotAuth = r.Header.Get("X-Auth-Email"), r.Header.Get("X-Auth-Key"), r.Header.Get("Authorization")
		cfJSON(w, 200, map[string]any{"success": true, "result": []map[string]string{{"id": "z1", "name": "example.com"}}})
	})
	// Rebuild the provider with the legacy pair.
	legacy, err := Factory{}.New(map[string]any{"email": "me@example.com", "api_key": "da67746d10727818930406f56525ec37a2481"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = p
	if err := legacy.Validate(context.Background()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if gotEmail != "me@example.com" || gotKey != "da67746d10727818930406f56525ec37a2481" {
		t.Fatalf("legacy headers missing: email=%q key=%q", gotEmail, gotKey)
	}
	if gotAuth != "" {
		t.Fatalf("legacy auth must not set Authorization, got %q", gotAuth)
	}
}

// A missing half of either credential pair is rejected with a specific hint.
func TestValidateConfigPairs(t *testing.T) {
	cases := []map[string]any{
		{"email": "me@example.com"},                          // email without key
		{"api_key": "da67746d10727818930406f56525ec37a2481"}, // key without email
		{}, // nothing at all
	}
	for _, cfg := range cases {
		if err := (Factory{}).ValidateConfig(cfg); err == nil {
			t.Errorf("config %v should be rejected", cfg)
		}
	}
	// A real-looking token alone (not 37-hex) is fine.
	if err := (Factory{}).ValidateConfig(map[string]any{"api_token": "AbCdEf0123456789AbCdEf0123456789AbCdEf01"}); err != nil {
		t.Fatalf("a normal API token should be accepted: %v", err)
	}
}

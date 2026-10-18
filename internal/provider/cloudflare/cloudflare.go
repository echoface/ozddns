// Package cloudflare implements the Cloudflare DNS provider adapter.
package cloudflare

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/provider"
)

const Type = "cloudflare"

// baseURL is a variable so tests can point the adapter at an httptest server.
var baseURL = "https://api.cloudflare.com/client/v4"

// Factory builds Cloudflare providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "Cloudflare", Color: "cloudflare", Tag: "cloudflare",
		Desc: "API Token, zones & proxied records (A / AAAA)", SupportsProxied: true,
		Fields: []provider.Field{
			{Key: "api_token", Label: "API Token", Type: provider.FieldSecret, Secret: true, Required: true,
				Help: "Create one under My Profile → API Tokens with Zone.DNS Edit. ~40 characters. Not the 37-character Global API Key."},
			{Key: "email", Label: "Account email (only for Global API Key auth)", Type: provider.FieldText, Mono: false},
			{Key: "api_key", Label: "Global API key (only if you are not using a token)", Type: provider.FieldSecret, Secret: true,
				Help: "My Profile → API Tokens → Global API Key. 37 hex characters; requires the account email above."},
			{Key: "zone_id", Label: "Zone ID (optional)", Type: provider.FieldText, Help: "Leave blank to auto-discover from the domain."},
			{Key: "proxied", Label: "Proxy traffic (orange cloud)", Type: provider.FieldBool, Default: false},
			{Key: "ttl", Label: "TTL (seconds)", Type: provider.FieldNumber, Default: 300},
		},
	}
}

// looksLikeGlobalAPIKey reports whether s has Cloudflare's Global API Key shape:
// exactly 37 hexadecimal characters. API Tokens are ~40 characters and are not
// pure hex, so this is a safe discriminator.
func looksLikeGlobalAPIKey(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 37 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func (Factory) ValidateConfig(cfg map[string]any) error {
	c := provider.Config(cfg)
	token, email, key := c.String("api_token"), c.String("email"), c.String("api_key")

	// Catch the most common mistake before it becomes a confusing HTTP 400:
	// a Global API Key pasted into the API Token field.
	if token != "" && email != "" && key == "" && looksLikeGlobalAPIKey(token) {
		return fmt.Errorf("cloudflare: that value is a 37-character Global API Key, not an API Token — " +
			"move it to the \"Global API key\" field and keep the account email, then validate again " +
			"(or create an API Token with the Zone.DNS Edit permission and leave the email blank)")
	}
	if email != "" && key == "" && token == "" {
		return fmt.Errorf("cloudflare: an account email needs a matching Global API key")
	}
	if key != "" && email == "" && token == "" {
		return fmt.Errorf("cloudflare: a Global API key also needs the account email")
	}
	if token == "" && !(email != "" && key != "") {
		return fmt.Errorf("cloudflare: provide an API token, or an account email + Global API key")
	}
	return nil
}

func (Factory) New(cfg map[string]any) (provider.Provider, error) {
	if err := (Factory{}).ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Provider{cfg: provider.Config(cfg)}, nil
}

// Provider is a Cloudflare DNS adapter.
type Provider struct {
	cfg provider.Config
}

func (p *Provider) headers() map[string]string {
	h := map[string]string{}
	if p.cfg.Has("api_token") {
		h["Authorization"] = "Bearer " + strings.TrimSpace(p.cfg.String("api_token"))
	} else {
		h["X-Auth-Email"] = strings.TrimSpace(p.cfg.String("email"))
		h["X-Auth-Key"] = strings.TrimSpace(p.cfg.String("api_key"))
	}
	return h
}

type cfEnvelope struct {
	Success  bool         `json:"success"`
	Errors   []cfError    `json:"errors"`
	Messages []string     `json:"messages"`
	Info     cfResultInfo `json:"result_info"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfResultInfo struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
}

// Validate verifies the token (or zone listing as a fallback for key auth).
func (p *Provider) Validate(ctx context.Context) error {
	if p.cfg.Has("api_token") {
		var out struct {
			cfEnvelope
			Result struct{ Status string } `json:"result"`
		}
		if err := provider.DoJSON(ctx, http.MethodGet, baseURL+"/user/tokens/verify", p.headers(), nil, &out); err != nil {
			if isMalformedAuthHeader(err) && p.cfg.Has("email") {
				return fmt.Errorf("cloudflare: the API Token was rejected as malformed. If this is your "+
					"37-character Global API Key, move it to the \"Global API key\" field (keep the email) "+
					"or issue an API Token with Zone.DNS Edit: %w", err)
			}
			// Some tokens lack verify scope; fall back to a zone listing.
			if _, zerr := p.ListZones(ctx); zerr != nil {
				return err
			}
			return nil
		}
		if !out.Success {
			return fmt.Errorf("cloudflare: token rejected: %s", strings.Join(out.Messages, ", "))
		}
		return nil
	}
	_, err := p.ListZones(ctx)
	return err
}

// isMalformedAuthHeader recognises Cloudflare's "Invalid format for
// Authorization header" response (error codes 6003 / 6111).
func isMalformedAuthHeader(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Invalid format for Authorization header") ||
		strings.Contains(msg, `"code":6111`) || strings.Contains(msg, `"code":6003`)
}

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) {
	var zones []domain.Zone
	page := 1
	for {
		u := fmt.Sprintf("%s/zones?per_page=50&page=%d", baseURL, page)
		var out struct {
			cfEnvelope
			Result []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"result"`
		}
		if err := provider.DoJSON(ctx, http.MethodGet, u, p.headers(), nil, &out); err != nil {
			return nil, err
		}
		if !out.Success {
			return nil, fmt.Errorf("cloudflare: listing zones: %s", strings.Join(out.Messages, ", "))
		}
		for _, z := range out.Result {
			zones = append(zones, domain.Zone{ID: z.ID, Name: z.Name, Status: z.Status})
		}
		if out.Info.TotalPages == 0 || page >= out.Info.TotalPages {
			break
		}
		page++
	}
	return zones, nil
}

// resolveZone finds the zone for a full record name.
func (p *Provider) resolveZone(ctx context.Context, name string) (domain.Zone, error) {
	if zid := p.cfg.String("zone_id"); zid != "" {
		// We still need the zone name for the relative lookup; derive from the name.
		zones, err := p.ListZones(ctx)
		if err != nil {
			return domain.Zone{}, err
		}
		for _, z := range zones {
			if z.ID == zid {
				return z, nil
			}
		}
		return domain.Zone{ID: zid, Name: name}, nil
	}
	for _, cand := range provider.Suffixes(name) {
		u := fmt.Sprintf("%s/zones?name=%s&status=active&per_page=1", baseURL, url.QueryEscape(cand))
		var out struct {
			cfEnvelope
			Result []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"result"`
		}
		if err := provider.DoJSON(ctx, http.MethodGet, u, p.headers(), nil, &out); err != nil {
			return domain.Zone{}, err
		}
		if out.Success && len(out.Result) > 0 {
			return domain.Zone{ID: out.Result[0].ID, Name: out.Result[0].Name, Status: out.Result[0].Status}, nil
		}
	}
	return domain.Zone{}, fmt.Errorf("cloudflare: no zone found for %q", name)
}

func (p *Provider) ListRecords(ctx context.Context, name string, f domain.RecordFilter) ([]domain.Record, error) {
	zone, err := p.resolveZone(ctx, name)
	if err != nil {
		return nil, err
	}
	rt := f.Type
	if rt == "" {
		rt = domain.RecordA
	}
	u := fmt.Sprintf("%s/zones/%s/dns_records?type=%s&name=%s&per_page=100",
		baseURL, zone.ID, url.QueryEscape(string(rt)), url.QueryEscape(strings.ToLower(name)))
	var out struct {
		cfEnvelope
		Result []cfRecord `json:"result"`
	}
	if err := provider.DoJSON(ctx, http.MethodGet, u, p.headers(), nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, fmt.Errorf("cloudflare: listing records: %s", strings.Join(out.Messages, ", "))
	}
	recs := make([]domain.Record, 0, len(out.Result))
	for _, r := range out.Result {
		recs = append(recs, r.toDomain())
	}
	return recs, nil
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	zone, err := p.resolveZone(ctx, ch.Record.Name)
	if err != nil {
		return err
	}
	ttl := ch.Record.TTL
	if ttl <= 0 {
		ttl = p.cfg.Int("ttl", 300)
	}
	proxied := ch.Record.Proxied
	payload := map[string]any{
		"type":    string(ch.Record.Type),
		"name":    ch.Record.Name,
		"content": ch.Record.Content,
		"ttl":     ttl,
		"proxied": proxied,
	}
	if ch.Record.Comment != "" {
		payload["comment"] = ch.Record.Comment
	}
	switch ch.Kind {
	case domain.ChangeCreate:
		u := fmt.Sprintf("%s/zones/%s/dns_records", baseURL, zone.ID)
		return p.expect(ctx, http.MethodPost, u, payload)
	case domain.ChangeUpdate:
		if ch.Record.ID == "" {
			u := fmt.Sprintf("%s/zones/%s/dns_records", baseURL, zone.ID)
			return p.expect(ctx, http.MethodPost, u, payload)
		}
		u := fmt.Sprintf("%s/zones/%s/dns_records/%s", baseURL, zone.ID, ch.Record.ID)
		return p.expect(ctx, http.MethodPut, u, payload)
	case domain.ChangeDelete:
		u := fmt.Sprintf("%s/zones/%s/dns_records/%s", baseURL, zone.ID, ch.Record.ID)
		var out cfEnvelope
		if err := provider.DoJSON(ctx, http.MethodDelete, u, p.headers(), nil, &out); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("cloudflare: unknown change kind %q", ch.Kind)
	}
}

func (p *Provider) expect(ctx context.Context, method, u string, payload any) error {
	var out cfEnvelope
	if err := provider.DoJSON(ctx, method, u, p.headers(), payload, &out); err != nil {
		return err
	}
	if !out.Success {
		msgs := out.Messages
		for _, e := range out.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("cloudflare: %s", strings.Join(msgs, ", "))
	}
	return nil
}

type cfRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment"`
}

func (r cfRecord) toDomain() domain.Record {
	return domain.Record{ID: r.ID, Name: r.Name, Type: domain.RecordType(r.Type), Content: r.Content, TTL: r.TTL, Proxied: r.Proxied, Comment: r.Comment}
}

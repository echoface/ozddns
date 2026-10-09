// Package godaddy implements the GoDaddy DNS provider adapter.
package godaddy

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/provider"
)

const (
	Type    = "godaddy"
	baseURL = "https://api.godaddy.com/v1"
)

// Factory builds GoDaddy providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "GoDaddy", Color: "godaddy", Tag: "godaddy",
		Desc: "Production API key + secret",
		Fields: []provider.Field{
			{Key: "key", Label: "API Key", Type: provider.FieldSecret, Secret: true, Required: true},
			{Key: "secret", Label: "API Secret", Type: provider.FieldSecret, Secret: true, Required: true},
		},
	}
}

func (Factory) ValidateConfig(cfg map[string]any) error {
	return provider.Config(cfg).Require("key", "secret")
}

func (Factory) New(cfg map[string]any) (provider.Provider, error) {
	if err := (Factory{}).ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Provider{cfg: provider.Config(cfg)}, nil
}

// Provider is a GoDaddy adapter.
type Provider struct{ cfg provider.Config }

func (p *Provider) headers() map[string]string {
	return map[string]string{"Authorization": "sso-key " + p.cfg.String("key") + ":" + p.cfg.String("secret")}
}

func (p *Provider) Validate(ctx context.Context) error {
	_, err := p.ListZones(ctx)
	return err
}

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) {
	var out []struct {
		Domain string `json:"domain"`
		Status string `json:"status"`
	}
	if err := provider.DoJSON(ctx, http.MethodGet, baseURL+"/domains?limit=200", p.headers(), nil, &out); err != nil {
		return nil, err
	}
	zones := make([]domain.Zone, 0, len(out))
	for _, d := range out {
		zones = append(zones, domain.Zone{Name: d.Domain, Status: d.Status})
	}
	return zones, nil
}

func (p *Provider) resolve(ctx context.Context, name string) (domain.Zone, string, error) {
	zones, err := p.ListZones(ctx)
	if err != nil {
		return domain.Zone{}, "", err
	}
	z, sub, ok := provider.LongestZoneMatch(zones, name)
	if !ok {
		return domain.Zone{}, "", fmt.Errorf("godaddy: no zone found for %q", name)
	}
	if sub == "@" {
		sub = "@"
	}
	return z, sub, nil
}

func (p *Provider) ListRecords(ctx context.Context, name string, f domain.RecordFilter) ([]domain.Record, error) {
	z, sub, err := p.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	rt := f.Type
	if rt == "" {
		rt = domain.RecordA
	}
	u := fmt.Sprintf("%s/domains/%s/records/%s/%s", baseURL, z.Name, rt, sub)
	var out []struct {
		Data string `json:"data"`
		Name string `json:"name"`
		Type string `json:"type"`
		TTL  int    `json:"ttl"`
	}
	if err := provider.DoJSON(ctx, http.MethodGet, u, p.headers(), nil, &out); err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, nil
		}
		return nil, err
	}
	recs := make([]domain.Record, 0, len(out))
	for _, r := range out {
		recs = append(recs, domain.Record{Name: name, Type: domain.RecordType(r.Type), Content: r.Data, TTL: r.TTL})
	}
	return recs, nil
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	z, sub, err := p.resolve(ctx, ch.Record.Name)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("%s/domains/%s/records/%s/%s", baseURL, z.Name, ch.Record.Type, sub)
	if ch.Kind == domain.ChangeDelete {
		return provider.DoJSON(ctx, http.MethodDelete, u, p.headers(), nil, nil)
	}
	ttl := ch.Record.TTL
	if ttl <= 0 {
		ttl = 600
	}
	body := []map[string]any{{"data": ch.Record.Content, "ttl": ttl}}
	return provider.DoJSON(ctx, http.MethodPut, u, p.headers(), body, nil)
}

// Package desec implements the deSEC DNS provider adapter.
package desec

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/provider"
)

const (
	Type    = "desec"
	baseURL = "https://desec.io/api/v1"
)

// Factory builds deSEC providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "deSEC", Color: "desec", Tag: "desec",
		Desc: "Token-based, DNSSEC-ready",
		Fields: []provider.Field{
			{Key: "token", Label: "API Token", Type: provider.FieldSecret, Secret: true, Required: true},
		},
	}
}

func (Factory) ValidateConfig(cfg map[string]any) error { return provider.Config(cfg).Require("token") }

func (Factory) New(cfg map[string]any) (provider.Provider, error) {
	if err := (Factory{}).ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Provider{cfg: provider.Config(cfg)}, nil
}

// Provider is a deSEC adapter.
type Provider struct{ cfg provider.Config }

func (p *Provider) headers() map[string]string {
	return map[string]string{"Authorization": "Token " + p.cfg.String("token")}
}

func (p *Provider) Validate(ctx context.Context) error {
	_, err := p.ListZones(ctx)
	return err
}

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) {
	var out []struct {
		Name string `json:"name"`
	}
	if err := provider.DoJSON(ctx, http.MethodGet, baseURL+"/domains/?page_size=200", p.headers(), nil, &out); err != nil {
		return nil, err
	}
	zones := make([]domain.Zone, 0, len(out))
	for _, z := range out {
		zones = append(zones, domain.Zone{Name: z.Name})
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
		return domain.Zone{}, "", fmt.Errorf("desec: no zone found for %q", name)
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
	u := fmt.Sprintf("%s/domains/%s/rrsets/%s/%s/", baseURL, z.Name, sub, rt)
	var out struct {
		Records []string `json:"records"`
		TTL     int      `json:"ttl"`
	}
	if err := provider.DoJSON(ctx, http.MethodGet, u, p.headers(), nil, &out); err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, nil
		}
		return nil, err
	}
	recs := make([]domain.Record, 0, len(out.Records))
	for _, c := range out.Records {
		recs = append(recs, domain.Record{Name: name, Type: rt, Content: c, TTL: out.TTL})
	}
	return recs, nil
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	z, sub, err := p.resolve(ctx, ch.Record.Name)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("%s/domains/%s/rrsets/%s/%s/", baseURL, z.Name, sub, ch.Record.Type)
	if ch.Kind == domain.ChangeDelete {
		return provider.DoJSON(ctx, http.MethodDelete, u, p.headers(), nil, nil)
	}
	ttl := ch.Record.TTL
	if ttl <= 0 {
		ttl = 3600
	}
	body := map[string]any{"records": []string{ch.Record.Content}, "ttl": ttl, "type": string(ch.Record.Type)}
	return provider.DoJSON(ctx, http.MethodPut, u, p.headers(), body, nil)
}

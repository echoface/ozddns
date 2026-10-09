// Package vercel implements the Vercel DNS provider adapter.
package vercel

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/provider"
)

const (
	Type    = "vercel"
	baseURL = "https://api.vercel.com"
)

// Factory builds Vercel providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "Vercel DNS", Color: "vercel", Tag: "vercel",
		Desc: "Project-scoped DNS records",
		Fields: []provider.Field{
			{Key: "token", Label: "Access Token", Type: provider.FieldSecret, Secret: true, Required: true},
			{Key: "team_id", Label: "Team ID (optional)", Type: provider.FieldText},
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

// Provider is a Vercel adapter.
type Provider struct{ cfg provider.Config }

func (p *Provider) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + p.cfg.String("token")}
}

func (p *Provider) team() string {
	if t := p.cfg.String("team_id"); t != "" {
		return "?teamId=" + url.QueryEscape(t)
	}
	return ""
}

func (p *Provider) Validate(ctx context.Context) error {
	_, err := p.ListZones(ctx)
	return err
}

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) {
	var out struct {
		Domains []struct {
			Name string `json:"name"`
		} `json:"domains"`
	}
	u := baseURL + "/v5/domains?limit=200"
	if t := p.cfg.String("team_id"); t != "" {
		u += "&teamId=" + url.QueryEscape(t)
	}
	if err := provider.DoJSON(ctx, http.MethodGet, u, p.headers(), nil, &out); err != nil {
		return nil, err
	}
	zones := make([]domain.Zone, 0, len(out.Domains))
	for _, d := range out.Domains {
		zones = append(zones, domain.Zone{Name: d.Name})
	}
	return zones, nil
}

func (p *Provider) resolve(ctx context.Context, name string) (domain.Zone, error) {
	zones, err := p.ListZones(ctx)
	if err != nil {
		return domain.Zone{}, err
	}
	z, _, ok := provider.LongestZoneMatch(zones, name)
	if !ok {
		return domain.Zone{}, fmt.Errorf("vercel: no zone found for %q", name)
	}
	return z, nil
}

func (p *Provider) ListRecords(ctx context.Context, name string, f domain.RecordFilter) ([]domain.Record, error) {
	z, err := p.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	rt := f.Type
	if rt == "" {
		rt = domain.RecordA
	}
	u := fmt.Sprintf("%s/v4/domains/%s/records?limit=100", baseURL, z.Name) + p.teamQuerySuffix()
	var out struct {
		Records []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
			TTL   int    `json:"ttl"`
		} `json:"records"`
	}
	if err := provider.DoJSON(ctx, http.MethodGet, u, p.headers(), nil, &out); err != nil {
		return nil, err
	}
	recs := make([]domain.Record, 0)
	for _, r := range out.Records {
		full := r.Name
		if full == "" || full == "@" {
			full = z.Name
		} else if !strings.Contains(full, ".") {
			full = full + "." + z.Name
		}
		if !strings.EqualFold(full, name) || domain.RecordType(r.Type) != rt {
			continue
		}
		recs = append(recs, domain.Record{ID: r.ID, Name: full, Type: domain.RecordType(r.Type), Content: r.Value, TTL: r.TTL})
	}
	return recs, nil
}

func (p *Provider) teamQuerySuffix() string {
	if t := p.cfg.String("team_id"); t != "" {
		return "&teamId=" + url.QueryEscape(t)
	}
	return ""
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	z, err := p.resolve(ctx, ch.Record.Name)
	if err != nil {
		return err
	}
	rel := provider.RelativeName(z.Name, ch.Record.Name)
	ttl := ch.Record.TTL
	if ttl <= 0 {
		ttl = 60
	}
	body := map[string]any{"name": rel, "type": string(ch.Record.Type), "value": ch.Record.Content, "ttl": ttl}
	switch ch.Kind {
	case domain.ChangeCreate:
		u := fmt.Sprintf("%s/v4/domains/%s/records", baseURL, z.Name) + p.team()
		return provider.DoJSON(ctx, http.MethodPost, u, p.headers(), body, nil)
	case domain.ChangeUpdate:
		u := fmt.Sprintf("%s/v4/domains/%s/records/%s", baseURL, z.Name, ch.Record.ID) + p.team()
		return provider.DoJSON(ctx, http.MethodPatch, u, p.headers(), body, nil)
	case domain.ChangeDelete:
		u := fmt.Sprintf("%s/v4/domains/%s/records/%s", baseURL, z.Name, ch.Record.ID) + p.team()
		return provider.DoJSON(ctx, http.MethodDelete, u, p.headers(), nil, nil)
	}
	return nil
}

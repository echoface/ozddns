// Package duckdns implements the DuckDNS provider adapter.
package duckdns

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/provider"
)

const (
	Type     = "duckdns"
	endpoint = "https://www.duckdns.org/update"
)

// Factory builds DuckDNS providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "DuckDNS", Color: "duckdns", Tag: "duckdns",
		Desc: "Token + subdomain, free dynamic DNS", WriteOnly: true,
		Fields: []provider.Field{
			{Key: "token", Label: "Token", Type: provider.FieldSecret, Secret: true, Required: true},
			{Key: "subdomain", Label: "Subdomain", Type: provider.FieldText, Required: true, Help: "The name before .duckdns.org"},
		},
	}
}

func (Factory) ValidateConfig(cfg map[string]any) error {
	return provider.Config(cfg).Require("token", "subdomain")
}

func (Factory) New(cfg map[string]any) (provider.Provider, error) {
	if err := (Factory{}).ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Provider{cfg: provider.Config(cfg)}, nil
}

// Provider is a DuckDNS adapter. DuckDNS exposes no read API, so ListRecords
// returns no known records and the engine always writes.
type Provider struct{ cfg provider.Config }

func (p *Provider) Validate(ctx context.Context) error { return nil }

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) {
	return []domain.Zone{{Name: p.cfg.String("subdomain") + ".duckdns.org"}}, nil
}

func (p *Provider) ListRecords(ctx context.Context, name string, f domain.RecordFilter) ([]domain.Record, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	if ch.Kind == domain.ChangeDelete {
		return nil
	}
	q := url.Values{}
	q.Set("domains", p.cfg.String("subdomain"))
	q.Set("token", p.cfg.String("token"))
	if ch.Record.Type == domain.RecordAAAA {
		q.Set("ipv6", ch.Record.Content)
	} else {
		q.Set("ip", ch.Record.Content)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := provider.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "OK" {
		return fmt.Errorf("duckdns: update rejected (status %d, body %q)", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

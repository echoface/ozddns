// Package namecheap implements the Namecheap Dynamic DNS provider adapter.
package namecheap

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
	Type     = "namecheap"
	endpoint = "https://dynamicdns.park-your-domain.com/update"
)

// Factory builds Namecheap providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "Namecheap", Color: "namecheap", Tag: "namecheap",
		Desc: "DDNS password from Advanced DNS",
		Fields: []provider.Field{
			{Key: "password", Label: "DDNS Password", Type: provider.FieldSecret, Secret: true, Required: true},
			{Key: "domain", Label: "Domain", Type: provider.FieldText, Required: true, Help: "The root domain, e.g. example.com"},
		},
	}
}

func (Factory) ValidateConfig(cfg map[string]any) error {
	return provider.Config(cfg).Require("password", "domain")
}

func (Factory) New(cfg map[string]any) (provider.Provider, error) {
	if err := (Factory{}).ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Provider{cfg: provider.Config(cfg)}, nil
}

// Provider is a Namecheap adapter (write-only).
type Provider struct{ cfg provider.Config }

func (p *Provider) Validate(ctx context.Context) error { return nil }

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) {
	return []domain.Zone{{Name: p.cfg.String("domain")}}, nil
}

func (p *Provider) ListRecords(ctx context.Context, name string, f domain.RecordFilter) ([]domain.Record, error) {
	return nil, nil
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	if ch.Kind == domain.ChangeDelete || ch.Record.Type == domain.RecordAAAA {
		return nil // Namecheap dynamic DNS is IPv4-only.
	}
	host := provider.RelativeName(p.cfg.String("domain"), ch.Record.Name)
	if host == "@" {
		host = "@"
	}
	q := url.Values{}
	q.Set("host", host)
	q.Set("domain", p.cfg.String("domain"))
	q.Set("password", p.cfg.String("password"))
	q.Set("ip", ch.Record.Content)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := provider.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("namecheap: status %d", resp.StatusCode)
	}
	if strings.Contains(string(body), "<ErrCount>") && !strings.Contains(string(body), "<ErrCount>0</ErrCount>") {
		return fmt.Errorf("namecheap: update rejected: %s", firstError(string(body)))
	}
	return nil
}

func firstError(xml string) string {
	i := strings.Index(xml, "<Response>")
	if i < 0 {
		return provider.Truncate(strings.TrimSpace(xml), 200)
	}
	return provider.Truncate(strings.TrimSpace(xml[i:]), 200)
}

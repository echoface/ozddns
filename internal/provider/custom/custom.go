// Package custom implements a generic webhook provider: OzDDNS calls an HTTP
// endpoint of your choosing with the new address.
package custom

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/provider"
)

const Type = "custom"

// Factory builds custom webhook providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "Custom Webhook", Color: "custom", Tag: "custom",
		Desc: "Call your own HTTP endpoint with the new IP", WriteOnly: true,
		Fields: []provider.Field{
			{Key: "url", Label: "Endpoint URL", Type: provider.FieldText, Required: true, Help: "Supports {{ip}}, {{domain}} and {{type}} placeholders."},
			{Key: "method", Label: "Method", Type: provider.FieldSelect, Options: []string{"GET", "POST", "PUT"}, Default: "GET"},
			{Key: "body", Label: "Body template", Type: provider.FieldTextarea, Default: `{"domain":"{{domain}}","ip":"{{ip}}"}`},
			{Key: "auth", Label: "Authorization header", Type: provider.FieldSecret, Secret: true},
		},
	}
}

func (Factory) ValidateConfig(cfg map[string]any) error { return provider.Config(cfg).Require("url") }

func (Factory) New(cfg map[string]any) (provider.Provider, error) {
	if err := (Factory{}).ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Provider{cfg: provider.Config(cfg)}, nil
}

// Provider is a custom webhook adapter (write-only).
type Provider struct{ cfg provider.Config }

func (p *Provider) Validate(ctx context.Context) error { return nil }

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) { return nil, nil }

func (p *Provider) ListRecords(ctx context.Context, name string, f domain.RecordFilter) ([]domain.Record, error) {
	return nil, nil
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	if ch.Kind == domain.ChangeDelete {
		return nil
	}
	render := func(s string) string {
		r := strings.NewReplacer("{{ip}}", ch.Record.Content, "{{domain}}", ch.Record.Name, "{{type}}", string(ch.Record.Type))
		return r.Replace(s)
	}
	rawURL := render(p.cfg.String("url"))
	method := strings.ToUpper(p.cfg.String("method"))
	if method == "" {
		method = http.MethodGet
	}
	headers := map[string]string{}
	if a := p.cfg.String("auth"); a != "" {
		headers["Authorization"] = a
	}
	var bodyReader io.Reader
	if method == http.MethodGet {
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		q := u.Query()
		q.Set("domain", ch.Record.Name)
		q.Set("ip", ch.Record.Content)
		q.Set("type", string(ch.Record.Type))
		u.RawQuery = q.Encode()
		rawURL = u.String()
	} else {
		body := render(p.cfg.String("body"))
		bodyReader = bytes.NewReader([]byte(body))
		headers["Content-Type"] = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := provider.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("custom webhook: status %d", resp.StatusCode)
	}
	return nil
}

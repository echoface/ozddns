// Package dnspod implements the DNSPod (Tencent) provider adapter.
package dnspod

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
	Type    = "dnspod"
	baseURL = "https://dnsapi.cn"
)

// Factory builds DNSPod providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "DNSPod (Tencent)", Color: "dnspod", Tag: "dnspod",
		Desc: "DNSPod API token; also powers Tencent Cloud DNS",
		Fields: []provider.Field{
			{Key: "login_token_id", Label: "Token ID", Type: provider.FieldText, Required: true},
			{Key: "login_token", Label: "Token", Type: provider.FieldSecret, Secret: true, Required: true},
			{Key: "record_line", Label: "Record line", Type: provider.FieldText, Default: "默认"},
		},
	}
}

func (Factory) ValidateConfig(cfg map[string]any) error {
	return provider.Config(cfg).Require("login_token_id", "login_token")
}

func (Factory) New(cfg map[string]any) (provider.Provider, error) {
	if err := (Factory{}).ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Provider{cfg: provider.Config(cfg)}, nil
}

// Provider is a DNSPod adapter.
type Provider struct{ cfg provider.Config }

type status struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s status) ok() bool { return s.Code == "1" }

func (p *Provider) call(ctx context.Context, path string, form url.Values, out any) error {
	form.Set("login_token", p.cfg.String("login_token_id")+","+p.cfg.String("login_token"))
	form.Set("format", "json")
	form.Set("lang", "en")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "OzDDNS/1.0")
	return provider.DoJSONBody(ctx, req, out)
}

func (p *Provider) Validate(ctx context.Context) error {
	var out struct {
		Status status `json:"status"`
	}
	if err := p.call(ctx, "/Domain.List", url.Values{}, &out); err != nil {
		return err
	}
	if !out.Status.ok() {
		return fmt.Errorf("dnspod: %s", out.Status.Message)
	}
	return nil
}

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) {
	var out struct {
		Status  status `json:"status"`
		Domains []struct {
			ID   any    `json:"id"`
			Name string `json:"name"`
		} `json:"domains"`
	}
	if err := p.call(ctx, "/Domain.List", url.Values{}, &out); err != nil {
		return nil, err
	}
	if !out.Status.ok() {
		return nil, fmt.Errorf("dnspod: %s", out.Status.Message)
	}
	zones := make([]domain.Zone, 0, len(out.Domains))
	for _, d := range out.Domains {
		zones = append(zones, domain.Zone{ID: fmt.Sprintf("%v", d.ID), Name: d.Name})
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
		return domain.Zone{}, "", fmt.Errorf("dnspod: no zone found for %q", name)
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
	form := url.Values{}
	form.Set("domain", z.Name)
	form.Set("sub_domain", sub)
	form.Set("record_type", string(rt))
	var out struct {
		Status  status `json:"status"`
		Records []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
			TTL   string `json:"ttl"`
		} `json:"records"`
	}
	if err := p.call(ctx, "/Record.List", form, &out); err != nil {
		return nil, err
	}
	if !out.Status.ok() {
		// "no records" comes back as an error code; treat as empty.
		if strings.Contains(out.Status.Message, "No records") || out.Status.Code == "6" {
			return nil, nil
		}
		return nil, fmt.Errorf("dnspod: %s", out.Status.Message)
	}
	recs := make([]domain.Record, 0, len(out.Records))
	for _, r := range out.Records {
		recs = append(recs, domain.Record{ID: r.ID, Name: name, Type: domain.RecordType(r.Type), Content: r.Value, TTL: atoi(r.TTL, 600)})
	}
	return recs, nil
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	z, sub, err := p.resolve(ctx, ch.Record.Name)
	if err != nil {
		return err
	}
	line := p.cfg.String("record_line")
	if line == "" {
		line = "默认"
	}
	ttl := ch.Record.TTL
	if ttl <= 0 {
		ttl = 600
	}
	if ch.Kind == domain.ChangeDelete {
		return fmt.Errorf("dnspod: delete not supported by this adapter")
	}
	form := url.Values{}
	form.Set("domain", z.Name)
	form.Set("sub_domain", sub)
	form.Set("record_type", string(ch.Record.Type))
	form.Set("value", ch.Record.Content)
	form.Set("record_line", line)
	form.Set("ttl", fmt.Sprintf("%d", ttl))
	if ch.Record.ID != "" {
		form.Set("record_id", ch.Record.ID)
	}
	path := "/Record.Create"
	if ch.Record.ID != "" {
		path = "/Record.Modify"
	}
	var out struct {
		Status status `json:"status"`
	}
	if err := p.call(ctx, path, form, &out); err != nil {
		return err
	}
	if !out.Status.ok() {
		return fmt.Errorf("dnspod: %s", out.Status.Message)
	}
	return nil
}

func atoi(s string, def int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if s == "" {
		return def
	}
	return n
}

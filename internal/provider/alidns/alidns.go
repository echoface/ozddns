// Package alidns implements the Alibaba Cloud DNS (AliDNS) provider adapter
// using the classic RPC signature (HMAC-SHA1).
package alidns

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/provider"
)

const (
	Type     = "alidns"
	endpoint = "https://alidns.aliyuncs.com/"
	apiVer   = "2015-01-09"
)

// Factory builds AliDNS providers.
type Factory struct{}

func (Factory) Meta() provider.Meta {
	return provider.Meta{
		Type: Type, Label: "Alibaba Cloud DNS", Color: "ali", Tag: "ali",
		Desc: "AccessKey pair for AliDNS zones",
		Fields: []provider.Field{
			{Key: "access_key_id", Label: "AccessKey ID", Type: provider.FieldText, Required: true},
			{Key: "access_key_secret", Label: "AccessKey Secret", Type: provider.FieldSecret, Secret: true, Required: true},
			{Key: "region", Label: "Region", Type: provider.FieldSelect, Options: []string{"cn-hangzhou", "cn-beijing", "ap-southeast-1"}, Default: "cn-hangzhou"},
		},
	}
}

func (Factory) ValidateConfig(cfg map[string]any) error {
	return provider.Config(cfg).Require("access_key_id", "access_key_secret")
}

func (Factory) New(cfg map[string]any) (provider.Provider, error) {
	if err := (Factory{}).ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Provider{cfg: provider.Config(cfg)}, nil
}

// Provider is an AliDNS adapter.
type Provider struct{ cfg provider.Config }

func percentEncode(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}

func nonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (p *Provider) signedURL(action string, params map[string]string) string {
	common := map[string]string{
		"Format":           "JSON",
		"Version":          apiVer,
		"AccessKeyId":      p.cfg.String("access_key_id"),
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   nonce(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Action":           action,
	}
	for k, v := range params {
		common[k] = v
	}
	keys := make([]string, 0, len(common))
	for k := range common {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var canon strings.Builder
	for i, k := range keys {
		if i > 0 {
			canon.WriteByte('&')
		}
		canon.WriteString(percentEncode(k))
		canon.WriteByte('=')
		canon.WriteString(percentEncode(common[k]))
	}
	stringToSign := "GET&" + percentEncode("/") + "&" + percentEncode(canon.String())
	mac := hmac.New(sha1.New, []byte(p.cfg.String("access_key_secret")+"&"))
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return endpoint + "?" + canon.String() + "&Signature=" + percentEncode(sig)
}

func (p *Provider) call(ctx context.Context, action string, params map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.signedURL(action, params), nil)
	if err != nil {
		return err
	}
	if err := provider.DoJSONBody(ctx, req, out); err != nil {
		return err
	}
	return nil
}

func (p *Provider) Validate(ctx context.Context) error {
	_, err := p.ListZones(ctx)
	return err
}

func (p *Provider) ListZones(ctx context.Context) ([]domain.Zone, error) {
	var out struct {
		TotalCount int `json:"TotalCount"`
		Domains    struct {
			Domain []struct {
				DomainName string `json:"DomainName"`
				DomainID   string `json:"DomainId"`
			} `json:"Domain"`
		} `json:"Domains"`
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := p.call(ctx, "DescribeDomains", map[string]string{"PageSize": "100"}, &out); err != nil {
		return nil, err
	}
	if out.Code != "" {
		return nil, fmt.Errorf("alidns: %s (%s)", out.Message, out.Code)
	}
	zones := make([]domain.Zone, 0, len(out.Domains.Domain))
	for _, d := range out.Domains.Domain {
		zones = append(zones, domain.Zone{ID: d.DomainID, Name: d.DomainName})
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
		return domain.Zone{}, "", fmt.Errorf("alidns: no zone found for %q", name)
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
	rr := sub
	params := map[string]string{"DomainName": z.Name, "RRKeyWord": rr, "TypeKeyWord": string(rt), "PageSize": "100"}
	var out struct {
		DomainRecords struct {
			Record []struct {
				RecordID string `json:"RecordId"`
				RR       string `json:"RR"`
				Type     string `json:"Type"`
				Value    string `json:"Value"`
				TTL      int    `json:"TTL"`
				Line     string `json:"Line"`
			} `json:"Record"`
		} `json:"DomainRecords"`
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := p.call(ctx, "DescribeDomainRecords", params, &out); err != nil {
		return nil, err
	}
	if out.Code != "" {
		return nil, fmt.Errorf("alidns: %s (%s)", out.Message, out.Code)
	}
	recs := make([]domain.Record, 0, len(out.DomainRecords.Record))
	for _, r := range out.DomainRecords.Record {
		recs = append(recs, domain.Record{ID: r.RecordID, Name: name, Type: domain.RecordType(r.Type), Content: r.Value, TTL: r.TTL})
	}
	return recs, nil
}

func (p *Provider) Apply(ctx context.Context, ch domain.Change) error {
	z, sub, err := p.resolve(ctx, ch.Record.Name)
	if err != nil {
		return err
	}
	ttl := ch.Record.TTL
	if ttl <= 0 {
		ttl = 600
	}
	var action string
	params := map[string]string{
		"DomainName": z.Name,
		"RR":         sub,
		"Type":       string(ch.Record.Type),
		"Value":      ch.Record.Content,
		"TTL":        fmt.Sprintf("%d", ttl),
	}
	switch ch.Kind {
	case domain.ChangeCreate:
		action = "AddDomainRecord"
	case domain.ChangeUpdate:
		if ch.Record.ID == "" {
			action = "AddDomainRecord"
		} else {
			action = "UpdateDomainRecord"
			params["RecordId"] = ch.Record.ID
		}
	case domain.ChangeDelete:
		action = "DeleteDomainRecord"
		params = map[string]string{"RecordId": ch.Record.ID}
	default:
		return fmt.Errorf("alidns: unknown change kind %q", ch.Kind)
	}
	var out struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := p.call(ctx, action, params, &out); err != nil {
		return err
	}
	if out.Code != "" {
		return fmt.Errorf("alidns: %s (%s)", out.Message, out.Code)
	}
	return nil
}

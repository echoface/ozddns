// Package seed provides first-run defaults and an optional demo dataset used by
// the panel walkthrough and end-to-end tests.
package seed

import (
	"context"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/storage"
)

// EnsureDefaults inserts a couple of safe public IP sources and default settings
// on a fresh database. It is idempotent.
func EnsureDefaults(ctx context.Context, store storage.Store) error {
	sources, err := store.IPSources().List(ctx)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		defaults := []domain.IPSource{
			{Name: "ipify (IPv4)", Type: domain.IPHTTP, Family: "v4", Target: "https://api.ipify.org"},
			{Name: "ifconfig.co (IPv6)", Type: domain.IPHTTP, Family: "v6", Target: "https://v6.ifconfig.co"},
		}
		for _, s := range defaults {
			if err := store.IPSources().Create(ctx, s); err != nil {
				return err
			}
		}
	}
	return nil
}

// Demo inserts the dataset the design prototype uses, so the panel has realistic
// providers, tasks and history to show. It is a no-op when providers already
// exist.
func Demo(ctx context.Context, store storage.Store) error {
	existing, err := store.Providers().List(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	now := time.Now().UTC()
	min := func(n int) time.Time { return now.Add(-time.Duration(n) * time.Minute) }

	providers := []domain.Provider{
		{ID: "p-cf", Name: "Homelab Cloudflare", Type: "cloudflare", Enabled: true,
			Config: map[string]any{"api_token": "TEST_cf_token_0000007f2a", "proxied": true, "ttl": 300}},
		{ID: "p-ali", Name: "Aliyun DNS · 主站", Type: "alidns", Enabled: true,
			Config: map[string]any{"access_key_id": "TEST_LTAI5t0000", "access_key_secret": "TEST_secret_0000", "region": "cn-hangzhou"}},
		{ID: "p-dnspod", Name: "Tencent DNSPod", Type: "dnspod", Enabled: true,
			Config: map[string]any{"login_token_id": "TEST_447", "login_token": "TEST_token_0000", "record_line": "默认"}},
		{ID: "p-gd", Name: "GoDaddy (legacy)", Type: "godaddy", Enabled: false,
			Config: map[string]any{"key": "TEST_key", "secret": "TEST_secret"}},
		{ID: "p-duck", Name: "DuckDNS", Type: "duckdns", Enabled: true,
			Config: map[string]any{"token": "TEST_token-0000-0000", "subdomain": "ozddns"}},
		{ID: "p-hook", Name: "NAS Callback", Type: "custom", Enabled: true,
			Config: map[string]any{"url": "https://nas.lan/hooks/ddns", "method": "POST", "auth": "TEST_bearer"}},
	}
	for _, p := range providers {
		if err := store.Providers().Create(ctx, p); err != nil {
			return err
		}
	}

	sources := []domain.IPSource{
		{ID: "src-ipify-v4", Name: "ipify (IPv4)", Type: domain.IPHTTP, Family: "v4", Target: "https://api.ipify.org"},
		{ID: "src-ifconfig-v6", Name: "ifconfig.co (IPv6)", Type: domain.IPHTTP, Family: "v6", Target: "https://v6.ifconfig.co"},
		{ID: "src-en0", Name: "LAN interface · en0", Type: domain.IPInterface, Family: "v4", Target: "en0"},
		{ID: "src-cmd-v6", Name: "Tailscale address", Type: domain.IPCommand, Family: "v6", Target: "tailscale ip -6"},
	}
	for _, s := range sources {
		if err := store.IPSources().Create(ctx, s); err != nil {
			return err
		}
	}

	jobs := []domain.Job{
		{ID: "j-home-v4", Name: "Home IPv4", ProviderID: "p-cf", IPSourceID: "src-ipify-v4",
			Targets:         []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300, Proxied: true, Comment: "managed by ozddns"}},
			IntervalMinutes: 5, RunOnChangeOnly: true, Jitter: true, Enabled: true},
		{ID: "j-home-v6", Name: "Home IPv6", ProviderID: "p-cf", IPSourceID: "src-ifconfig-v6",
			Targets:         []domain.Target{{Domain: "home.example.com", Type: domain.RecordAAAA, TTL: 300, Comment: "managed by ozddns"}},
			IntervalMinutes: 5, RunOnChangeOnly: true, Jitter: true, Enabled: true},
		{ID: "j-nas", Name: "NAS & CDN edge", ProviderID: "p-ali", IPSourceID: "src-en0",
			Targets: []domain.Target{
				{Domain: "nas.ozhao.cn", Type: domain.RecordA, TTL: 600, Comment: "nas"},
				{Domain: "cdn.ozhao.cn", Type: domain.RecordA, TTL: 600, Comment: "edge cache"},
			},
			IntervalMinutes: 10, RunOnChangeOnly: true, Enabled: true},
		{ID: "j-blog", Name: "Blog (DNSPod)", ProviderID: "p-dnspod", IPSourceID: "src-ipify-v4",
			Targets:         []domain.Target{{Domain: "blog.family.site", Type: domain.RecordA, TTL: 120}},
			IntervalMinutes: 15, RunOnChangeOnly: false, Jitter: true, Enabled: true},
		{ID: "j-wild", Name: "Wildcard .ozhao.cn", ProviderID: "p-ali", IPSourceID: "src-ipify-v4",
			Targets:         []domain.Target{{Domain: "*.ozhao.cn", Type: domain.RecordA, TTL: 3600, Comment: "catch-all"}},
			IntervalMinutes: 30, RunOnChangeOnly: true, Jitter: true, Enabled: false},
		{ID: "j-iot", Name: "IoT callback", ProviderID: "p-hook", IPSourceID: "src-en0",
			Targets:         []domain.Target{{Domain: "iot.homelab.net", Type: domain.RecordA, TTL: 300, Comment: "webhook target"}},
			IntervalMinutes: 60, RunOnChangeOnly: true, Enabled: true},
		{ID: "j-tail", Name: "Tailscale node", ProviderID: "p-duck", IPSourceID: "src-cmd-v6",
			Targets:         []domain.Target{{Domain: "ozddns.duckdns.org", Type: domain.RecordAAAA, TTL: 60}},
			IntervalMinutes: 2, RunOnChangeOnly: true, Enabled: true},
	}
	for _, j := range jobs {
		if err := store.Jobs().Create(ctx, j); err != nil {
			return err
		}
	}

	states := []domain.JobState{
		{JobID: "j-home-v4", LastStatus: domain.StatusNoChange, LastRunAt: min(2), NextRunAt: now.Add(3 * time.Minute), CurrentIP: "203.0.113.42", DesiredIP: "203.0.113.42", LastChangedAt: min(60 * 9)},
		{JobID: "j-home-v6", LastStatus: domain.StatusNoChange, LastRunAt: min(2), NextRunAt: now.Add(3 * time.Minute), CurrentIP: "2001:db8:8a2e:370:7334::1", DesiredIP: "2001:db8:8a2e:370:7334::1", LastChangedAt: min(60 * 30)},
		{JobID: "j-nas", LastStatus: domain.StatusSuccess, LastRunAt: min(7), NextRunAt: now.Add(3 * time.Minute), CurrentIP: "198.51.100.7", DesiredIP: "198.51.100.7", LastChangedAt: min(7)},
		{JobID: "j-blog", LastStatus: domain.StatusFailed, LastRunAt: min(11), NextRunAt: now.Add(4 * time.Minute), CurrentIP: "192.0.2.55", DesiredIP: "203.0.113.42", LastError: "authentication failed: token rejected (401)", LastChangedAt: min(60 * 26)},
		{JobID: "j-wild", LastStatus: domain.StatusNoChange, LastRunAt: min(240), NextRunAt: now.Add(30 * time.Minute), CurrentIP: "203.0.113.42", DesiredIP: "203.0.113.42", LastChangedAt: min(60 * 40)},
		{JobID: "j-iot", LastStatus: domain.StatusSuccess, LastRunAt: min(35), NextRunAt: now.Add(25 * time.Minute), CurrentIP: "203.0.113.42", LastChangedAt: min(35)},
		{JobID: "j-tail", LastStatus: domain.StatusSkipped, LastRunAt: min(1), NextRunAt: now.Add(1 * time.Minute), LastError: "No IPv6 address detected; nothing to update."},
	}
	for _, st := range states {
		if err := store.States().Upsert(ctx, st); err != nil {
			return err
		}
	}

	runs := []domain.Run{
		{JobID: "j-home-v4", StartedAt: min(12), FinishedAt: min(12), DurationMS: 3120, Status: domain.StatusSuccess, OldIP: "203.0.113.9", NewIP: "203.0.113.42", Trigger: domain.TriggerSchedule, Message: "updated 1 record(s)", Records: 1},
		{JobID: "j-nas", StartedAt: min(7), FinishedAt: min(7), DurationMS: 1420, Status: domain.StatusSuccess, OldIP: "198.51.100.7", NewIP: "198.51.100.7", Trigger: domain.TriggerSchedule, Message: "updated 1 record(s)", Records: 1},
		{JobID: "j-blog", StartedAt: min(11), FinishedAt: min(11), DurationMS: 510, Status: domain.StatusFailed, Trigger: domain.TriggerSchedule, Message: "authentication failed: token rejected (401)"},
		{JobID: "j-home-v4", StartedAt: min(2), FinishedAt: min(2), DurationMS: 780, Status: domain.StatusNoChange, OldIP: "203.0.113.42", NewIP: "203.0.113.42", Trigger: domain.TriggerSchedule, Message: "records already match the desired address"},
		{JobID: "j-tail", StartedAt: min(1), FinishedAt: min(1), DurationMS: 320, Status: domain.StatusSkipped, Trigger: domain.TriggerSchedule, Message: "no v6 address for ozddns.duckdns.org"},
		{JobID: "j-iot", StartedAt: min(35), FinishedAt: min(35), DurationMS: 980, Status: domain.StatusSuccess, OldIP: "203.0.113.42", NewIP: "203.0.113.42", Trigger: domain.TriggerSchedule, Message: "updated 1 record(s)", Records: 1},
	}
	for _, r := range runs {
		if _, err := store.Runs().Insert(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// HealthSetter is the subset of the application the demo priming needs.
type HealthSetter interface {
	SetProviderHealth(id, status, statusText string, zones []string)
}

// PrimeDemoHealth gives the demo providers the states the design fixtures
// assume (ok / warn / err, and their zones) so the panel looks like the
// prototype on first run, without contacting any real provider.
func PrimeDemoHealth(a HealthSetter) {
	a.SetProviderHealth("p-cf", "ok", "Validated · token accepted", []string{"example.com", "homelab.net"})
	a.SetProviderHealth("p-ali", "ok", "Validated · 1 zone reachable", []string{"ozhao.cn"})
	a.SetProviderHealth("p-dnspod", "warn", "Token expires in 6 days", []string{"family.site"})
	a.SetProviderHealth("p-gd", "err", "Invalid credentials (401)", nil)
	a.SetProviderHealth("p-duck", "ok", "Validated · subdomain found", []string{"ozddns"})
	a.SetProviderHealth("p-hook", "ok", "Endpoint reachable", nil)
}

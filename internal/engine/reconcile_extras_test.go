package engine

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/provider"
)

// TTL is part of the desired state: the same IP with a different TTL must
// produce an update, not a "no change".
func TestReconcileTTLDrift(t *testing.T) {
	r, fp := setup("cf", map[string][]domain.Record{
		"home.example.com": {{ID: "r1", Name: "home.example.com", Type: domain.RecordA, Content: "203.0.113.9", TTL: 300}},
	}, "203.0.113.9", nil)
	res, err := r.Reconcile(context.Background(),
		job("t", domain.Target{Domain: "home.example.com", Type: domain.RecordA, TTL: 600}), prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.StatusSuccess || len(fp.applied) != 1 || fp.applied[0].Kind != domain.ChangeUpdate {
		t.Fatalf("status=%s applied=%+v", res.Status, fp.applied)
	}
	if fp.applied[0].Record.TTL != 600 {
		t.Fatalf("applied TTL = %d, want 600", fp.applied[0].Record.TTL)
	}
}

// A provider that does not report a TTL (0) must not be treated as drift, or a
// task would rewrite the same record on every tick.
func TestReconcileTTLUnknownIsNotDrift(t *testing.T) {
	r, fp := setup("cf", map[string][]domain.Record{
		"home.example.com": {{ID: "r1", Name: "home.example.com", Type: domain.RecordA, Content: "203.0.113.9", TTL: 0}},
	}, "203.0.113.9", nil)
	res, err := r.Reconcile(context.Background(),
		job("t", domain.Target{Domain: "home.example.com", Type: domain.RecordA, TTL: 600}), prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.StatusNoChange || len(fp.applied) != 0 {
		t.Fatalf("status=%s applied=%+v", res.Status, fp.applied)
	}
}

// Some provider APIs include an unpersisted placeholder for the requested
// name with no record ID. Treating that as "absent" would create a second
// record; the real record must be updated in place.
func TestReconcileIgnoresIDLessPlaceholderAndUpdatesRealRecord(t *testing.T) {
	r, fp := setup("cf", map[string][]domain.Record{
		"home.example.com": {
			{ID: "", Type: domain.RecordA, Name: "home.example.com", Content: "203.0.113.99"},
			{ID: "r1", Type: domain.RecordA, Name: "home.example.com", Content: "203.0.113.8"},
		},
	}, "203.0.113.9", nil)
	res, err := r.Reconcile(context.Background(),
		job("t", domain.Target{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}),
		prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.StatusSuccess || len(fp.applied) != 1 {
		t.Fatalf("status=%s applied=%+v", res.Status, fp.applied)
	}
	if got := fp.applied[0]; got.Kind != domain.ChangeUpdate || got.Record.ID != "r1" || got.Record.Content != "203.0.113.9" {
		t.Fatalf("applied = %+v, want in-place update of r1", got)
	}
}

// A CNAME target is not address-driven: it is skipped, never written with an IP.
func TestReconcileSkipsCNAME(t *testing.T) {
	r, fp := setup("cf", nil, "203.0.113.9", nil)
	res, err := r.Reconcile(context.Background(),
		job("t", domain.Target{Domain: "alias.example.com", Type: domain.RecordCNAME, TTL: 300}), prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.StatusSkipped || len(fp.applied) != 0 {
		t.Fatalf("status=%s applied=%+v", res.Status, fp.applied)
	}
	if len(res.Details) != 1 || res.Details[0].Action != domain.RunActionSkipped {
		t.Fatalf("details = %+v", res.Details)
	}
}

// DryRun reflects TTL drift and skips CNAME too.
func TestDryRunTTLAndCNAME(t *testing.T) {
	r, _ := setup("cf", map[string][]domain.Record{
		"home.example.com": {{ID: "r1", Name: "home.example.com", Type: domain.RecordA, Content: "203.0.113.9", TTL: 300}},
	}, "203.0.113.9", nil)
	diff, err := r.DryRun(context.Background(), job("t",
		domain.Target{Domain: "home.example.com", Type: domain.RecordA, TTL: 600},
		domain.Target{Domain: "alias.example.com", Type: domain.RecordCNAME},
	), prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Targets) != 2 {
		t.Fatalf("targets = %+v", diff.Targets)
	}
	if diff.Targets[0].Action != ActionUpdate || diff.Targets[0].CurrentTTL != 300 || diff.Targets[0].TTL != 600 {
		t.Fatalf("ttl target = %+v", diff.Targets[0])
	}
	if diff.Targets[1].Action != ActionSkip {
		t.Fatalf("cname target = %+v", diff.Targets[1])
	}
}

// The fingerprint changes whenever the address, provider, source or target set
// changes, so the skip decision never masks a real edit.
func TestFingerprintTracksEverythingThatMatters(t *testing.T) {
	base := job("t", domain.Target{Domain: "home.example.com", Type: domain.RecordA, TTL: 300})
	base.ProviderID, base.IPSourceID = "p1", "s1"
	addrs := map[string]netip.Addr{"v4": netip.MustParseAddr("203.0.113.9")}
	p := domain.Provider{ID: "p1", Type: "custom", Config: map[string]any{"url": "https://hook.example/x", "method": "GET"}}
	s := domain.IPSource{ID: "s1", Type: "static", Family: "v4", Target: "203.0.113.9"}

	fp := Fingerprint(base, p, s, addrs)
	if fp == "" {
		t.Fatal("fingerprint should not be empty")
	}
	if Fingerprint(base, p, s, addrs) != fp {
		t.Fatal("fingerprint must be stable")
	}
	if Fingerprint(base, p, s, map[string]netip.Addr{"v4": netip.MustParseAddr("203.0.113.9")}) != fp {
		t.Fatal("fingerprint must not depend on map identity or ordering")
	}

	changed := base
	changed.Targets = []domain.Target{{Domain: "nas.example.com", Type: domain.RecordA, TTL: 300}}
	if Fingerprint(changed, p, s, addrs) == fp {
		t.Fatal("adding/renaming a target must change the fingerprint")
	}

	moved := base
	moved.ProviderID = "p2"
	if Fingerprint(moved, p, s, addrs) == fp {
		t.Fatal("switching provider must change the fingerprint")
	}

	// Editing the provider config (a new webhook URL, a rotated token) must
	// force a write: a write-only provider would otherwise never see it.
	repointed := p
	repointed.Config = map[string]any{"url": "https://hook.example/other", "method": "GET"}
	if Fingerprint(base, repointed, s, addrs) == fp {
		t.Fatal("editing the provider config must change the fingerprint")
	}

	// Re-pointing the IP source is a change even when the address it resolves
	// happens to be identical.
	otherSource := s
	otherSource.Target = "different-endpoint"
	if Fingerprint(base, p, otherSource, addrs) == fp {
		t.Fatal("editing the source definition must change the fingerprint")
	}
}

// Secrets must never leak into the fingerprint: it is persisted and returned by
// the API, so provider config is folded in as a digest, not verbatim.
func TestFingerprintDoesNotLeakSecrets(t *testing.T) {
	j := job("t", domain.Target{Domain: "home.example.com", Type: domain.RecordA, TTL: 300})
	j.ProviderID, j.IPSourceID = "p1", "s1"
	p := domain.Provider{ID: "p1", Type: "custom", Config: map[string]any{"url": "https://hook/x", "auth": "Bearer super-secret-token"}}
	s := domain.IPSource{ID: "s1", Type: "static", Family: "v4", Target: "203.0.113.9"}
	fp := Fingerprint(j, p, s, map[string]netip.Addr{"v4": netip.MustParseAddr("203.0.113.9")})
	if strings.Contains(fp, "super-secret-token") || strings.Contains(fp, "hook") {
		t.Fatalf("fingerprint leaked provider config: %s", fp)
	}
}

// A write-only provider must never be read back. DuckDNS and Namecheap answer
// ListRecords with an error, so a reconciler that reads first would fail every
// single run and could never converge.
func TestReconcileWriteOnlyProviderNeverReads(t *testing.T) {
	fp := &fakeProvider{}
	reg := provider.NewRegistry()
	reg.Register(fakeFactory{typ: "duckdns", prov: fp, writeOnly: true})
	srcs := ipsource.NewRegistry()
	srcs.Register(fakeSource{addr: netip.MustParseAddr("203.0.113.9")})
	r := NewReconciler(reg, srcs)

	res, err := r.Reconcile(context.Background(),
		job("t", domain.Target{Domain: "oz.duckdns.org", Type: domain.RecordA, TTL: 60}),
		prov("duckdns"), src())
	if err != nil {
		t.Fatalf("write-only reconcile: %v", err)
	}
	if res.Status != domain.StatusSuccess || res.Records != 1 || len(fp.applied) != 1 {
		t.Fatalf("status=%s records=%d applied=%+v", res.Status, res.Records, fp.applied)
	}
	if fp.applied[0].Record.Content != "203.0.113.9" || fp.applied[0].Kind != domain.ChangeUpdate {
		t.Fatalf("applied = %+v", fp.applied[0])
	}
	if len(res.Details) != 1 || res.Details[0].Action != domain.RunActionApplied {
		t.Fatalf("details = %+v", res.Details)
	}

	// A dry run explains the write without pretending to know the old value.
	diff, err := r.DryRun(context.Background(),
		job("t", domain.Target{Domain: "oz.duckdns.org", Type: domain.RecordA}),
		prov("duckdns"), src())
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Targets) != 1 || diff.Targets[0].Action != ActionApply || diff.Targets[0].Desired != "203.0.113.9" {
		t.Fatalf("diff = %+v", diff.Targets)
	}
}

package engine

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/provider"
)

// ---- fakes ---------------------------------------------------------------

type fakeProvider struct {
	records  map[string][]domain.Record
	applied  []domain.Change
	listErr  error
	applyErr error
}

func (p *fakeProvider) Validate(context.Context) error { return nil }
func (p *fakeProvider) ListZones(context.Context) ([]domain.Zone, error) {
	return nil, nil
}
func (p *fakeProvider) ListRecords(_ context.Context, name string, _ domain.RecordFilter) ([]domain.Record, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return p.records[name], nil
}
func (p *fakeProvider) Apply(_ context.Context, ch domain.Change) error {
	if p.applyErr != nil {
		return p.applyErr
	}
	p.applied = append(p.applied, ch)
	return nil
}

type fakeFactory struct {
	typ       string
	prov      *fakeProvider
	writeOnly bool
}

func (f fakeFactory) Meta() provider.Meta {
	return provider.Meta{Type: f.typ, Label: f.typ, WriteOnly: f.writeOnly}
}
func (f fakeFactory) ValidateConfig(map[string]any) error           { return nil }
func (f fakeFactory) New(map[string]any) (provider.Provider, error) { return f.prov, nil }

type fakeSource struct {
	addr netip.Addr
	err  error
}

func (fakeSource) Meta() ipsource.Meta                   { return ipsource.Meta{ID: "fake", Label: "fake"} }
func (s fakeSource) New(string) (ipsource.Source, error) { return s, nil }
func (s fakeSource) Get(context.Context, string) (netip.Addr, error) {
	return s.addr, s.err
}

func setup(typ string, recs map[string][]domain.Record, addr string, srcErr error) (*Reconciler, *fakeProvider) {
	fp := &fakeProvider{records: recs}
	reg := provider.NewRegistry()
	reg.Register(fakeFactory{typ: typ, prov: fp})
	srcs := ipsource.NewRegistry()
	a, _ := netip.ParseAddr(addr)
	srcs.Register(fakeSource{addr: a, err: srcErr})
	return NewReconciler(reg, srcs), fp
}

func job(name string, targets ...domain.Target) domain.Job {
	return domain.Job{ID: "j1", Name: name, ProviderID: "p1", IPSourceID: "s1", Targets: targets}
}

func prov(typ string) domain.Provider { return domain.Provider{ID: "p1", Name: "p", Type: typ} }
func src() domain.IPSource            { return domain.IPSource{ID: "s1", Type: "fake", Family: "v4", Target: "x"} }
func srcFor(family string) domain.IPSource {
	return domain.IPSource{ID: "s1", Type: "fake", Family: family, Target: "x"}
}

// ---- cases ---------------------------------------------------------------

// REC-01: desired == current => no write.
func TestReconcileNoChange(t *testing.T) {
	r, fp := setup("cf", map[string][]domain.Record{
		"home.example.com": {{ID: "r1", Name: "home.example.com", Type: domain.RecordA, Content: "203.0.113.9"}},
	}, "203.0.113.9", nil)
	res, err := r.Reconcile(context.Background(), job("t", domain.Target{Domain: "home.example.com", Type: domain.RecordA}),
		prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.StatusNoChange || len(fp.applied) != 0 {
		t.Fatalf("status=%s applied=%d", res.Status, len(fp.applied))
	}
	if res.OldIP != "203.0.113.9" || res.NewIP != "203.0.113.9" {
		t.Fatalf("old=%s new=%s", res.OldIP, res.NewIP)
	}
}

// REC-02: drift => update, diff recorded.
func TestReconcileUpdate(t *testing.T) {
	r, fp := setup("cf", map[string][]domain.Record{
		"home.example.com": {{ID: "r1", Name: "home.example.com", Type: domain.RecordA, Content: "203.0.113.9"}},
	}, "203.0.113.42", nil)
	res, err := r.Reconcile(context.Background(), job("t", domain.Target{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}),
		prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.StatusSuccess || res.Records != 1 {
		t.Fatalf("status=%s records=%d", res.Status, res.Records)
	}
	if len(fp.applied) != 1 || fp.applied[0].Kind != domain.ChangeUpdate || fp.applied[0].Record.ID != "r1" {
		t.Fatalf("applied = %+v", fp.applied)
	}
	if res.OldIP != "203.0.113.9" || res.NewIP != "203.0.113.42" {
		t.Fatalf("old=%s new=%s", res.OldIP, res.NewIP)
	}
}

// REC-03: no existing record => create.
func TestReconcileCreate(t *testing.T) {
	r, fp := setup("cf", nil, "203.0.113.42", nil)
	res, err := r.Reconcile(context.Background(), job("t", domain.Target{Domain: "new.example.com", Type: domain.RecordA}),
		prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.StatusSuccess || len(fp.applied) != 1 || fp.applied[0].Kind != domain.ChangeCreate {
		t.Fatalf("status=%s applied=%+v", res.Status, fp.applied)
	}
}

// REC-04: two records, only one drifted => exactly one write.
func TestReconcilePartialUpdate(t *testing.T) {
	r, fp := setup("cf", map[string][]domain.Record{
		"a.example.com": {{ID: "ra", Name: "a.example.com", Type: domain.RecordA, Content: "203.0.113.42"}},
		"b.example.com": {{ID: "rb", Name: "b.example.com", Type: domain.RecordA, Content: "198.51.100.7"}},
	}, "203.0.113.42", nil)
	res, _ := r.Reconcile(context.Background(), job("t",
		domain.Target{Domain: "a.example.com", Type: domain.RecordA},
		domain.Target{Domain: "b.example.com", Type: domain.RecordA}), prov("cf"), src())
	if res.Records != 1 || len(fp.applied) != 1 || fp.applied[0].Record.Name != "b.example.com" {
		t.Fatalf("records=%d applied=%+v", res.Records, fp.applied)
	}
}

// REC-05: same IP but the proxied flag changed => still a write for Cloudflare.
func TestReconcileProxiedChange(t *testing.T) {
	r, fp := setup("cloudflare", map[string][]domain.Record{
		"prox.example.com": {{ID: "r1", Name: "prox.example.com", Type: domain.RecordA, Content: "203.0.113.42", Proxied: false}},
	}, "203.0.113.42", nil)
	res, _ := r.Reconcile(context.Background(), job("t", domain.Target{Domain: "prox.example.com", Type: domain.RecordA, Proxied: true}),
		prov("cloudflare"), src())
	if res.Status != domain.StatusSuccess || len(fp.applied) != 1 || fp.applied[0].Record.Proxied != true {
		t.Fatalf("status=%s applied=%+v", res.Status, fp.applied)
	}
}

// REC-06: a run reports one detail per record so the panel can show which
// records changed and which did not.
func TestReconcileReportsPerRecordDetails(t *testing.T) {
	r, _ := setup("cf", map[string][]domain.Record{
		"a.example.com": {{ID: "ra", Name: "a.example.com", Type: domain.RecordA, Content: "203.0.113.42"}},
		"b.example.com": {{ID: "rb", Name: "b.example.com", Type: domain.RecordA, Content: "198.51.100.7"}},
	}, "203.0.113.42", nil)
	res, err := r.Reconcile(context.Background(), job("t",
		domain.Target{Domain: "a.example.com", Type: domain.RecordA},
		domain.Target{Domain: "b.example.com", Type: domain.RecordA}), prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Details) != 2 {
		t.Fatalf("details = %+v", res.Details)
	}
	if res.Details[0].Action != domain.RunActionUnchanged || res.Details[1].Action != domain.RunActionUpdated {
		t.Fatalf("actions = %q, %q", res.Details[0].Action, res.Details[1].Action)
	}
	if res.Details[1].Current != "198.51.100.7" || res.Details[1].Desired != "203.0.113.42" {
		t.Fatalf("detail = %+v", res.Details[1])
	}
}

// REC-07: source-resolution and provider-write failures are both surfaced.
func TestReconcileDetailsForFailures(t *testing.T) {
	// A source failure is not the same as an unconfigured address: report it as
	// failed so the operator sees the actual error.
	r, _ := setup("cf", nil, "", errors.New("no v6 address"))
	res, err := r.Reconcile(context.Background(), job("t", domain.Target{Domain: "v6.example.com", Type: domain.RecordAAAA}),
		prov("cf"), srcFor("v6"))
	if err == nil || res.Status != domain.StatusFailed {
		t.Fatalf("source failure = %+v err=%v", res, err)
	}

	// Provider rejects the write => failed detail.
	r2, fp2 := setup("cf", nil, "203.0.113.42", nil)
	fp2.applyErr = errors.New("write rejected")
	res2, err := r2.Reconcile(context.Background(), job("t", domain.Target{Domain: "new.example.com", Type: domain.RecordA}),
		prov("cf"), src())
	if err == nil {
		t.Fatal("expected an error from the failed write")
	}
	if len(res2.Details) != 1 || res2.Details[0].Action != domain.RunActionFailed {
		t.Fatalf("details = %+v", res2.Details)
	}
}

// REC-10: a source resolution error is failed, not a misleading "no address"
// skip, and never reaches the provider.
func TestReconcileSourceFailureIsFailed(t *testing.T) {
	r, fp := setup("cf", nil, "", errors.New("no v6 address"))
	res, err := r.Reconcile(context.Background(), job("t", domain.Target{Domain: "v6.example.com", Type: domain.RecordAAAA}),
		prov("cf"), srcFor("v6"))
	if err == nil {
		t.Fatal("expected source failure")
	}
	if res.Status != domain.StatusFailed || len(fp.applied) != 0 {
		t.Fatalf("status=%s applied=%d", res.Status, len(fp.applied))
	}
}

func TestPlanRejectsTargetFamilyMismatch(t *testing.T) {
	r, _ := setup("cf", nil, "203.0.113.42", nil)
	if _, err := r.Plan(context.Background(),
		job("t", domain.Target{Domain: "v6.example.com", Type: domain.RecordAAAA}), src()); err == nil {
		t.Fatal("an AAAA target must be rejected for a v4 source")
	}
}

// REC-11: provider rejects the write => failed, error surfaced.
func TestReconcileFailure(t *testing.T) {
	r, fp := setup("cf", nil, "203.0.113.42", nil)
	fp.applyErr = errors.New("authentication failed: token rejected (401)")
	res, err := r.Reconcile(context.Background(), job("t", domain.Target{Domain: "x.example.com", Type: domain.RecordA}),
		prov("cf"), src())
	if err == nil || res.Status != domain.StatusFailed {
		t.Fatalf("expected failure, got %v / %s", err, res.Status)
	}
}

// REC-12: dry run never writes and classifies each target.
func TestDryRun(t *testing.T) {
	r, fp := setup("cf", map[string][]domain.Record{
		"same.example.com": {{ID: "r1", Name: "same.example.com", Type: domain.RecordA, Content: "203.0.113.42"}},
		"diff.example.com": {{ID: "r2", Name: "diff.example.com", Type: domain.RecordA, Content: "198.51.100.7"}},
	}, "203.0.113.42", nil)
	diff, err := r.DryRun(context.Background(), job("t",
		domain.Target{Domain: "same.example.com", Type: domain.RecordA},
		domain.Target{Domain: "diff.example.com", Type: domain.RecordA},
		domain.Target{Domain: "new.example.com", Type: domain.RecordA}), prov("cf"), src())
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.applied) != 0 {
		t.Fatalf("dry run wrote %d changes", len(fp.applied))
	}
	want := []ChangeAction{ActionNone, ActionUpdate, ActionCreate}
	if len(diff.Targets) != 3 {
		t.Fatalf("targets = %+v", diff.Targets)
	}
	for i, w := range want {
		if diff.Targets[i].Action != w {
			t.Fatalf("target %d = %s, want %s", i, diff.Targets[i].Action, w)
		}
	}
}

func TestSameAddr(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"203.0.113.9", "203.0.113.9", true},
		{"203.0.113.9", "203.0.113.42", false},
		{"2001:0db8::0001", "2001:db8::1", true},
		{"EXAMPLE.com", "example.COM", true},
	}
	for _, c := range cases {
		if got := sameAddr(c.a, c.b); got != c.same {
			t.Errorf("sameAddr(%q,%q)=%v want %v", c.a, c.b, got, c.same)
		}
	}
}

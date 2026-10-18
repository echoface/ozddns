package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

func TestResolveIPUsesSourceReferencedByEnabledTask(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	// This is the regression: an unused, broken source was created first. It
	// must not win over the source an enabled task actually uses.
	if _, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "stale interface", Type: domain.IPInterface, Family: "v6", Target: "ozddns-no-such-interface",
	}); err != nil {
		t.Fatal(err)
	}
	active, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "active IPv6", Type: domain.IPStatic, Family: "v6", Target: "2001:db8::10",
	})
	if err != nil {
		t.Fatal(err)
	}
	createIPTestJob(t, a, "j-v6", active.ID, domain.RecordAAAA, true)

	got := a.ResolveIP(ctx)
	if got.V6.Status != IPStatusOK {
		t.Fatalf("v6 status = %q error=%q", got.V6.Status, got.V6.Error)
	}
	if got.V6.Address != "2001:db8::10" || got.V6.SourceID != active.ID || got.V6.Source != active.Name {
		t.Fatalf("v6 result = %+v", got.V6)
	}
	if got.V6.Tasks != 1 {
		t.Fatalf("v6 tasks = %d, want 1", got.V6.Tasks)
	}
	if got.V4.Status != IPStatusNoActiveTasks || got.V4.Tasks != 0 {
		t.Fatalf("v4 result = %+v, want no active tasks", got.V4)
	}
	if got.CheckedAt.IsZero() || got.V6.CheckedAt == nil || got.V6.CheckedAt.IsZero() {
		t.Fatalf("checked timestamps missing: %+v", got)
	}
}

func TestResolveIPReportsActiveSourceFailure(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	failing, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "broken v6 command", Type: domain.IPCommand, Family: "v6", Target: "exit 7",
	})
	if err != nil {
		t.Fatal(err)
	}
	createIPTestJob(t, a, "j-failing", failing.ID, domain.RecordAAAA, true)

	got := a.ResolveIP(ctx).V6
	if got.Status != IPStatusError || got.Address != "" {
		t.Fatalf("v6 result = %+v, want an explicit error", got)
	}
	if got.SourceID != failing.ID || got.Source != failing.Name || got.Tasks != 1 {
		t.Fatalf("failure attribution = %+v", got)
	}
	if !strings.Contains(got.Error, "command failed") {
		t.Fatalf("error = %q, want command failure", got.Error)
	}
	if got.CheckedAt == nil || got.CheckedAt.IsZero() {
		t.Fatal("failed check must carry its own timestamp")
	}
}

func TestResolveIPFallsBackToWorkingActiveSource(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	broken, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "primary broken", Type: domain.IPCommand, Family: "v6", Target: "exit 9",
	})
	if err != nil {
		t.Fatal(err)
	}
	working, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "fallback working", Type: domain.IPStatic, Family: "v6", Target: "2001:db8::20",
	})
	if err != nil {
		t.Fatal(err)
	}
	createIPTestJob(t, a, "j-broken", broken.ID, domain.RecordAAAA, true)
	createIPTestJob(t, a, "j-working", working.ID, domain.RecordAAAA, true)

	now := time.Now().UTC()
	if err := a.Store.States().Upsert(ctx, domain.JobState{
		JobID: "j-broken", LastStatus: domain.StatusSuccess, LastRunAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.States().Upsert(ctx, domain.JobState{
		JobID: "j-working", LastStatus: domain.StatusSuccess, LastRunAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	got := a.ResolveIP(ctx).V6
	if got.Status != IPStatusOK || got.Address != "2001:db8::20" {
		t.Fatalf("v6 result = %+v, want fallback address", got)
	}
	if got.SourceID != working.ID || got.Source != working.Name || got.Tasks != 1 {
		t.Fatalf("fallback attribution = %+v", got)
	}
}

func TestResolveIPCountsOnlyEnabledTasksUsingSelectedSource(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	source, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "shared IPv6", Type: domain.IPStatic, Family: "v6", Target: "2001:db8::30",
	})
	if err != nil {
		t.Fatal(err)
	}
	createIPTestJob(t, a, "j-one", source.ID, domain.RecordAAAA, true)
	createIPTestJob(t, a, "j-two", source.ID, domain.RecordAAAA, true)
	createIPTestJob(t, a, "j-disabled", source.ID, domain.RecordAAAA, false)

	got := a.ResolveIP(ctx).V6
	if got.Status != IPStatusOK || got.Tasks != 2 {
		t.Fatalf("v6 result = %+v, want two enabled tasks", got)
	}
}

func createIPTestJob(t *testing.T, a *App, id, sourceID string, recordType domain.RecordType, enabled bool) {
	t.Helper()
	err := a.Store.Jobs().Create(context.Background(), domain.Job{
		ID: id, Name: id, ProviderID: "p-unused", IPSourceID: sourceID,
		Targets:         []domain.Target{{Domain: id + ".example.com", Type: recordType, TTL: 300}},
		IntervalMinutes: 5, Enabled: enabled,
	})
	if err != nil {
		t.Fatalf("create job %s: %v", id, err)
	}
}

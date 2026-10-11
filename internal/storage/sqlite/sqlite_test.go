package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ids"
	"github.com/echoface/ozddns/internal/storage"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestProviderCRUD(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	repo := db.Providers()

	p := domain.Provider{ID: ids.New("p"), Name: "CF", Type: "cloudflare",
		Config: map[string]any{"api_token": "tok", "ttl": 300}, Enabled: true}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "CF" || got.Type != "cloudflare" || got.Config["api_token"] != "tok" || !got.Enabled {
		t.Fatalf("got %+v", got)
	}
	if got.Config["ttl"] != float64(300) {
		t.Fatalf("ttl round-trip: %#v", got.Config["ttl"])
	}

	got.Name = "Cloudflare prod"
	got.Enabled = false
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	again, _ := repo.Get(ctx, p.ID)
	if again.Name != "Cloudflare prod" || again.Enabled {
		t.Fatalf("after update %+v", again)
	}

	list, _ := repo.List(ctx)
	if len(list) != 1 {
		t.Fatalf("list len %d", len(list))
	}
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, p.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestJobAndStateCRUD(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	j := domain.Job{ID: ids.New("j"), Name: "Home", ProviderID: "p1", IPSourceID: "s1",
		Targets:         []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300, Proxied: true, Comment: "c"}},
		IntervalMinutes: 5, RunOnChangeOnly: true, Jitter: true, Enabled: true}
	if err := db.Jobs().Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := db.Jobs().Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Targets) != 1 || got.Targets[0].Domain != "home.example.com" || !got.Targets[0].Proxied || got.Targets[0].Comment != "c" {
		t.Fatalf("targets round-trip: %+v", got.Targets)
	}
	if got.IntervalMinutes != 5 || !got.RunOnChangeOnly || !got.Jitter {
		t.Fatalf("flags: %+v", got)
	}

	// State upsert twice must not duplicate.
	st := domain.JobState{JobID: j.ID, LastStatus: domain.StatusSuccess, LastRunAt: time.Now().UTC(),
		NextRunAt: time.Now().UTC().Add(5 * time.Minute), CurrentIP: "203.0.113.9", DesiredIP: "203.0.113.42"}
	if err := db.States().Upsert(ctx, st); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	st.LastStatus = domain.StatusNoChange
	st.CurrentIP = "203.0.113.42"
	if err := db.States().Upsert(ctx, st); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}
	all, _ := db.States().List(ctx)
	if len(all) != 1 {
		t.Fatalf("state rows = %d", len(all))
	}
	gotSt, _ := db.States().Get(ctx, j.ID)
	if gotSt.LastStatus != domain.StatusNoChange || gotSt.CurrentIP != "203.0.113.42" {
		t.Fatalf("state = %+v", gotSt)
	}
}

// Deleting a job must not leave its state or run history behind: the panel's
// activity feed reads runs directly and would keep showing a task that is gone.
func TestJobDeleteRemovesStateAndRuns(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	j := domain.Job{ID: "j-del", Name: "Doomed", ProviderID: "p1", IPSourceID: "s1",
		Targets:         []domain.Target{{Domain: "gone.example.com", Type: domain.RecordA, TTL: 300}},
		IntervalMinutes: 5, Enabled: true}
	if err := db.Jobs().Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().UTC()
	if _, err := db.Runs().Insert(ctx, domain.Run{JobID: j.ID, StartedAt: now, FinishedAt: now,
		Status: domain.StatusSuccess, Trigger: domain.TriggerManual}); err != nil {
		t.Fatalf("Insert run: %v", err)
	}
	if err := db.States().Upsert(ctx, domain.JobState{JobID: j.ID, LastStatus: domain.StatusSuccess, LastRunAt: now}); err != nil {
		t.Fatalf("Upsert state: %v", err)
	}

	if err := db.Jobs().Delete(ctx, j.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := db.Jobs().Get(ctx, j.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("job still present: %v", err)
	}
	states, err := db.States().List(ctx)
	if err != nil {
		t.Fatalf("List states: %v", err)
	}
	for _, st := range states {
		if st.JobID == j.ID {
			t.Fatalf("state still present: %+v", st)
		}
	}
	runs, err := db.Runs().List(ctx, storage.RunFilter{JobID: j.ID})
	if err != nil {
		t.Fatalf("List runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs still present: %+v", runs)
	}
}

func TestRunsFilterAndCount(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	now := time.Now().UTC()
	insert := func(job string, st domain.RunStatus, ago time.Duration) {
		_, err := db.Runs().Insert(ctx, domain.Run{JobID: job, StartedAt: now.Add(-ago), FinishedAt: now.Add(-ago),
			Status: st, Trigger: domain.TriggerSchedule})
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	insert("j1", domain.StatusSuccess, 1*time.Minute)
	insert("j1", domain.StatusNoChange, 2*time.Minute)
	insert("j2", domain.StatusFailed, 3*time.Minute)
	insert("j3", domain.StatusFailed, 4*time.Minute)

	failed, _ := db.Runs().List(ctx, storage.RunFilter{Status: domain.StatusFailed})
	if len(failed) != 2 {
		t.Fatalf("failed runs = %d", len(failed))
	}
	j1, _ := db.Runs().List(ctx, storage.RunFilter{JobID: "j1"})
	if len(j1) != 2 || j1[0].StartedAt.Before(j1[1].StartedAt) {
		t.Fatalf("j1 runs not newest-first: %+v", j1)
	}
	limited, _ := db.Runs().List(ctx, storage.RunFilter{Limit: 1})
	if len(limited) != 1 {
		t.Fatalf("limit ignored: %d", len(limited))
	}
	n, _ := db.Runs().CountByStatus(ctx, domain.StatusFailed)
	if n != 2 {
		t.Fatalf("count failed = %d", n)
	}
	// A manually inserted run must be retrievable by its generated id.
	if _, err := db.Runs().Get(ctx, 1); err != nil {
		t.Fatalf("Get run 1: %v", err)
	}
}

// Run details survive a round-trip: the panel relies on them to show which
// records changed and which did not.
func TestRunDetailsRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	now := time.Now().UTC()
	details := []domain.RunDetail{
		{Domain: "a.example.com", Type: domain.RecordA, Action: domain.RunActionUpdated, Current: "203.0.113.9", Desired: "203.0.113.42", Proxied: true},
		{Domain: "b.example.com", Type: domain.RecordA, Action: domain.RunActionUnchanged, Current: "203.0.113.42", Desired: "203.0.113.42"},
		{Domain: "c.example.com", Type: domain.RecordAAAA, Action: domain.RunActionSkipped, Note: "no v6 address"},
	}
	id, err := db.Runs().Insert(ctx, domain.Run{JobID: "j1", StartedAt: now, FinishedAt: now,
		Status: domain.StatusSuccess, Trigger: domain.TriggerSchedule, Details: details})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := db.Runs().Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Details) != len(details) {
		t.Fatalf("details = %+v", got.Details)
	}
	if got.Details[0] != details[0] || got.Details[1].Action != domain.RunActionUnchanged || got.Details[2].Note != "no v6 address" {
		t.Fatalf("details = %+v", got.Details)
	}
}

// SRC/RUN retention: history older than the cutoff is really gone, newer rows
// survive, and the count reports what was removed.
func TestRunsDeleteBefore(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	now := time.Now().UTC()
	insert := func(job string, ago time.Duration) {
		t.Helper()
		at := now.Add(-ago)
		if _, err := db.Runs().Insert(ctx, domain.Run{JobID: job, StartedAt: at, FinishedAt: at,
			Status: domain.StatusNoChange, Trigger: domain.TriggerSchedule}); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	insert("j1", 2*time.Hour)     // inside a 1-day window
	insert("j1", 25*time.Hour)    // outside it
	insert("j1", 10*24*time.Hour) // well outside

	n, err := db.Runs().DeleteBefore(ctx, now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted %d rows, want 2", n)
	}
	left, _ := db.Runs().List(ctx, storage.RunFilter{})
	if len(left) != 1 {
		t.Fatalf("rows left = %d, want 1", len(left))
	}
	if age := now.Sub(left[0].StartedAt); age < time.Hour || age > 3*time.Hour {
		t.Fatalf("wrong row survived (%s old): %+v", age, left[0])
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	def, err := db.Settings().Get(ctx)
	if err != nil {
		t.Fatalf("Get default: %v", err)
	}
	if def.DefaultInterval != 5 || def.Timezone != "" {
		t.Fatalf("default settings = %+v", def)
	}
	def.Timezone = "Asia/Shanghai"
	def.Notifications = []domain.Notification{{ID: "n1", Kind: "slack", Name: "alerts", Target: "https://x", Enabled: true, On: []string{"failure"}}}
	if err := db.Settings().Save(ctx, def); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := db.Settings().Get(ctx)
	if got.Timezone != "Asia/Shanghai" || len(got.Notifications) != 1 || got.Notifications[0].Kind != "slack" {
		t.Fatalf("settings = %+v", got)
	}
}

func TestIPSourceCRUD(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	s := domain.IPSource{ID: ids.New("src"), Name: "ipify", Type: domain.IPHTTP, Family: "v4",
		Target: "https://api.ipify.org", Enabled: true}
	if err := db.IPSources().Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _ := db.IPSources().Get(ctx, s.ID)
	if got.Type != domain.IPHTTP || got.Family != "v4" {
		t.Fatalf("got %+v", got)
	}
	if err := db.IPSources().Delete(ctx, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := db.IPSources().Get(ctx, s.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

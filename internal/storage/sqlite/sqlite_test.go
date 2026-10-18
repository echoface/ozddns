package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
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
		NextRunAt: time.Now().UTC().Add(5 * time.Minute), CurrentIP: "203.0.113.9", DesiredIP: "203.0.113.42",
		SourceFingerprint: "prov=p1;addr=v4=203.0.113.9"}
	if err := db.States().Upsert(ctx, st); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	st.LastStatus = domain.StatusNoChange
	st.CurrentIP = "203.0.113.42"
	st.SourceFingerprint = "prov=p1;addr=v4=203.0.113.42"
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
	if gotSt.SourceFingerprint != "prov=p1;addr=v4=203.0.113.42" {
		t.Fatalf("fingerprint round-trip = %q", gotSt.SourceFingerprint)
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

func TestInsertWithStateCommitsRunAndSchedulingStateTogether(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	now := time.Now().UTC()
	state := domain.JobState{
		JobID: "j-atomic", LastStatus: domain.StatusSuccess, LastRunAt: now,
		NextRunAt: now.Add(5 * time.Minute), CurrentIP: "203.0.113.9",
	}
	id, err := db.Runs().InsertWithState(ctx, domain.Run{
		JobID: "j-atomic", StartedAt: now, FinishedAt: now,
		Status: domain.StatusSuccess, Trigger: domain.TriggerSchedule,
	}, state)
	if err != nil {
		t.Fatalf("InsertWithState: %v", err)
	}
	run, err := db.Runs().Get(ctx, id)
	if err != nil || run.JobID != state.JobID {
		t.Fatalf("run = %+v err=%v", run, err)
	}
	got, err := db.States().Get(ctx, state.JobID)
	if err != nil || got.NextRunAt.UnixMilli() != state.NextRunAt.UnixMilli() || got.CurrentIP != state.CurrentIP {
		t.Fatalf("state = %+v err=%v", got, err)
	}
}

func TestConfigImportRollsBackOnMidWriteFailure(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	old := domain.Provider{ID: "old", Name: "old", Type: "custom", Config: map[string]any{"url": "http://old"}, Enabled: true}
	if err := db.Providers().Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	// The duplicate primary key fails after the replace step has deleted the old
	// row. The transaction must restore that row rather than leave a partial state.
	err := db.Config().Import(ctx, []domain.Provider{
		{ID: "new", Name: "new", Type: "custom", Config: map[string]any{"url": "http://new"}, Enabled: true},
		{ID: "new", Name: "duplicate", Type: "custom", Config: map[string]any{"url": "http://dup"}, Enabled: true},
	}, nil, nil, nil, true)
	if err == nil {
		t.Fatal("duplicate provider id should fail")
	}
	providers, err := db.Providers().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].ID != "old" {
		t.Fatalf("failed replace import mutated configuration: %+v", providers)
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

func TestAuthStatePersistsAndCascades(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	user := domain.AuthUser{
		Username: "admin", PasswordHash: "pbkdf2-sha256$1$c2FsdA$aGFzaA",
		TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", TOTPLastStep: 100,
		CreatedAt: time.Now().UTC(),
	}
	codes := []domain.RecoveryCode{
		{ID: "rc1", CodeHash: "hash1"},
		{ID: "rc2", CodeHash: "hash2"},
	}
	if err := db.Auth().CompleteSetup(ctx, user, codes); err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}
	got, err := db.Auth().User(ctx)
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if got.Username != "admin" || got.TOTPLastStep != 100 || got.PasswordHash != user.PasswordHash {
		t.Fatalf("user = %+v", got)
	}

	ok, err := db.Auth().UseTOTPStep(ctx, 101)
	if err != nil || !ok {
		t.Fatalf("UseTOTPStep(101) = %v, %v", ok, err)
	}
	ok, err = db.Auth().UseTOTPStep(ctx, 101)
	if err != nil || ok {
		t.Fatalf("replayed TOTP step = %v, %v", ok, err)
	}

	if err := db.Auth().InsertSession(ctx, domain.AuthSession{
		TokenHash: "session", CreatedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(),
		IdleExpiresAt: time.Now().Add(time.Hour), AbsoluteExpiresAt: time.Now().Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Auth().ClearAuth(ctx); err != nil {
		t.Fatalf("ClearAuth: %v", err)
	}
	if _, err := db.Auth().User(ctx); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("user after clear: %v", err)
	}
	if count, _ := db.Auth().SessionCount(ctx); count != 0 {
		t.Fatalf("sessions after clear = %d", count)
	}
	if codes, _ := db.Auth().ListRecoveryCodes(ctx); len(codes) != 0 {
		t.Fatalf("recovery codes after clear = %d", len(codes))
	}
}

func TestAuthEventRetentionKeepsNewestRows(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	if _, err := db.db.ExecContext(ctx,
		`WITH RECURSIVE seq(n) AS (
			SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n < ?
		)
		INSERT INTO auth_events (at,event,username,detail,ip,user_agent)
		SELECT ?, 'login_failed', 'admin', printf('attempt-%d', n), '127.0.0.1', 'test'
		FROM seq`,
		authEventRetention+1, time.Now().UTC().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := db.Auth().AddEvent(ctx, domain.AuthEvent{
		At: time.Now().UTC(), Event: "login_succeeded", Username: "admin", IP: "127.0.0.1",
	}); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != authEventRetention {
		t.Fatalf("retained events = %d, want %d", count, authEventRetention)
	}
	events, err := db.Auth().ListEvents(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Event != "login_succeeded" {
		t.Fatalf("newest event = %+v", events)
	}
}

func TestLegacyPasswordColumnIsRemoved(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	rows, err := db.db.QueryContext(ctx, `PRAGMA table_info(settings)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, typ        string
			defaultValue     sql.NullString
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "password_hash" {
			t.Fatal("legacy settings.password_hash column still exists")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	settings, err := db.Settings().Get(ctx)
	if err != nil {
		t.Fatalf("Get settings: %v", err)
	}
	if err := db.Settings().Save(ctx, settings); err != nil {
		t.Fatalf("Save settings: %v", err)
	}
	var raw string
	if err := db.db.QueryRowContext(ctx, `SELECT json FROM settings WHERE id=1`).Scan(&raw); err != nil {
		t.Fatalf("read settings JSON: %v", err)
	}
	if strings.Contains(raw, "passwordHash") || strings.Contains(raw, "passwordSet") {
		t.Fatalf("authentication material leaked into settings JSON: %s", raw)
	}
}

func TestLegacySettingsPasswordColumnMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `CREATE TABLE settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		json TEXT NOT NULL,
		password_hash TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `CREATE TABLE ip_sources (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		family TEXT NOT NULL,
		target TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx,
		`INSERT INTO settings (id,json,password_hash) VALUES (1,?,?)`,
		`{"language":"zh-CN","theme":"dark"}`, "legacy-hash"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy database: %v", err)
	}
	defer db.Close()
	rows, err := db.db.QueryContext(ctx, `PRAGMA table_info(settings)`)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, typ        string
			defaultValue     sql.NullString
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if name == "password_hash" {
			found = true
		}
	}
	rows.Close()
	if found {
		t.Fatal("legacy settings.password_hash column survived migration")
	}
	settings, err := db.Settings().Get(ctx)
	if err != nil {
		t.Fatalf("Get after migration: %v", err)
	}
	if settings.Language != "zh-CN" || settings.Theme != "dark" {
		t.Fatalf("legacy settings were not preserved: %+v", settings)
	}
	if err := db.Settings().Save(ctx, settings); err != nil {
		t.Fatalf("Save after dropping legacy column: %v", err)
	}

	rows, err = db.db.QueryContext(ctx, `PRAGMA table_info(ip_sources)`)
	if err != nil {
		t.Fatal(err)
	}
	ipSourceEnabled := false
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, typ        string
			defaultValue     sql.NullString
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if name == "enabled" {
			ipSourceEnabled = true
		}
	}
	rows.Close()
	if ipSourceEnabled {
		t.Fatal("legacy ip_sources.enabled column survived migration")
	}
	if err := db.IPSources().Create(ctx, domain.IPSource{
		ID: "src-after-migration", Name: "static", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9",
	}); err != nil {
		t.Fatalf("Create IP source after migration: %v", err)
	}
}

func TestIPSourceCRUD(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	s := domain.IPSource{ID: ids.New("src"), Name: "ipify", Type: domain.IPHTTP, Family: "v4",
		Target: "https://api.ipify.org"}
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

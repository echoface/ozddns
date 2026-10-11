// Package sqlite is the default storage backend: a single file, pure-Go driver,
// embedded migrations. It implements storage.Store.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ids"
	"github.com/echoface/ozddns/internal/storage"
)

// DB wraps a *sql.DB and implements storage.Store.
type DB struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database and runs migrations.
func Open(path string) (*DB, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: serialize writes for predictable behaviour
	if err := db.Ping(); err != nil {
		return nil, err
	}
	d := &DB{db: db}
	if err := d.migrate(context.Background()); err != nil {
		return nil, err
	}
	return d, nil
}

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }

// Providers implements storage.Store.
func (d *DB) Providers() storage.ProviderRepo { return providerRepo{d} }

// IPSources implements storage.Store.
func (d *DB) IPSources() storage.IPSourceRepo { return ipSourceRepo{d} }

// Jobs implements storage.Store.
func (d *DB) Jobs() storage.JobRepo { return jobRepo{d} }

// Runs implements storage.Store.
func (d *DB) Runs() storage.RunRepo { return runRepo{d} }

// States implements storage.Store.
func (d *DB) States() storage.StateRepo { return stateRepo{d} }

// Settings implements storage.Store.
func (d *DB) Settings() storage.SettingsRepo { return settingsRepo{d} }

// ---------------------------------------------------------------------------
// migrations
// ---------------------------------------------------------------------------

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS provider_instances (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		config_json TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS ip_sources (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		family TEXT NOT NULL,
		target TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS sync_jobs (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		provider_instance_id TEXT NOT NULL,
		ip_source_id TEXT NOT NULL,
		targets_json TEXT NOT NULL,
		interval_seconds INTEGER NOT NULL DEFAULT 300,
		run_on_change_only INTEGER NOT NULL DEFAULT 1,
		jitter INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS sync_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		job_id TEXT NOT NULL,
		started_at INTEGER NOT NULL,
		finished_at INTEGER NOT NULL,
		duration_ms INTEGER NOT NULL,
		status TEXT NOT NULL,
		old_ip TEXT,
		new_ip TEXT,
		trigger TEXT NOT NULL,
		message TEXT,
		records INTEGER NOT NULL DEFAULT 0
	);`,
	`CREATE INDEX IF NOT EXISTS idx_runs_job_started ON sync_runs(job_id, started_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_runs_status ON sync_runs(status);`,
	`CREATE TABLE IF NOT EXISTS job_state (
		job_id TEXT PRIMARY KEY,
		last_status TEXT,
		last_run_at INTEGER,
		next_run_at INTEGER,
		current_ip TEXT,
		desired_ip TEXT,
		last_error TEXT,
		last_changed_at INTEGER
	);`,
	`CREATE TABLE IF NOT EXISTS settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		json TEXT NOT NULL
	);`,
}

func (d *DB) migrate(ctx context.Context) error {
	for i, m := range migrations {
		if _, err := d.db.ExecContext(ctx, m); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	// Columns added after the first release. ensureColumn is a no-op when the
	// column already exists, so this stays idempotent across restarts.
	if err := d.ensureColumn(ctx, "sync_runs", "details_json", "TEXT"); err != nil {
		return fmt.Errorf("migration: sync_runs.details_json: %w", err)
	}
	return nil
}

// ensureColumn adds a column if the table does not already have it. SQLite has
// no "ADD COLUMN IF NOT EXISTS", so we inspect the schema first.
func (d *DB) ensureColumn(ctx context.Context, table, column, typ string) error {
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, colType    string
			defaultValue     sql.NullString
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, typ))
	return err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newID(prefix string) string {
	return ids.New(prefix)
}

func toMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func jsonEncode(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------------------------------------------------------------------------
// providers
// ---------------------------------------------------------------------------

type providerRepo struct{ d *DB }

func (r providerRepo) List(ctx context.Context) ([]domain.Provider, error) {
	rows, err := r.d.db.QueryContext(ctx, `SELECT id,name,type,config_json,enabled,created_at,updated_at FROM provider_instances ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r providerRepo) Get(ctx context.Context, id string) (domain.Provider, error) {
	row := r.d.db.QueryRowContext(ctx, `SELECT id,name,type,config_json,enabled,created_at,updated_at FROM provider_instances WHERE id=?`, id)
	p, err := scanProvider(row)
	if err == sql.ErrNoRows {
		return domain.Provider{}, storage.ErrNotFound
	}
	return p, err
}

func (r providerRepo) Create(ctx context.Context, p domain.Provider) error {
	if p.ID == "" {
		p.ID = newID("p")
	}
	now := time.Now().UTC()
	_, err := r.d.db.ExecContext(ctx,
		`INSERT INTO provider_instances (id,name,type,config_json,enabled,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Type, jsonEncode(p.Config), boolInt(p.Enabled), toMillis(now), toMillis(now))
	return err
}

func (r providerRepo) Update(ctx context.Context, p domain.Provider) error {
	_, err := r.d.db.ExecContext(ctx,
		`UPDATE provider_instances SET name=?,type=?,config_json=?,enabled=?,updated_at=? WHERE id=?`,
		p.Name, p.Type, jsonEncode(p.Config), boolInt(p.Enabled), toMillis(time.Now().UTC()), p.ID)
	return err
}

func (r providerRepo) Delete(ctx context.Context, id string) error {
	_, err := r.d.db.ExecContext(ctx, `DELETE FROM provider_instances WHERE id=?`, id)
	return err
}

type scanner interface{ Scan(...any) error }

func scanProvider(s scanner) (domain.Provider, error) {
	var p domain.Provider
	var cfg string
	var enabled int
	var created, updated int64
	if err := s.Scan(&p.ID, &p.Name, &p.Type, &cfg, &enabled, &created, &updated); err != nil {
		return domain.Provider{}, err
	}
	p.Config = map[string]any{}
	_ = json.Unmarshal([]byte(cfg), &p.Config)
	p.Enabled = enabled == 1
	p.CreatedAt = fromMillis(created)
	p.UpdatedAt = fromMillis(updated)
	return p, nil
}

// ---------------------------------------------------------------------------
// ip sources
// ---------------------------------------------------------------------------

type ipSourceRepo struct{ d *DB }

func (r ipSourceRepo) List(ctx context.Context) ([]domain.IPSource, error) {
	rows, err := r.d.db.QueryContext(ctx, `SELECT id,name,type,family,target,enabled,created_at,updated_at FROM ip_sources ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.IPSource
	for rows.Next() {
		s, err := scanIPSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r ipSourceRepo) Get(ctx context.Context, id string) (domain.IPSource, error) {
	row := r.d.db.QueryRowContext(ctx, `SELECT id,name,type,family,target,enabled,created_at,updated_at FROM ip_sources WHERE id=?`, id)
	s, err := scanIPSource(row)
	if err == sql.ErrNoRows {
		return domain.IPSource{}, storage.ErrNotFound
	}
	return s, err
}

func (r ipSourceRepo) Create(ctx context.Context, s domain.IPSource) error {
	if s.ID == "" {
		s.ID = newID("src")
	}
	now := time.Now().UTC()
	_, err := r.d.db.ExecContext(ctx,
		`INSERT INTO ip_sources (id,name,type,family,target,enabled,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		s.ID, s.Name, s.Type, s.Family, s.Target, boolInt(s.Enabled), toMillis(now), toMillis(now))
	return err
}

func (r ipSourceRepo) Update(ctx context.Context, s domain.IPSource) error {
	_, err := r.d.db.ExecContext(ctx,
		`UPDATE ip_sources SET name=?,type=?,family=?,target=?,enabled=?,updated_at=? WHERE id=?`,
		s.Name, s.Type, s.Family, s.Target, boolInt(s.Enabled), toMillis(time.Now().UTC()), s.ID)
	return err
}

func (r ipSourceRepo) Delete(ctx context.Context, id string) error {
	_, err := r.d.db.ExecContext(ctx, `DELETE FROM ip_sources WHERE id=?`, id)
	return err
}

func scanIPSource(s scanner) (domain.IPSource, error) {
	var src domain.IPSource
	var enabled int
	var created, updated int64
	if err := s.Scan(&src.ID, &src.Name, &src.Type, &src.Family, &src.Target, &enabled, &created, &updated); err != nil {
		return domain.IPSource{}, err
	}
	src.Enabled = enabled == 1
	src.CreatedAt = fromMillis(created)
	src.UpdatedAt = fromMillis(updated)
	return src, nil
}

// ---------------------------------------------------------------------------
// jobs
// ---------------------------------------------------------------------------

type jobRepo struct{ d *DB }

func (r jobRepo) List(ctx context.Context) ([]domain.Job, error) {
	rows, err := r.d.db.QueryContext(ctx, `SELECT id,name,provider_instance_id,ip_source_id,targets_json,interval_seconds,run_on_change_only,jitter,enabled,created_at,updated_at FROM sync_jobs ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r jobRepo) Get(ctx context.Context, id string) (domain.Job, error) {
	row := r.d.db.QueryRowContext(ctx, `SELECT id,name,provider_instance_id,ip_source_id,targets_json,interval_seconds,run_on_change_only,jitter,enabled,created_at,updated_at FROM sync_jobs WHERE id=?`, id)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return domain.Job{}, storage.ErrNotFound
	}
	return j, err
}

func (r jobRepo) Create(ctx context.Context, j domain.Job) error {
	if j.ID == "" {
		j.ID = newID("j")
	}
	if j.IntervalMinutes <= 0 {
		j.IntervalMinutes = 5
	}
	now := time.Now().UTC()
	_, err := r.d.db.ExecContext(ctx,
		`INSERT INTO sync_jobs (id,name,provider_instance_id,ip_source_id,targets_json,interval_seconds,run_on_change_only,jitter,enabled,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.Name, j.ProviderID, j.IPSourceID, jsonEncode(j.Targets), j.IntervalMinutes*60,
		boolInt(j.RunOnChangeOnly), boolInt(j.Jitter), boolInt(j.Enabled), toMillis(now), toMillis(now))
	return err
}

func (r jobRepo) Update(ctx context.Context, j domain.Job) error {
	_, err := r.d.db.ExecContext(ctx,
		`UPDATE sync_jobs SET name=?,provider_instance_id=?,ip_source_id=?,targets_json=?,interval_seconds=?,run_on_change_only=?,jitter=?,enabled=?,updated_at=? WHERE id=?`,
		j.Name, j.ProviderID, j.IPSourceID, jsonEncode(j.Targets), j.IntervalMinutes*60,
		boolInt(j.RunOnChangeOnly), boolInt(j.Jitter), boolInt(j.Enabled), toMillis(time.Now().UTC()), j.ID)
	return err
}

func (r jobRepo) Delete(ctx context.Context, id string) error {
	// Jobs own their state and run history, so remove them together — otherwise
	// the dashboard keeps showing activity for a task that no longer exists.
	tx, err := r.d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM sync_runs WHERE job_id=?`,
		`DELETE FROM job_state WHERE job_id=?`,
		`DELETE FROM sync_jobs WHERE id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanJob(s scanner) (domain.Job, error) {
	var j domain.Job
	var targets string
	var intervalSeconds, onchange, jitter, enabled int
	var created, updated int64
	if err := s.Scan(&j.ID, &j.Name, &j.ProviderID, &j.IPSourceID, &targets, &intervalSeconds, &onchange, &jitter, &enabled, &created, &updated); err != nil {
		return domain.Job{}, err
	}
	_ = json.Unmarshal([]byte(targets), &j.Targets)
	j.IntervalMinutes = intervalSeconds / 60
	if j.IntervalMinutes <= 0 {
		j.IntervalMinutes = 5
	}
	j.RunOnChangeOnly = onchange == 1
	j.Jitter = jitter == 1
	j.Enabled = enabled == 1
	j.CreatedAt = fromMillis(created)
	j.UpdatedAt = fromMillis(updated)
	return j, nil
}

// ---------------------------------------------------------------------------
// runs
// ---------------------------------------------------------------------------

type runRepo struct{ d *DB }

func (r runRepo) Insert(ctx context.Context, run domain.Run) (int64, error) {
	details, err := marshalRunDetails(run.Details)
	if err != nil {
		return 0, err
	}
	res, err := r.d.db.ExecContext(ctx,
		`INSERT INTO sync_runs (job_id,started_at,finished_at,duration_ms,status,old_ip,new_ip,trigger,message,records,details_json)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		run.JobID, toMillis(run.StartedAt), toMillis(run.FinishedAt), run.DurationMS,
		string(run.Status), run.OldIP, run.NewIP, string(run.Trigger), run.Message, run.Records, details)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r runRepo) Get(ctx context.Context, id int64) (domain.Run, error) {
	row := r.d.db.QueryRowContext(ctx, `SELECT id,job_id,started_at,finished_at,duration_ms,status,old_ip,new_ip,trigger,message,records,details_json FROM sync_runs WHERE id=?`, id)
	run, err := scanRun(row)
	if err == sql.ErrNoRows {
		return domain.Run{}, storage.ErrNotFound
	}
	return run, err
}

func (r runRepo) List(ctx context.Context, f storage.RunFilter) ([]domain.Run, error) {
	q := `SELECT id,job_id,started_at,finished_at,duration_ms,status,old_ip,new_ip,trigger,message,records,details_json FROM sync_runs`
	var where []string
	var args []any
	if f.JobID != "" {
		where = append(where, "job_id=?")
		args = append(args, f.JobID)
	}
	if f.Status != "" {
		where = append(where, "status=?")
		args = append(args, string(f.Status))
	}
	if len(where) > 0 {
		q += " WHERE " + joinAnd(where)
	}
	q += " ORDER BY started_at DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := r.d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r runRepo) CountByStatus(ctx context.Context, status domain.RunStatus) (int, error) {
	var n int
	err := r.d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_runs WHERE status=?`, string(status)).Scan(&n)
	return n, err
}

// DeleteBefore removes the history that fell out of the retention window. The
// delete is real (no soft-delete flag) so the file cannot grow forever.
func (r runRepo) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.d.db.ExecContext(ctx, `DELETE FROM sync_runs WHERE started_at < ?`, toMillis(cutoff))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanRun(s scanner) (domain.Run, error) {
	var run domain.Run
	var started, finished int64
	var status, trigger string
	var oldIP, newIP, message, details sql.NullString
	if err := s.Scan(&run.ID, &run.JobID, &started, &finished, &run.DurationMS, &status, &oldIP, &newIP, &trigger, &message, &run.Records, &details); err != nil {
		return domain.Run{}, err
	}
	run.StartedAt = fromMillis(started)
	run.FinishedAt = fromMillis(finished)
	run.Status = domain.RunStatus(status)
	run.Trigger = domain.Trigger(trigger)
	run.OldIP = oldIP.String
	run.NewIP = newIP.String
	run.Message = message.String
	if details.Valid && details.String != "" {
		if err := json.Unmarshal([]byte(details.String), &run.Details); err != nil {
			return domain.Run{}, err
		}
	}
	return run, nil
}

// marshalRunDetails encodes the per-record outcome as a JSON blob, or NULL when
// there is nothing to store.
func marshalRunDetails(details []domain.RunDetail) (any, error) {
	if len(details) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(details)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// ---------------------------------------------------------------------------
// job state
// ---------------------------------------------------------------------------

type stateRepo struct{ d *DB }

func (r stateRepo) Get(ctx context.Context, jobID string) (domain.JobState, error) {
	row := r.d.db.QueryRowContext(ctx, `SELECT job_id,last_status,last_run_at,next_run_at,current_ip,desired_ip,last_error,last_changed_at FROM job_state WHERE job_id=?`, jobID)
	var st domain.JobState
	var lastRun, nextRun, lastChanged int64
	var status, cur, des, errMsg sql.NullString
	if err := row.Scan(&st.JobID, &status, &lastRun, &nextRun, &cur, &des, &errMsg, &lastChanged); err != nil {
		if err == sql.ErrNoRows {
			return domain.JobState{JobID: jobID}, nil
		}
		return domain.JobState{}, err
	}
	st.LastStatus = domain.RunStatus(status.String)
	st.LastRunAt = fromMillis(lastRun)
	st.NextRunAt = fromMillis(nextRun)
	st.CurrentIP = cur.String
	st.DesiredIP = des.String
	st.LastError = errMsg.String
	st.LastChangedAt = fromMillis(lastChanged)
	return st, nil
}

func (r stateRepo) Upsert(ctx context.Context, s domain.JobState) error {
	_, err := r.d.db.ExecContext(ctx,
		`INSERT INTO job_state (job_id,last_status,last_run_at,next_run_at,current_ip,desired_ip,last_error,last_changed_at)
		 VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT(job_id) DO UPDATE SET last_status=excluded.last_status,last_run_at=excluded.last_run_at,
		   next_run_at=excluded.next_run_at,current_ip=excluded.current_ip,desired_ip=excluded.desired_ip,
		   last_error=excluded.last_error,last_changed_at=excluded.last_changed_at`,
		s.JobID, string(s.LastStatus), toMillis(s.LastRunAt), toMillis(s.NextRunAt),
		s.CurrentIP, s.DesiredIP, s.LastError, toMillis(s.LastChangedAt))
	return err
}

func (r stateRepo) List(ctx context.Context) ([]domain.JobState, error) {
	rows, err := r.d.db.QueryContext(ctx, `SELECT job_id,last_status,last_run_at,next_run_at,current_ip,desired_ip,last_error,last_changed_at FROM job_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.JobState
	for rows.Next() {
		var st domain.JobState
		var lastRun, nextRun, lastChanged int64
		var status, cur, des, errMsg sql.NullString
		if err := rows.Scan(&st.JobID, &status, &lastRun, &nextRun, &cur, &des, &errMsg, &lastChanged); err != nil {
			return nil, err
		}
		st.LastStatus = domain.RunStatus(status.String)
		st.LastRunAt = fromMillis(lastRun)
		st.NextRunAt = fromMillis(nextRun)
		st.CurrentIP = cur.String
		st.DesiredIP = des.String
		st.LastError = errMsg.String
		st.LastChangedAt = fromMillis(lastChanged)
		out = append(out, st)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// settings
// ---------------------------------------------------------------------------

type settingsRepo struct{ d *DB }

func (r settingsRepo) Get(ctx context.Context) (domain.Settings, error) {
	var raw string
	err := r.d.db.QueryRowContext(ctx, `SELECT json FROM settings WHERE id=1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return defaultSettings(), nil
	}
	if err != nil {
		return domain.Settings{}, err
	}
	var s domain.Settings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return domain.Settings{}, err
	}
	applySettingsDefaults(&s, raw)
	return s, nil
}

func (r settingsRepo) Save(ctx context.Context, s domain.Settings) error {
	_, err := r.d.db.ExecContext(ctx,
		`INSERT INTO settings (id,json) VALUES (1,?) ON CONFLICT(id) DO UPDATE SET json=excluded.json`,
		jsonEncode(s))
	return err
}

func defaultSettings() domain.Settings {
	return domain.Settings{
		// An empty timezone means "follow the browser's zone" in the panel; the
		// server never needs a zone of its own, so this is the friendlier default
		// than a hard-coded UTC.
		Language: "en", Theme: "light", Timezone: "", DefaultInterval: 5,
		SchedulerTickSeconds: 5, SchedulerWorkers: 4, RunOnStart: true,
	}
}

// applySettingsDefaults fills in values that were never persisted, so a settings
// row written by an older build keeps working without a migration.
func applySettingsDefaults(s *domain.Settings, raw string) {
	var probe map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &probe)
	if _, ok := probe["schedulerTickSeconds"]; !ok || s.SchedulerTickSeconds <= 0 {
		s.SchedulerTickSeconds = 5
	}
	if _, ok := probe["schedulerWorkers"]; !ok || s.SchedulerWorkers <= 0 {
		s.SchedulerWorkers = 4
	}
	if _, ok := probe["runOnStart"]; !ok {
		s.RunOnStart = true
	}
	if s.DefaultInterval <= 0 {
		s.DefaultInterval = 5
	}
	if s.Language == "" {
		s.Language = "en"
	}
	if s.Theme == "" {
		s.Theme = "light"
	}
	// "UTC" was the implicit default before the panel started following the
	// browser's zone, and it is what older saves persisted without the user ever
	// choosing it. Treat it as "not set" so those installs show local time too.
	if s.Timezone == "UTC" {
		s.Timezone = ""
	}
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

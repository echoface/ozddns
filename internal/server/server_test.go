package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/echoface/ozddns/internal/app"
	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/providers"
	"github.com/echoface/ozddns/internal/storage"
	"github.com/echoface/ozddns/internal/storage/sqlite"
)

func newTestServer(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "srv.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := app.New(store, providers.NewRegistry(), ipsource.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	a.DBPath = "test.db"
	srv := httptest.NewServer(New(a, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(func() { srv.Close(); _ = store.Close() })
	return srv, a
}

func do(t *testing.T, srv *httptest.Server, method, path string, body any, want int) map[string]any {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d (want %d): %s", method, path, resp.StatusCode, want, data)
	}
	if len(data) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s %s: bad JSON: %s", method, path, data)
	}
	return out
}

// The bootstrap payload carries everything the panel needs.
func TestBootstrapPayload(t *testing.T) {
	srv, a := newTestServer(t)
	a.ListenAddr = "0.0.0.0:9999"
	b := do(t, srv, "GET", "/api/v1/bootstrap", nil, 200)
	if _, ok := b["providers"]; !ok {
		t.Fatalf("bootstrap missing providers: %v", b)
	}
	types, _ := b["providerTypes"].([]any)
	if len(types) != 9 {
		t.Fatalf("expected 9 provider types, got %d", len(types))
	}
	status, _ := b["status"].(map[string]any)
	if status["listen"] != "0.0.0.0:9999" {
		t.Fatalf("status listen = %v", status["listen"])
	}
}

// PRV-01..03 as an API flow: create a provider, list it, validate it.
func TestProviderLifecycle(t *testing.T) {
	srv, _ := newTestServer(t)

	created := do(t, srv, "POST", "/api/v1/providers", map[string]any{
		"name": "Hook", "type": "custom",
		"config": map[string]any{"url": "http://127.0.0.1:9/hook", "method": "POST"},
	}, 201)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("no id in %v", created)
	}
	if created["type"] != "custom" {
		t.Fatalf("type = %v", created["type"])
	}

	// The list endpoint returns an array; check it directly.
	resp, err := http.Get(srv.URL + "/api/v1/providers")
	if err != nil {
		t.Fatal(err)
	}
	listBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Contains(listBody, []byte(id)) {
		t.Fatalf("provider list %d: %s", resp.StatusCode, listBody)
	}
	do(t, srv, "GET", "/api/v1/providers/"+id, nil, 200)
	do(t, srv, "GET", "/api/v1/providers/does-not-exist", nil, 404)
	do(t, srv, "DELETE", "/api/v1/providers/"+id, nil, 200)
}

// PRV-10: secret config is masked in API responses.
func TestProviderSecretsMasked(t *testing.T) {
	srv, _ := newTestServer(t)
	created := do(t, srv, "POST", "/api/v1/providers", map[string]any{
		"name": "CF", "type": "cloudflare", "config": map[string]any{"api_token": "supersecrettoken1234"},
	}, 201)
	cfg, _ := created["config"].(map[string]any)
	tok, _ := cfg["api_token"].(string)
	if tok == "supersecrettoken1234" || tok == "" {
		t.Fatalf("token not masked: %q", tok)
	}
}

// REC-12 + SCH-05: a full job flow — dry-run then run.
func TestJobFlowRunAndDryRun(t *testing.T) {
	srv, _ := newTestServer(t)
	p := do(t, srv, "POST", "/api/v1/providers", map[string]any{
		"name": "Hook", "type": "custom", "config": map[string]any{"url": "http://127.0.0.1:9/hook"},
	}, 201)
	s := do(t, srv, "POST", "/api/v1/ip-sources", map[string]any{
		"name": "Fixed", "type": "static", "family": "v4", "target": "203.0.113.9",
	}, 201)
	j := do(t, srv, "POST", "/api/v1/jobs", map[string]any{
		"name": "Home", "providerId": p["id"], "ipSourceId": s["id"],
		"targets":         []map[string]any{{"domain": "home.example.com", "type": "A", "ttl": 300}},
		"intervalMinutes": 5,
	}, 201)
	jobID, _ := j["id"].(string)

	diff := do(t, srv, "POST", "/api/v1/jobs/"+jobID+"/dry-run", nil, 200)
	targets, _ := diff["targets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("dry-run targets = %v", diff)
	}
	first, _ := targets[0].(map[string]any)
	if first["action"] != "create" || first["desired"] != "203.0.113.9" {
		t.Fatalf("dry-run target = %v", first)
	}

	run := do(t, srv, "POST", "/api/v1/jobs/"+jobID+"/run", nil, 200)
	if run["status"] != "failed" { // the webhook at :9 is not listening
		t.Fatalf("expected failed run, got %v", run["status"])
	}
	if run["jobId"] != jobID || run["trigger"] != "manual" {
		t.Fatalf("run = %v", run)
	}
}

// SRC-16: a stored source can be edited, but its address family is locked while
// tasks reference it — those tasks derive their A / AAAA records from it.
func TestIPSourceUpdateAndFamilyLock(t *testing.T) {
	srv, _ := newTestServer(t)
	p := do(t, srv, "POST", "/api/v1/providers", map[string]any{
		"name": "Hook", "type": "custom", "config": map[string]any{"url": "http://127.0.0.1:9/hook"},
	}, 201)
	s := do(t, srv, "POST", "/api/v1/ip-sources", map[string]any{
		"name": "Fixed", "type": "static", "family": "v4", "target": "203.0.113.9",
	}, 201)
	srcID, _ := s["id"].(string)

	// Renaming and re-pointing a source is always allowed.
	up := do(t, srv, "PATCH", "/api/v1/ip-sources/"+srcID, map[string]any{
		"name": "Fixed v2", "type": "static", "family": "v4", "target": "203.0.113.10",
	}, 200)
	if up["name"] != "Fixed v2" || up["target"] != "203.0.113.10" {
		t.Fatalf("update not applied: %v", up)
	}

	j := do(t, srv, "POST", "/api/v1/jobs", map[string]any{
		"name": "Home", "providerId": p["id"], "ipSourceId": srcID,
		"targets": []map[string]any{{"domain": "home.example.com", "type": "A", "ttl": 300}},
	}, 201)

	// Flipping the family would contradict that A record, so it is refused.
	do(t, srv, "PATCH", "/api/v1/ip-sources/"+srcID, map[string]any{
		"name": "Fixed v2", "type": "static", "family": "v6", "target": "2001:db8::1",
	}, 400)

	// Removing the last reference unlocks it.
	do(t, srv, "DELETE", "/api/v1/jobs/"+j["id"].(string), nil, 200)
	do(t, srv, "PATCH", "/api/v1/ip-sources/"+srcID, map[string]any{
		"name": "Fixed v2", "type": "static", "family": "v6", "target": "2001:db8::1",
	}, 200)
}

// RUN-10: the retention window is a setting (default 1 day), the purge really
// deletes, and out-of-range windows are rejected.
func TestRunRetention(t *testing.T) {
	srv, a := newTestServer(t)
	st, err := a.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got := app.RetentionDays(st); got != 1 {
		t.Fatalf("default window = %d days, want 1", got)
	}

	// Old history, injected straight into the store.
	old := time.Now().UTC().Add(-72 * time.Hour)
	if _, err := a.Store.Runs().Insert(t.Context(), domain.Run{JobID: "j-any", StartedAt: old,
		FinishedAt: old, Status: domain.StatusNoChange, Trigger: domain.TriggerSchedule}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := a.Store.Runs().Insert(t.Context(), domain.Run{JobID: "j-any", StartedAt: time.Now().UTC(),
		FinishedAt: time.Now().UTC(), Status: domain.StatusNoChange, Trigger: domain.TriggerSchedule}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	purge := do(t, srv, "POST", "/api/v1/runs/purge", nil, 200)
	if purge["deleted"].(float64) != 1 || purge["keptDays"].(float64) != 1 {
		t.Fatalf("purge = %v", purge)
	}
	left, err := a.Store.Runs().List(t.Context(), storage.RunFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("runs after purge = %d, want 1", len(left))
	}

	// A wider window keeps the rest, and nonsense is refused.
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{"runRetentionDays": 30}, 200)
	purge = do(t, srv, "POST", "/api/v1/runs/purge", nil, 200)
	if purge["deleted"].(float64) != 0 || purge["keptDays"].(float64) != 30 {
		t.Fatalf("wide purge = %v", purge)
	}
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{"runRetentionDays": 0}, 400)
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{"runRetentionDays": 5000}, 400)
}

// SET-01/02: partial settings patches merge, and passwords gate the API.
func TestSettingsPatchAndAuth(t *testing.T) {
	srv, a := newTestServer(t)
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{"timezone": "Asia/Shanghai", "defaultInterval": 7}, 200)
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{"timezone": "Not/A_Timezone"}, 400)
	got := do(t, srv, "GET", "/api/v1/settings", nil, 200)
	if got["timezone"] != "Asia/Shanghai" {
		t.Fatalf("timezone = %v", got["timezone"])
	}
	if _, present := got["listen"]; present {
		// The bind address is deployment config, not a stored setting.
		t.Fatalf("settings should not expose listen: %v", got["listen"])
	}

	// Enabling a password must start rejecting anonymous requests.
	if err := a.SetPassword(t.Context(), "admin", "hunter2"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	resp, err := http.Get(srv.URL + "/api/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/status", nil)
	req.SetBasicAuth("admin", "hunter2")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("authenticated status = %d, want 200", resp2.StatusCode)
	}
}

func TestConfigExportAndReplaceImport(t *testing.T) {
	srv, a := newTestServer(t)
	p := do(t, srv, "POST", "/api/v1/providers", map[string]any{
		"name": "Hook", "type": "custom",
		"config": map[string]any{"url": "https://example.com/hook", "token": "secret"},
	}, 201)
	s := do(t, srv, "POST", "/api/v1/ip-sources", map[string]any{
		"name": "Fixed", "type": "static", "family": "v4", "target": "203.0.113.9",
	}, 201)
	do(t, srv, "POST", "/api/v1/jobs", map[string]any{
		"name": "Home", "providerId": p["id"], "ipSourceId": s["id"],
		"targets": []map[string]any{{"domain": "home.example.com", "type": "A", "ttl": 300}},
	}, 201)
	if err := a.SetPassword(t.Context(), "admin", "hunter2"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/config/export", nil)
	req.SetBasicAuth("admin", "hunter2")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var bundle map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	settings := bundle["settings"].(map[string]any)
	if _, exists := settings["passwordHash"]; exists || settings["passwordSet"] != false {
		t.Fatalf("export contains authentication material: %v", settings)
	}

	body, _ := json.Marshal(bundle)
	importReq, _ := http.NewRequest("POST", srv.URL+"/api/v1/config/import", bytes.NewReader(body))
	importReq.Header.Set("Content-Type", "application/json")
	importReq.SetBasicAuth("admin", "hunter2")
	importResp, err := http.DefaultClient.Do(importReq)
	if err != nil {
		t.Fatal(err)
	}
	defer importResp.Body.Close()
	if importResp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(importResp.Body)
		t.Fatalf("import status %d: %s", importResp.StatusCode, data)
	}
	var stats map[string]any
	if err := json.NewDecoder(importResp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats["providers"].(float64) != 1 || stats["ipSources"].(float64) != 1 || stats["jobs"].(float64) != 1 {
		t.Fatalf("import stats = %v", stats)
	}

	if err := a.SetPassword(t.Context(), "admin", ""); err != nil {
		t.Fatal(err)
	}
	settings["timezone"] = "Not/A_Timezone"
	do(t, srv, "POST", "/api/v1/config/import", bundle, 400)
	providers, err := a.Store.Providers().List(t.Context())
	if err != nil || len(providers) != 1 {
		t.Fatalf("invalid import changed existing config: providers=%d err=%v", len(providers), err)
	}
}

// CFG: the scheduler tuning round-trips through /settings and "Run all" hits
// the aggregate endpoint (POST /jobs/run-all).
func TestSchedulerConfigAndRunAll(t *testing.T) {
	srv, _ := newTestServer(t)

	// Scheduler tuning is merged and persisted (SET-01a).
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{
		"schedulerTickSeconds": 30, "schedulerWorkers": 2, "runOnStart": false,
	}, 200)
	got := do(t, srv, "GET", "/api/v1/settings", nil, 200)
	if got["schedulerTickSeconds"].(float64) != 30 || got["schedulerWorkers"].(float64) != 2 {
		t.Fatalf("scheduler settings not merged: %v", got)
	}
	if got["runOnStart"] != false {
		t.Fatalf("runOnStart not applied: %v", got["runOnStart"])
	}

	// Bootstrap exposes the live tuning.
	boot := do(t, srv, "GET", "/api/v1/bootstrap", nil, 200)
	sched, _ := boot["scheduler"].(map[string]any)
	if sched["tickSeconds"].(float64) != 30 || sched["workers"].(float64) != 2 {
		t.Fatalf("bootstrap scheduler = %v", sched)
	}

	// Pausing the scheduler via the quick endpoint persists the flag.
	do(t, srv, "POST", "/api/v1/scheduler", map[string]any{"running": false}, 200)
	after := do(t, srv, "GET", "/api/v1/settings", nil, 200)
	if after["schedulerPaused"] != true {
		t.Fatalf("pause not persisted: %v", after["schedulerPaused"])
	}
	do(t, srv, "POST", "/api/v1/scheduler", map[string]any{"running": true}, 200)

	// Run-all reconciles every enabled task and reports a summary.
	p := do(t, srv, "POST", "/api/v1/providers", map[string]any{
		"name": "Hook", "type": "custom", "config": map[string]any{"url": "http://127.0.0.1:9/hook"},
	}, 201)
	s := do(t, srv, "POST", "/api/v1/ip-sources", map[string]any{
		"name": "Fixed", "type": "static", "family": "v4", "target": "203.0.113.9",
	}, 201)
	do(t, srv, "POST", "/api/v1/jobs", map[string]any{
		"name": "Home", "providerId": p["id"], "ipSourceId": s["id"],
		"targets":         []map[string]any{{"domain": "home.example.com", "type": "A", "ttl": 300}},
		"intervalMinutes": 5, "enabled": true,
	}, 201)
	sum := do(t, srv, "POST", "/api/v1/jobs/run-all", nil, 200)
	if sum["total"].(float64) != 1 || len(sum["results"].([]any)) != 1 {
		t.Fatalf("run-all summary = %v", sum)
	}
}

// Unknown routes and unknown ids behave predictably.
func TestErrors(t *testing.T) {
	srv, _ := newTestServer(t)
	do(t, srv, "GET", "/api/v1/jobs/nope", nil, 404)
	do(t, srv, "POST", "/api/v1/providers", map[string]any{"name": "x", "type": "nope"}, 400)
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/echoface/ozddns/internal/app"
	authpkg "github.com/echoface/ozddns/internal/auth"
	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/providers"
	"github.com/echoface/ozddns/internal/storage"
	"github.com/echoface/ozddns/internal/storage/sqlite"
)

func newTestServer(t *testing.T) (*httptest.Server, *app.App) {
	return newTestServerWithConfig(t, nil)
}

func newTestServerWithConfig(t *testing.T, configure func(*Server)) (*httptest.Server, *app.App) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "srv.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := app.New(store, providers.NewRegistry(), ipsource.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	a.DBPath = "test.db"
	handler := New(a, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if configure != nil {
		configure(handler)
	}
	srv := httptest.NewServer(handler.Handler())
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

func TestRequestBodyLimitAndSecurityHeaders(t *testing.T) {
	srv, _ := newTestServer(t)

	oversized := `{"username":"admin","password":"` + strings.Repeat("x", maxAuthRequestBody+1) + `"}`
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(oversized))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("oversized auth request = %d %s", resp.StatusCode, body)
	}

	// A chunked request has no Content-Length, so the decoder must still map the
	// MaxBytesReader error to 413 instead of reporting malformed JSON as 400.
	chunked, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/login", struct{ io.Reader }{strings.NewReader(oversized)})
	if err != nil {
		t.Fatal(err)
	}
	chunked.Header.Set("Content-Type", "application/json")
	chunkedResp, err := http.DefaultClient.Do(chunked)
	if err != nil {
		t.Fatal(err)
	}
	defer chunkedResp.Body.Close()
	if chunkedResp.StatusCode != http.StatusRequestEntityTooLarge {
		body, _ := io.ReadAll(chunkedResp.Body)
		t.Fatalf("oversized chunked auth request = %d %s", chunkedResp.StatusCode, body)
	}

	state, err := http.Get(srv.URL + "/api/v1/auth/state")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Body.Close()
	if got := state.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := state.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := state.Header.Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
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

func TestIPEndpointResolvesSelectedSources(t *testing.T) {
	srv, a := newTestServer(t)
	ctx := context.Background()
	if _, err := a.CreateIPSource(ctx, app.IPSourceInput{
		Name: "broken default", Type: domain.IPInterface, Family: "v6", Target: "ozddns-no-such-interface",
	}); err != nil {
		t.Fatal(err)
	}
	source, err := a.CreateIPSource(ctx, app.IPSourceInput{
		Name: "selected v6", Type: domain.IPStatic, Family: "v6", Target: "2001:db8::40",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := do(t, srv, "GET", "/api/v1/ip?v6SourceId="+source.ID, nil, 200)
	v6, ok := body["v6"].(map[string]any)
	if !ok {
		t.Fatalf("v6 payload = %#v", body["v6"])
	}
	if v6["status"] != app.IPStatusOK || v6["address"] != "2001:db8::40" ||
		v6["sourceId"] != source.ID {
		t.Fatalf("v6 payload = %#v", v6)
	}
	v4, _ := body["v4"].(map[string]any)
	if v4["status"] != app.IPStatusNoSource {
		t.Fatalf("v4 payload = %#v", v4)
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
	// The custom provider is write-only, so there is no record to diff against:
	// the plan says the address will be pushed, not that it will be created.
	if first["action"] != "apply" || first["desired"] != "203.0.113.9" {
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

// SET-01/02: partial settings patches merge, and authentication material is
// not accepted through the generic settings endpoint.
func TestSettingsPatchAndAuthFieldsRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{"timezone": "Asia/Shanghai", "defaultInterval": 7}, 200)
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{"timezone": "Not/A_Timezone"}, 400)
	do(t, srv, "PATCH", "/api/v1/settings", map[string]any{"password": "should-not-be-accepted"}, 400)
	got := do(t, srv, "GET", "/api/v1/settings", nil, 200)
	if got["timezone"] != "Asia/Shanghai" {
		t.Fatalf("timezone = %v", got["timezone"])
	}
	if _, present := got["listen"]; present {
		// The bind address is deployment config, not a stored setting.
		t.Fatalf("settings should not expose listen: %v", got["listen"])
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
	bundle := do(t, srv, "GET", "/api/v1/config/export", nil, 200)
	settings := bundle["settings"].(map[string]any)
	if _, exists := settings["passwordHash"]; exists {
		t.Fatalf("export contains authentication material: %v", settings)
	}
	if _, exists := settings["passwordSet"]; exists {
		t.Fatalf("export contains authentication material: %v", settings)
	}

	body, _ := json.Marshal(bundle)
	importReq, _ := http.NewRequest("POST", srv.URL+"/api/v1/config/import", bytes.NewReader(body))
	importReq.Header.Set("Content-Type", "application/json")
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

// A manual "run now" must be recorded even when the client disconnects before
// the run finishes (a reload or navigating away right after clicking Run). The
// handler detaches the run from the request context, so the aborted POST still
// leaves a run row behind.
func TestManualRunRecordedAfterClientAbort(t *testing.T) {
	srv, a := newTestServer(t)

	// A webhook that stalls long enough for us to cancel the request first.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	p := do(t, srv, "POST", "/api/v1/providers", map[string]any{
		"name": "Slow", "type": "custom", "config": map[string]any{"url": slow.URL},
	}, 201)
	s := do(t, srv, "POST", "/api/v1/ip-sources", map[string]any{
		"name": "Fixed", "type": "static", "family": "v4", "target": "203.0.113.9",
	}, 201)
	j := do(t, srv, "POST", "/api/v1/jobs", map[string]any{
		"name": "Home", "providerId": p["id"], "ipSourceId": s["id"],
		"targets": []map[string]any{{"domain": "home.example.com", "type": "A", "ttl": 300}},
	}, 201)
	jobID, _ := j["id"].(string)

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/api/v1/jobs/"+jobID+"/run", nil)
	time.AfterFunc(50*time.Millisecond, cancel)
	// The request is expected to fail; we only care that the run is persisted.
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := a.Store.Runs().List(context.Background(), storage.RunFilter{JobID: jobID})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(runs) > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("manual run was not recorded after the client aborted")
}

// A CNAME target is rejected over the API with 400, not accepted and then
// written with an IP address.
func TestCreateJobRejectsCNAMETarget(t *testing.T) {
	srv, _ := newTestServer(t)
	p := do(t, srv, "POST", "/api/v1/providers", map[string]any{
		"name": "Hook", "type": "custom", "config": map[string]any{"url": "http://127.0.0.1:9/hook"},
	}, 201)
	s := do(t, srv, "POST", "/api/v1/ip-sources", map[string]any{
		"name": "Fixed", "type": "static", "family": "v4", "target": "203.0.113.9",
	}, 201)
	do(t, srv, "POST", "/api/v1/jobs", map[string]any{
		"name": "Home", "providerId": p["id"], "ipSourceId": s["id"],
		"targets": []map[string]any{{"domain": "alias.example.com", "type": "CNAME"}},
	}, 400)
}

// newTestServerWithPanel is newTestServer with a stand-in panel so the
// middleware order around static assets can be exercised.
func newTestServerWithPanel(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "srv.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := app.New(store, providers.NewRegistry(), ipsource.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	panel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>OzDDNS panel</title>")
	})
	srv := httptest.NewServer(New(a, panel, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(func() { srv.Close(); _ = store.Close() })
	return srv, a
}

type authHTTPResponse struct {
	status int
	body   map[string]any
	header http.Header
}

func authRequest(t *testing.T, client *http.Client, srv *httptest.Server, method, path string, body any, headers map[string]string) authHTTPResponse {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rdr = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if len(data) > 0 {
		_ = json.Unmarshal(data, &parsed)
	}
	return authHTTPResponse{status: resp.StatusCode, body: parsed, header: resp.Header.Clone()}
}

func newCookieClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func enableAuthHTTP(t *testing.T, srv *httptest.Server) (secret string, recovery []string, client *http.Client) {
	t.Helper()
	client = newCookieClient(t)
	start := authRequest(t, client, srv, "POST", "/api/v1/auth/setup/start", map[string]any{
		"username": "admin", "password": "correct horse battery",
	}, nil)
	if start.status != http.StatusOK {
		t.Fatalf("setup start = %d %+v", start.status, start.body)
	}
	secret, _ = start.body["secret"].(string)
	code, err := authpkg.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	verify := authRequest(t, client, srv, "POST", "/api/v1/auth/setup/verify", map[string]any{"code": code}, nil)
	if verify.status != http.StatusOK {
		t.Fatalf("setup verify = %d %+v", verify.status, verify.body)
	}
	for _, item := range verify.body["recoveryCodes"].([]any) {
		recovery = append(recovery, item.(string))
	}
	return secret, recovery, client
}

func TestAuthOpenModeThenSessionGateAndPublicShell(t *testing.T) {
	srv, _ := newTestServerWithPanel(t)
	anonymous := newCookieClient(t)

	state := authRequest(t, anonymous, srv, "GET", "/api/v1/auth/state", nil, nil)
	if state.status != 200 || state.body["enabled"] != false || state.body["authenticated"] != false {
		t.Fatalf("open auth state = %d %+v", state.status, state.body)
	}
	if got := authRequest(t, anonymous, srv, "GET", "/api/v1/bootstrap", nil, nil); got.status != 200 {
		t.Fatalf("open bootstrap = %d", got.status)
	}

	_, _, session := enableAuthHTTP(t, srv)
	state = authRequest(t, anonymous, srv, "GET", "/api/v1/auth/state", nil, nil)
	if state.status != 200 || state.body["enabled"] != true || state.body["authenticated"] != false {
		t.Fatalf("anonymous state after enable = %+v", state.body)
	}
	if _, leaked := state.body["username"]; leaked {
		t.Fatalf("anonymous state leaked username: %+v", state.body)
	}
	if _, leaked := state.body["sessions"]; leaked {
		t.Fatalf("anonymous state leaked session count: %+v", state.body)
	}

	if got := authRequest(t, anonymous, srv, "GET", "/api/v1/bootstrap", nil, nil); got.status != http.StatusUnauthorized {
		t.Fatalf("anonymous bootstrap after enable = %d", got.status)
	} else if got.header.Get("WWW-Authenticate") != "" {
		t.Fatalf("unexpected Basic challenge: %q", got.header.Get("WWW-Authenticate"))
	}
	if got := authRequest(t, session, srv, "GET", "/api/v1/bootstrap", nil, nil); got.status != 200 {
		t.Fatalf("session bootstrap = %d", got.status)
	}
	if got := authRequest(t, anonymous, srv, "GET", "/", nil, nil); got.status != 200 {
		t.Fatalf("anonymous public panel = %d", got.status)
	}
	if got := authRequest(t, anonymous, srv, "GET", "/healthz", nil, nil); got.status != 200 {
		t.Fatalf("anonymous healthz = %d", got.status)
	}
}

func TestAuthLoginMFAReplayLogoutAndCSRF(t *testing.T) {
	srv, _ := newTestServer(t)
	secret, recovery, authenticated := enableAuthHTTP(t, srv)
	client := newCookieClient(t)

	badPassword := authRequest(t, client, srv, "POST", "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "wrong password",
	}, nil)
	if badPassword.status != http.StatusUnauthorized || badPassword.body["error"] != "invalid username or password" {
		t.Fatalf("bad password response = %d %+v", badPassword.status, badPassword.body)
	}
	login := authRequest(t, client, srv, "POST", "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "correct horse battery",
	}, nil)
	if login.status != 200 || login.body["challenge"] == "" {
		t.Fatalf("login = %d %+v", login.status, login.body)
	}
	challenge := login.body["challenge"].(string)
	badCode := authRequest(t, client, srv, "POST", "/api/v1/auth/mfa", map[string]any{
		"challenge": challenge, "code": "000000",
	}, nil)
	if badCode.status != http.StatusUnauthorized {
		t.Fatalf("bad MFA = %d %+v", badCode.status, badCode.body)
	}

	code, err := authpkg.TOTPCode(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	mfa := authRequest(t, client, srv, "POST", "/api/v1/auth/mfa", map[string]any{
		"challenge": challenge, "code": code,
	}, nil)
	if mfa.status != 200 {
		t.Fatalf("MFA = %d %+v", mfa.status, mfa.body)
	}
	setCookie := mfa.header.Get("Set-Cookie")
	for _, required := range []string{"ozddns_session=", "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(setCookie, required) {
			t.Fatalf("session cookie %q missing %q", setCookie, required)
		}
	}
	if got := authRequest(t, client, srv, "GET", "/api/v1/bootstrap", nil, nil); got.status != 200 {
		t.Fatalf("bootstrap with session = %d", got.status)
	}

	crossSite := authRequest(t, client, srv, "POST", "/api/v1/scheduler", map[string]any{"running": false}, map[string]string{
		"Origin": "https://evil.example",
	})
	if crossSite.status != http.StatusForbidden {
		t.Fatalf("cross-origin POST = %d %+v", crossSite.status, crossSite.body)
	}
	sameSite := authRequest(t, client, srv, "POST", "/api/v1/scheduler", map[string]any{"running": true}, map[string]string{
		"Origin": srv.URL,
	})
	if sameSite.status != 200 {
		t.Fatalf("same-origin POST = %d %+v", sameSite.status, sameSite.body)
	}

	replayChallenge := authRequest(t, client, srv, "POST", "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "correct horse battery",
	}, nil).body["challenge"].(string)
	replay := authRequest(t, client, srv, "POST", "/api/v1/auth/mfa", map[string]any{
		"challenge": replayChallenge, "code": code,
	}, nil)
	if replay.status != http.StatusUnauthorized {
		t.Fatalf("TOTP replay = %d %+v", replay.status, replay.body)
	}

	recoveryChallenge := authRequest(t, client, srv, "POST", "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "correct horse battery",
	}, nil).body["challenge"].(string)
	recoveryLogin := authRequest(t, client, srv, "POST", "/api/v1/auth/mfa", map[string]any{
		"challenge": recoveryChallenge, "code": recovery[0],
	}, nil)
	if recoveryLogin.status != 200 {
		t.Fatalf("recovery login = %d %+v", recoveryLogin.status, recoveryLogin.body)
	}

	if got := authRequest(t, authenticated, srv, "POST", "/api/v1/auth/logout", nil, nil); got.status != 200 {
		t.Fatalf("logout = %d", got.status)
	}
	if got := authRequest(t, authenticated, srv, "GET", "/api/v1/bootstrap", nil, nil); got.status != http.StatusUnauthorized {
		t.Fatalf("bootstrap after logout = %d", got.status)
	}
}

func TestLogoutDoesNotReportSuccessWhenRevocationFails(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "logout-failure.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := app.New(store, providers.NewRegistry(), ipsource.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	s := New(a, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "still-valid-token"})
	rec := httptest.NewRecorder()
	s.handleAuthLogout(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("logout status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("failed revocation cleared the retry cookie: %q", got)
	}
}

func TestAuthDisableAndReenableNewSecret(t *testing.T) {
	srv, _ := newTestServer(t)
	firstSecret, recovery, client := enableAuthHTTP(t, srv)

	disable := authRequest(t, client, srv, "POST", "/api/v1/auth/disable", map[string]any{
		"password": "correct horse battery", "code": recovery[0],
	}, nil)
	if disable.status != 200 {
		t.Fatalf("disable = %d %+v", disable.status, disable.body)
	}
	if got := authRequest(t, client, srv, "GET", "/api/v1/bootstrap", nil, nil); got.status != 200 {
		t.Fatalf("bootstrap after disable = %d", got.status)
	}

	secondSecret, _, _ := enableAuthHTTP(t, srv)
	if secondSecret == firstSecret {
		t.Fatal("re-enable reused the old TOTP secret")
	}
}

func TestAuthSetupValidationRateLimitAndSecureProxyCookie(t *testing.T) {
	srv, _ := newTestServer(t)
	client := newCookieClient(t)
	if got := authRequest(t, client, srv, "POST", "/api/v1/auth/setup/start", map[string]any{
		"username": "admin", "password": "short",
	}, nil); got.status != http.StatusBadRequest {
		t.Fatalf("short setup password = %d %+v", got.status, got.body)
	}
	for range 6 {
		authRequest(t, client, srv, "POST", "/api/v1/auth/setup/start", map[string]any{
			"username": "admin", "password": "correct horse battery",
		}, nil)
	}
	if got := authRequest(t, client, srv, "POST", "/api/v1/auth/setup/start", map[string]any{
		"username": "admin", "password": "correct horse battery",
	}, nil); got.status != http.StatusTooManyRequests {
		t.Fatalf("setup rate limit = %d %+v", got.status, got.body)
	}

	// A trusted proxy can make an HTTP backend issue a Secure cookie for its
	// HTTPS public URL.
	srv2, _ := newTestServerWithConfig(t, func(s *Server) {
		s.TrustProxy = true
		s.App.ListenAddr = "127.0.0.1:9876"
	})
	proxyClient := newCookieClient(t)
	start := authRequest(t, proxyClient, srv2, "POST", "/api/v1/auth/setup/start", map[string]any{
		"username": "admin", "password": "correct horse battery",
	}, nil)
	if start.status != 200 {
		t.Fatalf("proxy setup start = %d %+v", start.status, start.body)
	}
	code, err := authpkg.TOTPCode(start.body["secret"].(string), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	verify := authRequest(t, proxyClient, srv2, "POST", "/api/v1/auth/setup/verify", map[string]any{
		"code": code,
	}, map[string]string{"X-Forwarded-Proto": "https"})
	if verify.status != 200 || !strings.Contains(verify.header.Get("Set-Cookie"), "Secure") {
		t.Fatalf("secure proxy cookie = %d %q", verify.status, verify.header.Get("Set-Cookie"))
	}
}

func TestAuthLoginRateLimitCannotBeBypassedWithRotatedUsernames(t *testing.T) {
	srv, _ := newTestServer(t)
	enableAuthHTTP(t, srv)
	client := newCookieClient(t)

	for i := range 8 {
		got := authRequest(t, client, srv, "POST", "/api/v1/auth/login", map[string]any{
			"username": fmt.Sprintf("attacker-%d", i),
			"password": "definitely wrong",
		}, nil)
		if got.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %+v", i+1, got.status, got.body)
		}
	}
	got := authRequest(t, client, srv, "POST", "/api/v1/auth/login", map[string]any{
		"username": "another-name",
		"password": "definitely wrong",
	}, nil)
	if got.status != http.StatusTooManyRequests || got.header.Get("Retry-After") == "" {
		t.Fatalf("rotated-username rate limit = %d %+v headers=%v", got.status, got.body, got.header)
	}
}

func TestAuthSetupIsRejectedFromNonLoopbackClient(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "remote-setup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := app.New(store, providers.NewRegistry(), ipsource.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := New(a, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()

	body := strings.NewReader(`{"username":"admin","password":"correct horse battery"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup/start", body)
	req.RemoteAddr = "198.51.100.10:4321"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("remote setup = %d %s", rec.Code, rec.Body.String())
	}
	if enabled, err := a.AuthEnabled(context.Background()); err != nil || enabled {
		t.Fatalf("remote setup changed auth state: enabled=%v err=%v", enabled, err)
	}
}

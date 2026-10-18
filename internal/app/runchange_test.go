package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

// openApp builds a fresh SQLite-backed application for a test.
func openApp(t *testing.T) *App {
	t.Helper()
	a, closer, err := Open(context.Background(), Options{DBPath: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(closer)
	return a
}

// End-to-end: run-on-change-only skips the provider while nothing changed, and
// resumes the moment the address does.
func TestRunOnChangeOnlySkipsUnchangedProvider(t *testing.T) {
	var hits int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	ctx := context.Background()
	a := openApp(t)
	enabled := true
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": hook.URL + "/hook", "method": "POST"}, Enabled: &enabled})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	s, err := a.CreateIPSource(ctx, IPSourceInput{Name: "fixed", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	j, err := a.CreateJob(ctx, JobInput{Name: "home", ProviderID: p.ID, IPSourceID: s.ID,
		Targets: []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}}, IntervalMinutes: 5})
	if err != nil {
		t.Fatalf("job: %v", err)
	}

	run1, err := a.RunNow(ctx, j.ID)
	if err != nil || run1.Status != domain.StatusSuccess {
		t.Fatalf("first run = %+v err=%v", run1, err)
	}
	// A scheduled run with nothing changed is skipped.
	run2, err := a.RunJob(ctx, j.ID, domain.TriggerSchedule)
	if err != nil {
		t.Fatalf("scheduled run: %v", err)
	}
	if run2.Status != domain.StatusNoChange {
		t.Fatalf("scheduled run status = %s, want nochange", run2.Status)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("provider contacted %d times, want 1 (scheduled run should skip)", got)
	}

	// Re-pointing the source changes the fingerprint, so the next run writes.
	if _, err := a.UpdateIPSource(ctx, s.ID, IPSourceInput{Target: "203.0.113.10"}); err != nil {
		t.Fatalf("update source: %v", err)
	}
	run3, err := a.RunJob(ctx, j.ID, domain.TriggerSchedule)
	if err != nil || run3.Status != domain.StatusSuccess {
		t.Fatalf("third run = %+v err=%v", run3, err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("provider contacted %d times, want 2", got)
	}

	// A manual run always does the work, even with nothing changed.
	run4, err := a.RunNow(ctx, j.ID)
	if err != nil || run4.Status != domain.StatusSuccess {
		t.Fatalf("manual run = %+v err=%v", run4, err)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("manual run should contact the provider: got %d, want 3", got)
	}
}

// A replace import must not delete a task while that task is mid-run; otherwise
// the run can finish after the import and leave orphaned history/state behind.
func TestReplaceImportWaitsForRunningJob(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	ctx := context.Background()
	a := openApp(t)
	enabled := true
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": hook.URL + "/hook", "method": "POST"}, Enabled: &enabled})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	s, err := a.CreateIPSource(ctx, IPSourceInput{Name: "fixed", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	j, err := a.CreateJob(ctx, JobInput{Name: "home", ProviderID: p.ID, IPSourceID: s.ID,
		Targets: []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}}, IntervalMinutes: 5})
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	bundle, err := a.Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	bundle.Jobs = nil

	runDone := make(chan error, 1)
	go func() {
		_, err := a.RunNow(ctx, j.ID)
		runDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the provider")
	}

	importDone := make(chan error, 1)
	go func() {
		_, err := a.Import(ctx, bundle, true)
		importDone <- err
	}()
	select {
	case err := <-importDone:
		t.Fatalf("replace import completed while the job was running: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	if err := <-runDone; err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := <-importDone; err != nil {
		t.Fatalf("import: %v", err)
	}
	if runs, err := a.ListRuns(ctx, "", "", 10); err != nil || len(runs) != 0 {
		t.Fatalf("replace import left run history behind: runs=%+v err=%v", runs, err)
	}
	if jobs, err := a.ListJobs(ctx); err != nil || len(jobs) != 0 {
		t.Fatalf("replace import left jobs behind: jobs=%+v err=%v", jobs, err)
	}
}

// The skip decision: on-change-only skips an unchanged, in-sync task; a write-
// only provider skips even when run-on-change-only is off; anything else runs.
func TestShouldSkipRunMatrix(t *testing.T) {
	a := openApp(t)
	ok := domain.JobState{LastStatus: domain.StatusSuccess, SourceFingerprint: "fp"}
	readable := domain.Provider{ID: "p", Type: "cloudflare"}
	writeOnly := domain.Provider{ID: "p", Type: "custom"}

	cases := []struct {
		name  string
		job   domain.Job
		prov  domain.Provider
		state domain.JobState
		fp    string
		trig  domain.Trigger
		want  bool
	}{
		{"on-change, unchanged, ok", domain.Job{RunOnChangeOnly: true}, readable, ok, "fp", domain.TriggerSchedule, true},
		{"manual never skips", domain.Job{RunOnChangeOnly: true}, readable, ok, "fp", domain.TriggerManual, false},
		{"on-change, address changed", domain.Job{RunOnChangeOnly: true}, readable, ok, "other", domain.TriggerSchedule, false},
		{"on-change, last failed", domain.Job{RunOnChangeOnly: true}, readable, domain.JobState{LastStatus: domain.StatusFailed, SourceFingerprint: "fp"}, "fp", domain.TriggerSchedule, false},
		{"off-change, readable", domain.Job{RunOnChangeOnly: false}, readable, ok, "fp", domain.TriggerSchedule, false},
		{"off-change, write-only", domain.Job{RunOnChangeOnly: false}, writeOnly, ok, "fp", domain.TriggerSchedule, true},
		{"no resolved address", domain.Job{RunOnChangeOnly: true}, readable, ok, "", domain.TriggerSchedule, false},
		{"never run before", domain.Job{RunOnChangeOnly: true}, readable, domain.JobState{}, "fp", domain.TriggerSchedule, false},
	}
	for _, c := range cases {
		if got := a.shouldSkipRun(c.job, c.prov, c.state, c.fp, c.trig); got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// A CNAME target cannot be driven by an IP source and must be refused up front.
func TestCreateJobRejectsCNAME(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	enabled := true
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": "http://127.0.0.1:9/h"}, Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	s, err := a.CreateIPSource(ctx, IPSourceInput{Name: "f", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateJob(ctx, JobInput{Name: "home", ProviderID: p.ID, IPSourceID: s.ID,
		Targets: []domain.Target{{Domain: "alias.example.com", Type: domain.RecordCNAME}}}); err == nil {
		t.Fatal("CNAME target must be rejected")
	}
}

// A write-only provider (custom webhook, DuckDNS, Namecheap DDNS) has no read
// API, so a run must write straight through and report "applied" instead of
// pretending it diffed a record it cannot read.
func TestWriteOnlyProviderRunApplies(t *testing.T) {
	var hits int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	ctx := context.Background()
	a := openApp(t)
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": hook.URL + "/hook", "method": "POST"}})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	// The panel must be told the truth about what validation can and cannot do.
	if p.Zones != nil && len(p.Zones) > 0 {
		t.Fatalf("write-only provider should not claim zones: %v", p.Zones)
	}
	s, err := a.CreateIPSource(ctx, IPSourceInput{Name: "fixed", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := a.CreateJob(ctx, JobInput{Name: "home", ProviderID: p.ID, IPSourceID: s.ID,
		Targets: []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}}})
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	run, err := a.RunNow(ctx, j.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != domain.StatusSuccess || run.Records != 1 {
		t.Fatalf("run = %+v", run)
	}
	if len(run.Details) != 1 || run.Details[0].Action != domain.RunActionApplied {
		t.Fatalf("details = %+v", run.Details)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("webhook called %d times, want 1", got)
	}
}

// Disabling a provider takes it out of service: every task that writes through
// it is skipped (and the provider is never contacted), including manual runs.
func TestDisabledProviderIsSkipped(t *testing.T) {
	var hits int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	ctx := context.Background()
	a := openApp(t)
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": hook.URL + "/hook", "method": "POST"}})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	s, err := a.CreateIPSource(ctx, IPSourceInput{Name: "fixed", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := a.CreateJob(ctx, JobInput{Name: "home", ProviderID: p.ID, IPSourceID: s.ID,
		Targets: []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}}})
	if err != nil {
		t.Fatalf("job: %v", err)
	}

	off := false
	if _, err := a.UpdateProvider(ctx, p.ID, ProviderInput{Enabled: &off}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	run, err := a.RunNow(ctx, j.ID)
	if err != nil {
		t.Fatalf("manual run on a disabled provider: %v", err)
	}
	if run.Status != domain.StatusSkipped {
		t.Fatalf("status = %s, want skipped", run.Status)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("a disabled provider must not be contacted: hits=%d", got)
	}
	// The reason has to survive into job state, or the panel would show a
	// generic "no address available" note.
	if state, err := a.Store.States().Get(ctx, j.ID); err != nil || state.LastError != "provider is disabled; task skipped" {
		t.Fatalf("state = %+v err=%v", state, err)
	}

	on := true
	if _, err := a.UpdateProvider(ctx, p.ID, ProviderInput{Enabled: &on}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	run, err = a.RunNow(ctx, j.ID)
	if err != nil || run.Status != domain.StatusSuccess {
		t.Fatalf("run after re-enabling = %+v err=%v", run, err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("hits = %d, want 1", got)
	}
}

// Editing a provider's configuration (a new webhook URL, a rotated token) must
// force the next scheduled run to write again — the fingerprint covers the
// provider config, not just the address.
func TestProviderConfigChangeForcesWrite(t *testing.T) {
	var first, second int32
	hookA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&first, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hookA.Close()
	hookB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&second, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hookB.Close()

	ctx := context.Background()
	a := openApp(t)
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": hookA.URL + "/hook", "method": "POST"}})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	s, err := a.CreateIPSource(ctx, IPSourceInput{Name: "fixed", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := a.CreateJob(ctx, JobInput{Name: "home", ProviderID: p.ID, IPSourceID: s.ID,
		Targets: []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}}})
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	if run, err := a.RunJob(ctx, j.ID, domain.TriggerSchedule); err != nil || run.Status != domain.StatusSuccess {
		t.Fatalf("first run = %+v err=%v", run, err)
	}
	if run, err := a.RunJob(ctx, j.ID, domain.TriggerSchedule); err != nil || run.Status != domain.StatusNoChange {
		t.Fatalf("second run = %+v err=%v", run, err)
	}
	if got := atomic.LoadInt32(&first); got != 1 {
		t.Fatalf("first hook hits = %d, want 1 (second run should skip)", got)
	}

	if _, err := a.UpdateProvider(ctx, p.ID, ProviderInput{Config: map[string]any{"url": hookB.URL + "/hook", "method": "POST"}}); err != nil {
		t.Fatalf("repoint provider: %v", err)
	}
	if run, err := a.RunJob(ctx, j.ID, domain.TriggerSchedule); err != nil || run.Status != domain.StatusSuccess {
		t.Fatalf("run after repointing = %+v err=%v", run, err)
	}
	if got := atomic.LoadInt32(&second); got != 1 {
		t.Fatalf("the new endpoint must receive the address: hits=%d", got)
	}
}

// Shortening a task's interval takes effect at once instead of waiting out the
// schedule the old interval already booked.
func TestUpdateJobShortensNextRun(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	ctx := context.Background()
	a := openApp(t)
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": hook.URL + "/hook", "method": "POST"}})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	s, err := a.CreateIPSource(ctx, IPSourceInput{Name: "fixed", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := a.CreateJob(ctx, JobInput{Name: "home", ProviderID: p.ID, IPSourceID: s.ID,
		Targets:         []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}},
		IntervalMinutes: 120})
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	if _, err := a.RunNow(ctx, j.ID); err != nil {
		t.Fatalf("run: %v", err)
	}
	state, err := a.Store.States().Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(state.NextRunAt); d < 100*time.Minute {
		t.Fatalf("next run %s away, want ~120m", d)
	}

	if _, err := a.UpdateJob(ctx, j.ID, JobInput{IntervalMinutes: 5}); err != nil {
		t.Fatalf("update: %v", err)
	}
	state, err = a.Store.States().Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(state.NextRunAt); d > 6*time.Minute {
		t.Fatalf("next run %s away, want the shortened interval to apply now", d)
	}
}

func TestCreateJobRejectsTargetSourceFamilyMismatch(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": "http://127.0.0.1:9/hook"}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := a.CreateIPSource(ctx, IPSourceInput{Name: "v4", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateJob(ctx, JobInput{Name: "bad", ProviderID: p.ID, IPSourceID: source.ID,
		Targets: []domain.Target{{Domain: "v6.example.com", Type: domain.RecordAAAA}}}); err == nil {
		t.Fatal("a v4 source must not be allowed to drive an AAAA target")
	}
}

func TestConcurrentManualRunsOfSameJobAreSerialized(t *testing.T) {
	var (
		mu        sync.Mutex
		active    int
		maxActive int
		hits      int
	)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		active++
		hits++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	ctx := context.Background()
	a := openApp(t)
	p, err := a.CreateProvider(ctx, ProviderInput{Name: "hook", Type: "custom",
		Config: map[string]any{"url": hook.URL, "method": "POST"}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := a.CreateIPSource(ctx, IPSourceInput{Name: "v4", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := a.CreateJob(ctx, JobInput{Name: "home", ProviderID: p.ID, IPSourceID: source.ID,
		Targets: []domain.Target{{Domain: "home.example.com", Type: domain.RecordA}}})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run, err := a.RunNow(ctx, job.ID)
			if err != nil {
				errs <- err
				return
			}
			if run.Status != domain.StatusSuccess {
				errs <- fmt.Errorf("run status = %s", run.Status)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if maxActive != 1 || hits != 2 {
		t.Fatalf("same-job runs overlapped: maxActive=%d hits=%d", maxActive, hits)
	}
}

func TestStartPausedOverridesPersistedRunningState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := openApp(t)
	settings, err := a.Store.Settings().Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.SchedulerPaused = false
	settings.RunOnStart = true
	if err := a.Store.Settings().Save(ctx, settings); err != nil {
		t.Fatal(err)
	}
	a.Start(ctx, true)
	defer a.Scheduler.Stop()
	if a.Running() {
		t.Fatal("paused start must override the persisted scheduler state before the loop starts")
	}
}

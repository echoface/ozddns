package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/echoface/ozddns/internal/domain"
)

// CFG-01: scheduler tuning from the settings panel reaches the live scheduler
// and is persisted.
func TestPatchSettingsAppliesSchedulerConfig(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	tick, workers, paused, runOnStart := 30, 2, true, false
	if _, err := a.PatchSettings(ctx, SettingsInput{
		SchedulerTickSeconds: &tick, SchedulerWorkers: &workers,
		SchedulerPaused: &paused, RunOnStart: &runOnStart,
	}); err != nil {
		t.Fatalf("PatchSettings: %v", err)
	}

	cfg := a.SchedulerConfig(ctx)
	if cfg.TickSeconds != 30 || cfg.Workers != 2 || cfg.RunOnStart || cfg.Running {
		t.Fatalf("scheduler not reconfigured: %+v", cfg)
	}
	// Persisted for the next boot.
	s, _ := a.GetSettings(ctx)
	if s.SchedulerTickSeconds != 30 || s.SchedulerWorkers != 2 || !s.SchedulerPaused || s.RunOnStart {
		t.Fatalf("settings not persisted: %+v", s)
	}

	// Resuming clears the pause flag.
	resume := false
	if _, err := a.PatchSettings(ctx, SettingsInput{SchedulerPaused: &resume}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !a.Running() {
		t.Fatal("scheduler should be running after resume")
	}
}

// CFG-03: "Run all" reconciles every enabled job and reports a summary.
func TestRunAllEnabled(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := a.CreateProvider(ctx, ProviderInput{Name: "Hook", Type: "custom",
		Config: map[string]any{"url": srv.URL + "/{{ip}}"}})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	src, err := a.CreateIPSource(ctx, IPSourceInput{Name: "Fixed", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatalf("CreateIPSource: %v", err)
	}
	mk := func(name string, enabled bool) string {
		j, err := a.CreateJob(ctx, JobInput{Name: name, ProviderID: p.ID, IPSourceID: src.ID,
			Targets:         []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}},
			IntervalMinutes: 5, Enabled: &enabled})
		if err != nil {
			t.Fatalf("CreateJob %s: %v", name, err)
		}
		return j.ID
	}
	mk("on", true)
	mk("off", false)

	sum := a.RunAllEnabled(ctx)
	if sum.Total != 1 || sum.Success != 1 || sum.Failed != 0 || len(sum.Results) != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("webhook hits = %d, want 1 (disabled job must be skipped)", got)
	}
}

// Package app is the application layer: use-cases shared by the REST API and the
// CLI. It depends on the domain, provider/ipsource registries, the reconciler
// and the storage interfaces — never on HTTP or a specific database.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/engine"
	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/provider"
	"github.com/echoface/ozddns/internal/storage"
)

// App bundles the use-cases and their dependencies.
type App struct {
	Store      storage.Store
	Providers  *provider.Registry
	Sources    *ipsource.Registry
	Reconciler *engine.Reconciler
	Scheduler  *engine.Scheduler
	Log        *slog.Logger
	DBDriver   string
	DBPath     string

	healthMu sync.RWMutex
	health   map[string]providerHealth
	started  time.Time
}

type providerHealth struct {
	status      string // ok | warn | err
	statusText  string
	zones       []string
	validatedAt time.Time
}

// New wires an App from its dependencies.
func New(store storage.Store, reg *provider.Registry, srcs *ipsource.Registry, log *slog.Logger) *App {
	if log == nil {
		log = slog.Default()
	}
	a := &App{
		Store: store, Providers: reg, Sources: srcs, Log: log,
		Reconciler: engine.NewReconciler(reg, srcs),
		health:     map[string]providerHealth{},
		started:    time.Now(),
		DBDriver:   "sqlite",
	}
	a.Scheduler = engine.NewScheduler(store.Jobs(), store.States(), a, log)
	return a
}

// Start applies the persisted scheduler settings and begins background work.
func (a *App) Start(ctx context.Context) {
	a.ApplySchedulerSettings(ctx)
	a.Scheduler.Start(ctx)
	a.RunRetentionLoop(ctx)
}

// Running reports scheduler state.
func (a *App) Running() bool { return a.Scheduler.Running() }

// SetRunning pauses/resumes scheduling.
func (a *App) SetRunning(v bool) { a.Scheduler.SetRunning(v) }

// ApplySchedulerSettings pushes the persisted tuning into the live scheduler.
// It is called at start-up and after a settings update.
func (a *App) ApplySchedulerSettings(ctx context.Context) {
	s, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return
	}
	tick := time.Duration(s.SchedulerTickSeconds) * time.Second
	a.Scheduler.Configure(tick, s.SchedulerWorkers, s.RunOnStart)
	a.Scheduler.SetRunning(!s.SchedulerPaused)
}

// SchedulerConfig is the effective scheduler tuning, for status/bootstrap.
type SchedulerConfig struct {
	Running      bool `json:"running"`
	TickSeconds  int  `json:"tickSeconds"`
	Workers      int  `json:"workers"`
	RunOnStart   bool `json:"runOnStart"`
	DefaultEvery int  `json:"defaultIntervalMinutes"`
}

// SchedulerConfig returns the live scheduler configuration.
func (a *App) SchedulerConfig(ctx context.Context) SchedulerConfig {
	tick, workers := a.Scheduler.Config()
	settings, _ := a.Store.Settings().Get(ctx)
	secs := int(tick / time.Second)
	if secs <= 0 {
		secs = int(engine.DefaultTick / time.Second)
	}
	return SchedulerConfig{
		Running: a.Running(), TickSeconds: secs, Workers: workers,
		RunOnStart: settings.RunOnStart, DefaultEvery: settings.DefaultInterval,
	}
}

// Uptime returns process uptime.
func (a *App) Uptime() time.Duration { return time.Since(a.started) }

// RunJob executes a job now and records the run. It implements engine.JobRunner.
func (a *App) RunJob(ctx context.Context, jobID string, trigger domain.Trigger) (domain.Run, error) {
	job, err := a.Store.Jobs().Get(ctx, jobID)
	if err != nil {
		return domain.Run{}, err
	}
	prov, err := a.Store.Providers().Get(ctx, job.ProviderID)
	if err != nil {
		return domain.Run{}, fmt.Errorf("job %s: provider missing: %w", jobID, err)
	}
	src, err := a.Store.IPSources().Get(ctx, job.IPSourceID)
	if err != nil {
		return domain.Run{}, fmt.Errorf("job %s: ip source missing: %w", jobID, err)
	}

	started := time.Now().UTC()
	state, _ := a.Store.States().Get(ctx, jobID)
	result, recErr := a.Reconciler.Reconcile(ctx, job, prov, src)
	finished := time.Now().UTC()

	run := domain.Run{
		JobID: jobID, StartedAt: started, FinishedAt: finished,
		DurationMS: int(finished.Sub(started).Milliseconds()),
		Status:     result.Status, OldIP: result.OldIP, NewIP: result.NewIP,
		Trigger: trigger, Message: result.Message, Records: result.Records,
	}
	if recErr != nil {
		run.Status = domain.StatusFailed
		run.Message = recErr.Error()
	}
	id, err := a.Store.Runs().Insert(ctx, run)
	if err != nil {
		return run, err
	}
	run.ID = id

	// Update job runtime state (including the next scheduled run).
	ns := state
	ns.JobID = jobID
	ns.LastStatus = run.Status
	ns.LastRunAt = finished
	ns.NextRunAt = engine.NextRun(job, finished)
	ns.CurrentIP = firstNonEmpty(run.NewIP, run.OldIP, state.CurrentIP)
	ns.DesiredIP = run.NewIP
	if run.Status == domain.StatusFailed {
		ns.LastError = run.Message
	} else {
		ns.LastError = ""
	}
	if run.Status == domain.StatusSuccess {
		ns.LastChangedAt = finished
	}
	_ = a.Store.States().Upsert(ctx, ns)

	// Best-effort notifications.
	a.notify(ctx, run, job)

	if run.Status == domain.StatusFailed {
		return run, fmt.Errorf("%s", run.Message)
	}
	return run, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

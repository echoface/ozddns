// Package app is the application layer: use-cases shared by the REST API and the
// CLI. It depends on the domain, provider/ipsource registries, the reconciler
// and the storage interfaces — never on HTTP or a specific database.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
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
	// ListenAddr/ListenSource are the resolved bind address and where it came
	// from (flag | env | config | default). They are set by the CLI at start-up;
	// the bind address is deployment config and is never stored in the database.
	ListenAddr   string
	ListenSource string
	// BasePath/BasePathSource are the resolved public prefix the panel and API
	// are served under, and where it came from. Like the bind address it is
	// deployment configuration, set by the CLI at start-up, never stored in the
	// database or edited from the panel.
	BasePath       string
	BasePathSource string
	// TrustedNetwork asserts that the bind address is only reachable through
	// networks the operator controls (a LAN, or an overlay such as Tailscale).
	// Like the bind address it is deployment configuration, set by the CLI at
	// start-up from --trusted-network / OZDDNS_TRUSTED_NETWORK / config.yaml,
	// never stored in the database. It silences the "reachable beyond loopback"
	// exposure warning and panel banner but grants no access control itself.
	TrustedNetwork bool

	healthMu sync.RWMutex
	health   map[string]providerHealth
	started  time.Time

	// Public-address cache. Resolving it hits external "what is my IP"
	// endpoints, so bootstrap reads the cache and a background probe keeps it
	// warm instead of blocking the panel on a slow (often IPv6) probe.
	ipMu      sync.RWMutex
	ipCache   PublicIP
	ipFilled  time.Time
	ipFilling bool

	// configMu keeps configuration-dependent work on a coherent snapshot.
	// Imports take the write lock; reads, edits and runs take the read lock.
	// In particular, a replace import cannot delete a task while that task is
	// still talking to a provider and later write an orphaned run/state row.
	configMu sync.RWMutex

	// Runs of the same job are serialized across manual, "run all" and scheduled
	// triggers. A bounded stripe table avoids an ever-growing per-job mutex map;
	// hash collisions merely serialize two unrelated jobs occasionally.
	jobGates [128]chan struct{}
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
	for i := range a.jobGates {
		a.jobGates[i] = make(chan struct{}, 1)
	}
	a.Scheduler = engine.NewScheduler(store.Jobs(), store.States(), a, log)
	return a
}

// Start applies the persisted scheduler settings and begins background work.
// A paused start overrides the persisted pause flag before the scheduler loop
// begins, so --no-scheduler cannot race its run-on-start dispatch.
func (a *App) Start(ctx context.Context, paused bool) {
	a.ApplySchedulerSettings(ctx)
	if paused {
		a.Scheduler.SetRunning(false)
	}
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
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	release, err := a.acquireJobGate(ctx, jobID)
	if err != nil {
		return domain.Run{}, err
	}
	defer release()

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
	state, err := a.Store.States().Get(ctx, jobID)
	if err != nil {
		return domain.Run{}, err
	}
	run := domain.Run{JobID: jobID, StartedAt: started, Trigger: trigger}
	addrs := map[string]netip.Addr{}
	fingerprint := ""
	if !prov.Enabled {
		// A disabled provider is a destination the operator has taken out of
		// service. Never write through it — not even on a manual run — and say
		// so instead of reporting a puzzling failure.
		run.Status = domain.StatusSkipped
		run.Message = "provider is disabled; task skipped"
		run.OldIP = state.CurrentIP
	} else {
		// Resolve the source once: the same address drives both the
		// run-on-change decision and the reconciliation itself.
		var planErr error
		addrs, planErr = a.Reconciler.Plan(ctx, job, src)
		switch {
		case planErr != nil:
			run.Status = domain.StatusFailed
			run.Message = planErr.Error()
			run.OldIP = state.CurrentIP
		default:
			fingerprint = engine.Fingerprint(job, prov, src, addrs)
			if a.shouldSkipRun(job, prov, state, fingerprint, trigger) {
				run.Status = domain.StatusNoChange
				run.Message = "source address unchanged; provider not contacted"
				run.OldIP = state.CurrentIP
				run.NewIP = primaryAddr(addrs)
				break
			}
			result, recErr := a.Reconciler.ReconcileWith(ctx, job, prov, addrs)
			run.Status, run.OldIP, run.NewIP = result.Status, result.OldIP, result.NewIP
			run.Message, run.Records, run.Details = result.Message, result.Records, result.Details
			if recErr != nil {
				run.Status = domain.StatusFailed
				run.Message = recErr.Error()
			}
		}
	}

	finished := time.Now().UTC()
	run.FinishedAt = finished
	run.DurationMS = int(finished.Sub(started).Milliseconds())

	nextState := a.nextJobState(job, state, run, fingerprint, finished)
	id, err := a.Store.Runs().InsertWithState(ctx, run, nextState)
	if err != nil {
		return run, err
	}
	run.ID = id
	a.notify(ctx, run, job)

	if run.Status == domain.StatusFailed {
		return run, fmt.Errorf("%s", run.Message)
	}
	return run, nil
}

// shouldSkipRun reports whether a scheduled run can be skipped without
// contacting the provider. That is only safe when the last run left the record
// in sync and nothing about the task (provider, source, targets, resolved
// address) has changed since. Skipping is enabled by run-on-change-only, and
// forced for write-only providers, which cannot be read back and so have
// nothing to learn from a write of the value we already wrote.
//
// A manual "run now" is never skipped: the user asked for it explicitly, and it
// is the way to force a re-sync after the record was changed outside OzDDNS.
func (a *App) shouldSkipRun(job domain.Job, prov domain.Provider, state domain.JobState, fingerprint string, trigger domain.Trigger) bool {
	if trigger == domain.TriggerManual {
		return false
	}
	if fingerprint == "" {
		return false // no address resolved: let the reconciler record a skip
	}
	if state.SourceFingerprint == "" || state.SourceFingerprint != fingerprint {
		return false
	}
	switch state.LastStatus {
	case domain.StatusSuccess, domain.StatusNoChange:
	default:
		return false // retry after a failure or a skipped run
	}
	if job.RunOnChangeOnly {
		return true
	}
	if f, ok := a.Providers.Get(prov.Type); ok && f.Meta().WriteOnly {
		return true
	}
	return false
}

// nextJobState derives the runtime state that must be committed with the run.
func (a *App) nextJobState(job domain.Job, state domain.JobState, run domain.Run, fingerprint string, finished time.Time) domain.JobState {
	ns := state
	ns.JobID = job.ID
	ns.LastStatus = run.Status
	ns.LastRunAt = finished
	ns.NextRunAt = engine.NextRun(job, finished)
	ns.CurrentIP = firstNonEmpty(run.NewIP, run.OldIP, state.CurrentIP)
	ns.DesiredIP = run.NewIP
	// Keep the fingerprint only when the record is known to be in sync, so an
	// unresolved or failed attempt always retries next time.
	if run.Status == domain.StatusSuccess || run.Status == domain.StatusNoChange {
		ns.SourceFingerprint = fingerprint
	} else {
		ns.SourceFingerprint = ""
	}
	if run.Status == domain.StatusFailed {
		ns.LastError = run.Message
	} else if run.Status == domain.StatusSkipped {
		// Keep the reason a run was skipped so the panel can explain it
		// ("provider is disabled", "no address available", ...).
		ns.LastError = run.Message
	} else {
		ns.LastError = ""
	}
	if run.Status == domain.StatusSuccess {
		ns.LastChangedAt = finished
	}
	return ns
}

func (a *App) acquireJobGate(ctx context.Context, jobID string) (func(), error) {
	gate := a.jobGates[jobGateIndex(jobID, len(a.jobGates))]
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func jobGateIndex(jobID string, buckets int) int {
	hash := uint32(2166136261)
	for i := 0; i < len(jobID); i++ {
		hash ^= uint32(jobID[i])
		hash *= 16777619
	}
	return int(hash % uint32(buckets))
}

// primaryAddr returns the first resolved address (v4 preferred) so a skipped
// run can still report a meaningful "desired" address.
func primaryAddr(addrs map[string]netip.Addr) string {
	if a, ok := addrs["v4"]; ok {
		return a.String()
	}
	if a, ok := addrs["v6"]; ok {
		return a.String()
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

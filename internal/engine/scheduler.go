package engine

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/storage"
)

// JobRunner executes a single job. Implemented by the application layer so the
// scheduler never touches providers or repositories directly.
type JobRunner interface {
	RunJob(ctx context.Context, jobID string, trigger domain.Trigger) (domain.Run, error)
}

// Scheduler fires due jobs. It is deliberately simple: one ticker that asks
// "which jobs are due?" and a bounded worker pool.
type Scheduler struct {
	Jobs   storage.JobRepo
	States storage.StateRepo
	Runner JobRunner
	Log    *slog.Logger

	tick       time.Duration
	workers    int
	runOnStart bool
	mu         sync.RWMutex
	running    bool
	started    time.Time
	stop       chan struct{}
	stopOnce   sync.Once
	reload     chan struct{}
}

// Defaults used when the settings row does not specify a value.
const (
	DefaultTick    = 5 * time.Second
	DefaultWorkers = 4
)

// NewScheduler constructs a scheduler (call Start to run it).
func NewScheduler(jobs storage.JobRepo, states storage.StateRepo, runner JobRunner, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{Jobs: jobs, States: states, Runner: runner, Log: log,
		tick: DefaultTick, workers: DefaultWorkers, runOnStart: true, running: true,
		stop: make(chan struct{}), reload: make(chan struct{}, 1)}
}

// Start begins the scheduling loop (non-blocking).
func (s *Scheduler) Start(ctx context.Context) {
	s.started = time.Now()
	go s.loop(ctx)
}

// Stop ends the loop.
func (s *Scheduler) Stop() { s.stopOnce.Do(func() { close(s.stop) }) }

// Running reports whether automatic runs are enabled.
func (s *Scheduler) Running() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// SetRunning pauses or resumes automatic runs.
func (s *Scheduler) SetRunning(v bool) {
	s.mu.Lock()
	s.running = v
	s.mu.Unlock()
}

// Configure applies runtime tuning. Non-positive values keep the current value.
// A changed tick takes effect on the next loop iteration; RunOnStart only
// affects a future Start.
func (s *Scheduler) Configure(tick time.Duration, workers int, runOnStart bool) {
	s.mu.Lock()
	if tick > 0 && tick != s.tick {
		s.tick = tick
	}
	if workers > 0 {
		s.workers = workers
	}
	s.runOnStart = runOnStart
	s.mu.Unlock()
	select {
	case s.reload <- struct{}{}:
	default:
	}
}

// Config reports the effective tick and worker count.
func (s *Scheduler) Config() (time.Duration, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tick, s.workers
}

// Uptime returns how long the scheduler has been up.
func (s *Scheduler) Uptime() time.Duration {
	if s.started.IsZero() {
		return 0
	}
	return time.Since(s.started)
}

func (s *Scheduler) loop(ctx context.Context) {
	s.mu.RLock()
	tick, runOnStart := s.tick, s.runOnStart
	s.mu.RUnlock()
	timer := time.NewTimer(tick)
	defer timer.Stop()
	if runOnStart {
		// Kick off promptly so a fresh install reconciles on start.
		s.dispatchDue(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-s.reload:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(s.currentTick())
		case <-timer.C:
			s.dispatchDue(ctx)
			timer.Reset(s.currentTick())
		}
	}
}

func (s *Scheduler) currentTick() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.tick <= 0 {
		return DefaultTick
	}
	return s.tick
}

func (s *Scheduler) dispatchDue(ctx context.Context) {
	if !s.Running() {
		return
	}
	jobs, err := s.Jobs.List(ctx)
	if err != nil {
		s.Log.Error("scheduler: listing jobs", "err", err)
		return
	}
	states, err := s.States.List(ctx)
	if err != nil {
		s.Log.Error("scheduler: listing state", "err", err)
		return
	}
	stateByJob := map[string]domain.JobState{}
	for _, st := range states {
		stateByJob[st.JobID] = st
	}
	now := time.Now().UTC()
	_, workers := s.Config()
	if workers <= 0 {
		workers = DefaultWorkers
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		st := stateByJob[j.ID]
		if !st.NextRunAt.IsZero() && st.NextRunAt.After(now) {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(j domain.Job) {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := s.Runner.RunJob(ctx, j.ID, domain.TriggerSchedule); err != nil {
				s.Log.Warn("scheduler: job failed", "job", j.ID, "err", err)
			}
		}(j)
	}
	wg.Wait()
}

// NextRun computes the next due time for a job, applying optional jitter.
func NextRun(j domain.Job, from time.Time) time.Time {
	interval := time.Duration(j.IntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if j.Jitter {
		delta := float64(interval) * 0.1
		interval += time.Duration((rand.Float64()*2 - 1) * delta)
	}
	return from.Add(interval)
}

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/storage"
)

// fakeJobRepo is a JobRepo that serves a fixed slice.
type fakeJobRepo struct{ jobs []domain.Job }

func (f fakeJobRepo) List(context.Context) ([]domain.Job, error) { return f.jobs, nil }
func (f fakeJobRepo) Get(_ context.Context, id string) (domain.Job, error) {
	for _, j := range f.jobs {
		if j.ID == id {
			return j, nil
		}
	}
	return domain.Job{}, nil
}
func (f fakeJobRepo) Create(context.Context, domain.Job) error { return nil }
func (f fakeJobRepo) Update(context.Context, domain.Job) error { return nil }
func (f fakeJobRepo) Delete(context.Context, string) error     { return nil }

type fakeStateRepo struct{}

func (fakeStateRepo) Get(context.Context, string) (domain.JobState, error) {
	return domain.JobState{}, nil
}
func (fakeStateRepo) Upsert(context.Context, domain.JobState) error   { return nil }
func (fakeStateRepo) List(context.Context) ([]domain.JobState, error) { return nil, nil }

type runnerFunc func()

func (r runnerFunc) RunJob(context.Context, string, domain.Trigger) (domain.Run, error) {
	r()
	return domain.Run{Status: domain.StatusSuccess}, nil
}

// jobsWithOne returns a repo holding a single enabled, already-due job.
func jobsWithOne() storage.JobRepo {
	return fakeJobRepo{jobs: []domain.Job{{ID: "j1", Name: "job", Enabled: true, IntervalMinutes: 5}}}
}

// SCH-08: interval drives the next run time.
func TestNextRunInterval(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := NextRun(domain.Job{IntervalMinutes: 10}, from)
	if !got.Equal(from.Add(10 * time.Minute)) {
		t.Fatalf("NextRun = %s", got)
	}
}

// A missing interval falls back to 5 minutes.
func TestNextRunDefault(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := NextRun(domain.Job{}, from)
	if !got.Equal(from.Add(5 * time.Minute)) {
		t.Fatalf("NextRun = %s", got)
	}
}

// SCH-06: jitter stays within ±10% of the interval.
func TestNextRunJitterBounds(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 200; i++ {
		d := NextRun(domain.Job{IntervalMinutes: 10, Jitter: true}, from).Sub(from)
		if d < 9*time.Minute || d > 11*time.Minute {
			t.Fatalf("jittered interval out of bounds: %s", d)
		}
	}
}

// CFG-02: the tick interval and worker count are runtime-configurable.
func TestSchedulerConfigure(t *testing.T) {
	s := NewScheduler(nil, nil, nil, nil)
	if tick, workers := s.Config(); tick != DefaultTick || workers != DefaultWorkers {
		t.Fatalf("defaults = %s / %d", tick, workers)
	}
	s.Configure(15*time.Second, 8, false)
	tick, workers := s.Config()
	if tick != 15*time.Second || workers != 8 {
		t.Fatalf("after configure = %s / %d", tick, workers)
	}
	// A non-positive value must not clobber the current setting.
	s.Configure(0, 0, false)
	if tick, workers := s.Config(); tick != 15*time.Second || workers != 8 {
		t.Fatalf("zero configure changed values: %s / %d", tick, workers)
	}
}

// runOnStart=false must not reconcile the moment the loop begins.
func TestSchedulerRunOnStartDisabled(t *testing.T) {
	ran := make(chan struct{}, 1)
	runner := runnerFunc(func() { ran <- struct{}{} })
	s := NewScheduler(jobsWithOne(), fakeStateRepo{}, runner, nil)
	s.Configure(time.Hour, 1, false) // long tick so only the boot dispatch could fire
	s.Start(context.Background())
	defer s.Stop()
	select {
	case <-ran:
		t.Fatal("dispatched despite runOnStart=false")
	case <-time.After(150 * time.Millisecond):
	}
}

// A job still in flight must not be dispatched again while it runs, or a slow
// reconciliation would write to the provider twice.
func TestSchedulerDoesNotRedispatchInFlightJob(t *testing.T) {
	calls := 0
	s := NewScheduler(jobsWithOne(), fakeStateRepo{}, runnerFunc(func() { calls++ }), nil)

	if !s.begin("j1") {
		t.Fatal("first begin should succeed")
	}
	s.dispatchDue(context.Background())
	if calls != 0 {
		t.Fatalf("job was dispatched while already in flight: calls=%d", calls)
	}

	s.end("j1")
	s.dispatchDue(context.Background())
	if calls != 1 {
		t.Fatalf("job was not dispatched after release: calls=%d", calls)
	}
}

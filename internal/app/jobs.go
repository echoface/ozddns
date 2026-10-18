package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/engine"
	"github.com/echoface/ozddns/internal/ids"
)

// JobView is a job plus its runtime state and denormalised labels for the panel.
type JobView struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	ProviderID      string          `json:"providerId"`
	ProviderName    string          `json:"providerName"`
	ProviderType    string          `json:"providerType"`
	IPSourceID      string          `json:"ipSourceId"`
	IPSourceName    string          `json:"ipSourceName"`
	Targets         []domain.Target `json:"targets"`
	IntervalMinutes int             `json:"intervalMinutes"`
	RunOnChangeOnly bool            `json:"runOnChangeOnly"`
	Jitter          bool            `json:"jitter"`
	Enabled         bool            `json:"enabled"`
	State           domain.JobState `json:"state"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// JobInput is the payload for creating/updating a job.
type JobInput struct {
	Name            string          `json:"name"`
	ProviderID      string          `json:"providerId"`
	IPSourceID      string          `json:"ipSourceId"`
	Targets         []domain.Target `json:"targets"`
	IntervalMinutes int             `json:"intervalMinutes"`
	RunOnChangeOnly *bool           `json:"runOnChangeOnly"`
	Jitter          *bool           `json:"jitter"`
	Enabled         *bool           `json:"enabled"`
}

// ListJobs returns every job as a view.
func (a *App) ListJobs(ctx context.Context) ([]JobView, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	jobs, err := a.Store.Jobs().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]JobView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, a.jobView(ctx, j))
	}
	return out, nil
}

// GetJob returns one job view.
func (a *App) GetJob(ctx context.Context, id string) (JobView, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	j, err := a.Store.Jobs().Get(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	return a.jobView(ctx, j), nil
}

// CreateJob validates and stores a job.
func (a *App) CreateJob(ctx context.Context, in JobInput) (JobView, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	if err := a.validateJobInput(ctx, in); err != nil {
		return JobView{}, err
	}
	j := domain.Job{
		ID:   ids.New("j"),
		Name: in.Name, ProviderID: in.ProviderID, IPSourceID: in.IPSourceID,
		Targets: in.Targets, IntervalMinutes: in.IntervalMinutes,
		RunOnChangeOnly: boolOr(in.RunOnChangeOnly, true), Jitter: boolOr(in.Jitter, false),
		Enabled: boolOr(in.Enabled, true),
	}
	if err := a.Store.Jobs().Create(ctx, j); err != nil {
		return JobView{}, err
	}
	created, err := a.Store.Jobs().Get(ctx, j.ID)
	if err != nil {
		created = j
	}
	// Schedule the first run promptly.
	if err := a.Store.States().Upsert(ctx, domain.JobState{JobID: created.ID, NextRunAt: time.Now().UTC()}); err != nil {
		a.Log.Warn("job created but initial schedule state was not persisted", "job", created.ID, "err", err)
	}
	return a.jobView(ctx, created), nil
}

// UpdateJob updates a job.
func (a *App) UpdateJob(ctx context.Context, id string, in JobInput) (JobView, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	release, err := a.acquireJobGate(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	defer release()

	j, err := a.Store.Jobs().Get(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	if in.Name != "" {
		j.Name = in.Name
	}
	if in.ProviderID != "" {
		j.ProviderID = in.ProviderID
	}
	if in.IPSourceID != "" {
		j.IPSourceID = in.IPSourceID
	}
	if in.Targets != nil {
		j.Targets = in.Targets
	}
	if in.IntervalMinutes > 0 {
		j.IntervalMinutes = in.IntervalMinutes
	}
	if in.RunOnChangeOnly != nil {
		j.RunOnChangeOnly = *in.RunOnChangeOnly
	}
	if in.Jitter != nil {
		j.Jitter = *in.Jitter
	}
	if in.Enabled != nil {
		j.Enabled = *in.Enabled
	}
	if err := a.validateJobInput(ctx, JobInput{Name: j.Name, ProviderID: j.ProviderID, IPSourceID: j.IPSourceID, Targets: j.Targets}); err != nil {
		return JobView{}, err
	}
	if err := a.Store.Jobs().Update(ctx, j); err != nil {
		return JobView{}, err
	}
	// Re-plan the next run so a shortened interval takes effect immediately
	// instead of waiting out the schedule the old interval already booked. The
	// new time is only ever earlier, so an edit can never postpone a run.
	if j.Enabled {
		if st, err := a.Store.States().Get(ctx, id); err == nil {
			next := engine.NextRun(j, time.Now().UTC())
			if st.NextRunAt.IsZero() || next.Before(st.NextRunAt) {
				st.JobID = id
				st.NextRunAt = next
				if err := a.Store.States().Upsert(ctx, st); err != nil {
					a.Log.Warn("job updated but next run was not rescheduled", "job", id, "err", err)
				}
			}
		}
	}
	return a.jobView(ctx, j), nil
}

// DeleteJob removes a job and its state.
func (a *App) DeleteJob(ctx context.Context, id string) error {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	release, err := a.acquireJobGate(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	return a.Store.Jobs().Delete(ctx, id)
}

// RunNow triggers an immediate run.
func (a *App) RunNow(ctx context.Context, id string) (domain.Run, error) {
	return a.RunJob(ctx, id, domain.TriggerManual)
}

// RunAllResult is one job's outcome from RunAllEnabled.
type RunAllResult struct {
	JobID   string `json:"jobId"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// RunAllSummary is the aggregate outcome of "run everything now".
type RunAllSummary struct {
	Total    int            `json:"total"`
	Success  int            `json:"success"`
	NoChange int            `json:"noChange"`
	Failed   int            `json:"failed"`
	Skipped  int            `json:"skipped"`
	Results  []RunAllResult `json:"results"`
}

// RunAllEnabled reconciles every enabled job immediately, sequentially. It is
// what the panel's "Run all" button calls — the periodic schedule is a separate
// concern owned by the scheduler.
func (a *App) RunAllEnabled(ctx context.Context) RunAllSummary {
	jobs, err := a.Store.Jobs().List(ctx)
	if err != nil {
		return RunAllSummary{}
	}
	sum := RunAllSummary{Results: []RunAllResult{}}
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		sum.Total++
		run, err := a.RunJob(ctx, j.ID, domain.TriggerManual)
		res := RunAllResult{JobID: j.ID, Name: j.Name, Status: string(run.Status), Message: run.Message}
		if err != nil && res.Status == "" {
			res.Status = string(domain.StatusFailed)
			res.Message = err.Error()
		}
		switch domain.RunStatus(res.Status) {
		case domain.StatusSuccess:
			sum.Success++
		case domain.StatusFailed:
			sum.Failed++
		case domain.StatusSkipped:
			sum.Skipped++
		default:
			sum.NoChange++
		}
		sum.Results = append(sum.Results, res)
	}
	return sum
}

// DryRun returns the diff a job would apply, without writing.
func (a *App) DryRun(ctx context.Context, id string) (engine.Diff, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	j, err := a.Store.Jobs().Get(ctx, id)
	if err != nil {
		return engine.Diff{}, err
	}
	prov, err := a.Store.Providers().Get(ctx, j.ProviderID)
	if err != nil {
		return engine.Diff{}, err
	}
	src, err := a.Store.IPSources().Get(ctx, j.IPSourceID)
	if err != nil {
		return engine.Diff{}, err
	}
	addrs, err := a.Reconciler.Plan(ctx, j, src)
	if err != nil {
		return engine.Diff{}, err
	}
	return a.Reconciler.DryRunWith(ctx, j, prov, addrs)
}

func (a *App) jobView(ctx context.Context, j domain.Job) JobView {
	v := JobView{
		ID: j.ID, Name: j.Name, ProviderID: j.ProviderID, IPSourceID: j.IPSourceID,
		Targets: j.Targets, IntervalMinutes: j.IntervalMinutes,
		RunOnChangeOnly: j.RunOnChangeOnly, Jitter: j.Jitter, Enabled: j.Enabled,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	if p, err := a.Store.Providers().Get(ctx, j.ProviderID); err == nil {
		v.ProviderName, v.ProviderType = p.Name, p.Type
	}
	if s, err := a.Store.IPSources().Get(ctx, j.IPSourceID); err == nil {
		v.IPSourceName = s.Name
	}
	if st, err := a.Store.States().Get(ctx, j.ID); err == nil {
		v.State = st
	}
	return v
}

func (a *App) validateJobInput(ctx context.Context, in JobInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("job name is required")
	}
	if _, err := a.Store.Providers().Get(ctx, in.ProviderID); err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	source, err := a.Store.IPSources().Get(ctx, in.IPSourceID)
	if err != nil {
		return fmt.Errorf("ip source: %w", err)
	}
	if len(in.Targets) == 0 {
		return fmt.Errorf("at least one target record is required")
	}
	for _, t := range in.Targets {
		if strings.TrimSpace(t.Domain) == "" {
			return fmt.Errorf("each target needs a domain")
		}
		switch t.Type {
		case domain.RecordA, domain.RecordAAAA:
		default:
			// CNAME (and anything else) is refused: a dynamic-DNS target is
			// driven by an IP source, and a CNAME's value is a hostname, which
			// no source can supply. See engine.Reconciler.Reconcile.
			return fmt.Errorf("target %s: unsupported record type %q (only A and AAAA are address-driven)", t.Domain, t.Type)
		}
		if family := t.Type.Family(); family != source.Family {
			return fmt.Errorf("target %s: %s requires an IP source for %s, but %q provides %s addresses",
				t.Domain, t.Type, family, source.Name, source.Family)
		}
	}
	return nil
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

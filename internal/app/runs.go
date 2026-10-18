package app

import (
	"context"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/storage"
)

// Execution history is a rolling window, not an archive: the daemon keeps
// `RetentionDays` days and deletes the rest, hourly. The window is bounded on
// both ends so a typo cannot silently keep (or wipe) everything.
const (
	DefaultRunRetentionDays = 1
	MaxRunRetentionDays     = 365
)

// RetentionDays resolves the effective window from the stored settings.
func RetentionDays(s domain.Settings) int {
	switch {
	case s.RunRetentionDays <= 0:
		return DefaultRunRetentionDays
	case s.RunRetentionDays > MaxRunRetentionDays:
		return MaxRunRetentionDays
	default:
		return s.RunRetentionDays
	}
}

// PurgeRuns deletes execution history older than the retention window and
// reports how many rows went away.
func (a *App) PurgeRuns(ctx context.Context) (int64, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	s, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return 0, err
	}
	days := RetentionDays(s)
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	n, err := a.Store.Runs().DeleteBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		a.Log.Info("retention: purged execution history", "runs", n, "keptDays", days)
	}
	return n, nil
}

// RunRetentionLoop purges once shortly after boot and then every hour. It runs
// for the lifetime of ctx.
func (a *App) RunRetentionLoop(ctx context.Context) {
	go func() {
		// Let start-up work (and a first reconciliation) settle first.
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if _, err := a.PurgeRuns(ctx); err != nil {
					a.Log.Warn("retention: purge failed", "err", err)
				}
				timer.Reset(time.Hour)
			}
		}
	}()
}

// ListRuns returns execution history, newest first.
func (a *App) ListRuns(ctx context.Context, jobID string, status domain.RunStatus, limit int) ([]domain.Run, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	if limit <= 0 {
		limit = 200
	}
	return a.Store.Runs().List(ctx, storage.RunFilter{JobID: jobID, Status: status, Limit: limit})
}

// GetRun returns one run.
func (a *App) GetRun(ctx context.Context, id int64) (domain.Run, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.Store.Runs().Get(ctx, id)
}

// Package storage defines the repository interfaces the application layer
// depends on. Concrete implementations (SQLite today, MySQL later) live in
// sub-packages so the business layer never sees a *sql.DB or dialect.
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

// ErrNotFound is returned when an entity does not exist.
var ErrNotFound = errors.New("not found")

// ProviderRepo persists provider instances.
type ProviderRepo interface {
	List(ctx context.Context) ([]domain.Provider, error)
	Get(ctx context.Context, id string) (domain.Provider, error)
	Create(ctx context.Context, p domain.Provider) error
	Update(ctx context.Context, p domain.Provider) error
	Delete(ctx context.Context, id string) error
}

// IPSourceRepo persists IP sources.
type IPSourceRepo interface {
	List(ctx context.Context) ([]domain.IPSource, error)
	Get(ctx context.Context, id string) (domain.IPSource, error)
	Create(ctx context.Context, s domain.IPSource) error
	Update(ctx context.Context, s domain.IPSource) error
	Delete(ctx context.Context, id string) error
}

// JobRepo persists sync jobs.
type JobRepo interface {
	List(ctx context.Context) ([]domain.Job, error)
	Get(ctx context.Context, id string) (domain.Job, error)
	Create(ctx context.Context, j domain.Job) error
	Update(ctx context.Context, j domain.Job) error
	Delete(ctx context.Context, id string) error
}

// RunFilter narrows run queries.
type RunFilter struct {
	JobID  string
	Status domain.RunStatus
	Limit  int
}

// RunRepo persists execution history.
type RunRepo interface {
	Insert(ctx context.Context, r domain.Run) (int64, error)
	Get(ctx context.Context, id int64) (domain.Run, error)
	List(ctx context.Context, f RunFilter) ([]domain.Run, error)
	CountByStatus(ctx context.Context, status domain.RunStatus) (int, error)
	// DeleteBefore hard-deletes history older than the cutoff and reports how
	// many rows went away. It backs the retention policy.
	DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// StateRepo persists per-job runtime state.
type StateRepo interface {
	Get(ctx context.Context, jobID string) (domain.JobState, error)
	Upsert(ctx context.Context, s domain.JobState) error
	List(ctx context.Context) ([]domain.JobState, error)
}

// SettingsRepo persists instance settings.
type SettingsRepo interface {
	Get(ctx context.Context) (domain.Settings, error)
	Save(ctx context.Context, s domain.Settings) error
}

// Store bundles every repository plus lifecycle.
type Store interface {
	Providers() ProviderRepo
	IPSources() IPSourceRepo
	Jobs() JobRepo
	Runs() RunRepo
	States() StateRepo
	Settings() SettingsRepo
	Close() error
}

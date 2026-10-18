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

// ErrConflict is returned when a state-changing operation loses a race or
// would violate a uniqueness rule.
var ErrConflict = errors.New("conflict")

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
	// InsertWithState records a completed run and its resulting job state in
	// one transaction. A run must never be visible without the scheduling state
	// it produced, or the next tick can repeat or indefinitely retry the work.
	InsertWithState(ctx context.Context, r domain.Run, s domain.JobState) (int64, error)
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

// ConfigRepo applies a portable configuration bundle. The implementation owns
// the transaction so a replace import can never delete the old configuration
// and leave only part of the new one behind.
type ConfigRepo interface {
	Import(ctx context.Context, providers []domain.Provider, sources []domain.IPSource,
		jobs []domain.Job, settings *domain.Settings, replace bool) error
}

// AuthRepo persists the optional single-administrator authorization state.
// The presence of a user is the source of truth: no user means the panel is in
// open local mode. Sessions and challenges store hashes, never raw tokens.
type AuthRepo interface {
	User(ctx context.Context) (domain.AuthUser, error)
	// CompleteSetup atomically creates the administrator, stores its recovery
	// codes and clears the pending enrollment.
	CompleteSetup(ctx context.Context, u domain.AuthUser, codes []domain.RecoveryCode) error
	// ClearAuth removes the administrator and every dependent session,
	// challenge and recovery code. It is also safe when authorization is off.
	ClearAuth(ctx context.Context) error
	// UseTOTPStep atomically consumes a TOTP time step, preventing the same
	// code from being accepted by two concurrent login flows.
	UseTOTPStep(ctx context.Context, step int64) (bool, error)

	Pending(ctx context.Context) (domain.AuthPending, error)
	SavePending(ctx context.Context, p domain.AuthPending) error
	DeletePending(ctx context.Context) error

	Challenge(ctx context.Context, tokenHash string) (domain.AuthChallenge, error)
	SaveChallenge(ctx context.Context, c domain.AuthChallenge) error
	ConsumeChallenge(ctx context.Context, tokenHash string) (bool, error)
	DeleteChallenge(ctx context.Context, tokenHash string) error
	DeleteExpiredChallenges(ctx context.Context, now time.Time) error
	IncrementChallengeAttempts(ctx context.Context, tokenHash string) (int, error)

	InsertSession(ctx context.Context, s domain.AuthSession) error
	Session(ctx context.Context, tokenHash string) (domain.AuthSession, error)
	TouchSession(ctx context.Context, tokenHash string, lastSeen, idleExpires time.Time) error
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteAllSessions(ctx context.Context) error
	SessionCount(ctx context.Context) (int, error)
	DeleteExpiredSessions(ctx context.Context, now time.Time) error

	InsertRecoveryCodes(ctx context.Context, codes []domain.RecoveryCode) error
	ReplaceRecoveryCodes(ctx context.Context, codes []domain.RecoveryCode) error
	ListRecoveryCodes(ctx context.Context) ([]domain.RecoveryCode, error)
	UseRecoveryCode(ctx context.Context, id string, at time.Time) error
	DeleteRecoveryCodes(ctx context.Context) error

	AddEvent(ctx context.Context, e domain.AuthEvent) error
	ListEvents(ctx context.Context, limit int) ([]domain.AuthEvent, error)
}

// Store bundles every repository plus lifecycle.
type Store interface {
	Providers() ProviderRepo
	IPSources() IPSourceRepo
	Jobs() JobRepo
	Runs() RunRepo
	States() StateRepo
	Settings() SettingsRepo
	Config() ConfigRepo
	Auth() AuthRepo
	Close() error
}

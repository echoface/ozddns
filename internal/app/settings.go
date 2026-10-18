package app

import (
	"context"
	"fmt"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

// SettingsInput is a partial update: only the fields that are non-nil are
// applied, so the panel can PATCH one tab without clobbering the others.
type SettingsInput struct {
	Language             *string                `json:"language"`
	Theme                *string                `json:"theme"`
	Timezone             *string                `json:"timezone"`
	DefaultInterval      *int                   `json:"defaultInterval"`
	SchedulerPaused      *bool                  `json:"schedulerPaused"`
	SchedulerTickSeconds *int                   `json:"schedulerTickSeconds"`
	SchedulerWorkers     *int                   `json:"schedulerWorkers"`
	RunOnStart           *bool                  `json:"runOnStart"`
	RunRetentionDays     *int                   `json:"runRetentionDays"`
	Notifications        *[]domain.Notification `json:"notifications"`
}

// PatchSettings merges a partial update into the stored settings.
func (a *App) PatchSettings(ctx context.Context, in SettingsInput) (domain.Settings, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	s, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	if in.Language != nil && *in.Language != "" {
		s.Language = *in.Language
	}
	if in.Theme != nil && *in.Theme != "" {
		s.Theme = *in.Theme
	}
	// An explicit empty string clears the override and follows the browser again.
	if in.Timezone != nil {
		if *in.Timezone != "" {
			if _, err := time.LoadLocation(*in.Timezone); err != nil {
				return domain.Settings{}, fmt.Errorf("invalid timezone %q", *in.Timezone)
			}
		}
		s.Timezone = *in.Timezone
	}
	if in.DefaultInterval != nil && *in.DefaultInterval > 0 {
		s.DefaultInterval = *in.DefaultInterval
	}
	if in.SchedulerPaused != nil {
		s.SchedulerPaused = *in.SchedulerPaused
	}
	if in.SchedulerTickSeconds != nil && *in.SchedulerTickSeconds > 0 {
		s.SchedulerTickSeconds = *in.SchedulerTickSeconds
	}
	if in.SchedulerWorkers != nil && *in.SchedulerWorkers > 0 {
		s.SchedulerWorkers = *in.SchedulerWorkers
	}
	if in.RunOnStart != nil {
		s.RunOnStart = *in.RunOnStart
	}
	if in.RunRetentionDays != nil {
		if *in.RunRetentionDays < 1 || *in.RunRetentionDays > MaxRunRetentionDays {
			return domain.Settings{}, fmt.Errorf("execution history must be between 1 and %d days", MaxRunRetentionDays)
		}
		s.RunRetentionDays = *in.RunRetentionDays
	}
	if in.Notifications != nil {
		s.Notifications = *in.Notifications
	}
	if err := a.Store.Settings().Save(ctx, s); err != nil {
		return domain.Settings{}, err
	}
	// Push the new tuning into the running scheduler right away.
	a.Scheduler.Configure(time.Duration(s.SchedulerTickSeconds)*time.Second, s.SchedulerWorkers, s.RunOnStart)
	a.Scheduler.SetRunning(!s.SchedulerPaused)
	return s, nil
}

// GetSettings returns the persisted instance settings.
func (a *App) GetSettings(ctx context.Context) (domain.Settings, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.Store.Settings().Get(ctx)
}

// SaveSettings persists instance settings.
func (a *App) SaveSettings(ctx context.Context, s domain.Settings) (domain.Settings, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	if err := a.Store.Settings().Save(ctx, s); err != nil {
		return domain.Settings{}, err
	}
	return s, nil
}

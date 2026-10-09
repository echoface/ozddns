package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Username             *string                `json:"username"`
	Password             *string                `json:"password"`
	AllowWAN             *bool                  `json:"allowWan"`
	TLS                  *bool                  `json:"tls"`
	Listen               *string                `json:"listen"`
	SchedulerPaused      *bool                  `json:"schedulerPaused"`
	SchedulerTickSeconds *int                   `json:"schedulerTickSeconds"`
	SchedulerWorkers     *int                   `json:"schedulerWorkers"`
	RunOnStart           *bool                  `json:"runOnStart"`
	RunRetentionDays     *int                   `json:"runRetentionDays"`
	Notifications        *[]domain.Notification `json:"notifications"`
}

// PatchSettings merges a partial update into the stored settings.
func (a *App) PatchSettings(ctx context.Context, in SettingsInput) (domain.Settings, error) {
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
	if in.Timezone != nil && *in.Timezone != "" {
		s.Timezone = *in.Timezone
	}
	if in.DefaultInterval != nil && *in.DefaultInterval > 0 {
		s.DefaultInterval = *in.DefaultInterval
	}
	if in.Username != nil {
		s.Username = *in.Username
	}
	if in.Password != nil && *in.Password != "" {
		s.PasswordHash = hashPassword(*in.Password)
	}
	if in.AllowWAN != nil {
		s.AllowWAN = *in.AllowWAN
	}
	if in.TLS != nil {
		s.TLS = *in.TLS
	}
	if in.Listen != nil && *in.Listen != "" {
		s.Listen = *in.Listen
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
	s.PasswordSet = s.PasswordHash != ""
	if err := a.Store.Settings().Save(ctx, s); err != nil {
		return domain.Settings{}, err
	}
	// Push the new tuning into the running scheduler right away.
	a.Scheduler.Configure(time.Duration(s.SchedulerTickSeconds)*time.Second, s.SchedulerWorkers, s.RunOnStart)
	a.Scheduler.SetRunning(!s.SchedulerPaused)
	s.PasswordHash = ""
	return s, nil
}

// GetSettings returns instance settings with the password hash removed.
func (a *App) GetSettings(ctx context.Context) (domain.Settings, error) {
	s, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	s.PasswordHash = ""
	return s, nil
}

// SaveSettings persists instance settings (password only replaced when set).
func (a *App) SaveSettings(ctx context.Context, s domain.Settings) (domain.Settings, error) {
	existing, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	if s.PasswordHash == "" {
		s.PasswordHash = existing.PasswordHash
	}
	if s.Listen == "" {
		s.Listen = existing.Listen
	}
	s.PasswordSet = s.PasswordHash != ""
	if err := a.Store.Settings().Save(ctx, s); err != nil {
		return domain.Settings{}, err
	}
	s.PasswordHash = ""
	return s, nil
}

// SetPassword stores a hashed password.
func (a *App) SetPassword(ctx context.Context, username, password string) error {
	s, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return err
	}
	if username != "" {
		s.Username = username
	}
	s.PasswordHash = hashPassword(password)
	s.PasswordSet = password != ""
	return a.Store.Settings().Save(ctx, s)
}

// CheckPassword verifies a login attempt.
func (a *App) CheckPassword(ctx context.Context, username, password string) bool {
	s, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return false
	}
	if s.PasswordHash == "" {
		return true // no password configured: local-first
	}
	if username != "" && s.Username != "" && username != s.Username {
		return false
	}
	return s.PasswordHash == hashPassword(password)
}

func hashPassword(p string) string {
	sum := sha256.Sum256([]byte("ozddns:" + p))
	return hex.EncodeToString(sum[:])
}

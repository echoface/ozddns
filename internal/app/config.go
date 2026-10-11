package app

import (
	"context"
	"fmt"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/version"
)

// ConfigBundle is the portable snapshot of an instance: the configuration a user
// wants to back up or move between hosts. Run history is intentionally excluded.
// It backs both `ozddns config export|import` and the panel's backup buttons.
type ConfigBundle struct {
	Version   string            `json:"version"`
	Providers []domain.Provider `json:"providers"`
	IPSources []domain.IPSource `json:"ipSources"`
	Jobs      []domain.Job      `json:"jobs"`
	Settings  domain.Settings   `json:"settings"`
}

// ImportStats reports what an import changed.
type ImportStats struct {
	Providers int `json:"providers"`
	IPSources int `json:"ipSources"`
	Jobs      int `json:"jobs"`
}

// Export returns a snapshot of the instance configuration.
func (a *App) Export(ctx context.Context) (ConfigBundle, error) {
	prov, err := a.Store.Providers().List(ctx)
	if err != nil {
		return ConfigBundle{}, err
	}
	srcs, err := a.Store.IPSources().List(ctx)
	if err != nil {
		return ConfigBundle{}, err
	}
	jobs, err := a.Store.Jobs().List(ctx)
	if err != nil {
		return ConfigBundle{}, err
	}
	settings, _ := a.Store.Settings().Get(ctx)
	settings.PasswordHash = "" // never export the hash
	settings.PasswordSet = false
	return ConfigBundle{
		Version: version.Version, Providers: prov, IPSources: srcs,
		Jobs: jobs, Settings: settings,
	}, nil
}

// Import applies a bundle. When replace is true the existing providers, sources
// and jobs are deleted first (a clean restore); otherwise entries are added and
// collisions are reported as errors, leaving what already exists untouched.
func (a *App) Import(ctx context.Context, b ConfigBundle, replace bool) (ImportStats, error) {
	var stats ImportStats
	if b.Settings.Timezone != "" {
		if _, err := time.LoadLocation(b.Settings.Timezone); err != nil {
			return stats, fmt.Errorf("invalid timezone %q", b.Settings.Timezone)
		}
	}

	if replace {
		for _, j := range mustList(a.Store.Jobs().List(ctx)) {
			_ = a.Store.Jobs().Delete(ctx, j.ID)
		}
		for _, s := range mustList(a.Store.IPSources().List(ctx)) {
			_ = a.Store.IPSources().Delete(ctx, s.ID)
		}
		for _, p := range mustList(a.Store.Providers().List(ctx)) {
			_ = a.Store.Providers().Delete(ctx, p.ID)
		}
	}

	for _, p := range b.Providers {
		if err := a.Store.Providers().Create(ctx, p); err != nil {
			return stats, fmt.Errorf("import provider %q: %w", p.Name, err)
		}
		stats.Providers++
	}
	for _, s := range b.IPSources {
		if err := a.Store.IPSources().Create(ctx, s); err != nil {
			return stats, fmt.Errorf("import ip source %q: %w", s.Name, err)
		}
		stats.IPSources++
	}
	for _, j := range b.Jobs {
		if err := a.Store.Jobs().Create(ctx, j); err != nil {
			return stats, fmt.Errorf("import job %q: %w", j.Name, err)
		}
		stats.Jobs++
	}
	if !replace && b.Settings.Language == "" && len(b.Settings.Notifications) == 0 {
		return stats, nil
	}
	if b.Settings.Language != "" || len(b.Settings.Notifications) > 0 {
		existing, err := a.Store.Settings().Get(ctx)
		if err != nil {
			return stats, err
		}
		b.Settings.PasswordHash = existing.PasswordHash
		b.Settings.PasswordSet = existing.PasswordHash != ""
		if err := a.Store.Settings().Save(ctx, b.Settings); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func mustList[T any](v []T, _ error) []T { return v }

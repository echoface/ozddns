package app

import (
	"context"
	"fmt"
	"strings"
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
	a.configMu.RLock()
	defer a.configMu.RUnlock()

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
	settings, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return ConfigBundle{}, err
	}
	return ConfigBundle{
		Version: version.Version, Providers: prov, IPSources: srcs,
		Jobs: jobs, Settings: settings,
	}, nil
}

// Import applies a bundle. When replace is true the existing providers, sources
// and jobs are deleted first (a clean restore); otherwise entries are added and
// collisions are reported as errors, leaving what already exists untouched.
//
// The whole bundle is validated up front and applied by the storage layer in a
// single transaction. A malformed bundle, a duplicate id, or a write failure
// therefore leaves the running configuration untouched.
func (a *App) Import(ctx context.Context, b ConfigBundle, replace bool) (ImportStats, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	var stats ImportStats
	if err := a.validateBundle(ctx, b, replace); err != nil {
		return stats, err
	}
	var settings *domain.Settings
	if replace {
		// Settings travel with a full restore only; a merge import leaves the
		// instance's own preferences alone. Authorization state is never part of
		// a configuration bundle: it is local security material, not DNS config.
		settings = &b.Settings
	}
	if err := a.Store.Config().Import(ctx, b.Providers, b.IPSources, b.Jobs, settings, replace); err != nil {
		return stats, err
	}
	if replace {
		// Settings are part of a full restore. Apply them before releasing the
		// write lock so the running scheduler does not keep using stale tuning.
		a.ApplySchedulerSettings(ctx)
	}
	stats.Providers = len(b.Providers)
	stats.IPSources = len(b.IPSources)
	stats.Jobs = len(b.Jobs)
	return stats, nil
}

// validateBundle checks a bundle is internally consistent and every entity is
// buildable before anything is mutated.
func (a *App) validateBundle(ctx context.Context, b ConfigBundle, replace bool) error {
	if b.Settings.Timezone != "" {
		if _, err := time.LoadLocation(b.Settings.Timezone); err != nil {
			return fmt.Errorf("invalid timezone %q", b.Settings.Timezone)
		}
	}
	providers := map[string]bool{}
	sources := map[string]domain.IPSource{}
	jobs := map[string]bool{}
	if !replace {
		existingProviders, err := a.Store.Providers().List(ctx)
		if err != nil {
			return err
		}
		for _, p := range existingProviders {
			providers[p.ID] = true
		}
		existingSources, err := a.Store.IPSources().List(ctx)
		if err != nil {
			return err
		}
		for _, s := range existingSources {
			sources[s.ID] = s
		}
		existingJobs, err := a.Store.Jobs().List(ctx)
		if err != nil {
			return err
		}
		for _, j := range existingJobs {
			jobs[j.ID] = true
		}
	}
	for _, p := range b.Providers {
		if p.ID == "" {
			return fmt.Errorf("import provider %q: missing id", p.Name)
		}
		if providers[p.ID] {
			return fmt.Errorf("import provider %q: duplicate id %s", p.Name, p.ID)
		}
		f, err := a.Providers.MustGet(p.Type)
		if err != nil {
			return fmt.Errorf("import provider %q: %w", p.Name, err)
		}
		if err := f.ValidateConfig(p.Config); err != nil {
			return fmt.Errorf("import provider %q: %w", p.Name, err)
		}
		providers[p.ID] = true
	}
	for _, s := range b.IPSources {
		if s.ID == "" {
			return fmt.Errorf("import ip source %q: missing id", s.Name)
		}
		if _, exists := sources[s.ID]; exists {
			return fmt.Errorf("import ip source %q: duplicate id %s", s.Name, s.ID)
		}
		if err := a.validateIPSource(IPSourceInput{Name: s.Name, Type: s.Type, Family: s.Family, Target: s.Target}); err != nil {
			return fmt.Errorf("import ip source %q: %w", s.Name, err)
		}
		sources[s.ID] = s
	}
	for _, j := range b.Jobs {
		if strings.TrimSpace(j.Name) == "" {
			return fmt.Errorf("import job: name is required")
		}
		if j.ID == "" {
			return fmt.Errorf("import job %q: missing id", j.Name)
		}
		if jobs[j.ID] {
			return fmt.Errorf("import job %q: duplicate id %s", j.Name, j.ID)
		}
		if !providers[j.ProviderID] {
			return fmt.Errorf("import job %q: provider %s is missing from this import", j.Name, j.ProviderID)
		}
		source, ok := sources[j.IPSourceID]
		if !ok {
			return fmt.Errorf("import job %q: ip source %s is missing from this import", j.Name, j.IPSourceID)
		}
		if len(j.Targets) == 0 {
			return fmt.Errorf("import job %q: at least one target record is required", j.Name)
		}
		for _, t := range j.Targets {
			if strings.TrimSpace(t.Domain) == "" {
				return fmt.Errorf("import job %q: each target needs a domain", j.Name)
			}
			if !t.Type.IsAddressRecord() {
				return fmt.Errorf("import job %q: unsupported record type %q", j.Name, t.Type)
			}
			if family := t.Type.Family(); family != source.Family {
				return fmt.Errorf("import job %q: target %s is an %s record but ip source %s provides %s addresses",
					j.Name, t.Domain, t.Type, source.Name, source.Family)
			}
		}
		jobs[j.ID] = true
	}
	return nil
}

package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ids"
)

// ProviderView is a provider plus derived health, zones and usage — the shape the
// panel consumes. Secrets are masked.
type ProviderView struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Enabled       bool           `json:"enabled"`
	Config        map[string]any `json:"config"`
	Status        string         `json:"status"`     // ok | warn | err
	StatusText    string         `json:"statusText"` // human explanation
	Zones         []string       `json:"zones"`
	Records       int            `json:"records"`
	LastValidated time.Time      `json:"lastValidated"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// ProviderInput is the payload for creating/updating a provider.
type ProviderInput struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Config  map[string]any `json:"config"`
	Enabled *bool          `json:"enabled"`
}

// ListProviders returns all providers as masked views.
func (a *App) ListProviders(ctx context.Context) ([]ProviderView, error) {
	providers, err := a.Store.Providers().List(ctx)
	if err != nil {
		return nil, err
	}
	jobs, _ := a.Store.Jobs().List(ctx)
	out := make([]ProviderView, 0, len(providers))
	for _, p := range providers {
		out = append(out, a.providerView(p, jobs))
	}
	return out, nil
}

// GetProvider returns one provider view.
func (a *App) GetProvider(ctx context.Context, id string) (ProviderView, error) {
	p, err := a.Store.Providers().Get(ctx, id)
	if err != nil {
		return ProviderView{}, err
	}
	jobs, _ := a.Store.Jobs().List(ctx)
	return a.providerView(p, jobs), nil
}

// CreateProvider validates config, stores the provider and attempts a live check.
func (a *App) CreateProvider(ctx context.Context, in ProviderInput) (ProviderView, error) {
	f, err := a.Providers.MustGet(in.Type)
	if err != nil {
		return ProviderView{}, err
	}
	if masked := MaskedSecretFields(a.Providers, in.Type, in.Config); len(masked) > 0 {
		return ProviderView{}, fmt.Errorf("secret field(s) %s look masked; enter the credential in full", strings.Join(masked, ", "))
	}
	if err := f.ValidateConfig(in.Config); err != nil {
		return ProviderView{}, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	p := domain.Provider{ID: ids.New("p"), Name: in.Name, Type: in.Type, Config: in.Config, Enabled: enabled}
	if err := a.Store.Providers().Create(ctx, p); err != nil {
		return ProviderView{}, err
	}
	// Resolve the generated id.
	created, err := a.Store.Providers().Get(ctx, p.ID)
	if err != nil {
		created = p
	}
	a.validateAndCache(ctx, created)
	jobs, _ := a.Store.Jobs().List(ctx)
	return a.providerView(created, jobs), nil
}

// UpdateProvider updates fields and re-runs validation.
func (a *App) UpdateProvider(ctx context.Context, id string, in ProviderInput) (ProviderView, error) {
	p, err := a.Store.Providers().Get(ctx, id)
	if err != nil {
		return ProviderView{}, err
	}
	if in.Name != "" {
		p.Name = in.Name
	}
	if in.Type != "" {
		p.Type = in.Type
	}
	if in.Config != nil {
		// Fields left showing their mask keep the stored credential.
		p.Config = MergeSecrets(a.Providers, p.Type, in.Config, p.Config)
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if f, err := a.Providers.MustGet(p.Type); err != nil {
		return ProviderView{}, err
	} else if err := f.ValidateConfig(p.Config); err != nil {
		return ProviderView{}, err
	}
	if err := a.Store.Providers().Update(ctx, p); err != nil {
		return ProviderView{}, err
	}
	a.validateAndCache(ctx, p)
	jobs, _ := a.Store.Jobs().List(ctx)
	return a.providerView(p, jobs), nil
}

// DeleteProvider removes a provider. It refuses when jobs still reference it.
func (a *App) DeleteProvider(ctx context.Context, id string) error {
	jobs, _ := a.Store.Jobs().List(ctx)
	inUse := 0
	for _, j := range jobs {
		if j.ProviderID == id {
			inUse++
		}
	}
	if inUse > 0 {
		return fmt.Errorf("provider is used by %d task(s); reassign them first", inUse)
	}
	return a.Store.Providers().Delete(ctx, id)
}

// ValidateProvider performs a live validation and caches the result.
func (a *App) ValidateProvider(ctx context.Context, id string) (ProviderView, error) {
	p, err := a.Store.Providers().Get(ctx, id)
	if err != nil {
		return ProviderView{}, err
	}
	zones, verr := a.validateAndCache(ctx, p)
	jobs, _ := a.Store.Jobs().List(ctx)
	view := a.providerView(p, jobs)
	if verr != nil {
		return view, verr
	}
	_ = zones
	return view, nil
}

// Zones lists a provider's zones (live).
func (a *App) Zones(ctx context.Context, id string) ([]domain.Zone, error) {
	p, err := a.Store.Providers().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	adapter, err := a.Providers.Build(p)
	if err != nil {
		return nil, err
	}
	return adapter.ListZones(ctx)
}

func (a *App) validateAndCache(ctx context.Context, p domain.Provider) ([]domain.Zone, error) {
	adapter, err := a.Providers.Build(p)
	if err != nil {
		a.setHealth(p.ID, providerHealth{status: "err", statusText: err.Error(), validatedAt: time.Now().UTC()})
		return nil, err
	}
	if err := adapter.Validate(ctx); err != nil {
		a.setHealth(p.ID, providerHealth{status: "err", statusText: err.Error(), validatedAt: time.Now().UTC()})
		return nil, err
	}
	zones, err := adapter.ListZones(ctx)
	names := make([]string, 0, len(zones))
	for _, z := range zones {
		names = append(names, z.Name)
	}
	if err != nil {
		a.setHealth(p.ID, providerHealth{status: "warn", statusText: "validated, but zones could not be listed", validatedAt: time.Now().UTC()})
		return nil, nil
	}
	a.setHealth(p.ID, providerHealth{status: "ok", statusText: "validated successfully", zones: names, validatedAt: time.Now().UTC()})
	return zones, nil
}

func (a *App) setHealth(id string, h providerHealth) {
	a.healthMu.Lock()
	a.health[id] = h
	a.healthMu.Unlock()
}

// SetProviderHealth primes the cached health of a provider. It is used when
// seeding the demo dataset (so the panel shows the designed states without
// contacting real providers) and by tests.
func (a *App) SetProviderHealth(id, status, statusText string, zones []string) {
	a.setHealth(id, providerHealth{status: status, statusText: statusText, zones: zones, validatedAt: time.Now().UTC()})
}

func (a *App) getHealth(id string) (providerHealth, bool) {
	a.healthMu.RLock()
	defer a.healthMu.RUnlock()
	h, ok := a.health[id]
	return h, ok
}

func (a *App) providerView(p domain.Provider, jobs []domain.Job) ProviderView {
	records := 0
	failed := false
	for _, j := range jobs {
		if j.ProviderID != p.ID {
			continue
		}
		records += len(j.Targets)
		if st, err := a.Store.States().Get(context.Background(), j.ID); err == nil && st.LastStatus == domain.StatusFailed {
			failed = true
		}
	}
	v := ProviderView{
		ID: p.ID, Name: p.Name, Type: p.Type, Enabled: p.Enabled,
		Config:  MaskConfig(a.Providers, p.Type, p.Config),
		Records: records, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	if h, ok := a.getHealth(p.ID); ok {
		v.Status = h.status
		v.StatusText = h.statusText
		v.Zones = h.zones
		v.LastValidated = h.validatedAt
	} else {
		v.Status = "ok"
		v.StatusText = "not validated yet"
		if !p.Enabled {
			v.Status = "mute"
			v.StatusText = "disabled"
		} else if failed {
			v.Status = "err"
			v.StatusText = "a task using this provider is failing"
		}
	}
	if !p.Enabled {
		v.Status = "mute"
	}
	return v
}

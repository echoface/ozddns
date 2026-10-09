package app

import (
	"context"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/provider"
)

// Bootstrap is the single payload the panel loads on start: everything the UI
// needs, already masked and denormalised.
type Bootstrap struct {
	Providers     []ProviderView           `json:"providers"`
	IPSources     []domain.IPSource        `json:"ipSources"`
	Jobs          []JobView                `json:"jobs"`
	Runs          []domain.Run             `json:"runs"`
	IP            PublicIP                 `json:"ip"`
	Status        RuntimeStatus            `json:"status"`
	Scheduler     SchedulerConfig          `json:"scheduler"`
	Settings      domain.Settings          `json:"settings"`
	ProviderTypes []provider.Meta          `json:"providerTypes"`
	SourceTypes   []ipsource.Meta          `json:"ipSourceTypes"`
	Interfaces    []ipsource.InterfaceInfo `json:"interfaces"`
}

// Bootstrap gathers the initial panel state.
func (a *App) Bootstrap(ctx context.Context) (Bootstrap, error) {
	providers, err := a.ListProviders(ctx)
	if err != nil {
		return Bootstrap{}, err
	}
	sources, _ := a.ListIPSources(ctx)
	jobs, err := a.ListJobs(ctx)
	if err != nil {
		return Bootstrap{}, err
	}
	runs, _ := a.ListRuns(ctx, "", "", 200)
	status, _ := a.Status(ctx)
	settings, _ := a.GetSettings(ctx)
	return Bootstrap{
		Providers: providers, IPSources: sources, Jobs: jobs, Runs: runs,
		IP: a.CurrentIP(ctx), Status: status, Scheduler: a.SchedulerConfig(ctx),
		Settings:      settings,
		ProviderTypes: a.Providers.Metas(), SourceTypes: a.Sources.Metas(),
		Interfaces: a.Interfaces(),
	}, nil
}

// Interfaces lists the local NICs and their usable addresses so the task
// wizard can offer a real picker instead of a free-text NIC name.
func (a *App) Interfaces() []ipsource.InterfaceInfo {
	list, err := ipsource.ListInterfaces()
	if err != nil {
		return []ipsource.InterfaceInfo{}
	}
	return list
}

// ProviderTypes returns the registered provider metadata.
func (a *App) ProviderTypes() []provider.Meta { return a.Providers.Metas() }

// SourceTypes returns the registered IP source metadata.
func (a *App) SourceTypes() []ipsource.Meta { return a.Sources.Metas() }

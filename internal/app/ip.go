package app

import (
	"context"
	"fmt"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

// IP family states returned by the Overview endpoint.
const (
	IPStatusOK       = "ok"
	IPStatusNoSource = "no_source"
	IPStatusError    = "error"
)

// IPFamilyResult describes the address read from one configured IP source.
type IPFamilyResult struct {
	Address      string     `json:"address"`
	SourceID     string     `json:"sourceId,omitempty"`
	Source       string     `json:"source,omitempty"`
	SourceTarget string     `json:"sourceTarget,omitempty"`
	Status       string     `json:"status"` // ok | no_source | error
	Error        string     `json:"error,omitempty"`
	CheckedAt    *time.Time `json:"checkedAt"`
}

// PublicIP is the address read from the source selected for each family.
type PublicIP struct {
	V4        IPFamilyResult `json:"v4"`
	V6        IPFamilyResult `json:"v6"`
	CheckedAt time.Time      `json:"checkedAt"`
}

// IPSourceSelection identifies the optional source chosen for each family.
// Presentation code owns this choice; the application only resolves it.
type IPSourceSelection struct {
	V4ID string
	V6ID string
}

// ipCacheTTL bounds how long a resolved address is served before a background
// probe refreshes it.
const ipCacheTTL = 30 * time.Second

// CurrentIP returns the default public address without blocking on the network.
// On a cold or stale cache it launches a background probe and returns whatever
// is cached (possibly zero on the very first call), so bootstrap stays fast even
// when a probe endpoint is slow or unreachable.
func (a *App) CurrentIP(ctx context.Context) PublicIP {
	a.ipMu.RLock()
	cached, filled := a.ipCache, a.ipFilled
	a.ipMu.RUnlock()
	if filled.IsZero() || time.Since(filled) > ipCacheTTL {
		go a.refreshIP()
	}
	return cached
}

// ResolveIP resolves selected sources synchronously. An empty selection uses
// the first configured source for each family.
func (a *App) ResolveIP(ctx context.Context, selection IPSourceSelection) PublicIP {
	return a.resolveIP(ctx, selection)
}

// refreshIP probes in the background, guarding against overlapping probes.
func (a *App) refreshIP() {
	a.ipMu.Lock()
	if a.ipFilling {
		a.ipMu.Unlock()
		return
	}
	a.ipFilling = true
	a.ipMu.Unlock()
	defer func() {
		a.ipMu.Lock()
		a.ipFilling = false
		a.ipMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a.storeIP(a.resolveIP(ctx, IPSourceSelection{}))
}

func (a *App) storeIP(out PublicIP) {
	a.ipMu.Lock()
	a.ipCache = out
	a.ipFilled = time.Now()
	a.ipMu.Unlock()
}

// resolveIP reads one source per family. The source choice is independent of
// sync tasks: an operator may inspect any configured source from the Overview,
// including one that is not currently used by a task.
func (a *App) resolveIP(ctx context.Context, selection IPSourceSelection) PublicIP {
	out := PublicIP{CheckedAt: time.Now().UTC()}
	sources, _ := a.Store.IPSources().List(ctx)
	out.V4 = a.resolveFamily(ctx, sources, "v4", selection.V4ID)
	out.V6 = a.resolveFamily(ctx, sources, "v6", selection.V6ID)
	return out
}

func (a *App) resolveFamily(ctx context.Context, sources []domain.IPSource, family, selectedID string) IPFamilyResult {
	var source *domain.IPSource
	for i := range sources {
		if selectedID != "" {
			if sources[i].ID == selectedID {
				source = &sources[i]
				break
			}
			continue
		}
		if sources[i].Family == family {
			source = &sources[i]
			break
		}
	}
	if source == nil {
		if selectedID == "" {
			return IPFamilyResult{Status: IPStatusNoSource}
		}
		checkedAt := time.Now().UTC()
		return IPFamilyResult{
			SourceID: selectedID, Status: IPStatusError,
			Error: "IP source not found", CheckedAt: &checkedAt,
		}
	}
	if source.Family != family {
		checkedAt := time.Now().UTC()
		return IPFamilyResult{
			SourceID: source.ID, Source: source.Name, SourceTarget: source.Target,
			Status:    IPStatusError,
			Error:     fmt.Sprintf("IP source %q provides %s addresses, not %s", source.Name, source.Family, family),
			CheckedAt: &checkedAt,
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	checkedAt := time.Now().UTC()
	result := IPFamilyResult{
		SourceID:     source.ID,
		Source:       source.Name,
		SourceTarget: source.Target,
		CheckedAt:    &checkedAt,
	}
	addr, err := a.Sources.Resolve(ctx, *source)
	if err != nil {
		result.Status = IPStatusError
		result.Error = err.Error()
		return result
	}
	result.Address = addr.String()
	result.Status = IPStatusOK
	return result
}

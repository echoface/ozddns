package app

import (
	"context"
	"sort"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

// IP family states returned by the Overview endpoint. A family is only probed
// when an enabled task actually references a source for that family.
const (
	IPStatusOK            = "ok"
	IPStatusNoActiveTasks = "no_active_tasks"
	IPStatusError         = "error"
)

// IPFamilyResult describes the address currently used by OzDDNS for one
// family, including the source and the tasks that depend on that source.
type IPFamilyResult struct {
	Address      string     `json:"address"`
	SourceID     string     `json:"sourceId,omitempty"`
	Source       string     `json:"source,omitempty"`
	SourceTarget string     `json:"sourceTarget,omitempty"`
	Tasks        int        `json:"tasks"`
	Status       string     `json:"status"` // ok | no_active_tasks | error
	Error        string     `json:"error,omitempty"`
	CheckedAt    *time.Time `json:"checkedAt"`
}

// PublicIP is the address OzDDNS currently manages per family. It deliberately
// reflects enabled tasks rather than an arbitrary configured source: an unused
// source must never make the Overview look healthy or unhealthy.
type PublicIP struct {
	V4        IPFamilyResult `json:"v4"`
	V6        IPFamilyResult `json:"v6"`
	CheckedAt time.Time      `json:"checkedAt"`
}

// ipCacheTTL bounds how long a resolved address is served before a background
// probe refreshes it.
const ipCacheTTL = 30 * time.Second

// CurrentIP returns the public address without blocking on the network. On a
// cold or stale cache it launches a background probe and returns whatever is
// cached (possibly zero on the very first call), so bootstrap stays fast even
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

// ResolveIP resolves the public address synchronously, caches it and returns it.
// It backs the panel's explicit "Refresh IP" action, where blocking is expected.
func (a *App) ResolveIP(ctx context.Context) PublicIP {
	out := a.resolveIP(ctx)
	a.storeIP(out)
	return out
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
	a.storeIP(a.resolveIP(ctx))
}

func (a *App) storeIP(out PublicIP) {
	a.ipMu.Lock()
	a.ipCache = out
	a.ipFilled = time.Now()
	a.ipMu.Unlock()
}

type ipCandidate struct {
	source      domain.IPSource
	tasks       int
	lastRun     time.Time
	lastSuccess time.Time
	missing     bool
}

// resolveIP resolves the primary source used by enabled tasks. Multiple task
// sources may exist for one family; candidates are ordered by recent task
// success so the Overview follows the source that is actually managing DNS.
// Failed candidates fall through to the next one, which prevents a stale or
// obsolete source from blanking a family that another active task uses.
func (a *App) resolveIP(ctx context.Context) PublicIP {
	out := PublicIP{CheckedAt: time.Now().UTC()}
	sources, _ := a.Store.IPSources().List(ctx)
	jobs, _ := a.Store.Jobs().List(ctx)
	states, _ := a.Store.States().List(ctx)

	sourceByID := make(map[string]domain.IPSource, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	stateByJob := make(map[string]domain.JobState, len(states))
	for _, state := range states {
		stateByJob[state.JobID] = state
	}

	candidates := map[string]map[string]*ipCandidate{"v4": {}, "v6": {}}
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		source, ok := sourceByID[job.IPSourceID]
		family := jobFamily(job)
		key := job.IPSourceID
		if ok && (source.Family == "v4" || source.Family == "v6") {
			family = source.Family
		} else if !ok {
			key = "missing:" + job.IPSourceID
		}
		if family != "v4" && family != "v6" {
			continue
		}
		candidate := candidates[family][key]
		if candidate == nil {
			candidate = &ipCandidate{source: source, missing: !ok}
			if !ok {
				candidate.source.ID = job.IPSourceID
				candidate.source.Name = job.IPSourceID
				candidate.source.Family = family
			}
			candidates[family][key] = candidate
		}
		candidate.tasks++
		if state, ok := stateByJob[job.ID]; ok {
			if state.LastRunAt.After(candidate.lastRun) {
				candidate.lastRun = state.LastRunAt
			}
			if (state.LastStatus == domain.StatusSuccess || state.LastStatus == domain.StatusNoChange) &&
				state.LastRunAt.After(candidate.lastSuccess) {
				candidate.lastSuccess = state.LastRunAt
			}
		}
	}

	out.V4 = a.resolveFamily(ctx, sortedCandidates(candidates["v4"]))
	out.V6 = a.resolveFamily(ctx, sortedCandidates(candidates["v6"]))
	return out
}

func (a *App) resolveFamily(ctx context.Context, candidates []*ipCandidate) IPFamilyResult {
	if len(candidates) == 0 {
		return IPFamilyResult{Status: IPStatusNoActiveTasks}
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var firstErr string
	for _, candidate := range candidates {
		checkedAt := time.Now().UTC()
		result := IPFamilyResult{
			SourceID:     candidate.source.ID,
			Source:       candidate.source.Name,
			SourceTarget: candidate.source.Target,
			Tasks:        candidate.tasks,
			CheckedAt:    &checkedAt,
		}
		if candidate.missing {
			if firstErr == "" {
				firstErr = "IP source not found"
			}
			result.Status = IPStatusError
			result.Error = firstErr
			continue
		}
		addr, err := a.Sources.Resolve(ctx, candidate.source)
		if err == nil {
			result.Address = addr.String()
			result.Status = IPStatusOK
			return result
		}
		if firstErr == "" {
			firstErr = err.Error()
		}
		result.Status = IPStatusError
		result.Error = err.Error()
	}

	// Every candidate failed. Return the primary candidate rather than an
	// empty source so the panel can say which source needs attention.
	primary := candidates[0]
	checkedAt := time.Now().UTC()
	result := IPFamilyResult{
		SourceID:     primary.source.ID,
		Source:       primary.source.Name,
		SourceTarget: primary.source.Target,
		Tasks:        primary.tasks,
		Status:       IPStatusError,
		Error:        firstErr,
		CheckedAt:    &checkedAt,
	}
	if primary.missing {
		result.Error = "IP source not found"
	}
	return result
}

func sortedCandidates(byID map[string]*ipCandidate) []*ipCandidate {
	out := make([]*ipCandidate, 0, len(byID))
	for _, candidate := range byID {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if !left.lastSuccess.Equal(right.lastSuccess) {
			return left.lastSuccess.After(right.lastSuccess)
		}
		if !left.lastRun.Equal(right.lastRun) {
			return left.lastRun.After(right.lastRun)
		}
		if !left.source.CreatedAt.Equal(right.source.CreatedAt) {
			return left.source.CreatedAt.Before(right.source.CreatedAt)
		}
		return left.source.ID < right.source.ID
	})
	return out
}

func jobFamily(job domain.Job) string {
	for _, target := range job.Targets {
		if target.Type.IsAddressRecord() {
			return target.Type.Family()
		}
	}
	return ""
}

package app

import (
	"context"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

// PublicIP is the host's currently observed address per family.
type PublicIP struct {
	V4        string    `json:"v4"`
	V6        string    `json:"v6"`
	V4Source  string    `json:"v4Source"`
	V6Source  string    `json:"v6Source"`
	CheckedAt time.Time `json:"checkedAt"`
}

// default sources used when none configured.
var defaultSources = map[string]domain.IPSource{
	"v4": {Type: domain.IPHTTP, Family: "v4", Target: "https://api.ipify.org"},
	"v6": {Type: domain.IPHTTP, Family: "v6", Target: "https://v6.ifconfig.co"},
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

// resolveIP resolves the host's public address using configured sources (or the
// built-in defaults).
func (a *App) resolveIP(ctx context.Context) PublicIP {
	out := PublicIP{CheckedAt: time.Now().UTC()}
	sources, _ := a.Store.IPSources().List(ctx)
	pick := func(family string) (domain.IPSource, string, bool) {
		for _, s := range sources {
			if s.Enabled && s.Family == family {
				return s, s.Name, true
			}
		}
		if d, ok := defaultSources[family]; ok {
			return d, "default", true
		}
		return domain.IPSource{}, "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if s, name, ok := pick("v4"); ok {
		if addr, err := a.Sources.Resolve(ctx, s); err == nil {
			out.V4, out.V4Source = addr.String(), name
		}
	}
	if s, name, ok := pick("v6"); ok {
		if addr, err := a.Sources.Resolve(ctx, s); err == nil {
			out.V6, out.V6Source = addr.String(), name
		}
	}
	return out
}

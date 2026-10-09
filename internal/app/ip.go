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

// CurrentIP resolves the host's public address using configured sources (or the
// built-in defaults).
func (a *App) CurrentIP(ctx context.Context) PublicIP {
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

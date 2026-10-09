// Package ipsource resolves the address a host currently holds. Sources are
// pluggable exactly like DNS providers: a factory per kind, registered once.
package ipsource

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"sync"

	"github.com/echoface/ozddns/internal/domain"
)

// Meta describes an IP source kind for the panel.
type Meta struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
	Icon  string `json:"icon"`
}

// Source resolves an address for a family ("v4" or "v6").
type Source interface {
	Get(ctx context.Context, family string) (netip.Addr, error)
}

// Factory builds a Source from a target string.
type Factory interface {
	Meta() Meta
	New(target string) (Source, error)
}

// Registry maps source kinds to factories.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
	order     []string
}

// NewRegistry returns a registry with the built-in source kinds registered.
func NewRegistry() *Registry {
	r := &Registry{factories: map[string]Factory{}}
	registerBuiltins(r)
	return r
}

// Register adds a factory.
func (r *Registry) Register(f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := f.Meta().ID
	if _, ok := r.factories[id]; !ok {
		r.order = append(r.order, id)
	}
	r.factories[id] = f
}

// Build constructs a Source for a stored IP source entity.
func (r *Registry) Build(s domain.IPSource) (Source, error) {
	r.mu.RLock()
	f, ok := r.factories[string(s.Type)]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown ip source type %q", s.Type)
	}
	return f.New(s.Target)
}

// Resolve is a convenience that builds and immediately resolves.
func (r *Registry) Resolve(ctx context.Context, s domain.IPSource) (netip.Addr, error) {
	src, err := r.Build(s)
	if err != nil {
		return netip.Addr{}, err
	}
	return src.Get(ctx, s.Family)
}

// Metas returns every registered source kind, sorted by id.
func (r *Registry) Metas() []Meta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := append([]string(nil), r.order...)
	sort.Strings(ids)
	out := make([]Meta, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.factories[id].Meta())
	}
	return out
}

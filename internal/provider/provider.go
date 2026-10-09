// Package provider defines the DNS provider abstraction and the registry that
// maps a provider "type" to a factory. Adding a provider means writing a package
// that implements Factory and registering it in RegisterAll — never editing a
// central switch.
package provider

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/echoface/ozddns/internal/domain"
)

// FieldType controls how the panel renders a configuration field.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldSecret   FieldType = "secret"
	FieldBool     FieldType = "bool"
	FieldNumber   FieldType = "number"
	FieldSelect   FieldType = "select"
	FieldTextarea FieldType = "textarea"
)

// Field describes one provider configuration input.
type Field struct {
	Key      string    `json:"key"`
	Label    string    `json:"label"`
	Type     FieldType `json:"type"`
	Required bool      `json:"required,omitempty"`
	Secret   bool      `json:"secret,omitempty"`
	Help     string    `json:"help,omitempty"`
	Default  any       `json:"default,omitempty"`
	Options  []string  `json:"options,omitempty"`
	Mono     bool      `json:"mono,omitempty"`
}

// Meta is the static description of a provider type, surfaced to the panel so
// the "add provider" form can render itself without hard-coded knowledge.
type Meta struct {
	Type            string  `json:"id"`
	Label           string  `json:"label"`
	Desc            string  `json:"desc"`
	Color           string  `json:"color"`
	Tag             string  `json:"tag"`
	SupportsProxied bool    `json:"supportsProxied"`
	Fields          []Field `json:"fields"`
}

// Provider is the behaviour every DNS provider adapter implements. The engine
// owns reconciliation; a provider only reads and applies.
type Provider interface {
	// Validate checks credentials and connectivity.
	Validate(ctx context.Context) error
	// ListZones returns the zones the account can manage.
	ListZones(ctx context.Context) ([]domain.Zone, error)
	// ListRecords returns records matching the filter inside a zone.
	ListRecords(ctx context.Context, zone string, f domain.RecordFilter) ([]domain.Record, error)
	// Apply creates, updates or deletes a record.
	Apply(ctx context.Context, change domain.Change) error
}

// Factory builds provider instances from raw config and exposes their metadata.
type Factory interface {
	Meta() Meta
	ValidateConfig(cfg map[string]any) error
	New(cfg map[string]any) (Provider, error)
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// Registry is the runtime catalogue of provider factories.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
	order     []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

// Register adds a factory (last registration wins for a given type).
func (r *Registry) Register(f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := f.Meta().Type
	if _, ok := r.factories[t]; !ok {
		r.order = append(r.order, t)
	}
	r.factories[t] = f
}

// Get returns the factory for a type.
func (r *Registry) Get(t string) (Factory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[t]
	return f, ok
}

// MustGet returns the factory or an error describing the unknown type.
func (r *Registry) MustGet(t string) (Factory, error) {
	f, ok := r.Get(t)
	if !ok {
		return nil, fmt.Errorf("unknown provider type %q", t)
	}
	return f, nil
}

// Build constructs a provider instance for a stored provider entity.
func (r *Registry) Build(p domain.Provider) (Provider, error) {
	f, err := r.MustGet(p.Type)
	if err != nil {
		return nil, err
	}
	return f.New(p.Config)
}

// Metas returns every registered provider type, in registration order.
func (r *Registry) Metas() []Meta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Meta, 0, len(r.order))
	for _, t := range r.order {
		out = append(out, r.factories[t].Meta())
	}
	return out
}

// Types returns the registered type ids, sorted.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]string(nil), r.order...)
	sort.Strings(out)
	return out
}

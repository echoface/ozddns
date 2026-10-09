// Package providers wires every compiled-in provider adapter into a registry.
// Adding a provider is: create the adapter package, then add one Register line
// here. The engine, API and CLI never need to change.
package providers

import (
	"github.com/echoface/ozddns/internal/provider"
	"github.com/echoface/ozddns/internal/provider/alidns"
	"github.com/echoface/ozddns/internal/provider/cloudflare"
	"github.com/echoface/ozddns/internal/provider/custom"
	"github.com/echoface/ozddns/internal/provider/desec"
	"github.com/echoface/ozddns/internal/provider/dnspod"
	"github.com/echoface/ozddns/internal/provider/duckdns"
	"github.com/echoface/ozddns/internal/provider/godaddy"
	"github.com/echoface/ozddns/internal/provider/namecheap"
	"github.com/echoface/ozddns/internal/provider/vercel"
)

// RegisterAll registers every built-in provider factory.
func RegisterAll(r *provider.Registry) {
	r.Register(cloudflare.Factory{})
	r.Register(alidns.Factory{})
	r.Register(dnspod.Factory{})
	r.Register(duckdns.Factory{})
	r.Register(namecheap.Factory{})
	r.Register(godaddy.Factory{})
	r.Register(desec.Factory{})
	r.Register(vercel.Factory{})
	r.Register(custom.Factory{})
}

// NewRegistry returns a registry with all built-in providers registered.
func NewRegistry() *provider.Registry {
	r := provider.NewRegistry()
	RegisterAll(r)
	return r
}

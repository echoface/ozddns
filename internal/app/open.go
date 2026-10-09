package app

import (
	"context"
	"log/slog"

	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/providers"
	"github.com/echoface/ozddns/internal/seed"
	"github.com/echoface/ozddns/internal/storage/sqlite"
)

// Options configures how a full application is opened.
type Options struct {
	DBPath       string
	Logger       *slog.Logger
	SeedDefaults bool // insert safe default IP sources + settings on a fresh DB
	Demo         bool // insert the design-prototype dataset (first run only)
}

// Open wires a complete application (SQLite store + registries + engine) from
// options. This is the single wiring path shared by the daemon and the CLI, so
// both pieces of software behave identically. The returned closer releases the
// database.
func Open(ctx context.Context, opts Options) (*App, func(), error) {
	store, err := sqlite.Open(opts.DBPath)
	if err != nil {
		return nil, nil, err
	}
	a := New(store, providers.NewRegistry(), ipsource.NewRegistry(), opts.Logger)
	a.DBDriver = "sqlite"
	a.DBPath = opts.DBPath

	if opts.SeedDefaults {
		if err := seed.EnsureDefaults(ctx, store); err != nil {
			_ = store.Close()
			return nil, nil, err
		}
	}
	if opts.Demo {
		if err := seed.Demo(ctx, store); err != nil {
			_ = store.Close()
			return nil, nil, err
		}
		seed.PrimeDemoHealth(a)
	}
	return a, func() { _ = store.Close() }, nil
}

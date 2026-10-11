package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/echoface/ozddns/internal/app"
	"github.com/echoface/ozddns/internal/config"
	"github.com/echoface/ozddns/internal/server"
	"github.com/echoface/ozddns/internal/version"
	"github.com/echoface/ozddns/web"
)

func (e *env) cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(e.err)
	listen := fs.String("listen", "", "address to listen on (overrides OZDDNS_LISTEN and config.yaml)")
	basePath := fs.String("base-path", "", "public prefix when mounted behind a reverse proxy sub-path, e.g. /ddns (overrides OZDDNS_BASE_PATH and config.yaml)")
	configPath := fs.String("config", "", "path to config.yaml (overrides OZDDNS_CONFIG)")
	demo := fs.Bool("demo", false, "seed the demo dataset on first run")
	noScheduler := fs.Bool("no-scheduler", false, "start with scheduling paused")
	logLevel := fs.String("log-level", "", "log level: debug | info | warn | error (default info)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// --config may also be given after the subcommand; re-resolve if so.
	if *configPath != "" && *configPath != e.configPath {
		e.configPath = *configPath
		e.allowCreate = true
		if err := e.finalize(); err != nil {
			fmt.Fprintln(e.err, "error:", err)
			return 2
		}
	}

	// Validate the public prefix before anything touches the filesystem or the
	// database, so a typo fails immediately instead of after a config file has
	// been written. The value is deployment configuration: one build serves a
	// root mount and any sub-path, decided here at start-up. The daemon strips
	// the prefix from incoming requests and tells the panel about it, so the
	// reverse proxy can forward everything untouched.
	base, baseSource, err := e.resolveBasePath(*basePath)
	if err != nil {
		fmt.Fprintln(e.err, "error:", err)
		return 2
	}

	level := slog.LevelInfo
	switch strings.ToLower(e.resolveLogLevel(*logLevel)) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(e.err, &slog.HandlerOptions{Level: level}))

	// A missing config file is created with the documented defaults so a fresh
	// install has something to edit. A read-only directory is not fatal.
	if e.cfg == nil {
		if created, err := config.WriteDefault(e.cfgPath); err != nil {
			logger.Warn("could not create config file", "path", e.cfgPath, "err", err)
		} else if created {
			fmt.Fprintf(e.out, "created config file %s\n", e.cfgPath)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, closeFn, err := app.Open(ctx, app.Options{
		DBPath: e.dbPath, Logger: logger, SeedDefaults: true, Demo: *demo,
	})
	if err != nil {
		fmt.Fprintln(e.err, "error:", err)
		return 1
	}
	defer closeFn()

	// The bind address is deployment configuration, not runtime state: it comes
	// from --listen, OZDDNS_LISTEN or config.yaml (in that order), never the
	// database. Editing it from the panel would drop the very connection you are
	// using, so the panel only shows it.
	addr, source := e.resolveListen(*listen)
	a.ListenAddr = addr
	a.ListenSource = source

	a.BasePath = base
	a.BasePathSource = baseSource

	if !*noScheduler {
		a.Start(ctx)
	} else {
		a.SetRunning(false)
		a.Start(ctx)
	}

	srv := server.New(a, web.Handler(base), logger)
	srv.BasePath = base
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Fprintf(e.out, "OzDDNS %s — panel on http://%s%s/  (db: %s)\n", version.Version, addr, strings.TrimSuffix(base, "/"), e.dbPath)
	if !strings.HasPrefix(addr, "127.") && !strings.HasPrefix(addr, "localhost") && !strings.HasPrefix(addr, "[::1]") {
		logger.Warn("listening on a non-loopback address; make sure a password is set", "addr", addr)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		fmt.Fprintln(e.err, "error:", err)
		return 1
	case <-ctx.Done():
		fmt.Fprintln(e.out, "shutting down…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		a.Scheduler.Stop()
		return 0
	}
}

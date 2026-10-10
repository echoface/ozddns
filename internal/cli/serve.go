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
	"github.com/echoface/ozddns/internal/server"
	"github.com/echoface/ozddns/internal/version"
	"github.com/echoface/ozddns/web"
)

func (e *env) cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(e.err)
	listen := fs.String("listen", "", "address to listen on (default from settings, else 127.0.0.1:9876)")
	demo := fs.Bool("demo", false, "seed the demo dataset on first run")
	noScheduler := fs.Bool("no-scheduler", false, "start with scheduling paused")
	logLevel := fs.String("log-level", "info", "log level: debug | info | warn | error")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	level := slog.LevelInfo
	switch strings.ToLower(*logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(e.err, &slog.HandlerOptions{Level: level}))

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

	addr := *listen
	if addr == "" {
		if s, err := a.GetSettings(ctx); err == nil && s.Listen != "" {
			addr = s.Listen
		} else {
			addr = "127.0.0.1:9876"
		}
	}
	a.ListenAddr = addr

	if !*noScheduler {
		a.Start(ctx)
	} else {
		a.SetRunning(false)
		a.Start(ctx)
	}

	srv := server.New(a, web.Handler(), logger)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Fprintf(e.out, "OzDDNS %s — panel on http://%s/  (db: %s)\n", version.Version, addr, e.dbPath)
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

// Package cli implements the ozddns command-line interface. It is a thin shell
// over the application layer (internal/app) — the same use-cases the REST API
// serves — so the CLI never round-trips through HTTP and behaves identically.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/echoface/ozddns/internal/app"
	"github.com/echoface/ozddns/internal/config"
	"github.com/echoface/ozddns/internal/version"
)

const usage = `ozddns — provider-centric dynamic DNS manager

Usage:
  ozddns <command> [flags]

Commands:
  serve            Run the daemon: REST API + embedded web panel
  health           Probe a running daemon's /healthz endpoint
  version          Print version and build information
  status           Show runtime and reconciliation summary
  ip               Resolve the host's current public address
  provider         Manage DNS providers (list|add|validate|zones|rm)
  source           Manage IP sources      (list|add|test|rm)
  job              Manage sync tasks      (list|show|add|run|enable|disable|rm)
  run              Inspect execution history (list|show)
  config           Export or import configuration (export|import)

Global flags (before the command):
  --db <path>      Path to the SQLite database (default ./ozddns.db)
  --config <path>  Path to config.yaml (default ./config.yaml; created on first serve)
  --json           Emit machine-readable JSON where supported
  -h, --help       Show help

Deployment settings are resolved with one precedence everywhere:
  flag > environment > config.yaml > built-in default.
  --listen / OZDDNS_LISTEN and --db / OZDDNS_DB work the same way as in serve.
  A missing config file is written with defaults the first time serve runs.

Run "ozddns <command> -h" for command-specific flags.`

// Run executes the CLI and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	e := &env{out: stdout, err: stderr}

	// Parse leading global flags so `ozddns --db x job list` also works.
	rest, err := e.parseGlobal(args)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	if len(rest) == 0 {
		fmt.Fprintln(stdout, usage)
		return 0
	}

	switch rest[0] {
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, usage)
		return 0
	case "version", "--version", "-v":
		return e.cmdVersion(rest[1:])
	}

	// Everything else may need the database path and deployment defaults, which
	// come from config.yaml / environment.
	e.allowCreate = rest[0] == "serve"
	if err := e.finalize(); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}

	switch rest[0] {
	case "serve":
		return e.cmdServe(rest[1:])
	case "health":
		return e.cmdHealth(rest[1:])
	case "status":
		return e.cmdStatus(rest[1:])
	case "ip":
		return e.cmdIP(rest[1:])
	case "provider":
		return e.cmdProvider(rest[1:])
	case "source":
		return e.cmdSource(rest[1:])
	case "job":
		return e.cmdJob(rest[1:])
	case "run":
		return e.cmdRun(rest[1:])
	case "config":
		return e.cmdConfig(rest[1:])
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s\n", rest[0], usage)
		return 2
	}
}

type env struct {
	out        io.Writer
	err        io.Writer
	dbPath     string // resolved after finalize
	dbFlag     string // explicit --db, if any
	configPath string // explicit --config, if any
	cfgPath    string // resolved config path
	cfg        *config.File
	// allowCreate lets a missing config file be tolerated (serve creates it)
	// instead of failing: a typo'd --config on a one-off command is still an error.
	allowCreate bool
	json        bool
}

// parseGlobal consumes --db/--config/--json before the command, returning the rest.
func (e *env) parseGlobal(args []string) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--db":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--db needs a value")
			}
			i++
			e.dbFlag = args[i]
		case strings.HasPrefix(a, "--db="):
			e.dbFlag = strings.TrimPrefix(a, "--db=")
		case a == "--config":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--config needs a value")
			}
			i++
			e.configPath = args[i]
		case strings.HasPrefix(a, "--config="):
			e.configPath = strings.TrimPrefix(a, "--config=")
		case a == "--json":
			e.json = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, nil
}

const (
	defaultDBPath     = "./ozddns.db"
	defaultListenAddr = "127.0.0.1:9876"
	defaultConfigPath = "./config.yaml"
)

// finalize loads config.yaml (if present) and resolves the database path using
// the shared precedence: flag > environment > config file > default.
func (e *env) finalize() error {
	path := e.configPath
	explicit := path != ""
	if path == "" {
		path = os.Getenv("OZDDNS_CONFIG")
		explicit = path != ""
	}
	if path == "" {
		path = defaultConfigPath
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if cfg == nil && explicit && !e.allowCreate {
		return fmt.Errorf("config file %s not found", path)
	}
	e.cfg = cfg
	e.cfgPath = path

	switch {
	case e.dbFlag != "":
		e.dbPath = e.dbFlag
	case os.Getenv("OZDDNS_DB") != "":
		e.dbPath = os.Getenv("OZDDNS_DB")
	case cfg != nil && cfg.DB != "":
		e.dbPath = cfg.DB
	default:
		e.dbPath = defaultDBPath
	}
	return nil
}

// resolveListen picks the listen address from flag > environment > config file
// > default, and reports which source won so the panel can show it.
func (e *env) resolveListen(flagValue string) (addr, source string) {
	if flagValue != "" {
		return flagValue, "flag"
	}
	if v := os.Getenv("OZDDNS_LISTEN"); v != "" {
		return v, "env"
	}
	if e.cfg != nil && e.cfg.Listen != "" {
		return e.cfg.Listen, "config"
	}
	return defaultListenAddr, "default"
}

// resolveLogLevel applies the same precedence to the log level.
func (e *env) resolveLogLevel(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("OZDDNS_LOG_LEVEL"); v != "" {
		return v
	}
	if e.cfg != nil && e.cfg.LogLevel != "" {
		return e.cfg.LogLevel
	}
	return "info"
}

// withApp runs fn with an application, closing the store afterwards.
func (e *env) withApp(fn func(context.Context, *app.App) int) int {
	a, closeFn, err := app.Open(context.Background(), app.Options{DBPath: e.dbPath, SeedDefaults: true})
	if err != nil {
		fmt.Fprintln(e.err, "error:", err)
		return 1
	}
	defer closeFn()
	a.ListenAddr, a.ListenSource = e.resolveListen("")
	return fn(context.Background(), a)
}

// printJSON writes a value as pretty JSON.
func (e *env) printJSON(v any) {
	enc := json.NewEncoder(e.out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func (e *env) cmdVersion(args []string) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if e.json {
		e.printJSON(map[string]string{"version": version.Version, "build": version.Build, "commit": version.Commit})
		return 0
	}
	fmt.Fprintf(e.out, "ozddns %s (build %s, commit %s)\n", version.Version, version.Build, version.Commit)
	return 0
}

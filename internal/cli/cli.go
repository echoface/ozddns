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
	"github.com/echoface/ozddns/internal/version"
)

const usage = `ozddns — provider-centric dynamic DNS manager

Usage:
  ozddns <command> [flags]

Commands:
  serve            Run the daemon: REST API + embedded web panel
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
  --json           Emit machine-readable JSON where supported
  -h, --help       Show help

Run "ozddns <command> -h" for command-specific flags.`

// Run executes the CLI and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	e := &env{out: stdout, err: stderr, dbPath: defaultDBPath()}

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
	case "serve":
		return e.cmdServe(rest[1:])
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
	out    io.Writer
	err    io.Writer
	dbPath string
	json   bool
}

// parseGlobal consumes --db/--json/-h before the command, returning the rest.
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
			e.dbPath = args[i]
		case strings.HasPrefix(a, "--db="):
			e.dbPath = strings.TrimPrefix(a, "--db=")
		case a == "--json":
			e.json = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, nil
}

func defaultDBPath() string {
	if v := os.Getenv("OZDDNS_DB"); v != "" {
		return v
	}
	return "./ozddns.db"
}

// withApp runs fn with an application, closing the store afterwards.
func (e *env) withApp(fn func(context.Context, *app.App) int) int {
	a, closeFn, err := app.Open(context.Background(), app.Options{DBPath: e.dbPath, SeedDefaults: true})
	if err != nil {
		fmt.Fprintln(e.err, "error:", err)
		return 1
	}
	defer closeFn()
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

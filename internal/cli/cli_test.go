package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersionAndUsage(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != 0 || !strings.Contains(out, "ozddns") {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
	if code, out, _ := run(t); code != 0 || !strings.Contains(out, "Usage:") {
		t.Fatalf("bare invocation should print usage: %d %q", code, out)
	}
	if code, _, _ := run(t, "nonsense"); code != 2 {
		t.Fatalf("unknown command code = %d, want 2", code)
	}
}

func TestConfigFileProvidesDefaults(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "from-config.db")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("db: "+dbPath+"\nlisten: 127.0.0.1:4321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The database path from config.yaml is used when no flag/env is present.
	if code, _, errOut := run(t, "--config", cfgPath, "provider", "list"); code != 0 {
		t.Fatalf("provider list: %d %s", code, errOut)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("config db path was not used: %v", err)
	}
	// status reports the resolved listen address and its source.
	_, out, _ := run(t, "--config", cfgPath, "status")
	if !strings.Contains(out, "127.0.0.1:4321 (config)") {
		t.Fatalf("status listen = %q", out)
	}
}

func TestFlagBeatsEnvAndConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("db: "+filepath.Join(dir, "config.db")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	flagDB := filepath.Join(dir, "flag.db")
	t.Setenv("OZDDNS_DB", filepath.Join(dir, "env.db"))
	if code, _, errOut := run(t, "--config", cfgPath, "--db", flagDB, "provider", "list"); code != 0 {
		t.Fatalf("provider list: %d %s", code, errOut)
	}
	if _, err := os.Stat(flagDB); err != nil {
		t.Fatalf("--db should win over env/config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "env.db")); err == nil {
		t.Fatal("env db should not be used when --db is set")
	}
}

func TestExplicitMissingConfigIsAnErrorForOneOffCommands(t *testing.T) {
	// A typo'd --config on a non-serve command should be surfaced, not hidden.
	missing := filepath.Join(t.TempDir(), "typo.yaml")
	code, _, errOut := run(t, "--config", missing, "status")
	if code != 2 || !strings.Contains(errOut, "not found") {
		t.Fatalf("expected a not-found error, got code=%d err=%q", code, errOut)
	}
}

func TestHealthCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	if code, out, errOut := run(t, "health", "--listen", addr); code != 0 {
		t.Fatalf("health against a live server: %d %s %s", code, out, errOut)
	}
	// A wildcard bind address is probed through loopback; a closed port fails.
	if code, _, _ := run(t, "health", "--listen", "0.0.0.0:1", "--timeout", "500ms"); code != 1 {
		t.Fatalf("health against a closed port = %d, want 1", code)
	}
}

func TestCommandFlow(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cli.db")

	if code, _, errOut := run(t, "--db", db, "provider", "add", "--type", "custom", "--name", "Hook", "--set", "url=http://127.0.0.1:9/h"); code != 0 {
		t.Fatalf("provider add: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "--db", db, "source", "add", "--name", "Fixed", "--type", "static", "--family", "v4", "--target", "203.0.113.9"); code != 0 {
		t.Fatalf("source add: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "--db", db, "job", "add", "--name", "Home", "--provider", "Hook", "--source", "Fixed", "--target", "home.example.com:A:300"); code != 0 {
		t.Fatalf("job add: %d %s", code, errOut)
	}

	_, out, _ := run(t, "--db", db, "--json", "job", "list")
	var jobs []map[string]any
	if err := json.Unmarshal([]byte(out), &jobs); err != nil {
		t.Fatalf("job list --json: %v (%s)", err, out)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	jobID, _ := jobs[0]["id"].(string)

	// dry-run reports a create without writing.
	_, out, _ = run(t, "--db", db, "--json", "job", "run", "--dry-run", jobID)
	if !strings.Contains(out, `"action": "create"`) {
		t.Fatalf("dry-run output = %s", out)
	}

	// run now fails (nothing is listening on :9) but the run is recorded.
	if code, _, _ := run(t, "--db", db, "job", "run", jobID); code != 1 {
		t.Fatalf("failing run should exit 1")
	}
	_, out, _ = run(t, "--db", db, "--json", "run", "list")
	var runs []map[string]any
	if err := json.Unmarshal([]byte(out), &runs); err != nil {
		t.Fatalf("run list --json: %v (%s)", err, out)
	}
	if len(runs) != 1 || runs[0]["status"] != "failed" {
		t.Fatalf("runs = %v", runs)
	}

	// status reflects the recorded run.
	_, out, _ = run(t, "--db", db, "--json", "status")
	if !strings.Contains(out, `"runsFailed": 1`) {
		t.Fatalf("status output = %s", out)
	}
}

func TestConfigExportImport(t *testing.T) {
	dir := t.TempDir()
	dbA := filepath.Join(dir, "a.db")
	if code, _, e := run(t, "--db", dbA, "provider", "add", "--type", "custom", "--name", "Hook", "--set", "url=http://x"); code != 0 {
		t.Fatalf("provider add: %d %s", code, e)
	}
	bundle := filepath.Join(dir, "bundle.json")
	if code, _, e := run(t, "--db", dbA, "config", "export", "--out", bundle); code != 0 {
		t.Fatalf("export: %d %s", code, e)
	}
	dbB := filepath.Join(dir, "b.db")
	if code, out, e := run(t, "--db", dbB, "config", "import", bundle); code != 0 || !strings.Contains(out, "imported 1 providers") {
		t.Fatalf("import: %d %q %s", code, out, e)
	}
	_, out, _ := run(t, "--db", dbB, "--json", "provider", "list")
	if !strings.Contains(out, "Hook") {
		t.Fatalf("imported provider missing: %s", out)
	}
}

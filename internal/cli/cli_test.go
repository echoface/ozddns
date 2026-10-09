package cli

import (
	"bytes"
	"encoding/json"
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

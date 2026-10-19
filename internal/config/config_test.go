package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseYAMLSubset(t *testing.T) {
	f, err := Parse([]byte(`
# how the process runs
listen: 0.0.0.0:9876
db: /data/ozddns.db   # inline comment
logLevel: "debug"
unknownKey: ignored
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Listen != "0.0.0.0:9876" {
		t.Fatalf("listen = %q", f.Listen)
	}
	if f.DB != "/data/ozddns.db" {
		t.Fatalf("db = %q", f.DB)
	}
	if f.LogLevel != "debug" {
		t.Fatalf("logLevel = %q", f.LogLevel)
	}
}

func TestParseJSON(t *testing.T) {
	f, err := Parse([]byte(`{"listen": ":8080", "db": "x.db"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Listen != ":8080" || f.DB != "x.db" {
		t.Fatalf("parsed = %+v", f)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("this line has no colon\n")); err == nil {
		t.Fatal("expected an error for a key without a value")
	}
}

func TestLoadMissingIsNotAnError(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f != nil {
		t.Fatalf("expected nil File for a missing path, got %+v", f)
	}
}

func TestWriteDefaultCreatesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	created, err := WriteDefault(path)
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if !created {
		t.Fatal("expected the file to be created")
	}
	f, err := Load(path)
	if err != nil || f == nil {
		t.Fatalf("Load after create: %v (%+v)", err, f)
	}
	if *f != Default() {
		t.Fatalf("template does not parse back to the defaults: %+v", f)
	}
	// A second call must not overwrite an existing file.
	if err := os.WriteFile(path, []byte("listen: 1.2.3.4:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err = WriteDefault(path)
	if err != nil || created {
		t.Fatalf("WriteDefault over an existing file: created=%v err=%v", created, err)
	}
	if f, _ := Load(path); f.Listen != "1.2.3.4:1" {
		t.Fatalf("existing file was overwritten: %+v", f)
	}
}

func TestParseTrustedNetwork(t *testing.T) {
	f, err := Parse([]byte("trustedNetwork: true\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !f.TrustedNetwork {
		t.Fatal("trustedNetwork = false, want true")
	}
	// Aliases and the JSON form are accepted the same way as trustProxy.
	for _, raw := range []string{`trusted_network: true`, `trusted-network: true`, `{"trustedNetwork": true}`} {
		f, err := Parse([]byte(raw))
		if err != nil || !f.TrustedNetwork {
			t.Fatalf("Parse(%q) trustedNetwork = %v (err %v), want true", raw, f.TrustedNetwork, err)
		}
	}
	if _, err := Parse([]byte("trustedNetwork: maybe\n")); err == nil {
		t.Fatal("expected an error for a non-boolean trustedNetwork")
	}
}

func TestParseBasePath(t *testing.T) {
	f, err := Parse([]byte("basePath: /ddns/\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.BasePath != "/ddns/" {
		t.Fatalf("basePath = %q, want the raw value", f.BasePath)
	}
}

func TestNormalizeBasePath(t *testing.T) {
	ok := map[string]string{
		"":         "/",
		"/":        "/",
		"  /  ":    "/",
		"ddns":     "", // rejected: must be rooted
		"/ddns":    "/ddns",
		"/ddns/":   "/ddns",
		"//ddns//": "/ddns",
		"/a/b":     "/a/b",
	}
	for in, want := range ok {
		got, err := NormalizeBasePath(in)
		if want == "" {
			if err == nil {
				t.Errorf("NormalizeBasePath(%q) = %q, want an error", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("NormalizeBasePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	// Anything that could break out of the <base href> attribute, the URL, or
	// the path resolution is refused rather than escaped silently.
	for _, bad := range []string{"/a b", `/a"b`, "/a<b", "/a\\b", "/a?b", "/a#b", "/a/../b", "/a/./b"} {
		if got, err := NormalizeBasePath(bad); err == nil {
			t.Errorf("NormalizeBasePath(%q) = %q, want an error", bad, got)
		}
	}
}

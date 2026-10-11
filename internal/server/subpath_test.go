package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/echoface/ozddns/internal/app"
	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/providers"
	"github.com/echoface/ozddns/internal/storage/sqlite"
	"github.com/echoface/ozddns/web"
)

// TestSubPathDeployment locks in the contract behind docs/nginx.md: the daemon
// owns its public prefix, so a reverse proxy forwards requests untouched
// (`location /ddns { proxy_pass http://127.0.0.1:9876; }`) and everything -
// panel, assets, REST API - works under the mount point from one build.
func TestSubPathDeployment(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "subpath.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := app.New(store, providers.NewRegistry(), ipsource.NewRegistry(), logger)
	const mount = "/ddns"
	// The CLI resolves the prefix once and hands it to both the application
	// (which reports it) and the HTTP layer (which enforces it).
	a.BasePath, a.BasePathSource = mount, "flag"

	srv := New(a, web.Handler(mount), logger)
	srv.BasePath = mount
	front := httptest.NewServer(srv.Handler())
	t.Cleanup(front.Close)

	// The proxy is identity; this client only follows the mount point.
	index := fetchOK(t, front.URL+mount+"/")
	if !strings.Contains(index, `id="root"`) {
		t.Fatal("sub-path mount did not serve the panel")
	}
	if !strings.Contains(index, `<base href="/ddns/">`) {
		t.Fatalf("sub-path document did not carry a base element: %s", index)
	}
	asset := regexp.MustCompile(`src="\./(assets/[^"]+\.js)"`).FindStringSubmatch(index)
	if len(asset) != 2 {
		t.Fatalf("panel did not reference its JavaScript bundle: %s", index)
	}
	if body := fetchOK(t, front.URL+mount+"/"+asset[1]); !strings.Contains(body, "createElement") {
		t.Fatal("asset was not served through the sub-path")
	}

	if status := fetchOK(t, front.URL+mount+"/api/v1/status"); !strings.Contains(status, "goVersion") {
		t.Fatalf("API did not answer through the sub-path: %s", status)
	}
	if status := fetchOK(t, front.URL+mount+"/api/v1/status"); !strings.Contains(status, `"basePath":"/ddns"`) {
		t.Fatalf("status did not report the configured base path: %s", status)
	}

	// The mount point itself is canonicalized so relative URLs have a directory
	// to resolve against, and the redirect keeps the query string.
	noSlash := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := noSlash.Get(front.URL + mount + "?theme=dark")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("GET %s = %d, want 301", mount, response.StatusCode)
	}
	if loc := response.Header.Get("Location"); loc != mount+"/?theme=dark" {
		t.Fatalf("redirect Location = %q, want %q", loc, mount+"/?theme=dark")
	}

	// A request without the prefix still works: the daemon serves the root, so a
	// proxy that strips the prefix, or a health probe, is not broken by adoption
	// of a base path.
	if health := fetchOK(t, front.URL+"/healthz"); !strings.Contains(health, "ok") {
		t.Fatalf("root health probe did not answer: %q", health)
	}
}

func fetchOK(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return string(body)
}

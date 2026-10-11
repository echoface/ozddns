package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestHandlerServesVuePanelAtRoot(t *testing.T) {
	server := httptest.NewServer(Handler("/"))
	defer server.Close()

	index := getBody(t, server.URL+"/")
	if !strings.Contains(index, `id="root"`) {
		t.Fatal("root did not serve the Vue application")
	}

	// The panel is built with a relative base so one artifact runs at any mount
	// point; it must not carry an absolute /assets path.
	asset := assetURL(t, index)
	if len(asset) != 2 {
		t.Fatal("panel did not reference its JavaScript bundle")
	}
	if body := getBody(t, server.URL+assetPath(asset[1])); !strings.Contains(body, "createElement") {
		t.Fatal("JavaScript bundle was not served")
	}

	response, err := server.Client().Get(server.URL + "/v2/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /v2/ = %d, want 404", response.StatusCode)
	}
}

// TestHandlerServesPanelUnderSubPath covers the deployment the runtime base path
// exists for: the same artifact served under a prefix must point its relative
// URLs at that prefix through an injected <base> element.
func TestHandlerServesPanelUnderSubPath(t *testing.T) {
	server := httptest.NewServer(Handler("/ddns"))
	defer server.Close()

	index := getBody(t, server.URL+"/")
	if !strings.Contains(index, `<base href="/ddns/">`) {
		t.Fatalf("sub-path document did not carry a base element: %s", index)
	}
	asset := assetURL(t, index)
	if len(asset) != 2 {
		t.Fatal("panel did not reference its JavaScript bundle")
	}
	if strings.HasPrefix(asset[1], "/") {
		t.Fatalf("asset URL %q is absolute; the base element could not redirect it", asset[1])
	}
	if body := getBody(t, server.URL+assetPath(asset[1])); !strings.Contains(body, "createElement") {
		t.Fatal("JavaScript bundle was not served under the sub-path")
	}
}

// assetPath turns the script URL found in the document into a request path for
// the root mount ("/assets/index-*.js").
func assetPath(url string) string {
	return "/" + strings.TrimPrefix(strings.TrimPrefix(url, "./"), "/")
}

// assetURL returns the capture groups of the panel's first script URL. The URL
// is relative in the shipped build ("./assets/index-*.js"); an absolute form is
// accepted too, so an explicitly based build still parses here.
func assetURL(t *testing.T, index string) []string {
	t.Helper()
	return regexp.MustCompile(`src="(\.?/?assets/[^"]+\.js)"`).FindStringSubmatch(index)
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

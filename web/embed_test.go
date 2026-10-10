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
	server := httptest.NewServer(Handler())
	defer server.Close()

	index := getBody(t, server.URL+"/")
	if !strings.Contains(index, `id="root"`) {
		t.Fatal("root did not serve the Vue application")
	}

	asset := regexp.MustCompile(`src="(/assets/[^"]+\.js)"`).FindStringSubmatch(index)
	if len(asset) != 2 {
		t.Fatal("panel did not reference its JavaScript bundle")
	}
	if body := getBody(t, server.URL+asset[1]); !strings.Contains(body, "createElement") {
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

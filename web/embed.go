// Package web embeds the management panel so a single binary ships the UI.
package web

import (
	"embed"
	"html"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed v2/dist
var files embed.FS

// FS returns the embedded panel filesystem.
func FS() fs.FS {
	panel, err := fs.Sub(files, "v2/dist")
	if err != nil {
		panic(err)
	}
	return panel
}

// Handler serves the panel, mounted at basePath ("/" for a root mount, "/ddns"
// for a reverse-proxy sub-path).
//
// The panel is built with relative asset and API URLs, so the only thing a
// sub-path needs at run time is a <base href> telling the browser what those
// URLs are relative to. Injecting it here keeps one build working at every
// mount point: nothing is baked in at compile time and no rebuild is needed to
// move the panel.
func Handler(basePath string) http.Handler {
	panel := FS()
	files := http.FileServer(http.FS(panel))
	index, err := indexHTML(panel, basePath)
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			files.ServeHTTP(w, r)
			return
		}
		// The document now depends on configuration, so it must not be cached
		// across a restart that changes the base path. Hashed assets still are.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.WriteString(w, index)
	})
}

// indexHTML reads the built index and, for a sub-path mount, injects a <base>
// element so the document resolves its relative URLs under basePath.
func indexHTML(panel fs.FS, basePath string) (string, error) {
	raw, err := fs.ReadFile(panel, "index.html")
	if err != nil {
		return "", err
	}
	index := string(raw)
	if href := baseHref(basePath); href != "" {
		index = strings.Replace(index, "<head>", "<head>\n  <base href=\""+html.EscapeString(href)+"\">", 1)
	}
	return index, nil
}

// baseHref returns the value for the injected <base> element: empty for a root
// mount, which needs none because the document's own URL already ends in "/".
func baseHref(basePath string) string {
	prefix := strings.Trim(strings.TrimSpace(basePath), "/")
	if prefix == "" {
		return ""
	}
	return "/" + prefix + "/"
}

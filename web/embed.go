// Package web embeds the management panel so a single binary ships the UI.
// The panel is a static SPA (classic scripts) using hash routing, so serving the
// files is all that is required.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html assets
var files embed.FS

// FS returns the embedded panel filesystem.
func FS() fs.FS { return files }

// Handler serves the panel.
func Handler() http.Handler {
	return http.FileServer(http.FS(files))
}

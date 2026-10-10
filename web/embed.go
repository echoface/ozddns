// Package web embeds the management panel so a single binary ships the UI.
package web

import (
	"embed"
	"io/fs"
	"net/http"
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

// Handler serves the panel.
func Handler() http.Handler {
	return http.FileServer(http.FS(FS()))
}

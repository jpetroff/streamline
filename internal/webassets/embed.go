//go:build !dev

// Package webassets serves the frontend bundled into release binaries.
package webassets

import (
	"embed"
	"io/fs"
	"net/http"
)

// A release build requires the generated frontend: run make build.
//
//go:embed all:dist
var files embed.FS

func Handler() http.Handler {
	assets, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(assets))
}

// TrustedOrigins is empty in release builds; requests must be same-origin.
func TrustedOrigins() []string { return nil }

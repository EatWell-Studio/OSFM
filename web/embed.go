// Package web embeds the built frontend into the osfm binary.
//
// `pnpm build` writes the app to web/dist/app (not tracked). web/dist/placeholder is tracked, so the Go code always
// compiles; it is served only when the app has not been built.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built single-page app, or the placeholder page when the frontend has not been built.
func FS() fs.FS {
	if app, err := fs.Sub(dist, "dist/app"); err == nil {
		if _, err := fs.Stat(app, "index.html"); err == nil {
			return app
		}
	}
	placeholder, err := fs.Sub(dist, "dist/placeholder")
	if err != nil {
		// Unreachable: the placeholder directory is part of the repository and embedded with it.
		panic(err)
	}
	return placeholder
}

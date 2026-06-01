// Package web embeds the built Progressive Web App and serves it as a
// single-page application from the meshd binary.
package web

import (
	"embed"
	"io/fs"
)

// distFS holds the built frontend assets produced by `npm run build`.
//
// The `all:` prefix ensures the committed dist/.gitkeep placeholder is
// embedded even before the frontend is built, so the package always compiles.
//
//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend assets rooted at the dist directory.
func DistFS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Unreachable: dist is embedded above and always present.
		panic(err)
	}
	return sub
}

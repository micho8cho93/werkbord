// Package uipresentation serves product-neutral tokens, controls and planning assets.
// Products provide their own data, permissions and transition handlers.
package uipresentation

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var assets embed.FS

func Handler() http.Handler {
	root, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServerFS(root)
}

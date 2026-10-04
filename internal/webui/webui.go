// Package webui serves the built PWA, which `make web` copies into dist/
// before the Go build embeds it.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const notBuilt = `<!doctype html><meta charset="utf-8"><title>Devboard</title>
<p style="font-family:system-ui;padding:2rem">The controller is running, but the web app was not built into this binary.
Run <code>make build</code>, or use <code>make dev</code> for the Vite dev server.</p>`

// Handler serves static files with an SPA fallback to index.html.
func Handler() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		index = []byte(notBuilt)
	}
	files := http.FileServerFS(root)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" || !exists(root, name) {
			if name != "" && path.Ext(name) != "" && name != "index.html" {
				http.NotFound(w, r) // a missing asset, not a client-side route
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(index)
			return
		}
		switch {
		case strings.HasPrefix(name, "assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // content-hashed by Vite
		default:
			w.Header().Set("Cache-Control", "no-cache") // sw.js, manifest, icons
		}
		files.ServeHTTP(w, r)
	})
}

func exists(root fs.FS, name string) bool {
	info, err := fs.Stat(root, name)
	return err == nil && !info.IsDir()
}

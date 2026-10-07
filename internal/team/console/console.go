// Package console serves the Team console: a small web page, with no build step,
// for the workspace, its members and its projects. It is Team's own front end.
// The individual product's web app is a different program (web/) and is not
// changed or served by Team; sharing components between them is planned in
// docs/PRODUCTS.md.
package console

import (
	"embed"
	"io/fs"
	"net/http"

	"devboard/internal/nativebridge"
)

//go:embed static
var static embed.FS

// Handler serves the console's files at /.
func Handler() http.Handler {
	root, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/native-bridge.js" {
			nativebridge.Handler().ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

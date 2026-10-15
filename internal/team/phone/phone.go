// Package phone serves Team's read-only phone view. No workspace console or device controls are included.
package phone

import (
	"embed"
	"io/fs"
	"net/http"
	"time"
)

const SummaryTimeout = 20 * time.Second

//go:embed static
var files embed.FS

func Handler() http.Handler {
	root, _ := fs.Sub(files, "static")
	return http.FileServerFS(root)
}

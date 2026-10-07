// Package nativebridge serves a product-neutral browser-to-native call transport.
// It holds no product method, credential, execution policy, or native toolkit dependency.
package nativebridge

import (
	_ "embed"
	"net/http"
)

//go:embed bridge.js
var script []byte

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(script)
	})
}

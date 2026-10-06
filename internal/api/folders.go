package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devboard/internal/domain"
)

// The authenticated folder browser lists directory names, never file contents.
// It runs on the controller's computer, just like project registration.
func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path, _ = os.UserHomeDir()
	}
	if !filepath.IsAbs(path) {
		s.fail(w, r, fmt.Errorf("%w: choose an absolute folder", domain.ErrInvalid))
		return
	}
	path = filepath.Clean(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		s.fail(w, r, fmt.Errorf("%w: cannot open this folder; choose another folder", domain.ErrInvalid))
		return
	}
	type folder struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		Repository bool   `json:"repository"`
	}
	out := []folder{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		p := filepath.Join(path, entry.Name())
		_, gitErr := os.Stat(filepath.Join(p, ".git"))
		out = append(out, folder{Name: entry.Name(), Path: p, Repository: gitErr == nil})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Repository != out[j].Repository {
			return out[i].Repository
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "parent": filepath.Dir(path), "folders": out})
}

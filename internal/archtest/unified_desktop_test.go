package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The new UI is a client/launcher, never a route around either backend boundary.
// This adds constraints; all original Individual and Team dependency tests remain.
func TestUnifiedDesktopNeverLinksTeamOrExecutionEngines(t *testing.T) {
	dir := filepath.Join(moduleRoot(t), "desktop")
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		_, _ = os.ReadFile(path) // track nested module source changes in the Go test cache
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if isTeamPath(name) {
				t.Errorf("desktop shell links Team backend %s in %s", name, path)
			}
			for _, blocked := range []string{"internal/runner", "internal/agent", "internal/service", "internal/controller", "internal/gitrepo", "internal/remote"} {
				if name == module+"/"+blocked || strings.HasPrefix(name, module+"/"+blocked+"/") {
					t.Errorf("desktop shell links execution engine %s in %s", name, path)
				}
			}
			if strings.Contains(path, filepath.Join("internal", "workspaces")) && !strings.HasSuffix(path, "_test.go") && name == "os/exec" {
				t.Errorf("workspace selection may not launch processes: %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "bindingsFrom := \"\"") {
		t.Fatal("unified shell must refuse direct loopback page bindings")
	}
	// Team screens continue to be served by Team; the shell imports only shared Svelte primitives.
	frontend := filepath.Join(moduleRoot(t), "web", "src", "shell")
	err = filepath.WalkDir(frontend, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "internal/team/") {
			t.Errorf("shell frontend imports Team implementation: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

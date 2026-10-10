package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Team's installer is part of the desktop app (desktop/internal/teaminstall): the app's own executable, run with a fixed argument,
// and, for its one privileged step, run as root by macOS after an administrator's authorization. It is local, native and
// fixed. It must not become a way to run anything else, or a way for Team's code into the app.
func TestTeamInstallerIsFixedLocalOperations(t *testing.T) {
	desktop := filepath.Join(moduleRoot(t), "desktop")
	installer := filepath.Join(desktop, "internal", "teaminstall")
	if _, err := os.Stat(installer); err != nil {
		t.Skip("the desktop app is not in this tree")
	}
	// It imports no code of this repository: nothing of either product runs as root, and nothing of Team is linked.
	err := filepath.WalkDir(installer, func(path string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(name, module+"/") {
				t.Errorf("the Team installer imports code of this repository: %s in %s", name, path)
			}
			if name == "plugin" || name == "net/rpc" {
				t.Errorf("the Team installer imports %s in %s", name, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Programs are started only by the installer itself (launchctl, osascript, codesign: fixed paths) and by the app's main
	// package (which runs that installer and the window's own helpers). No other package of the app starts a process.
	err = filepath.WalkDir(desktop, func(path string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		for _, imp := range f.Imports {
			if name, _ := strconv.Unquote(imp.Path.Value); name == "os/exec" && dir != desktop && dir != installer {
				t.Errorf("a process is started outside the Team installer and the app's main package: %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The installer's commands are fixed absolute paths, never a shell built from input.
	for _, name := range []string{"service_darwin.go", "maintenance.go"} {
		path := filepath.Join(installer, name)
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
				arg := call.Args[0]
				if sel.Sel.Name == "CommandContext" {
					arg = call.Args[1]
				}
				if lit, ok := arg.(*ast.BasicLit); !ok || !strings.HasPrefix(lit.Value, `"/`) {
					t.Errorf("%s starts a program that is not a fixed absolute path", name)
				}
			}
			return true
		})
	}
}

package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The native installer is intentionally a separate module. Infrastructure/execution exceptions in it cannot leak
// into a Host or either controller. Its binding offers fixed local setup operations and no command, file or fetch API.
func TestTeamDesktopRemainsASeparateLocalInstaller(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team removed by isolation check")
	}
	dir := filepath.Join(moduleRoot(t), "cmd", "werkbord-team", "desktop")
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	own := "devboard/cmd/werkbord-team/desktop"
	if !strings.Contains(string(b), "module "+own+"\n") || strings.Contains(string(b), "replace devboard ") || strings.Contains(string(b), "require devboard ") {
		t.Fatal("desktop module reaches the root products")
	}
	var methods []string
	err = filepath.WalkDir(dir, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(name, "devboard/") && !strings.HasPrefix(name, own+"/") {
				t.Errorf("native installer imports product code: %s", name)
			}
			if name == "os/exec" && filepath.Dir(path) != filepath.Join(dir, "internal", "platform") {
				t.Errorf("execution outside the fixed native installer: %s", path)
			}
		}
		if filepath.Base(path) == "main.go" {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || !fn.Name.IsExported() {
					continue
				}
				methods = append(methods, fn.Name.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(methods)
	want := []string{"Connect", "Info", "OpenExternal", "PendingInvitation", "Service", "SetupRunner"}
	if !reflect.DeepEqual(methods, want) {
		t.Fatalf("review the native binding surface: %v", methods)
	}
	for _, p := range goList(t, "-deps", "./cmd/werkbord-team") {
		if strings.HasPrefix(p.ImportPath, own) || strings.HasPrefix(p.ImportPath, desktopToolkit) {
			t.Errorf("workspace links native installer/toolkit %s", p.ImportPath)
		}
	}
}

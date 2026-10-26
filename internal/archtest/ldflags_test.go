package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Release builds stamp keys and pins into the program with the linker's -X flag. A -X that names a
// package or variable that does not exist is silently ignored, so a renamed or moved package would
// ship a build that cannot check a Premium key or a release signature and nothing would say so.
// This test reads every -X flag in the build scripts and requires its target to be a package-level
// string variable that exists.
var xFlag = regexp.MustCompile(`-X ([A-Za-z0-9_./-]+)\.([A-Za-z_][A-Za-z0-9_]*)=`)

func TestEveryLinkerStampNamesARealVariable(t *testing.T) {
	root := moduleRoot(t)
	var files []string
	for _, pattern := range []string{"scripts/*.sh", "scripts/*.ps1", "scripts/*.cjs", "scripts/*.py", "Makefile", ".github/workflows/*.yml"} {
		m, _ := filepath.Glob(filepath.Join(root, pattern))
		files = append(files, m...)
	}
	found := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range xFlag.FindAllStringSubmatch(string(raw), -1) {
			importPath, name := m[1], m[2]
			if importPath == "main" {
				continue // the program's own main package; the build names the command separately
			}
			found++
			rel, ok := strings.CutPrefix(importPath, "werkbord/")
			if !ok {
				t.Errorf("%s: -X %s.%s is not in this module (werkbord/...)", filepath.Base(f), importPath, name)
				continue
			}
			if !packageDeclaresString(t, filepath.Join(root, filepath.FromSlash(rel)), name) {
				t.Errorf("%s: -X %s.%s: no package-level string variable %q in %s", filepath.Base(f), importPath, name, name, rel)
			}
		}
	}
	if found == 0 {
		t.Fatal("found no -X flags in the build scripts: the test is not looking in the right place")
	}
}

// packageDeclaresString reports whether the non-test files of the package in dir declare a
// package-level variable called name whose type is string (or that is initialised with a string).
func packageDeclaresString(t *testing.T, dir, name string) bool {
	t.Helper()
	pkgs, err := parser.ParseDir(token.NewFileSet(), dir, func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		return false
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, d := range file.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, s := range gd.Specs {
					vs := s.(*ast.ValueSpec)
					for _, n := range vs.Names {
						if n.Name != name {
							continue
						}
						if id, ok := vs.Type.(*ast.Ident); ok && id.Name == "string" {
							return true
						}
						for _, v := range vs.Values {
							if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

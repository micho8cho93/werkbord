package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Rule 14: the network supervisor is not a way to run anything.
//
// Rule 9 (infra_test.go) grants internal/team/infra/nebula the right to start one
// program, by name. This file is what keeps the grant from being stretched into a
// general-purpose "run a command" facility. It does not trust review to notice: it reads
// the package and fails if the package's shape changes.

const supervisorDir = "internal/team/infra/nebula"

// supervisorSurface is the package's whole exported API. A new exported function,
// method or type is a decision: add it here, in the same change, after asking whether
// it takes anything a request or a person could use to start something else.
var supervisorSurface = []string{
	"func ArtifactFor", "func New", "func Platforms", "func ThisPlatform", "func Validate",
	"method Supervisor.Reload", "method Supervisor.StartNebula", "method Supervisor.Status", "method Supervisor.Stop",
	"method ErrUnsupportedPlatform.Error",
	"type Artifact", "type ErrUnsupportedPlatform", "type NebulaConfig", "type Options", "type State", "type Status", "type Supervisor",
	"const BinaryName", "const ReleaseURL", "const Version",
	"const StateFailed", "const StateRestarting", "const StateRunning", "const StateStarting", "const StateStopped",
	"var ErrBinaryMismatch", "var ErrBinaryNotFound",
}

// Names that mean "a thing to run" or "how to run it". No exported field, parameter or
// result of the supervisor's API may be called one, and none may be a slice of strings
// (the usual shape of an argument list).
var runnerWords = []string{"command", "cmd", "args", "argv", "argument", "program", "binary", "executable", "exe", "shell", "script", "env", "environ", "environment", "run", "exec", "path", "workdir", "cwd"}

func supervisorFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), filepath.FromSlash(supervisorDir))
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	if len(files) < 3 {
		t.Fatalf("found %d source files in %s", len(files), supervisorDir)
	}
	return fset, files
}

func TestTheNetworkSupervisorIsNotAGeneralRunner(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	watchSources(t)
	fset, files := supervisorFiles(t)
	for _, v := range supervisorViolations(fset, files, supervisorSurface) {
		t.Error(v)
	}
}

// supervisorViolations applies rule 14 to a parsed package.
func supervisorViolations(fset *token.FileSet, files []*ast.File, surface []string) []string {
	var out []string
	fail := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }

	// 1. The exported surface is exactly the reviewed one.
	got := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					if d.Name.IsExported() {
						got["func "+d.Name.Name] = true
					}
					continue
				}
				recv := receiverName(d.Recv.List[0].Type)
				if d.Name.IsExported() && ast.IsExported(recv) {
					got["method "+recv+"."+d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							got["type "+s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								kind := "var"
								if d.Tok == token.CONST {
									kind = "const"
								}
								got[kind+" "+n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	want := map[string]bool{}
	for _, s := range surface {
		want[s] = true
	}
	var extra, missing []string
	for s := range got {
		if !want[s] {
			extra = append(extra, s)
		}
	}
	for s := range want {
		if !got[s] {
			missing = append(missing, s)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	if len(extra) > 0 || len(missing) > 0 {
		fail("the supervisor's exported API changed (new: %v, gone: %v). Review it against rule 14 (internal/archtest/supervisor_test.go) and update supervisorSurface.", extra, missing)
	}

	// 2. Nothing exported takes or holds a program, arguments, a shell, an environment or a path to run.
	checkFields := func(where string, fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, fld := range fl.List {
			if at, ok := fld.Type.(*ast.ArrayType); ok {
				if id, ok := at.Elt.(*ast.Ident); ok && id.Name == "string" && !allowedStringSlice(where, fld) {
					fail("%s: %s is a []string: the usual shape of an argument list", fset.Position(fld.Pos()), where)
				}
			}
			for _, n := range fld.Names {
				for _, w := range runnerWords {
					if strings.Contains(strings.ToLower(n.Name), w) && !(strings.ToLower(n.Name) == "binarydirs" || strings.ToLower(n.Name) == "binarysha256") {
						fail("%s: %s has a field or parameter named %s, which names something to run", fset.Position(n.Pos()), where, n.Name)
					}
				}
			}
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				exported := d.Name.IsExported()
				if d.Recv != nil {
					exported = exported && ast.IsExported(receiverName(d.Recv.List[0].Type))
				}
				if exported {
					checkFields(d.Name.Name+" parameter", d.Type.Params)
					checkFields(d.Name.Name+" result", d.Type.Results)
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.IsExported() {
						if st, ok := ts.Type.(*ast.StructType); ok {
							checkFields("type "+ts.Name.Name, st.Fields)
						}
					}
				}
			}
		}
	}

	// 3. The package starts a process in exactly one place, with a constant program, and
	//    never through a shell, a plugin or a lookup it did not make itself.
	calls := 0
	for _, f := range files {
		local := map[string]string{}
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			name := p[strings.LastIndex(p, "/")+1:]
			if im.Name != nil {
				name = im.Name.Name
			}
			local[name] = p
			switch p {
			case "plugin", "net/rpc", "unsafe", "reflect", "text/template", "html/template":
				fail("%s imports %s", fset.Position(im.Pos()), p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if local[id.Name] == "os/exec" {
					switch sel.Sel.Name {
					case "Command", "CommandContext":
						calls++
						if sel.Sel.Name != "CommandContext" {
							fail("%s: use CommandContext, so that stopping the supervisor ends the process", fset.Position(x.Pos()))
						}
						if len(x.Args) < 3 {
							break
						}
						if lit, ok := x.Args[1].(*ast.BasicLit); !ok || lit.Value != `"nebula"` {
							fail("%s: the program must be the literal \"nebula\"", fset.Position(x.Pos()))
						}
						if lit, ok := x.Args[2].(*ast.BasicLit); !ok || lit.Value != `"-config"` {
							fail("%s: the first argument must be the literal \"-config\"", fset.Position(x.Pos()))
						}
						if len(x.Args) != 4 {
							fail("%s: the program is started with exactly -config <file>", fset.Position(x.Pos()))
						}
					default:
						fail("%s: os/exec.%s", fset.Position(x.Pos()), sel.Sel.Name)
					}
				}
				if local[id.Name] == "os" && (sel.Sel.Name == "StartProcess" || sel.Sel.Name == "Setenv" || sel.Sel.Name == "Environ" || sel.Sel.Name == "Getenv") {
					fail("%s: os.%s", fset.Position(x.Pos()), sel.Sel.Name)
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					s, _ := strconv.Unquote(x.Value)
					for _, banned := range []string{"/bin/sh", "/bin/bash", "cmd.exe", "powershell", "sh -c", "bash -c"} {
						if strings.Contains(strings.ToLower(s), banned) {
							fail("%s: the string %q names a shell", fset.Position(x.Pos()), s)
						}
					}
				}
			}
			return true
		})
	}
	if calls != 1 {
		fail("the supervisor starts a process in %d places; it must be exactly one (spawn)", calls)
	}

	// 4. The one start lives in spawn, which is not exported and is reached only from the
	//    supervisor's own start and restart paths.
	spawnCallers := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "spawn" {
						spawnCallers[fd.Name.Name] = true
					}
				}
				return true
			})
		}
	}
	for caller := range spawnCallers {
		if caller != "StartNebula" && caller != "supervise" {
			fail("%s calls spawn: only StartNebula and its watcher may start the program", caller)
		}
	}
	return out
}
func receiverName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return receiverName(x.X)
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// allowedStringSlice names the three []string the supervisor's API has, none of which is
// something to run: the directories the pinned program may have been shipped in (absolute,
// searched in order, never PATH), the names of the platforms there is a pinned release for,
// and the last lines the node logged.
func allowedStringSlice(where string, f *ast.Field) bool {
	key := where
	if len(f.Names) > 0 {
		key += "." + f.Names[0].Name
	}
	switch key {
	case "type Options.BinaryDirs", "Platforms result", "type Status.Tail":
		return true
	}
	return false
}

// The rule itself, on packages that break it: a rule that cannot fail proves nothing.
func TestTheSupervisorRuleCatchesWhatItIsMeantTo(t *testing.T) {
	check := func(src string, surface ...string) []string {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return supervisorViolations(fset, []*ast.File{f}, surface)
	}
	const good = `package x
import ("context"; "os/exec")
type Supervisor struct{}
func (s *Supervisor) Start(ctx context.Context, cfg string) { _ = exec.CommandContext(ctx, "nebula", "-config", cfg) }
func (s *Supervisor) spawn() {}
`
	_ = good
	cases := map[string]struct {
		src     string
		surface []string
		want    string // a substring of one violation
	}{
		"a general-purpose Run": {`package x
import "os/exec"
func Run(command string, args []string) { exec.Command(command, args...) }`, []string{"func Run"}, "names something to run"},
		"a slice of arguments in a config": {`package x
type Config struct { Flags []string }`, []string{"type Config"}, "[]string"},
		"a program from a variable": {`package x
import ("context"; "os/exec")
func f(ctx context.Context, p string) { exec.CommandContext(ctx, p, "-config", "x") }`, nil, `the program must be the literal "nebula"`},
		"another program": {`package x
import ("context"; "os/exec")
func f(ctx context.Context) { exec.CommandContext(ctx, "git", "-config", "x") }`, nil, `the program must be the literal "nebula"`},
		"extra arguments": {`package x
import ("context"; "os/exec")
func f(ctx context.Context) { exec.CommandContext(ctx, "nebula", "-config", "x", "-extra") }`, nil, "exactly -config"},
		"a shell": {`package x
func f() string { return "/bin/sh" }`, nil, "names a shell"},
		"a second place that starts a process": {`package x
import ("context"; "os/exec")
func f(ctx context.Context) { exec.CommandContext(ctx, "nebula", "-config", "x"); exec.CommandContext(ctx, "nebula", "-config", "y") }`, nil, "must be exactly one"},
		"an exported surface that grew": {`package x
func Anything() {}`, nil, "exported API changed"},
		"reading the environment": {`package x
import "os"
func f() string { return os.Getenv("X") }`, nil, "os.Getenv"},
		"a plugin": {`package x
import _ "plugin"`, nil, "imports plugin"},
	}
	for name, c := range cases {
		got := check(c.src, c.surface...)
		found := false
		for _, v := range got {
			found = found || strings.Contains(v, c.want)
		}
		if !found {
			t.Errorf("%s: no violation containing %q in %v", name, c.want, got)
		}
	}
	// And a package of the right shape has none.
	if got := check(`package x
import ("context"; "os/exec")
type Supervisor struct{}
func (s *Supervisor) StartNebula(ctx context.Context) { s.spawn(ctx) }
func (s *Supervisor) spawn(ctx context.Context) { _ = exec.CommandContext(ctx, "nebula", "-config", s.path()) }
func (s *Supervisor) path() string { return "" }
`, "type Supervisor", "method Supervisor.StartNebula"); len(got) != 0 {
		t.Errorf("a package of the right shape was refused: %v", got)
	}
}

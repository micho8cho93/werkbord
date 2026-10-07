package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Rule 14, for the second supervisor: the database's.
//
// Rule 9 (infra_test.go) grants internal/team/infra/rqlite the right to start one program, by name.
// This is what keeps that grant from becoming a general "run a command" facility, for the same reasons
// and in the same way as the network supervisor's (supervisor_test.go): the package's whole API is a
// reviewed list, nothing in it takes something to run, and the process is started in exactly one place.
//
// The database program takes its settings as flags, so the shape of the one start differs from the
// network program's (`-config <file>`): the program is started by a single function, command, with the
// literal "rqlited" and whatever arguments its caller built; and command has two callers, spawn (which
// passes the arguments NodeConfig builds from validated addresses, a node name and its own paths) and
// verifyVersion (which passes the single literal "-version").

const rqliteDir = "internal/team/infra/rqlite"

var rqliteSurface = []string{
	"func ArtifactFor", "func Initialized", "func New", "func NewAdmin", "func NewCredentials", "func Platforms", "func ThisPlatform", "func Validate",
	"method Supervisor.Admin", "method Supervisor.BecomeVoter", "method Supervisor.Check", "method Supervisor.Config", "method Supervisor.Readdress", "method Supervisor.Start", "method Supervisor.Status", "method Supervisor.Stop", "method Supervisor.WaitReady",
	"method Admin.Alive", "method Admin.Health", "method Admin.Leader", "method Admin.Nodes", "method Admin.Ready", "method Admin.Remove", "method Admin.StepDown",
	"method Credentials.Valid", "method NodeConfig.Validate", "method ErrUnsupportedPlatform.Error",
	"type Admin", "type Artifact", "type Credentials", "type ErrUnsupportedPlatform", "type Health", "type Member", "type Node", "type NodeConfig", "type Options", "type State", "type Status", "type Supervisor",
	"const BinaryName", "const RecordName", "const ReleaseURL", "const SourceCommit", "const Tag", "const Version",
	"const UserAdmin", "const UserApp", "const UserNode",
	"const StateFailed", "const StateRestarting", "const StateRunning", "const StateStarting", "const StateStopped",
	"var ErrBinaryMismatch", "var ErrBinaryNotFound", "var ErrNoLeader",
}

var rqliteSpec = supervisorSpec{
	Program: "rqlited",
	checkExec: func(fset *token.FileSet, x *ast.CallExpr, fail func(string, ...any)) {
		if len(x.Args) != 3 || !x.Ellipsis.IsValid() {
			fail("%s: the program is started as CommandContext(ctx, \"rqlited\", args...), with the arguments its caller built", fset.Position(x.Pos()))
			return
		}
		if lit, ok := x.Args[1].(*ast.BasicLit); !ok || lit.Value != `"rqlited"` {
			fail("%s: the program must be the literal \"rqlited\"", fset.Position(x.Pos()))
		}
	},
	Callers: []string{"Start", "supervise"},
	Slices:  []string{"type Options.BinaryDirs", "Platforms result", "type Status.Tail"},
	Extra: func(fset *token.FileSet, files []*ast.File, fail func(string, ...any)) {
		inCommand := 0
		callers := map[string]bool{}
		for _, f := range files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					switch fn := call.Fun.(type) {
					case *ast.SelectorExpr:
						if id, ok := fn.X.(*ast.Ident); ok && id.Name == "exec" && fn.Sel.Name == "CommandContext" && fd.Name.Name == "command" {
							inCommand++
						}
					case *ast.Ident:
						if fn.Name != "command" {
							return true
						}
						callers[fd.Name.Name] = true
						switch fd.Name.Name {
						case "spawn":
							// command(ctx, v, cfg.args()...)
							if len(call.Args) != 3 || !call.Ellipsis.IsValid() {
								fail("%s: spawn passes the arguments NodeConfig builds, and nothing else", fset.Position(call.Pos()))
								return true
							}
							ce, ok := call.Args[2].(*ast.CallExpr)
							sel, ok2 := ast.Expr(nil), false
							if ok {
								sel, ok2 = ce.Fun.(*ast.SelectorExpr)
							}
							if !ok || !ok2 || sel.(*ast.SelectorExpr).Sel.Name != "args" {
								fail("%s: spawn must pass cfg.args()..., the arguments built from a validated configuration", fset.Position(call.Pos()))
							}
						case "verifyVersion":
							if len(call.Args) != 3 || call.Ellipsis.IsValid() {
								fail("%s: verifyVersion starts the program with the one argument -version", fset.Position(call.Pos()))
								return true
							}
							if lit, ok := call.Args[2].(*ast.BasicLit); !ok || lit.Value != `"-version"` {
								fail("%s: verifyVersion passes the literal \"-version\"", fset.Position(call.Pos()))
							}
						}
					}
					return true
				})
			}
		}
		if inCommand != 1 {
			fail("os/exec.CommandContext is called %d times inside command; it must be exactly once", inCommand)
		}
		for c := range callers {
			if c != "spawn" && c != "verifyVersion" {
				fail("%s calls command: only spawn and verifyVersion may start the program", c)
			}
		}
		if !callers["spawn"] || !callers["verifyVersion"] {
			fail("command is not called from spawn and verifyVersion (callers: %v): this rule needs updating with the code", callers)
		}
	},
}

func rqliteFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), filepath.FromSlash(rqliteDir))
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
	if len(files) < 6 {
		t.Fatalf("found %d source files in %s", len(files), rqliteDir)
	}
	return fset, files
}

func TestTheDatabaseSupervisorIsNotAGeneralRunner(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	watchSources(t)
	fset, files := rqliteFiles(t)
	for _, v := range supervisorViolationsFor(rqliteSpec, fset, files, rqliteSurface) {
		t.Error(v)
	}
}

// The rule on code that breaks it.
func TestTheDatabaseSupervisorRuleCatchesWhatItIsMeantTo(t *testing.T) {
	check := func(src string, surface ...string) []string {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return supervisorViolationsFor(rqliteSpec, fset, []*ast.File{f}, surface)
	}
	const shaped = `package x
import ("context"; "os/exec")
type verified struct{}
type NodeConfig struct{}
func (c NodeConfig) args() []string { return nil }
func command(ctx context.Context, v verified, args ...string) *exec.Cmd { return exec.CommandContext(ctx, "rqlited", args...) }
type Supervisor struct{}
func (s *Supervisor) Start(ctx context.Context, cfg NodeConfig) { s.spawn(ctx, cfg) }
func (s *Supervisor) spawn(ctx context.Context, cfg NodeConfig) { _ = command(ctx, verified{}, cfg.args()...) }
func verifyVersion(ctx context.Context) { _ = command(ctx, verified{}, "-version") }
`
	surface := []string{"type NodeConfig", "type Supervisor", "method Supervisor.Start"}
	if got := check(shaped, surface...); len(got) != 0 {
		t.Fatalf("a package of the right shape was refused: %v", got)
	}
	cases := map[string]struct{ src, want string }{
		"another program":           {strings.Replace(shaped, `"rqlited", args...`, `"git", args...`, 1), `the program must be the literal "rqlited"`},
		"a program from a variable": {strings.Replace(shaped, `ctx, "rqlited", args...`, `ctx, string(v.x), args...`, 1), "the program must be the literal"},
		"a second start": {shaped + `
func other(ctx context.Context) { _ = exec.CommandContext(ctx, "rqlited", "x") }`, "the program is started as"},
		"a start that is not command":       {strings.Replace(shaped, "func verifyVersion(ctx context.Context) { _ = command(ctx, verified{}, \"-version\") }", "func verifyVersion(ctx context.Context) { _ = exec.CommandContext(ctx, \"rqlited\", \"-version\") }", 1), "the program is started as"},
		"spawn passing something else":      {strings.Replace(shaped, "cfg.args()...", "os.Args...", 1), "spawn must pass cfg.args()"},
		"verify passing more than -version": {strings.Replace(shaped, `"-version")`, `"-version", "-x")`, 1), "-version"},
		"a third caller": {shaped + `
func sneaky(ctx context.Context) { _ = command(ctx, verified{}, "-help") }`, "only spawn and verifyVersion"},
		"a runnable field": {shaped + `
type Config struct{ Command string }`, "names something to run"},
		"a shell": {shaped + `
func f() string { return "/bin/sh" }`, "names a shell"},
	}
	for name, c := range cases {
		got := check(c.src, append(surface, "type Config")...)
		found := false
		for _, v := range got {
			found = found || strings.Contains(v, c.want)
		}
		if !found {
			t.Errorf("%s: no violation containing %q in %v", name, c.want, got)
		}
	}
}

// Rule 18: only a Workspace Host's own wiring talks to the database.
//
// The storage layer over the cluster (internal/team/store/replicated) is the one client of the database. A handler, the service
// or the domain that could import it could be made to reach the cluster, and nothing in the API may take an address or a
// statement. So the wiring (internal/team/server) and the commands that work on a host's own files and node
// (cmd/werkbord-team) import it, and nothing else does; and what it imports of Team is the domain and the store's interface only.
func TestOnlyTheWiringTalksToTheDatabase(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	const replicatedPkg = teamTree + "/store/replicated"
	pkgs := append(goList(t, "./cmd/werkbord-team/..."), goList(t, "./internal/team/...")...)
	found := false
	for _, p := range pkgs {
		if p.ImportPath == replicatedPkg {
			found = true
			// What it needs of Team is the domain and the store's interface.
			for _, imp := range p.Imports {
				if isTeamPath(imp) && imp != teamTree+"/domain" && imp != teamTree+"/store" {
					t.Errorf("%s imports %s: the replicated store knows only Team's domain and the store's interface, so that no use case can reach the cluster through it", p.ImportPath, imp)
				}
			}
		}
		if p.ImportPath == replicatedPkg || p.ImportPath == teamTree+"/server" || p.ImportPath == teamCmd {
			continue
		}
		for _, imp := range p.Imports {
			if imp == replicatedPkg {
				t.Errorf("%s imports %s: only Team's wiring (internal/team/server, cmd/werkbord-team) may talk to the database", p.ImportPath, imp)
			}
		}
	}
	if !found {
		t.Fatal("the replicated store is not in the tree this test looks at")
	}
}

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

// Rule 9: Team's infrastructure may supervise its own processes, and nothing else.
//
// Rule 4 says Team never starts a process. The production design gives a Team
// deployment machinery of its own (a private-network node, a replicated database)
// that something has to start and watch. That is not the thing rule 4 protects
// against, which is *developer* execution: running a person's shell, Git, agent,
// model provider, or anything else on a person's computer on someone's say-so. So
// the rule is not relaxed. It gains one narrow, named exception (teamInfraExec in
// boundary_test.go), and this file is what keeps the exception narrow.

// developerPrograms are programs that are a way to run developer work. An
// infrastructure package may never start any of them, whatever its grant says.
var developerPrograms = []string{
	"sh", "bash", "zsh", "fish", "dash", "ksh", "csh", "tcsh", "cmd", "cmd.exe", "powershell", "pwsh", "env", "xargs", "sudo", "su", "doas", "osascript",
	"git", "gh", "ssh", "scp", "sftp", "rsync", "claude", "codex", "gemini", "aider", "cursor", "copilot",
	"node", "npm", "npx", "pnpm", "yarn", "bun", "deno", "python", "python3", "pip", "ruby", "perl", "php", "go", "cargo", "rustc", "make", "java", "mvn", "gradle", "docker", "podman",
	"curl", "wget", "nc", "ncat", "socat", "tmux", "screen", "script", "expect",
}

// infraViolations checks the infrastructure exception: reg maps a Team package to
// the programs it may start; pkgs is the module's packages (with the Team ones
// among them); read returns a package's non-test source files, parsed.
func infraViolations(reg map[string][]string, pkgs []pkg, read func(pkg) ([]*ast.File, *token.FileSet, error)) []string {
	var out []string
	byPath := map[string]pkg{}
	for _, p := range pkgs {
		byPath[p.ImportPath] = p
	}
	for path, programs := range reg {
		fail := func(format string, a ...any) { out = append(out, path+": "+fmt.Sprintf(format, a...)) }
		p, ok := byPath[path]
		switch {
		case path == teamInfraTree || !strings.HasPrefix(path, teamInfraTree+"/"):
			fail("an infrastructure grant must be for a package under %s/, not here", teamInfraTree)
			continue
		case !ok:
			fail("the package does not exist: remove the stale grant")
			continue
		case len(programs) == 0:
			fail("a grant names the programs it may start; an empty one is no grant")
			continue
		}
		allowed := map[string]bool{}
		for _, prog := range programs {
			base := strings.TrimSuffix(strings.ToLower(prog), ".exe")
			switch {
			case strings.ContainsAny(prog, `/\ `):
				fail("program %q must be a bare name, found on a fixed path by the code, not given a path or arguments here", prog)
			case isDeveloperProgram(base):
				fail("program %q is a way to run developer work and may never be granted", prog)
			}
			allowed[prog] = true
		}
		// Request-handling Team code must be unreachable from here: an infrastructure
		// package is configured by typed settings and cannot be handed anything a
		// member wrote.
		for _, imp := range p.Imports {
			if isTeamPath(imp) && !strings.HasPrefix(imp, teamInfraTree) && imp != teamTree+"/config" {
				fail("imports %s: infrastructure code must not import Team's domain, service, store, API or console, so nothing a member wrote can reach a process it starts", imp)
			}
			for _, bad := range []string{"plugin", "net/rpc"} {
				if imp == bad {
					fail("imports %s", bad)
				}
			}
		}
		// Every start of a process names its program with a constant, from the grant.
		files, fset, err := read(p)
		if err != nil {
			fail("cannot read the package: %v", err)
			continue
		}
		for _, f := range files {
			out = append(out, execCallViolations(fset, f, allowed)...)
		}
	}
	// Nothing outside Team's wiring (and the infrastructure tree itself) may import an
	// infrastructure package: a handler that could reach one could be made to start something.
	for _, p := range pkgs {
		if !isTeamPath(p.ImportPath) || strings.HasPrefix(p.ImportPath, teamInfraTree) {
			continue
		}
		for _, imp := range p.Imports {
			if strings.HasPrefix(imp, teamInfraTree) && p.ImportPath != teamTree+"/server" && p.ImportPath != teamCmd {
				out = append(out, fmt.Sprintf("%s imports %s: only Team's wiring (internal/team/server, cmd/werkbord-team) may use infrastructure code", p.ImportPath, imp))
			}
		}
	}
	sort.Strings(out)
	return out
}

func isDeveloperProgram(base string) bool {
	for _, d := range developerPrograms {
		if base == d {
			return true
		}
	}
	return false
}

// execCallViolations finds every way a file starts a process other than a call of
// os/exec whose program is a constant string in allowed.
func execCallViolations(fset *token.FileSet, f *ast.File, allowed map[string]bool) []string {
	var out []string
	local := map[string]string{} // local import name → path
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		local[name] = p
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch local[id.Name] {
		case "os":
			if sel.Sel.Name == "StartProcess" || sel.Sel.Name == "FindProcess" {
				out = append(out, fmt.Sprintf("%s: os.%s starts or reaches a process by a route this rule cannot check; use os/exec with a constant program", fset.Position(sel.Pos()), sel.Sel.Name))
			}
		case "syscall":
			switch sel.Sel.Name {
			case "Exec", "ForkExec", "StartProcess":
				out = append(out, fmt.Sprintf("%s: syscall.%s", fset.Position(sel.Pos()), sel.Sel.Name))
			}
		case "os/exec":
			// Only Command and CommandContext name a program; the others (LookPath, Cmd's
			// fields) are checked by where they are used, and the type may only be held.
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || local[id.Name] != "os/exec" {
			return true
		}
		argIdx := -1
		switch sel.Sel.Name {
		case "Command":
			argIdx = 0
		case "CommandContext":
			argIdx = 1
		default:
			return true
		}
		if len(call.Args) <= argIdx {
			return true
		}
		lit, ok := call.Args[argIdx].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			out = append(out, fmt.Sprintf("%s: the program is not a string literal: infrastructure code names what it starts in the code, never from a variable", fset.Position(call.Pos())))
			return true
		}
		prog, _ := strconv.Unquote(lit.Value)
		if !allowed[prog] {
			out = append(out, fmt.Sprintf("%s: starts %q, which this package is not granted", fset.Position(call.Pos()), prog))
		}
		return true
	})
	return out
}

func readPackageSources(p pkg) ([]*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		return nil, nil, err
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(p.Dir, e.Name()), nil, 0)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, f)
	}
	return files, fset, nil
}

// The real tree: every grant (there are none) is narrow, and nothing in Team starts
// a process except through one.
func TestTheInfrastructureExceptionIsNarrow(t *testing.T) {
	pkgs := append(goList(t, "./cmd/werkbord-team/..."), goList(t, "./internal/team/...")...)
	if v := infraViolations(teamInfraExec, pkgs, readPackageSources); len(v) > 0 {
		t.Errorf("Team's infrastructure exception is not narrow enough:\n  %s", strings.Join(v, "\n  "))
	}
	// Whatever the grants, Team code starts a process only through os/exec, which only a
	// granted package may import (rule 4): no other route exists in its own code.
	for _, p := range pkgs {
		files, fset, err := readPackageSources(p)
		if err != nil {
			t.Fatal(err)
		}
		granted := map[string]bool{}
		for _, prog := range teamInfraExec[p.ImportPath] {
			granted[prog] = true
		}
		for _, f := range files {
			for _, v := range execCallViolations(fset, f, granted) {
				t.Errorf("%s", v)
			}
		}
	}
}

// The rule itself, on code that breaks it: a rule that cannot fail proves nothing.
func TestTheInfrastructureRuleCatchesWhatItIsMeantTo(t *testing.T) {
	parse := func(src string) (*token.FileSet, *ast.File) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return fset, f
	}
	cases := map[string]struct {
		src  string
		want int // violations
	}{
		"a granted constant program": {`package x
import "os/exec"
func f() { _ = exec.Command("rqlited", "-node-id", "1") }`, 0},
		"an ungranted program": {`package x
import "os/exec"
func f() { _ = exec.Command("git", "status") }`, 1},
		"a program from a variable": {`package x
import "os/exec"
func f(p string) { _ = exec.Command(p) }`, 1},
		"a program from a constant identifier": {`package x
import "os/exec"
const p = "rqlited"
func f() { _ = exec.Command(p) }`, 1}, // a literal at the call keeps the program readable where it is started
		"with a context": {`package x
import ("context"; "os/exec")
func f(c context.Context) { _ = exec.CommandContext(c, "rqlited") }`, 0},
		"with a context and a bad program": {`package x
import ("context"; "os/exec")
func f(c context.Context) { _ = exec.CommandContext(c, "bash", "-c", "x") }`, 1},
		"an aliased import": {`package x
import ex "os/exec"
func f() { _ = ex.Command("bash") }`, 1},
		"os.StartProcess": {`package x
import "os"
func f() { _, _ = os.StartProcess("/bin/sh", nil, nil) }`, 1},
		"syscall.Exec": {`package x
import "syscall"
func f() { _ = syscall.Exec("/bin/sh", nil, nil) }`, 1},
		"only holding the type": {`package x
import "os/exec"
var c *exec.Cmd`, 0},
	}
	for name, c := range cases {
		fset, f := parse(c.src)
		got := execCallViolations(fset, f, map[string]bool{"rqlited": true})
		if len(got) != c.want {
			t.Errorf("%s: %d violations (%v), want %d", name, len(got), got, c.want)
		}
	}

	// The grants.
	good := teamInfraTree + "/supervise"
	pkgs := func(imports ...string) []pkg {
		return []pkg{{ImportPath: good, Imports: imports}, {ImportPath: teamTree + "/server"}, {ImportPath: teamCmd}}
	}
	noFiles := func(pkg) ([]*ast.File, *token.FileSet, error) { return nil, token.NewFileSet(), nil }
	for name, c := range map[string]struct {
		reg  map[string][]string
		pkgs []pkg
		want int
	}{
		"no grants":                      {nil, pkgs(), 0},
		"a narrow grant":                 {map[string][]string{good: {"rqlited"}}, pkgs("os/exec"), 0},
		"outside the infrastructure dir": {map[string][]string{teamTree + "/service": {"rqlited"}}, []pkg{{ImportPath: teamTree + "/service"}}, 1},
		"the tree itself":                {map[string][]string{teamInfraTree: {"rqlited"}}, []pkg{{ImportPath: teamInfraTree}}, 1},
		"a package that is not there":    {map[string][]string{good: {"rqlited"}}, nil, 1},
		"an empty grant":                 {map[string][]string{good: {}}, pkgs(), 1},
		"a shell":                        {map[string][]string{good: {"bash"}}, pkgs(), 1},
		"a shell by its other name":      {map[string][]string{good: {"SH.EXE"}}, pkgs(), 1},
		"Git":                            {map[string][]string{good: {"git"}}, pkgs(), 1},
		"an agent":                       {map[string][]string{good: {"claude"}}, pkgs(), 1},
		"a path":                         {map[string][]string{good: {"/usr/bin/rqlited"}}, pkgs(), 1},
		"a program with arguments":       {map[string][]string{good: {"rqlited -x"}}, pkgs(), 1},
		"reaching the service":           {map[string][]string{good: {"rqlited"}}, pkgs(teamTree + "/service"), 1},
		"reaching the domain":            {map[string][]string{good: {"rqlited"}}, pkgs(teamTree + "/domain"), 1},
		"reaching the API":               {map[string][]string{good: {"rqlited"}}, pkgs(teamTree + "/api"), 1},
		"using its own config":           {map[string][]string{good: {"rqlited"}}, pkgs(teamTree + "/config"), 0},
		"plugins":                        {map[string][]string{good: {"rqlited"}}, pkgs("plugin"), 1},
		"a handler importing it":         {map[string][]string{good: {"rqlited"}}, append(pkgs(), pkg{ImportPath: teamTree + "/api", Imports: []string{good}}), 1},
		"the wiring importing it":        {map[string][]string{good: {"rqlited"}}, append(pkgs(), pkg{ImportPath: teamTree + "/server", Imports: []string{good}}), 0},
	} {
		got := infraViolations(c.reg, c.pkgs, noFiles)
		if len(got) != c.want {
			t.Errorf("%s: %d violations (%v), want %d", name, len(got), got, c.want)
		}
	}
}

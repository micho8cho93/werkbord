// Package archtest holds the tests that keep Werkbord's two products apart:
// the individual product (cmd/werkbord) and Werkbord Team (cmd/werkbord-team).
// It has no code of its own. The rules are in docs/PRODUCTS.md; each test below
// says which one it enforces and what to do when it fails.
package archtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	module        = "devboard"
	individualCmd = module + "/cmd/werkbord"
	teamCmd       = module + "/cmd/werkbord-team"
	teamTree      = module + "/internal/team"
)

// teamAllowed is every package of this module Team may import: its own, and the
// shared plumbing. Anything else is the individual product's, and in particular
// everything that executes things (runner, remote, agent, daemon, gitrepo,
// controller): Team coordinates and never runs anyone's agents or commands, so it
// must not even link the code that does. To share something new with Team, move it
// into a shared package that holds no individual-product behaviour (as sqlitekit
// and httpkit were) and add it here, in the same change.
var teamAllowed = []string{
	teamCmd,
	teamTree, // and below
	module + "/internal/httpkit",
	module + "/internal/nativebridge", // neutral web-to-native messaging; native methods stay in each product
	module + "/internal/sqlitekit",
	module + "/internal/logging",
	// What a Team server needs to know a device is who it says: public keys and the
	// signed-message format and its checks. Not deviceid/localidentity, the one place
	// a private key lives, which no Team code may link (TestTeamNeverLinksADevicesPrivateKey).
	module + "/internal/deviceid",
	module + "/internal/envelope",
	// How a device joins a workspace: the signed invitation, the enrollment protocol and its
	// client. Like deviceid it holds no private key (the caller supplies a signer) and names no
	// network.
	module + "/internal/enrollment",
	// The contract for a private network node. Team's network (internal/team/infra/overlaynet) is one
	// implementation of it, so that what is written against the contract never names the network.
	module + "/internal/transport",
}

// teamOnlyModules are third-party modules that only Team may use: they must never
// appear in the individual product's build (go.mod is shared, so the build is where
// "does not leak" is checked). When Team gains a dependency the individual product
// does not use, name it here; teamDependenciesAreClassified fails until it is named.
var teamOnlyModules = []string{
	// The private network's certificates: Nebula's own `cert` package, used in-process to make and check them (MIT).
	"github.com/slackhq/nebula",
	// What that package is built on.
	"filippo.io/bigmod",
	"google.golang.org/protobuf",
}

// teamForbiddenStdlib are standard-library packages Team's own code must not
// import: it never starts a process or reaches into a machine.
var teamForbiddenStdlib = []string{"os/exec", "plugin", "net/rpc"}

// teamInfraTree is where Team's *infrastructure* code may one day live: the
// narrowly scoped supervision of the processes a Team deployment itself needs (its
// private-network node, its replicated database), as distinct from the developer
// execution Team must never do (shell, Git, agents, model providers, a member's
// computer). Nothing is there yet. The line is drawn now, so that when something
// is, it has to be granted here, by name, in review, and cannot be added by
// loosening the rule that keeps developer execution out. See infra_test.go.
const teamInfraTree = teamTree + "/infra"

// teamInfraExec is the whole of the exception to "Team never starts a process": a
// package under teamInfraTree, and the exact programs it may start. Today that is two
// packages and two programs: the supervisors of the private network's node (the pinned Nebula
// release Werkbord ships) and of the replicated database's node (the pinned rqlite release). A grant is checked by
// TestTheInfrastructureExceptionIsNarrow: the programs must be named by constants in
// the code and be ones that are not a way to run developer work, the package may not
// import any other Team package that handles a request, and nothing but Team's wiring
// may import it. TestTheNetworkSupervisorIsNotAGeneralRunner (nebula_test.go) holds it
// to its one entry point.
var teamInfraExec = map[string][]string{
	teamInfraTree + "/nebula": {"nebula"},
	// The replicated database: the pinned rqlite release, one program, started by one function
	// (TestTheDatabaseSupervisorIsNotAGeneralRunner).
	teamInfraTree + "/rqlite": {"rqlited"},
}

// teamForbiddenDeps are third-party packages that give remote access to a machine
// (a private-network node, SSH, a pseudo-terminal) and so have no place anywhere in
// Team's build, however indirectly they are reached.
var teamForbiddenDeps = []string{"tailscale.com", "golang.org/x/crypto/ssh", "github.com/creack/pty"}

type pkg struct {
	ImportPath string
	Dir        string
	Standard   bool
	Module     string
	Imports    []string
	TestImps   []string // imports of the package's tests, in-package and external
}

var root string

func moduleRoot(t *testing.T) string {
	t.Helper()
	if root != "" {
		return root
	}
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	root = strings.TrimSpace(string(out))
	return root
}

// watchSources reads every Go file and the module files, in this process. The go
// command runs "go list" in a subprocess, which `go test`'s result cache cannot
// see, so without this a change to an import could replay a stale pass. Reading
// the files here makes the cache notice any edit.
func watchSources(t *testing.T) {
	t.Helper()
	dir := moduleRoot(t)
	_, _ = os.ReadFile(filepath.Join(dir, "go.mod"))
	_, _ = os.ReadFile(filepath.Join(dir, "go.sum"))
	for _, top := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(dir, top), func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") {
				_, _ = os.ReadFile(path)
			}
			return nil
		})
	}
}

func goList(t *testing.T, args ...string) []pkg {
	t.Helper()
	watchSources(t)
	cmd := exec.Command("go", append([]string{"list", "-e",
		"-f", `{{.ImportPath}}|{{.Dir}}|{{.Standard}}|{{with .Module}}{{.Path}}{{end}}|{{join .Imports ","}}|{{join .TestImports ","}},{{join .XTestImports ","}}`}, args...)...)
	cmd.Dir = moduleRoot(t)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %v: %v\n%s", args, err, stderr.String())
	}
	var pkgs []pkg
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "|", 6)
		if len(f) != 6 {
			t.Fatalf("unexpected go list line %q", line)
		}
		split := func(s string) []string {
			var out []string
			for _, p := range strings.Split(s, ",") {
				if p != "" {
					out = append(out, p)
				}
			}
			return out
		}
		pkgs = append(pkgs, pkg{ImportPath: f[0], Dir: f[1], Standard: f[2] == "true", Module: f[3], Imports: split(f[4]), TestImps: split(f[5])})
	}
	return pkgs
}

func hasTeam(t *testing.T) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(moduleRoot(t), "cmd", "werkbord-team"))
	return err == nil
}

func isTeamPath(p string) bool {
	// Offline tooling beneath cmd/werkbord-team obeys the same product boundary as its executable.
	return p == teamCmd || strings.HasPrefix(p, teamCmd+"/") || p == teamTree || strings.HasPrefix(p, teamTree+"/")
}

func allowedForTeam(p string) bool {
	for _, a := range teamAllowed {
		if p == a || (a == teamTree && strings.HasPrefix(p, teamTree+"/")) {
			return true
		}
	}
	return false
}

// Rule 1: nothing the individual product is built from, or tested with, knows Team exists.
func TestIndividualProductDoesNotDependOnTeam(t *testing.T) {
	closure := goList(t, "-deps", "./cmd/werkbord")
	for _, p := range closure {
		if isTeamPath(p.ImportPath) {
			t.Errorf("the individual product's build includes %s: Team code must not be reachable from cmd/werkbord", p.ImportPath)
		}
	}
	// Tests too: a test in the individual product that imports Team would make Team a
	// requirement of testing it, and so of the isolation check.
	for _, p := range closure {
		if !strings.HasPrefix(p.ImportPath, module+"/") {
			continue
		}
		for _, imp := range append(append([]string(nil), p.Imports...), p.TestImps...) {
			if isTeamPath(imp) {
				t.Errorf("%s imports %s", p.ImportPath, imp)
			}
		}
	}
	if !containsPkg(closure, individualCmd) {
		t.Fatalf("%s is not in its own dependency list: go list is not doing what this test expects", individualCmd)
	}
}

// Rule 2: only Team's own packages may import Team's packages, so a shared package
// can never become the way the individual product reaches Team.
func TestOnlyTeamImportsTeam(t *testing.T) {
	for _, p := range goList(t, "./...") {
		if isTeamPath(p.ImportPath) || p.ImportPath == module+"/internal/archtest" {
			continue
		}
		for _, imp := range append(append([]string(nil), p.Imports...), p.TestImps...) {
			if isTeamPath(imp) {
				t.Errorf("%s imports %s, but only internal/team and cmd/werkbord-team may use Team's packages", p.ImportPath, imp)
			}
		}
	}
}

// Rule 3: Team shares only the shared plumbing. See teamAllowed.
func TestTeamImportsOnlyWhatItMayShare(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree (the isolation check removes it)")
	}
	for _, p := range goList(t, "-deps", "./cmd/werkbord-team") {
		if !strings.HasPrefix(p.ImportPath, module+"/") && p.ImportPath != module {
			continue
		}
		if !allowedForTeam(p.ImportPath) {
			t.Errorf("Team's build includes %s, which is not shared plumbing. Team must not link the individual product's code, "+
				"least of all anything that runs agents, commands or Git for a person (runner, remote, agent, gitrepo, controller, daemon). "+
				"Move what Team needs into a shared package and add that package to teamAllowed.", p.ImportPath)
		}
	}
}

// Rule 4: Team's own code never starts a process or reaches into a machine.
func TestTeamCodeNeverExecutesAnything(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	for _, p := range goList(t, "-deps", "./cmd/werkbord-team") {
		for _, bad := range teamForbiddenDeps {
			if p.ImportPath == bad || strings.HasPrefix(p.ImportPath, bad+"/") {
				t.Errorf("Team's build includes %s: remote access to a computer has no place in Team", p.ImportPath)
			}
		}
	}
	pkgs := append(goList(t, "./cmd/werkbord-team/..."), goList(t, "./internal/team/...")...)
	if len(pkgs) < 5 {
		t.Fatalf("found only %d Team packages", len(pkgs))
	}
	for _, p := range pkgs {
		for _, imp := range p.Imports {
			for _, bad := range teamForbiddenStdlib {
				// The one exception is an infrastructure package granted os/exec by name
				// (teamInfraExec), and only that package, and only os/exec.
				if imp == bad && !(bad == "os/exec" && len(teamInfraExec[p.ImportPath]) > 0) {
					t.Errorf("%s imports %s: Team coordinates and must never run commands or reach into a computer", p.ImportPath, imp)
				}
			}
		}
	}
}

// Rule 5: a Team-only third-party module never reaches the individual product, and
// a new third-party module in Team's build has to be classified.
func TestTeamDependenciesAreClassified(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	modules := func(pkgs []pkg) map[string]bool {
		m := map[string]bool{}
		for _, p := range pkgs {
			if !p.Standard && p.Module != "" && p.Module != module {
				m[p.Module] = true
			}
		}
		return m
	}
	individual := modules(goList(t, "-deps", "./cmd/werkbord"))
	team := modules(goList(t, "-deps", "./cmd/werkbord-team"))
	only := map[string]bool{}
	for _, m := range teamOnlyModules {
		only[m] = true
		if individual[m] {
			t.Errorf("%s is Team-only, and is in the individual product's build", m)
		}
	}
	var unclassified []string
	for m := range team {
		if !individual[m] && !only[m] {
			unclassified = append(unclassified, m)
		}
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Errorf("Team's build uses third-party modules the individual product does not: %v. "+
			"List them in teamOnlyModules (internal/archtest/boundary_test.go) so that the individual product is checked never to pick them up.", unclassified)
	}
}

// Rule 6: each product has a well-formed version, and the two files are separate.
// Prerelease identifiers follow SemVer: numeric identifiers have no leading zero.
var productSemver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-((0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?$`)

func TestProductVersionSyntax(t *testing.T) {
	for _, v := range []string{"0.1.0", "1.3.1", "1.3.1-preview.1", "2.0.0-rc.0", "1.0.0-0alpha"} {
		if !productSemver.MatchString(v) {
			t.Errorf("valid product version rejected: %q", v)
		}
	}
	for _, v := range []string{"v1.3.1", "01.3.1", "1.3", "1.3.1-", "1.3.1-preview..1", "1.3.1-01", "1.3.1-preview.01", "1.3.1-preview_1", "1.3.1\n"} {
		if productSemver.MatchString(v) {
			t.Errorf("invalid product version accepted: %q", v)
		}
	}
}
func TestEachProductHasItsOwnVersionFile(t *testing.T) {
	files := map[string]string{"werkbord": "cmd/werkbord/VERSION", "werkbord-team": "cmd/werkbord-team/VERSION"}
	for product, rel := range files {
		b, err := os.ReadFile(filepath.Join(moduleRoot(t), rel))
		if os.IsNotExist(err) && product == "werkbord-team" && !hasTeam(t) {
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", product, err)
			continue
		}
		if v := strings.TrimSpace(string(b)); !productSemver.MatchString(v) || string(b) != v+"\n" {
			t.Errorf("%s says %q: it must be a semantic version (optionally with prerelease identifiers) and a newline", rel, string(b))
		}
	}
}

// desktopToolkit is the native window toolkit of the desktop app (desktop/). It needs cgo and the system's web
// view, so it is a requirement of building the app and of nothing else.
const desktopToolkit = "github.com/wailsapp"

// Rule 8: the desktop app is the individual product's, and a module of its own. Neither executable's build
// (or go.mod) knows its toolkit, so the controller, the command line, the installers and Linux CI never
// need a native toolchain; and it never reaches Team.
func TestTheDesktopAppIsASeparateModuleOfTheIndividualProduct(t *testing.T) {
	dir := filepath.Join(moduleRoot(t), "desktop")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("the desktop app is not in this tree")
	}
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("desktop/ must be a module of its own: %v", err)
	}
	if !regexp.MustCompile(`(?m)^module devboard/desktop$`).Match(mod) {
		t.Errorf("desktop/go.mod must declare module devboard/desktop: that path is inside devboard/, which is what lets it use devboard/internal/…")
	}
	root, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(root), desktopToolkit) {
		t.Errorf("go.mod requires %s: the desktop toolkit belongs in desktop/go.mod, not in the controller's module", desktopToolkit)
	}
	for _, cmd := range []string{"./cmd/werkbord"} {
		for _, p := range goList(t, "-deps", cmd) {
			if strings.HasPrefix(p.ImportPath, desktopToolkit) || strings.HasPrefix(p.ImportPath, module+"/desktop") {
				t.Errorf("%s's build includes %s: the desktop app must not be reachable from the controller", cmd, p.ImportPath)
			}
		}
	}
	if hasTeam(t) {
		for _, p := range goList(t, "-deps", "./cmd/werkbord-team") {
			if strings.HasPrefix(p.ImportPath, desktopToolkit) || strings.HasPrefix(p.ImportPath, module+"/desktop") {
				t.Errorf("Team's build includes %s", p.ImportPath)
			}
		}
	}
	// The desktop app opens the individual product's controller and nobody else's.
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, _ := os.ReadFile(path)
		for _, banned := range []string{teamTree, teamCmd} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s mentions %s: the desktop app is the individual product's and never reaches Team", path, banned)
			}
		}
		return nil
	})
}

func containsPkg(pkgs []pkg, path string) bool {
	for _, p := range pkgs {
		if p.ImportPath == path {
			return true
		}
	}
	return false
}

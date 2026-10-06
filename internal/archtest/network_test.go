package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The customer-owned network's rules (15–17).
//
// Werkbord operates no relay, rendezvous server, control plane, discovery server,
// network registry or customer key service, and a Team workspace must work with every
// one of them gone. These tests cannot show that nothing Werkbord runs exists; they show
// that nothing in Team's code or configuration asks for one.

var networkRuntimeDirs = []string{
	"internal/team/api", "internal/team/config", "internal/team/domain", "internal/team/server", "internal/team/service", "internal/team/store",
	"internal/team/infra", "internal/enrollment", "cmd/werkbord-team",
}

// urlRE finds "scheme://host" in a string.
var urlRE = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*)://([^\s/"'\\<>)\x60]*)`)

// onlyOurOwnURLs reports whether every "scheme://host" in a string is one of: the join link's own scheme (which names
// no host), this computer, a format verb that is filled in from a setting, or a scheme being looked for or put before
// an address that was given.
func onlyOurOwnURLs(lit string) bool {
	for _, m := range urlRE.FindAllStringSubmatch(lit, -1) {
		scheme, host := strings.ToLower(m[1]), m[2]
		hostname := strings.SplitN(host, ":", 2)[0]
		switch {
		case scheme == "werkbord", host == "", strings.HasPrefix(host, "%"):
		case hostname == "127.0.0.1", hostname == "localhost", hostname == "[::1]":
		default:
			return false
		}
	}
	return true
}

func TestTheURLRuleCatchesWhatItIsMeantTo(t *testing.T) {
	for _, bad := range []string{"https://relay.werkbord.com/v1", "http://169.254.169.254/latest", "wss://control.example.net", "see https://login.tailscale.com/a/x for more", "https://127.0.0.1.evil.example/x"} {
		if onlyOurOwnURLs(bad) {
			t.Errorf("%q passed", bad)
		}
	}
	for _, ok := range []string{"werkbord://join/", "http://127.0.0.1:7430", "http://localhost:7430/x", "http://%s/#token=%s", "://", "https://", "usage: <werkbord://join/…> and http://127.0.0.1:7430"} {
		if !onlyOurOwnURLs(ok) {
			t.Errorf("%q was refused", ok)
		}
	}
}

func nonTestFiles(t *testing.T, rel string) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	_ = filepath.WalkDir(filepath.Join(moduleRoot(t), filepath.FromSlash(rel)), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out[path] = f
		return nil
	})
	return fset, out
}

// Rule 15: no URL, in the code that runs, leads anywhere. A workspace's machines are found by
// the addresses its owner gave, never by a name Werkbord holds.
func TestNoServiceURLIsBuiltIntoTheNetworkCode(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	watchSources(t)
	count := 0
	for _, dir := range networkRuntimeDirs {
		fset, files := nonTestFiles(t, dir)
		for path, f := range files {
			count++
			base := filepath.Base(path)
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, _ := strconv.Unquote(lit.Value)
				if !strings.Contains(s, "://") || onlyOurOwnURLs(s) {
					return true
				}
				// Two places legitimately hold a URL, and each is held to its purpose below:
				//  - the pinned release's address in the manifest, used by a build script and never by the program;
				//  - the Team console's links to a project's own repository (domain), which are for people to click.
				if base == "manifest.go" && strings.HasPrefix(s, "https://github.com/slackhq/nebula/releases/download/") {
					return true
				}
				if strings.HasPrefix(dir, "internal/team/domain") && (base == "links.go" || base == "team.go" || base == "repo.go") {
					return true
				}
				if base == "handoff.go" {
					return true // the handoff client addresses the person's own local Werkbord and their Team server
				}
				t.Errorf("%s: the string %q is a URL: the network code must not know any service's address; give it a setting the customer fills in, or none", fset.Position(lit.Pos()), s)
				return true
			})
		}
	}
	if count < 30 {
		t.Fatalf("scanned only %d files", count)
	}
	// The release address is for the build script: nothing that runs may use it.
	for _, dir := range networkRuntimeDirs {
		fset, files := nonTestFiles(t, dir)
		for path, f := range files {
			if filepath.Base(path) == "manifest.go" {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "ReleaseURL" {
					t.Errorf("%s uses ReleaseURL: the running program never downloads anything", fset.Position(id.Pos()))
				}
				return true
			})
		}
	}
}

// Rule 16: nothing the operator configures is a service, an account or an address that is not theirs.
func TestTheNetworkConfigurationNamesOnlyTheCustomersOwnThings(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	watchSources(t)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(moduleRoot(t), "internal", "team", "config", "config.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	bad := []string{"url", "uri", "registry", "controlplane", "control_plane", "coordinat", "account", "apikey", "api_key", "license", "cloud", "vendor", "rendezvous", "tailnet", "authkey", "relayserver", "lighthouse"}
	fields := 0
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Config" {
			return true
		}
		for _, fld := range ts.Type.(*ast.StructType).Fields.List {
			for _, name := range fld.Names {
				fields++
				for _, b := range bad {
					if strings.Contains(strings.ToLower(name.Name), b) {
						t.Errorf("config.Config.%s: a setting that names %q suggests a service Werkbord operates or an account with one", name.Name, b)
					}
				}
			}
		}
		return true
	})
	if fields < 10 {
		t.Fatalf("found %d settings", fields)
	}
	// Every environment variable is Team's own.
	vars := 0
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, _ := strconv.Unquote(lit.Value)
		if strings.ToUpper(s) == s && strings.Contains(s, "_") && !strings.ContainsAny(s, " .") && len(s) > 6 {
			vars++
			if !strings.HasPrefix(s, "WERKBORD_TEAM_") {
				t.Errorf("%s: Team reads only WERKBORD_TEAM_* variables, so that it never picks up another product's or another vendor's settings", s)
			}
		}
		return true
	})
	if vars < 8 {
		t.Fatalf("found only %d environment variables", vars)
	}
}

// Rule 17: a signing key never gets as far as anything that answers a request. The
// workspace's key and the network authority's key are handled by infra/pki and by the
// wiring that gives them to the service as an interface; the service, domain, API and
// store do not name them, so nothing there could put one in a response.
func TestNoSigningKeyCanReachAResponse(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	watchSources(t)
	secretWords := []string{"privatekey", "signingkey", "trustseed", "cakey", "secretkey", "seed(", "pemkey"}
	for _, dir := range []string{"internal/team/api", "internal/team/service", "internal/team/domain", "internal/team/store"} {
		fset, files := nonTestFiles(t, dir)
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				var name string
				switch x := n.(type) {
				case *ast.Ident:
					name = x.Name
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						name = x.Value
					}
				}
				for _, w := range secretWords {
					if name != "" && strings.Contains(strings.ToLower(name), strings.TrimSuffix(w, "(")) && !strings.Contains(strings.ToLower(name), "pubkey") {
						// A device's *public* signing key (deviceid.PublicKey) is the registry's business.
						if strings.Contains(strings.ToLower(name), "public") {
							continue
						}
						t.Errorf("%s: %s names a private key; only the key vault and the wiring touch one", fset.Position(n.Pos()), name)
					}
				}
				return true
			})
		}
	}
}

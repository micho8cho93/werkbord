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

// Rule 7: the Team server never reaches out.
//
// Team coordinates people; it is not a client of anything. Its server packages
// (internal/team/...) make no outbound connection, read no file but the ones their
// own configuration names, read the environment only in the configuration, and
// never hand one member's request to anything on another member's computer. The
// only program in the Team product that is an HTTP client is `werkbord-team
// handoff` (cmd/werkbord-team/handoff.go), which runs on a developer's own
// computer and addresses their own loopback Werkbord and their Team server.
//
// Rule 4 already keeps os/exec out; this rule looks at what the code does with the
// packages it may import.

// serverImportAllowed is where a Team server package may import a package that can
// touch the machine: the package (relative to internal/team) it is allowed in.
var serverImportAllowed = map[string][]string{
	"os":                           {"config"},           // the environment and the data directory
	"path/filepath":                {"config"},           // the data directory's path
	"net":                          {"config", "server"}, // validating and opening the listen address
	"syscall":                      {},                   // nothing
	"unsafe":                       {},                   // nothing
	"net/http/httputil":            {},                   // a reverse proxy is how one request becomes another
	"net/http/cgi":                 {},                   // runs programs
	"net/http/fcgi":                {},                   // runs programs
	"net/smtp":                     {},                   // outbound mail
	"net/rpc":                      {},                   // remote calls
	"crypto/tls":                   {},                   // Team serves plain HTTP behind the operator's proxy
	"golang.org/x/net":             {},                   // websockets, proxies
	"golang.org/x/crypto/ssh":      {},                   // remote shells
	"github.com/gorilla/websocket": {},                   // sockets are a way into a machine; sync is a plain long poll
	"nhooyr.io/websocket":          {},
}

// outboundCalls are selectors on net/http and net that open a connection from the server.
var outboundCalls = map[string]map[string]bool{
	"http": {"Client": true, "DefaultClient": true, "Get": true, "Post": true, "PostForm": true, "Head": true, "NewRequest": true,
		"NewRequestWithContext": true, "DefaultTransport": true, "Transport": true, "ProxyFromEnvironment": true, "ProxyURL": true},
	"net": {"Dial": true, "DialTimeout": true, "DialTCP": true, "DialUDP": true, "DialUnix": true, "DialIP": true, "Dialer": true,
		"LookupHost": true, "LookupIP": true, "LookupAddr": true, "LookupCNAME": true, "LookupTXT": true, "LookupSRV": true, "LookupMX": true,
		"Resolver": true, "DefaultResolver": true},
}

func TestTeamServerNeverReachesOut(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	watchSources(t)
	base := filepath.Join(moduleRoot(t), "internal", "team")
	fset := token.NewFileSet()
	files := 0
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(base, filepath.Dir(path))
		pkgName := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		files++
		names := map[string]string{} // local name → import path
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			local := p[strings.LastIndex(p, "/")+1:]
			if im.Name != nil {
				local = im.Name.Name
			}
			names[local] = p
			for banned, where := range serverImportAllowed {
				// Standard-library entries match exactly (net must not catch net/http);
				// third-party ones match the whole module.
				thirdParty := strings.Contains(strings.SplitN(banned, "/", 2)[0], ".")
				if p != banned && !(thirdParty && strings.HasPrefix(p, banned+"/")) {
					continue
				}
				ok := false
				for _, w := range where {
					ok = ok || w == pkgName
				}
				if !ok {
					t.Errorf("%s imports %s: the Team server may not (allowed only in %v). See rule 7 in internal/archtest/reach_test.go.", filepath.ToSlash(path[len(moduleRoot(t))+1:]), p, where)
				}
			}
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
			pkg := names[id.Name]
			short := pkg[strings.LastIndex(pkg, "/")+1:]
			if (pkg == "net/http" || pkg == "net") && outboundCalls[short][sel.Sel.Name] {
				t.Errorf("%s uses %s.%s: the Team server makes no outbound connections", fset.Position(sel.Pos()), id.Name, sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 20 {
		t.Fatalf("scanned only %d files: the scan is not finding Team's code", files)
	}
}

// The individual product does not know Team exists: none of the code it is built
// from names Team's API, settings or executable in a string (comments may explain
// why the updater ignores Team's tags, for instance).
func TestIndividualProductDoesNotMentionTeam(t *testing.T) {
	watchSources(t)
	closure := goList(t, "-deps", "./cmd/werkbord")
	fset := token.NewFileSet()
	checked := 0
	for _, p := range closure {
		if !strings.HasPrefix(p.ImportPath, module+"/") {
			continue
		}
		entries, err := os.ReadDir(p.Dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, e.Name()), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			checked++
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				for _, banned := range []string{"/api/team/v1", "WERKBORD_TEAM_", "werkbord-team"} {
					if strings.Contains(lit.Value, banned) {
						t.Errorf("%s has the string %s: the individual product must not know about Team", fset.Position(lit.Pos()), lit.Value)
					}
				}
				return true
			})
		}
	}
	if checked < 50 {
		t.Fatalf("checked only %d files of the individual product", checked)
	}
}

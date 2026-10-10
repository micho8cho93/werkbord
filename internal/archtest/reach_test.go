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
// never hand one member's request to anything on another member's computer. What
// talks to a member's own Werkbord is the Team service's synchronization on that
// member's computer (internal/team/connector), over loopback and with a narrow grant.
//
// Rule 4 already keeps os/exec out; this rule looks at what the code does with the
// packages it may import.

// serverImportAllowed is where a Team server package may import a package that can
// touch the machine: the package (relative to internal/team) it is allowed in.
var serverImportAllowed = map[string][]string{
	"os":                           {"config", "infra", "server", "store", "devicestate"},                 // the environment and the data directory; infrastructure reads and writes its own key and node files; the wiring opens the key vault and reads the passphrase file that the configuration names, and nothing else; the replicated store keeps its local copy and its backups in directories the configuration names
	"path/filepath":                {"config", "infra", "server", "store", "devicestate"},                 // the data directory's path
	"net":                          {"config", "server", "infra", "store", "localwerkbord", "hostclient"}, // validating and opening the listen address; infrastructure parses the addresses of its own network; the replicated store tells a refused connection (nothing was sent) from a lost answer, and may dial only what outboundAllowed grants it
	"syscall":                      {"infra"},                                                             // only infrastructure, to check who owns the files it starts or reads keys from, and to signal its own node; infra_test.go bans syscall.Exec and the like there
	"unsafe":                       {},                                                                    // nothing
	"net/http/httputil":            {},                                                                    // a reverse proxy is how one request becomes another
	"net/http/cgi":                 {},                                                                    // runs programs
	"net/http/fcgi":                {},                                                                    // runs programs
	"net/smtp":                     {},                                                                    // outbound mail
	"net/rpc":                      {},                                                                    // remote calls
	"crypto/tls":                   {},                                                                    // Team serves plain HTTP behind the operator's proxy
	"golang.org/x/net":             {},                                                                    // websockets, proxies
	"golang.org/x/crypto/ssh":      {},                                                                    // remote shells
	"github.com/gorilla/websocket": {},                                                                    // sockets are a way into a machine; sync is a plain long poll
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

// outboundAllowed is the whole of the exception to "the Team server makes no outbound connection": the
// package that is the workspace's private network as a transport, whose Dial refuses anything outside the
// workspace's own address range (overlaynet_test.go proves it), and which only Team's wiring may import
// (rule 9). Everything else in Team may still open no connection.
//
// The database is the second. A Workspace Host's own storage layer (store/replicated) and its supervisor
// (infra/rqlite) are clients of the rqlite nodes of its own workspace, and of nothing else: each refuses
// any address that is not loopback or inside a private network (TestTheDatabaseClientsTalkOnlyToPrivateAddresses
// in each package proves it), and only Team's wiring may import them. The test harness for real clusters
// (infra/rqlite/rqlitetest) opens connections on loopback.
var outboundAllowed = map[string]map[string]bool{
	"infra/overlaynet":        {"Dialer": true},
	"infra/rqlite":            {"Client": true, "NewRequestWithContext": true},
	"infra/rqlite/rqlitetest": {"DialTimeout": true},
	"store/replicated":        {"Client": true, "NewRequestWithContext": true, "Transport": true, "Dialer": true},
	// The bridge to the person's own Werkbord on this computer. Its transport dials only a literal loopback address
	// (TestTheBridgeTalksOnlyToThisComputer in localwerkbord proves it: a name, a private address and a redirect are all refused),
	// and what it sends is the short list the individual product's local access allows. Only the daemon's wiring uses it.
	"localwerkbord": {"Client": true, "NewRequestWithContext": true, "Transport": true, "Dialer": true},
	// A device's client of its own workspace: the Team API of a Workspace Host, with the device's own credential. It dials literal
	// loopback addresses and addresses inside the workspace's private network and nothing else (TestTheClientDialsOnlyThisComputerAndTheWorkspacesNetwork
	// in hostclient), follows no redirect, and only the daemon's wiring uses it.
	"hostclient": {"Client": true, "NewRequestWithContext": true, "Transport": true, "Dialer": true},
}

func TestTeamServerNeverReachesOut(t *testing.T) {
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
		relDir := filepath.ToSlash(rel)
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
			if (pkg == "net/http" || pkg == "net") && outboundCalls[short][sel.Sel.Name] && !outboundAllowed[relDir][sel.Sel.Name] {
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

// The controller holds no Team credential and makes no call to Team: none of the code it is built from names Team's API,
// settings or executable in a string (comments may explain why the updater ignores Team's old tags, for instance). Team's
// service on a member's own computer reaches the controller with a narrow grant; the controller never reaches back.
func TestTheControllerHoldsNoTeamCredentialOrAPI(t *testing.T) {
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
						t.Errorf("%s has the string %s: the controller must not call Team or hold its settings", fset.Position(lit.Pos()), lit.Value)
					}
				}
				return true
			})
		}
	}
	if checked < 50 {
		t.Fatalf("checked only %d files of the controller", checked)
	}
}

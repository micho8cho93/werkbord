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

// The foundation rules (10–13): the product-neutral packages stay neutral, a
// device's private key stays on the device, and no application or domain code
// names a particular network or database.

// sharedPackages are the packages both products may use. They hold no behaviour
// of either product, so they import the standard library, third-party modules that
// say nothing about a product, and each other, and no package of either product.
var sharedPackages = []string{
	module + "/internal/integration",
	module + "/internal/workspace",
	module + "/internal/nativebridge",
	module + "/internal/httpkit",
	module + "/internal/sqlitekit",
	module + "/internal/logging",
	module + "/internal/transport",
	module + "/internal/transport/memtransport",
	module + "/internal/transport/transporttest",
	module + "/internal/deviceid",
	module + "/internal/deviceid/localidentity",
	module + "/internal/envelope",
	module + "/internal/enrollment",
}

func isShared(p string) bool {
	for _, s := range sharedPackages {
		if p == s {
			return true
		}
	}
	return false
}

// vendorWords name a particular network, relay, coordination or storage product.
// They belong to the one package that adapts that product, never to code written
// against a contract.
var vendorWords = []string{"tailscale", "tsnet", "tailnet", "headscale", "nebula", "lighthouse", "derp", "rqlite", "wireguard", "magicdns"}

// Rule 10: a shared package depends on neither product, and on no network or
// database vendor: only on the standard library and on other shared packages.
func TestSharedPackagesAreProductNeutral(t *testing.T) {
	byPath := map[string]pkg{}
	for _, p := range goList(t, "./...") {
		byPath[p.ImportPath] = p
	}
	for _, path := range sharedPackages {
		p, ok := byPath[path]
		if !ok {
			t.Errorf("%s is listed as shared but does not exist", path)
			continue
		}
		for _, imp := range append(append([]string(nil), p.Imports...), p.TestImps...) {
			if strings.HasPrefix(imp, module+"/") && !isShared(imp) {
				t.Errorf("%s imports %s: a shared package must not depend on either product's code (move what it needs into a shared package)", path, imp)
			}
			if path == module+"/internal/sqlitekit" || path == module+"/internal/httpkit" || path == module+"/internal/logging" {
				continue // older plumbing: their own rules are in boundary_test.go
			}
			for _, w := range vendorWords {
				if strings.Contains(strings.ToLower(imp), w) {
					t.Errorf("%s imports %s: the product-neutral packages name no network or database vendor", path, imp)
				}
			}
		}
	}
}

// Rule 11: a device's private key lives in one package, and Team does not link it.
// Team sees public keys (deviceid) and checks signatures (envelope); it has no code
// that could create, hold, load or use a key that signs for a device.
func TestTeamNeverLinksADevicesPrivateKey(t *testing.T) {
	if !hasTeam(t) {
		t.Skip("Team is not in this tree")
	}
	const keyHolder = module + "/internal/deviceid/localidentity"
	for _, p := range goList(t, "-deps", "./cmd/werkbord-team") {
		if p.ImportPath == keyHolder {
			t.Errorf("Team's build includes %s: private signing keys exist only on a device, and Team must not have code that holds one", keyHolder)
		}
	}
	for _, p := range goList(t, "./...") {
		if !isTeamPath(p.ImportPath) {
			continue
		}
		for _, imp := range p.Imports {
			if imp == keyHolder {
				t.Errorf("%s imports %s", p.ImportPath, imp)
			}
		}
	}
	// And the public package has no way to make or hold a private key.
	dir := filepath.Join(moduleRoot(t), "internal", "deviceid")
	entries, _ := os.ReadDir(dir)
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "ed25519" && (x.Sel.Name == "PrivateKey" || x.Sel.Name == "GenerateKey" || x.Sel.Name == "Sign" || x.Sel.Name == "NewKeyFromSeed") {
					t.Errorf("%s: internal/deviceid (which Team links) uses ed25519.%s: signing belongs in localidentity", fset.Position(x.Pos()), x.Sel.Name)
				}
			}
			return true
		})
	}
}

// codeNames returns the identifiers and string literals of a package's non-test
// source (comments are not code, and are free to explain what the package is not).
func codeNames(t *testing.T, dir string) (names map[string]token.Position, files int) {
	t.Helper()
	names = map[string]token.Position{}
	fset := token.NewFileSet()
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				names[strings.ToLower(x.Name)] = fset.Position(x.Pos())
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					names[strings.ToLower(x.Value)] = fset.Position(x.Pos())
				}
			case *ast.ImportSpec:
				names[strings.ToLower(x.Path.Value)] = fset.Position(x.Pos())
			}
			return true
		})
		return nil
	})
	return names, files
}

// Rule 12: code written against a contract does not name what is behind it. The
// transport contract, device identity, envelopes, and Team's domain, service and
// API know nothing of Tailscale, Nebula, DERP, lighthouses, Headscale or rqlite:
// those are implementations, in the packages that adapt them, and the code above
// can be moved from one to another without edit.
func TestApplicationCodeNamesNoNetworkOrDatabaseVendor(t *testing.T) {
	dirs := []string{
		"internal/transport", "internal/deviceid", "internal/envelope", "internal/enrollment",
		"internal/team/domain", "internal/team/service", "internal/team/api",
	}
	total := 0
	for _, d := range dirs {
		dir := filepath.Join(moduleRoot(t), filepath.FromSlash(d))
		if _, err := os.Stat(dir); err != nil {
			if strings.HasPrefix(d, "internal/team") && !hasTeam(t) {
				continue
			}
			t.Fatalf("%s: %v", d, err)
		}
		names, files := codeNames(t, dir)
		total += files
		for name, pos := range names {
			for _, w := range vendorWords {
				if strings.Contains(name, w) {
					t.Errorf("%s: %s in %s's code: it is written against a contract and must not name a vendor (%s)", pos, name, d, w)
				}
			}
		}
	}
	// The scan must be finding the code: fewer files is expected only when Team has
	// been removed (the isolation check), which leaves the shared packages alone.
	want := 15
	if !hasTeam(t) {
		want = 8
	}
	if total < want {
		t.Fatalf("scanned only %d files", total)
	}
}

// Rule 13: the Tailscale client is linked by one package, the adapter, so the rest
// of the product reaches a private network only through netprivate's own types or
// internal/transport.
func TestOnlyTheAdapterImportsTailscale(t *testing.T) {
	for _, p := range goList(t, "./...") {
		for _, imp := range p.Imports {
			if (imp == "tailscale.com" || strings.HasPrefix(imp, "tailscale.com/")) && p.ImportPath != module+"/internal/netprivate" {
				t.Errorf("%s imports %s: only internal/netprivate adapts Tailscale", p.ImportPath, imp)
			}
		}
	}
}

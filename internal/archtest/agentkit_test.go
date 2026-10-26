package archtest

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Asking a coding agent's command line about itself (is it there, which version, is anyone signed in) is agentkit's job,
// for the adapters (internal/agent) and the providers (internal/assistant/provider) alike. Each family used to carry its
// own copy, so a fix had to be made twice and the same CLI was asked the same questions again. This test fails when a
// copy comes back.
var agentFamilies = []string{"internal/agent", "internal/assistant/provider"}

func TestNoAdapterOrProviderAsksAnAgentAboutItselfOnItsOwn(t *testing.T) {
	watchSources(t)
	count := 0
	for _, dir := range agentFamilies {
		fset, files := nonTestFiles(t, dir)
		for path, f := range files {
			if strings.Contains(filepath.ToSlash(path), "/testdata/") {
				continue
			}
			count++
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, _ := strconv.Unquote(lit.Value)
				switch {
				case s == "--version":
					t.Errorf("%s: asks a command for its version: use agentkit.Prober, which asks once for both families", fset.Position(lit.Pos()))
				case strings.Contains(s, `\d+\.\d+\.\d+`):
					t.Errorf("%s: its own version regexp: use agentkit.Version", fset.Position(lit.Pos()))
				}
				return true
			})
			// A call to a sign-in status command: `"auth", "status"` or `"login", "status"`.
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for i := 0; i+1 < len(call.Args); i++ {
					a, ok1 := call.Args[i].(*ast.BasicLit)
					b, ok2 := call.Args[i+1].(*ast.BasicLit)
					if ok1 && ok2 && a.Kind == token.STRING && b.Kind == token.STRING {
						x, _ := strconv.Unquote(a.Value)
						y, _ := strconv.Unquote(b.Value)
						if y == "status" && (x == "auth" || x == "login") {
							t.Errorf("%s: asks whether the agent is signed in: use agentkit.Prober", fset.Position(call.Pos()))
						}
					}
				}
				return true
			})
		}
	}
	if count < 20 {
		t.Fatalf("looked at %d files: the test is not looking in the right place", count)
	}
}

// agentkit is a leaf: the adapters and the providers import it, so it can import neither (or anything else of the
// product but the domain).
func TestAgentkitIsALeaf(t *testing.T) {
	watchSources(t)
	fset, files := nonTestFiles(t, "internal/agentkit")
	for path, f := range files {
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, "werkbord/internal/") && p != "werkbord/internal/domain" {
				t.Errorf("%s: agentkit imports %s: it is a leaf; adapters and providers import it, not the reverse", fset.Position(imp.Pos()), p)
			}
		}
		_ = path
	}
}

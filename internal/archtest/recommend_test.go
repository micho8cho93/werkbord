package archtest

import (
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

// Suggested next steps are advice. These tests keep it so, at the level of source: the rules are a pure function; the code
// that gathers facts and the operation that offers them only read; and nothing a suggestion carries starts anything.
// Memory can order suggestions, never extend them, and nothing a model writes becomes an action; the behaviour is
// tested where it lives (internal/domain, internal/service, internal/appops). What is pinned here is the shape that
// makes the behaviour safe to rely on.

func sourceFiles(t *testing.T, rel string, match func(name string) bool) []string {
	t.Helper()
	watchSources(t)
	entries, err := os.ReadDir(filepath.Join(moduleRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") && match(e.Name()) {
			out = append(out, filepath.Join(moduleRoot(t), rel, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func parse(t *testing.T, path string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestTheRulesForSuggestionsArePure: the rules import no I/O, no process, no network, no store and no model, and read no
// clock. Facts in, answer out.
func TestTheRulesForSuggestionsArePure(t *testing.T) {
	allowed := map[string]bool{"fmt": true, "regexp": true, "sort": true, "strings": true, "time": true, "unicode": true, "unicode/utf8": true, "crypto/sha256": true, "encoding/hex": true}
	files := sourceFiles(t, "internal/domain", func(n string) bool { return strings.HasPrefix(n, "recommend") })
	if len(files) < 4 {
		t.Fatalf("expected the rules' sources, found %v", files)
	}
	for _, path := range files {
		f := parse(t, path)
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !allowed[p] {
				t.Errorf("%s imports %s: the rules for suggestions are a pure function of the facts they are given", filepath.Base(path), p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && (sel.Sel.Name == "Now" || sel.Sel.Name == "Since" || sel.Sel.Name == "Until") {
						t.Errorf("%s reads the clock (time.%s): the caller supplies the time", filepath.Base(path), sel.Sel.Name)
					}
				}
			}
			return true
		})
	}
}

// TestSuggestionsOnlyReadAndNeverStartAnything: the code that gathers the facts, offers them to the assistant and serves
// them over HTTP calls nothing that changes a repository, a pull request, a task, a run or the deck. An action a suggestion
// carries is a way to OPEN a screen that asks.
func TestSuggestionsOnlyReadAndNeverStartAnything(t *testing.T) {
	banned := map[string]bool{
		"Push": true, "Merge": true, "MergePlan": true, "DeleteBranch": true, "CleanWorktree": true, "CreatePullRequest": true, "Fetch": true,
		"StartDeckWorkflows": true, "PrepareDeckTask": true, "PrepareDeckTasks": true, "SetDeck": true,
		"CreateTask": true, "CreateTicket": true, "UpdateTicket": true, "AnswerQuestion": true, "Start": true, "Stop": true, "Finish": true,
		"AddWorktree": true, "Command": true, "CommandContext": true, "Dismiss": true, "Reopen": true, "Refresh": true,
	}
	targets := map[string][]string{
		"internal/service": {"recommend.go"},
		"internal/appops":  {"recommend.go"},
		"internal/api":     {"recommend.go"},
	}
	for dir, names := range targets {
		for _, name := range names {
			path := filepath.Join(moduleRoot(t), dir, name)
			f := parse(t, path)
			for _, imp := range f.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); p == "os/exec" || p == module+"/internal/gitrepo" || p == module+"/internal/runner" {
					t.Errorf("%s imports %s: suggestions read, they do not run anything", name, p)
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && banned[sel.Sel.Name] {
						// Feedback writes the person's own memory; it is the one write there is, and it is in the Recommender.
						t.Errorf("%s/%s calls .%s(): a suggestion only reads, and opens a screen where the person decides", dir, name, sel.Sel.Name)
					}
				}
				return true
			})
		}
	}
}

// TestNoSuggestionActionStartsExecution: every kind of action is an "open_" one, and nowhere is StartsExecution made true.
func TestNoSuggestionActionStartsExecution(t *testing.T) {
	f := parse(t, filepath.Join(moduleRoot(t), "internal", "domain", "recommend.go"))
	opens := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok {
			for i, name := range vs.Names {
				if strings.HasPrefix(name.Name, "Action") && i < len(vs.Values) {
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
						v, _ := strconv.Unquote(lit.Value)
						if !strings.HasPrefix(v, "open_") {
							t.Errorf("action kind %s = %q: every suggestion action opens a screen", name.Name, v)
						}
						opens++
					}
				}
			}
		}
		return true
	})
	if opens < 5 {
		t.Fatalf("found only %d action kinds", opens)
	}
	for _, path := range sourceFiles(t, "internal/domain", func(n string) bool { return strings.HasPrefix(n, "recommend") }) {
		ast.Inspect(parse(t, path), func(n ast.Node) bool {
			if kv, ok := n.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "StartsExecution" {
					if v, ok := kv.Value.(*ast.Ident); !ok || v.Name != "false" {
						t.Errorf("%s sets StartsExecution to something other than false", filepath.Base(path))
					}
				}
			}
			if as, ok := n.(*ast.AssignStmt); ok {
				for i, lhs := range as.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "StartsExecution" && i < len(as.Rhs) {
						if v, ok := as.Rhs[i].(*ast.Ident); !ok || v.Name != "false" {
							t.Errorf("%s assigns StartsExecution", filepath.Base(path))
						}
					}
				}
			}
			return true
		})
	}
}

// TestSuggestionsAddNoPermissionToTheAssistant: the operation that offers them uses the existing memory:read permission and
// is a read; there is no new grant, so a read-only assistant and a restricted one keep exactly the scope they had.
func TestSuggestionsAddNoPermissionToTheAssistant(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "appops", "operations.go"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), `Name: "recommend_next_actions"`)
	if i < 0 {
		t.Fatal("the operation is not in the catalog")
	}
	decl := string(src)[i : i+strings.Index(string(src)[i:], "run: opRecommend")]
	if !strings.Contains(decl, "Kind: KindRead") || !strings.Contains(decl, "Permission: PermMemoryRead") || strings.Contains(decl, "KindMutation") {
		t.Fatalf("recommend_next_actions must be a read needing only memory:read: %s", decl)
	}
}

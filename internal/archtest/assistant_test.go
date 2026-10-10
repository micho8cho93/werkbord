package archtest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The assistant lets a model, which reads text written by other people and programs, look at the board and propose
// changes. These tests keep the shape that makes that safe:
//
//   - the engine and the operations are Individual's: Team's build links none of them, so a Team workspace stays a place
//     that coordinates and never a place that runs somebody's model or agent;
//   - the operations reach the board only through the domain services, and the engine only through the operations: a
//     model-driven change cannot skip the rules the services enforce or the confirmation the operations require;
//   - only the provider packages start a process, and none of them is ever told to skip its own safety checks.

const (
	appopsPkg    = module + "/internal/appops"
	assistantPkg = module + "/internal/assistant"
	providerTree = assistantPkg + "/provider"
)

func assistantPackages(t *testing.T) []pkg {
	t.Helper()
	return goList(t, "./internal/appops/...", "./internal/assistant/...")
}

// TestTeamLinksNeitherTheAssistantNorItsOperations: Team coordinates; it never executes, and an assistant runs the person's own
// coding agent. (Rule 2 already fails on any package Team links that is not in its allow-list; this says why for these.)
func TestTeamLinksNeitherTheAssistantNorItsOperations(t *testing.T) {
	deps := goList(t, "-deps", teamCmd)
	for _, p := range deps {
		if p.ImportPath == appopsPkg || p.ImportPath == assistantPkg || strings.HasPrefix(p.ImportPath, assistantPkg+"/") || strings.HasPrefix(p.ImportPath, appopsPkg+"/") {
			t.Errorf("Team's build links %s: the assistant and its operations run the person's own agent runtime on their own computer, which Team never does (docs/STRUCTURE.md, docs/ASSISTANT.md)", p.ImportPath)
		}
	}
	for _, p := range goList(t, "./internal/team/...", "./cmd/werkbord-team/...") {
		for _, imp := range append(append([]string{}, p.Imports...), p.TestImps...) {
			if imp == appopsPkg || strings.HasPrefix(imp, assistantPkg) {
				t.Errorf("%s imports %s", p.ImportPath, imp)
			}
		}
	}
}

// TestTheOperationsReachTheBoardOnlyThroughTheServices and the engine only through the operations.
func TestTheOperationsReachTheBoardOnlyThroughTheServices(t *testing.T) {
	allowed := map[string][]string{
		// The operations: the domain services and the vocabulary they speak; the store only for the assistant's own records.
		appopsPkg: {module + "/internal/domain", module + "/internal/planning", module + "/internal/service", module + "/internal/store"},
		// The engine: the operations, the providers, the vocabulary and the assistant's own records. Never a service, a runner,
		// Git, an agent adapter or the HTTP layer.
		assistantPkg: {appopsPkg, providerTree, module + "/internal/domain", module + "/internal/store"},
	}
	for _, p := range assistantPackages(t) {
		want, ok := allowed[p.ImportPath]
		if !ok {
			continue
		}
		for _, imp := range p.Imports {
			if !strings.HasPrefix(imp, module+"/") {
				switch imp {
				case "os/exec", "net/http", "net", "syscall", "plugin", "unsafe":
					t.Errorf("%s imports %s: only a provider starts a process or opens a connection", p.ImportPath, imp)
				}
				continue
			}
			ok := false
			for _, a := range want {
				if imp == a {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s imports %s. It may import only %s. Anything else is a way round the rules the services and the operations enforce; give the operation or the provider what it needs through them instead.",
					p.ImportPath, imp, strings.Join(want, ", "))
			}
		}
	}
}

// TestOnlyProvidersStartProcesses: of everything the assistant is made of, only the process runner and the providers
// may start one; the providers import nothing of the board.
func TestOnlyProvidersStartProcesses(t *testing.T) {
	boardPackages := []string{"/internal/service", "/internal/store", "/internal/runner", "/internal/gitrepo", "/internal/github", "/internal/api", "/internal/appops", "/internal/controller"}
	for _, p := range assistantPackages(t) {
		inProviders := strings.HasPrefix(p.ImportPath, providerTree)
		for _, imp := range p.Imports {
			if imp == "os/exec" && !inProviders {
				t.Errorf("%s starts a process", p.ImportPath)
			}
			if inProviders {
				for _, b := range boardPackages {
					if imp == module+b {
						t.Errorf("%s imports %s: a provider runs a model and knows nothing of the board", p.ImportPath, imp)
					}
				}
			}
		}
	}
}

// TestNoProviderIsEverToldToSkipItsSafetyChecks reads the providers' source: the flags that would let a model act without
// asking are not there to be switched on by a mistake or a setting.
func TestNoProviderIsEverToldToSkipItsSafetyChecks(t *testing.T) {
	watchSources(t)
	banned := []string{"dangerously", "bypassPermissions", "danger-full-access", "--yolo", "skip-permissions", "--full-auto", "acceptEdits", "--mcp-config", "--add-dir", "workspace-write"}
	var files []string
	_ = filepath.WalkDir(filepath.Join(moduleRoot(t), "internal", "assistant", "provider"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	if len(files) < 4 {
		t.Fatalf("expected the providers' sources, found %v", files)
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		src := string(b)
		for _, w := range banned {
			for _, line := range strings.Split(src, "\n") {
				trim := strings.TrimSpace(line)
				if strings.HasPrefix(trim, "//") {
					continue // a comment may say what is not done
				}
				if strings.Contains(line, w) {
					t.Errorf("%s mentions %q in code: the assistant's providers never run with those protections off", filepath.Base(f), w)
				}
			}
		}
	}
	// And the protections that are there to stay.
	need := map[string][]string{
		"claude/claude.go": {`"--tools", ""`, `"--include-partial-messages"`, "--safe-mode", "--strict-mcp-config", `"--setting-sources"`},
		"codex/run.go":     {`"approvalPolicy": "never"`, `"sandbox": "read-only"`, "allowedItems"},
		"codex/codex.go":   {`"shell_tool"`},
	}
	for rel, wants := range need {
		b, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "assistant", "provider", rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range wants {
			if !strings.Contains(string(b), w) {
				t.Errorf("provider/%s no longer has %s", rel, w)
			}
		}
	}
}

// TestTheAssistantOffersNoRouteToRunsGitOrSettings reads the operation catalog's source: the permissions that exist are the
// ones reviewed, and none of them is about running, stopping or configuring anything.
func TestTheAssistantOffersNoRouteToRunsGitOrSettings(t *testing.T) {
	watchSources(t)
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "appops", "appops.go"))
	if err != nil {
		t.Fatal(err)
	}
	var perms []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Perm") && strings.Contains(line, `Permission = "`) {
			perms = append(perms, line[strings.Index(line, `"`)+1:strings.LastIndex(line, `"`)])
		}
	}
	sort.Strings(perms)
	want := "projects:read,questions:answer,questions:read,runs:read,schedule:read,tickets:create,tickets:read,tickets:update"
	if got := strings.Join(perms, ","); got != want {
		t.Fatalf("the assistant's permissions are %s, reviewed as %s. Adding one is a decision about what a model may do; update this test and docs/ASSISTANT.md in the same change, and say why.", got, want)
	}
	for _, f := range []string{"backend.go", "operations.go"} {
		src, _ := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "appops", f))
		for _, w := range []string{"Runner.", ".Start(", ".Stop(", ".Finish(", "Git.", "gitrepo", "SetExecution", "Settings.", "Worktrees.", "Archived:", "Orchestration:", "Execution:"} {
			for _, line := range strings.Split(string(src), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				if strings.Contains(line, w) {
					t.Errorf("appops/%s uses %q: the assistant has no operation that runs, stops, configures or archives anything", f, w)
				}
			}
		}
	}
}

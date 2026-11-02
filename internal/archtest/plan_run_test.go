package archtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Running a plan and falling back to another agent arm and start work on the person's own computer. A Team plan coordinates
// who holds which step; it can never start another member's step. That is guaranteed by what Team can reach, so this pins
// it three ways: Team's build does not link the services or the runner, no Team source names the plan-run or fallback
// operations, and the Team API has no route that could carry them.

func TestATeamPlanCanNeverRunAnotherMembersStep(t *testing.T) {
	forbidden := []string{module + "/internal/service", module + "/internal/runner"}
	for _, p := range goList(t, "-deps", teamCmd) {
		for _, bad := range forbidden {
			if p.ImportPath == bad {
				t.Errorf("Team's build links %s, which can arm and start work on a computer", bad)
			}
		}
	}
	for _, p := range goList(t, "./internal/team/...", "./cmd/werkbord-team/...") {
		// Shipped code only: Team's integration tests drive a real Individual controller on purpose.
		for _, imp := range p.Imports {
			for _, bad := range forbidden {
				if imp == bad {
					t.Errorf("%s imports %s", p.ImportPath, imp)
				}
			}
		}
	}
	// Names of the operations that arm, pause or continue work. A Team source that mentions one is reaching for them.
	operations := []string{"RunPlan", "HoldPlan", "PlanRunPreview", "PlanRunInput", "FallbackCandidates", "ClaimFallback", "ResolveFallback", "FallbackSettings"}
	routes := []string{"work-plans/{plan}/run", "work-plans/{plan}/hold", "/fallback"}
	root := moduleRoot(t)
	scanned := 0
	for _, dir := range []string{filepath.Join(root, "internal", "team"), filepath.Join(root, "cmd", "werkbord-team")} {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				t.Error(e)
				return nil
			}
			scanned++
			text := string(raw)
			for _, name := range append(operations, routes...) {
				if strings.Contains(text, name) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s mentions %q: Team coordinates plans and never runs them", rel, name)
				}
			}
			return nil
		})
	}
	if scanned < 20 {
		t.Fatalf("scanned only %d Team files", scanned)
	}
}

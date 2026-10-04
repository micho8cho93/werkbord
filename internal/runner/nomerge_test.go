package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
)

func headOf(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", rev).CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse %s: %v %s", rev, err, out)
	}
	return strings.TrimSpace(string(out))
}

// An agent that commits and finishes leaves its work on its own branch. Nothing
// merges it: not the run ending, not the task being moved, not the worktree
// being kept. Merging is only ever something a person asks for, through the Git
// Control Center, which this package does not have.
func TestFinishingARunNeverMergesIntoTheTargetBranch(t *testing.T) {
	e := newEnv(t)
	mainBefore := headOf(t, e.repo, "main")
	run := e.start(e.task("Ship it"))
	wt := e.worktreeOf(run)

	if err := os.WriteFile(filepath.Join(wt.Path, "feature.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, wt.Path, "add", "-A")
	git(t, wt.Path, "commit", "-q", "-m", "the agent's work")
	e.session().Assistant("All done, committed.")
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
	if _, err := e.mgr.Finish(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	e.waitState(run.ID, domain.RunCompleted)

	if got := headOf(t, e.repo, "main"); got != mainBefore {
		t.Fatalf("main moved from %s to %s when the run finished", mainBefore, got)
	}
	if branchTip := headOf(t, e.repo, wt.Branch); branchTip == mainBefore {
		t.Fatal("the agent's commit is not on its branch")
	}
	if out, err := exec.Command("git", "-C", e.repo, "merge-base", "--is-ancestor", wt.Branch, "main").CombinedOutput(); err == nil {
		t.Fatalf("the agent's branch was merged into main: %s", out)
	}
	if st, _ := exec.Command("git", "-C", e.repo, "status", "--porcelain").CombinedOutput(); strings.TrimSpace(string(st)) != "" {
		t.Errorf("the user's checkout was touched:\n%s", st)
	}
	for _, ev := range e.log(run.ID) {
		if strings.HasPrefix(string(ev.Type), "git.") {
			t.Errorf("a git event was recorded without anyone acting: %s", ev.Type)
		}
	}
}

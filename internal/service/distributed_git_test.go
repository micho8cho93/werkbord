package service

import (
	"path/filepath"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func TestDistributedReviewFetchesAndMergeRefusesAdvancedRemote(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "local contribution", 1)
	// A recorded remote run makes the shared remote authoritative for review.
	task, err := f.tasks.Create(f.ctx, f.project.ID, "remote contribution", "work")
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.runs.Create(f.ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "work", Remote: true, RunnerID: "rnr_other"})
	if err != nil {
		t.Fatal(err)
	}
	branch := "devboard/rnr_other/" + run.ID
	peer := filepath.Join(t.TempDir(), "peer")
	gitOut(t, f.repo, "clone", "-q", f.remote, peer)
	gitOut(t, peer, "checkout", "-q", "-b", branch)
	commitIn(t, peer, "peer.txt", "peer work\n", "peer work")
	gitOut(t, peer, "push", "-q", "origin", branch)
	gitOut(t, peer, "checkout", "-q", "main")
	advanced := commitIn(t, peer, "other.txt", "target moved\n", "another runner merged")
	gitOut(t, peer, "push", "-q", "origin", "main")
	err = f.deps.Store.Update(f.ctx, func(tx store.Tx) error { run.Branch = branch; return tx.Runs().Update(f.ctx, run) })
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.runs.MarkStarted(f.ctx, run.ID, Started{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.runs.End(f.ctx, run.ID, Ended{State: domain.RunCompleted})
	if err != nil {
		t.Fatal(err)
	}
	cmp, err := f.gc.Compare(f.ctx, f.project.ID, "remote", "origin/"+branch, "", 0, 10)
	if err != nil || cmp.TargetSha != advanced || cmp.Behind != 1 {
		t.Fatalf("fresh review: %+v %v", cmp, err)
	}
	overview, err := f.gc.Overview(f.ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	owned := false
	for _, b := range overview.Branches {
		if b.Name == "origin/"+branch {
			owned = b.DevBoard.RunID == run.ID
		}
	}
	if !owned {
		t.Fatal("remote branch lost run ownership")
	}
	plan, err := f.gc.MergePlan(f.ctx, f.project.ID, mergeIn(a, f, t))
	if err != nil || plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockTargetMoved) {
		t.Fatalf("stale target: %+v %v", plan, err)
	}
	// After the owner updates the target, fresh conflict checks can run normally.
	gitOut(t, f.repo, "merge", "--ff-only", "origin/main")
	plan, err = f.gc.MergePlan(f.ctx, f.project.ID, mergeIn(a, f, t))
	if err != nil || !plan.CanMerge {
		t.Fatalf("updated target: %+v %v", plan, err)
	}
}

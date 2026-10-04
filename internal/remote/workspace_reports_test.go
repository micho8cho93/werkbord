package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
)

func TestRetainedWorkspaceReconciliationIsReportedWithoutRestartingExecution(t *testing.T) {
	e := workerFixture(t)
	run := e.start()
	root := e.fake.Last().Req.WorkDir
	if err := os.WriteFile(filepath.Join(root, "after.txt"), []byte("retained work"), 0600); err != nil {
		t.Fatal(err)
	}
	e.fake.Last().Exit(0, "")
	waitPending(t, e.worker, run.ID, "ended")
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	ended, err := e.runs.Get(testCtx, run.ID)
	if err != nil || ended.Uncommitted == nil || !*ended.Uncommitted {
		t.Fatalf("uncommitted work unknown: %+v %v", ended, err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "owner reconciles work")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	reconciled, err := e.runs.Get(testCtx, run.ID)
	if err != nil || reconciled.HeadCommit != head || reconciled.Uncommitted == nil || *reconciled.Uncommitted {
		t.Fatalf("reconciliation absent: %+v %v", reconciled, err)
	}
	if reconciled.State != domain.RunCompleted || !reconciled.EndedAt.Equal(*ended.EndedAt) || len(e.fake.Sessions()) != 1 {
		t.Fatal("Git refresh changed execution history")
	}
	e.worker.mu.Lock()
	next := e.worker.records[run.ID].Next
	e.worker.mu.Unlock()
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	e.worker.mu.Lock()
	if e.worker.records[run.ID].Next != next {
		t.Fatal("unchanged Git report repeated")
	}
	e.worker.mu.Unlock()
	if err := e.worker.Load(testCtx); err != nil {
		t.Fatal(err)
	}
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Sessions()) != 1 {
		t.Fatal("terminal workspace relaunched after restart")
	}
}

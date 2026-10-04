package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
)

func TestRemoteContinuationUsesPublishedWorkInsteadOfOlderLocalWorktree(t *testing.T) {
	e := newEnv(t)
	task := e.task("Move back to controller")
	first := e.start(task)
	old := e.worktreeOf(first)
	e.session().Exit(0, "")
	e.waitState(first.ID, domain.RunCompleted)
	// Work left in the older local checkout must survive, but must not become
	// the continuation when a later run executed on a different machine.
	os.WriteFile(filepath.Join(old.Path, "older-local.txt"), []byte("retain"), 0600)
	base := strings.TrimSpace(git(t, e.repo, "rev-parse", "main"))
	branch := "devboard/rnr_peer/run_remote"
	git(t, e.repo, "checkout", "-b", branch)
	os.WriteFile(filepath.Join(e.repo, "remote-work.txt"), []byte("latest work"), 0600)
	git(t, e.repo, "add", ".")
	git(t, e.repo, "commit", "-m", "remote work")
	head := strings.TrimSpace(git(t, e.repo, "rev-parse", "HEAD"))
	git(t, e.repo, "checkout", "main")
	git(t, e.repo, "update-ref", "refs/remotes/origin/main", base)
	git(t, e.repo, "update-ref", "refs/remotes/origin/"+branch, head)
	clean := false
	last := domain.Run{ID: "run_remote", Remote: true, RunnerID: "rnr_peer", Branch: branch, HeadCommit: head, Uncommitted: &clean}
	prior := []domain.Run{*first, last, {ID: "run_failed_setup", Remote: true, State: domain.RunFailed}}
	for _, state := range []struct {
		name  string
		dirty *bool
	}{{"unknown", nil}, {"dirty", func() *bool { v := true; return &v }()}} {
		t.Run(state.name, func(t *testing.T) {
			bad := last
			bad.Uncommitted = state.dirty
			if _, err := e.mgr.prepareWorkspace(ctx, e.project, task, []domain.Run{*first, bad}); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("unsafe continuation: %v", err)
			}
		})
	}
	git(t, e.repo, "update-ref", "refs/remotes/origin/"+branch, base)
	if _, err := e.mgr.prepareWorkspace(ctx, e.project, task, prior); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unpublished commit admitted: %v", err)
	}
	git(t, e.repo, "update-ref", "refs/remotes/origin/"+branch, head)
	fresh, err := e.mgr.prepareWorkspace(ctx, e.project, task, prior)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.wt.ID == old.ID || !fresh.created {
		t.Fatal("fell back to older local worktree")
	}
	if data, err := os.ReadFile(filepath.Join(fresh.wt.Path, "remote-work.txt")); err != nil || string(data) != "latest work" {
		t.Fatalf("remote work lost: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(old.Path, "older-local.txt")); err != nil {
		t.Fatal("older local work discarded")
	}
}

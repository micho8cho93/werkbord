package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/runnerwire"
)

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, out)
	}
	return string(out)
}
func TestTwoRunnerWorktreesUseIndependentClonesAndUniqueBranches(t *testing.T) {
	e := workerFixture(t)
	source := e.project.RepoPath
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", source, clone)
	makeJob := func(id, runner string) runnerwire.Job {
		return runnerwire.Job{Run: domain.Run{ID: id, RunnerID: runner, ProjectID: e.project.ID}, TargetBranch: "main"}
	}
	first := &Worker{Dir: t.TempDir(), Bindings: map[string]string{e.project.ID: source}}
	second := &Worker{Dir: t.TempDir(), Bindings: map[string]string{e.project.ID: clone}}
	j1 := makeJob("run_one", "rnr_one")
	j2 := makeJob("run_two", "rnr_two")
	path1, branch1, base1, err := first.prepare(context.Background(), j1)
	if err != nil {
		t.Fatal(err)
	}
	path2, branch2, base2, err := second.prepare(context.Background(), j2)
	if err != nil {
		t.Fatal(err)
	}
	if branch1 == branch2 || path1 == path2 || base1 != base2 {
		t.Fatal("runner workspaces not independent")
	}
	if _, _, _, err := first.prepare(testCtx, j1); err == nil {
		t.Fatal("duplicate launch reused branch")
	}
	r1, _ := (&gitrepo.CLI{}).Inspect(testCtx, path1)
	r2, _ := (&gitrepo.CLI{}).Inspect(testCtx, path2)
	if r1.CommonDir == r2.CommonDir {
		t.Fatal("live Git directory shared")
	}
	os.WriteFile(filepath.Join(source, "new.txt"), []byte("new target"), 0600)
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "-m", "advance")
	stale := makeJob("run_stale", "rnr_one")
	stale.ExpectedCommit = base1
	if _, _, _, err := first.prepare(testCtx, stale); err == nil {
		t.Fatal("stale expected target admitted")
	}
	retry := makeJob("run_retry", "rnr_one")
	retry.PreviousBranch = branch1
	if _, _, _, err := first.prepare(testCtx, retry); err == nil {
		t.Fatal("stale previous branch admitted")
	}
}
func TestRemoteSynchronizationRejectsMismatchedCloneAndUnsafeURLs(t *testing.T) {
	e := workerFixture(t)
	w := e.worker
	job := runnerwire.Job{Run: domain.Run{ID: "run_remote", RunnerID: e.runner.ID, ProjectID: e.project.ID}, TargetBranch: "main", RemoteURL: "https://github.com/example/repo.git"}
	if _, _, _, err := w.prepare(testCtx, job); err == nil {
		t.Fatal("clone with unrelated origin accepted")
	}
	for _, remote := range []string{"https://secret@github.com/x/y.git", "file:///tmp/shared", "ext::sh -c bad", "--upload-pack=bad"} {
		if runnerwire.SafeRemote(remote) {
			t.Fatalf("unsafe remote accepted: %s", remote)
		}
	}
}

// Real fetch/clone synchronization, with only the transport replaced: the
// controller advertises an HTTPS origin, mapped to a temporary bare repository.
func TestAuthorizedCloneAndFetchFollowFreshRemoteTarget(t *testing.T) {
	e := workerFixture(t)
	central := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, "", "clone", "--bare", e.project.RepoPath, central)
	remoteURL := "https://github.com/example/authorized.git"
	transport := func(ctx context.Context, root string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "protocol.file.allow=always", "-c", "url." + central + ".insteadOf=" + remoteURL}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	project := e.project.ID
	first := &Worker{Dir: t.TempDir(), Bindings: map[string]string{}, AllowClone: true, Git: transport}
	job := runnerwire.Job{Run: domain.Run{ID: "run_clone", RunnerID: "rnr_first", ProjectID: project}, TargetBranch: "main", RemoteURL: remoteURL, AllowClone: true}
	_, _, base, err := first.prepare(testCtx, job)
	if err != nil {
		t.Fatal(err)
	}
	first.Identity.Projects = []string{project}
	first.LoadBindings = func() map[string]string { return map[string]string{} }
	first.refreshBindingsLocked()
	clone := filepath.Join(first.Dir, "repositories", project)
	if first.Bindings[project] != clone {
		t.Fatal("managed clone was not reported after binding reload")
	}
	actual, _ := (&gitrepo.CLI{}).Inspect(testCtx, clone)
	if len(actual.Remotes) != 1 || actual.Remotes[0].URL != remoteURL {
		t.Fatalf("origin changed: %+v", actual)
	}
	os.WriteFile(filepath.Join(e.project.RepoPath, "remote.txt"), []byte("advanced"), 0600)
	runGit(t, e.project.RepoPath, "add", ".")
	runGit(t, e.project.RepoPath, "commit", "-m", "remote advances")
	runGit(t, e.project.RepoPath, "push", central, "main")
	second := &Worker{Dir: t.TempDir(), Bindings: map[string]string{project: clone}, Git: transport}
	job.Run.ID = "run_fresh"
	job.Run.RunnerID = "rnr_second"
	job.ExpectedCommit = base
	if _, _, _, err := second.prepare(testCtx, job); err == nil {
		t.Fatal("fetch did not reject stale pinned target")
	}
	job.Run.ID = "run_fresh_unpinned"
	job.ExpectedCommit = ""
	path, _, fresh, err := second.prepare(testCtx, job)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == base {
		t.Fatal("remote target was not refreshed")
	}
	if _, err := os.Stat(filepath.Join(path, "remote.txt")); err != nil {
		t.Fatal("fresh remote content absent")
	}
}

func TestPublishedContinuationRequiresLatestOwnedCommitAndFreshTarget(t *testing.T) {
	e := workerFixture(t)
	central := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, "", "clone", "--bare", e.project.RepoPath, central)
	remoteURL := "https://github.com/example/continuation.git"
	transport := func(ctx context.Context, root string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "protocol.file.allow=always", "-c", "url." + central + ".insteadOf=" + remoteURL}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	first := &Worker{Dir: t.TempDir(), Bindings: map[string]string{e.project.ID: e.project.RepoPath}}
	job := runnerwire.Job{Run: domain.Run{ID: "run_prior", RunnerID: "rnr_prior", ProjectID: e.project.ID}, TargetBranch: "main"}
	path, branch, base, err := first.prepare(testCtx, job)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(path, "prior.txt"), []byte("latest owned work"), 0600)
	runGit(t, path, "add", ".")
	runGit(t, path, "commit", "-m", "prior runner work")
	head := strings.TrimSpace(runGit(t, path, "rev-parse", "HEAD"))
	second := &Worker{Dir: t.TempDir(), Bindings: map[string]string{}, AllowClone: true, Git: transport}
	job.Run.ID, job.Run.RunnerID = "run_next", "rnr_next"
	job.RemoteURL, job.AllowClone = remoteURL, true
	job.PreviousBranch, job.PreviousCommit, job.PreviousPublished = branch, head, true
	if _, _, _, err := second.prepare(testCtx, job); err == nil {
		t.Fatal("unpublished branch admitted")
	}
	// A published earlier version of the branch must also fail.
	runGit(t, e.project.RepoPath, "push", central, base+":refs/heads/"+branch)
	if _, _, _, err := second.prepare(testCtx, job); err == nil {
		t.Fatal("published branch lost latest owned commit")
	}
	runGit(t, path, "push", central, branch)
	fresh, _, _, err := second.prepare(testCtx, job)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(fresh, "prior.txt")); err != nil || string(data) != "latest owned work" {
		t.Fatalf("continuation lost work: %q %v", data, err)
	}
	os.WriteFile(filepath.Join(e.project.RepoPath, "target.txt"), []byte("advance target"), 0600)
	runGit(t, e.project.RepoPath, "add", ".")
	runGit(t, e.project.RepoPath, "commit", "-m", "target advances")
	runGit(t, e.project.RepoPath, "push", central, "main")
	job.Run.ID = "run_stale_continue"
	if _, _, _, err := second.prepare(testCtx, job); err == nil {
		t.Fatal("stale prior branch admitted after fresh fetch")
	}
}

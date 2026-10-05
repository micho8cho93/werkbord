package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
)

func TestListRefsAndUpstreamTracking(t *testing.T) {
	repo, _ := repoWithRemote(t)
	g := &CLI{}
	ctx := context.Background()

	run(t, repo, "checkout", "-q", "-b", "devboard/feature-abc123")
	commit(t, repo, "f.txt", "1\n", "feature work")
	run(t, repo, "push", "-q", "-u", "origin", "devboard/feature-abc123")
	commit(t, repo, "f.txt", "2\n", "more feature work") // one ahead of its upstream
	run(t, repo, "checkout", "-q", "-b", "no-upstream", "main")

	refs, err := g.ListRefs(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]RefInfo{}
	for _, r := range refs {
		by[r.Ref] = r
	}
	feat := by["refs/heads/devboard/feature-abc123"]
	if feat.Name != "devboard/feature-abc123" || feat.UpstreamRef != "refs/remotes/origin/devboard/feature-abc123" || feat.Track != "ahead 1" {
		t.Errorf("feature = %+v", feat)
	}
	if feat.Subject != "more feature work" || feat.Date.IsZero() {
		t.Errorf("feature tip = %q at %v", feat.Subject, feat.Date)
	}
	if nu := by["refs/heads/no-upstream"]; nu.UpstreamRef != "" {
		t.Errorf("no-upstream has upstream %q", nu.UpstreamRef)
	}
	rem := by["refs/remotes/origin/devboard/feature-abc123"]
	if rem.Remote != "origin" || rem.Name != "origin/devboard/feature-abc123" {
		t.Errorf("remote ref = %+v", rem)
	}
	if _, ok := by["refs/remotes/origin/HEAD"]; ok {
		t.Error("the symbolic origin/HEAD must not be listed as a branch")
	}
}

func TestDetectTarget(t *testing.T) {
	g := &CLI{}
	ctx := context.Background()

	repo, _ := repoWithRemote(t)
	got, err := g.DetectTarget(ctx, repo)
	if err != nil || got.Name != "main" || got.Source != TargetFromOriginHead || !got.LocalExists || got.Sha != sha(t, repo, "main") {
		t.Fatalf("with origin/HEAD: %+v, %v", got, err)
	}

	// origin/HEAD names a branch that only exists on the remote: still the target,
	// but there is nothing local to merge into.
	run(t, repo, "checkout", "-q", "-b", "other")
	run(t, repo, "branch", "-D", "main")
	got, err = g.DetectTarget(ctx, repo)
	if err != nil || got.Name != "main" || got.LocalExists || got.Ref != "refs/remotes/origin/main" {
		t.Fatalf("remote-only target: %+v, %v", got, err)
	}

	// No remote: the conventional name, then the current branch, then nothing.
	plain := plainRepo(t)
	if got, _ = g.DetectTarget(ctx, plain); got.Name != "main" || got.Source != TargetFromConvention {
		t.Errorf("conventional: %+v", got)
	}
	run(t, plain, "branch", "-m", "weird-trunk")
	if got, _ = g.DetectTarget(ctx, plain); got.Name != "weird-trunk" || got.Source != TargetFromCurrent {
		t.Errorf("current: %+v", got)
	}
	empty, _ := filepath.EvalSymlinks(t.TempDir())
	run(t, empty, "init", "-q", "-b", "main")
	if got, err = g.DetectTarget(ctx, empty); err != nil || got.Source != TargetFromNone || got.Name != "" {
		t.Errorf("empty repo: %+v, %v", got, err)
	}
}

func TestListWorktrees(t *testing.T) {
	repo := plainRepo(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	linked := filepath.Join(root, "linked")
	g := &CLI{}
	ctx := context.Background()
	if _, err := g.AddWorktree(ctx, repo, linked, "devboard/x-111111", sha(t, repo, "HEAD")); err != nil {
		t.Fatal(err)
	}
	detached := filepath.Join(root, "detached")
	run(t, repo, "worktree", "add", "-q", "--detach", detached)
	run(t, repo, "worktree", "lock", "--reason", "on a usb stick", detached)

	wts, err := g.ListWorktrees(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 3 || !wts[0].Primary || wts[0].Branch != "main" || wts[0].Path != repo {
		t.Fatalf("worktrees = %+v", wts)
	}
	byPath := map[string]WorktreeEntry{}
	for _, w := range wts {
		byPath[w.Path] = w
	}
	if w := byPath[linked]; w.Branch != "devboard/x-111111" || w.Primary || w.Detached {
		t.Errorf("linked = %+v", w)
	}
	if w := byPath[detached]; !w.Detached || w.Branch != "" || !w.Locked || w.LockReason != "on a usb stick" {
		t.Errorf("detached = %+v", w)
	}
}

func TestStatusReportsStagedUnstagedUntrackedAndConflicts(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()

	wt, err := g.Status(ctx, repo)
	if err != nil || !wt.Clean || wt.Branch != "main" || wt.Detached || wt.Head == "" {
		t.Fatalf("clean: %+v, %v", wt, err)
	}

	commit(t, repo, "tracked.txt", "v1\n", "add tracked")
	commit(t, repo, "gone.txt", "bye\n", "add gone")
	commit(t, repo, "moved.txt", "mv me, with enough content to be recognised as a rename\n", "add moved")
	write(t, repo, "tracked.txt", "v2\n") // modified, not staged
	write(t, repo, "staged.txt", "new\n") // added and staged
	run(t, repo, "add", "staged.txt")
	run(t, repo, "rm", "-q", "gone.txt")           // deleted and staged
	run(t, repo, "mv", "moved.txt", "renamed.txt") // renamed and staged
	write(t, repo, "both.txt", "a\n")
	run(t, repo, "add", "both.txt")
	write(t, repo, "both.txt", "a\nb\n")       // staged AND modified
	write(t, repo, "dir/untracked.txt", "u\n") // untracked, in a directory
	write(t, repo, "spaces in name.txt", "u\n")

	wt, err = g.Status(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	has := func(list []domain.GitFileChange, path, status string) bool {
		for _, f := range list {
			if f.Path == path && f.Status == status {
				return true
			}
		}
		return false
	}
	if !has(wt.Staged, "staged.txt", domain.FileAdded) || !has(wt.Staged, "gone.txt", domain.FileDeleted) {
		t.Errorf("staged = %+v", wt.Staged)
	}
	var renamed domain.GitFileChange
	for _, f := range wt.Staged {
		if f.Status == domain.FileRenamed {
			renamed = f
		}
	}
	if renamed.Path != "renamed.txt" || renamed.OldPath != "moved.txt" {
		t.Errorf("rename = %+v in %+v", renamed, wt.Staged)
	}
	if !has(wt.Unstaged, "tracked.txt", domain.FileModified) || !has(wt.Unstaged, "both.txt", domain.FileModified) || !has(wt.Staged, "both.txt", domain.FileAdded) {
		t.Errorf("unstaged = %+v, staged = %+v", wt.Unstaged, wt.Staged)
	}
	if !has(wt.Untracked, "dir/", domain.FileUntracked) || !has(wt.Untracked, "spaces in name.txt", domain.FileUntracked) {
		t.Errorf("untracked = %+v", wt.Untracked)
	}
	if wt.Clean || wt.Counts.Staged != 4 || wt.Counts.Unstaged != 2 || wt.Counts.Untracked != 2 || wt.Counts.Conflicted != 0 {
		t.Errorf("counts = %+v clean=%v", wt.Counts, wt.Clean)
	}
	if !wt.Counts.TrackedDirty() {
		t.Error("staged and modified files are tracked changes")
	}
}

func TestStatusDetachedUnbornAndInProgressMerge(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()

	run(t, repo, "checkout", "-q", "--detach")
	wt, _ := g.Status(ctx, repo)
	if !wt.Detached || wt.Branch != "" || wt.Head == "" {
		t.Errorf("detached = %+v", wt)
	}

	empty, _ := filepath.EvalSymlinks(t.TempDir())
	run(t, empty, "init", "-q", "-b", "main")
	write(t, empty, "a.txt", "x\n")
	wt, err := g.Status(ctx, empty)
	if err != nil || wt.Head != "" || wt.Branch != "main" || wt.Counts.Untracked != 1 {
		t.Errorf("unborn = %+v, %v", wt, err)
	}

	// A merge left half-done is reported, with its conflicts.
	run(t, repo, "checkout", "-q", "main")
	run(t, repo, "checkout", "-q", "-b", "side")
	commit(t, repo, "c.txt", "side\n", "side")
	run(t, repo, "checkout", "-q", "main")
	commit(t, repo, "c.txt", "main\n", "main")
	cmd := gitCmd(repo, "merge", "side")
	_ = cmd.Run() // conflicts: exit 1
	wt, err = g.Status(ctx, repo)
	if err != nil || wt.Operation != "merge" || wt.Counts.Conflicted != 1 || wt.Conflicted[0].Path != "c.txt" {
		t.Errorf("mid-merge = %+v, %v", wt, err)
	}
}

func TestCommitsPagingAndRanges(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	base := sha(t, repo, "HEAD")
	run(t, repo, "checkout", "-q", "-b", "work")
	for i := 0; i < 7; i++ {
		commit(t, repo, "f.txt", strings.Repeat("x", i+1), "work commit "+string(rune('a'+i)))
	}
	head := "refs/heads/work"

	page, err := g.Commits(ctx, repo, head, []string{"refs/heads/main"}, 0, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.Total != 7 || !page.Truncated || page.Items[0].Subject != "work commit g" {
		t.Fatalf("page = %+v", page)
	}
	rest, _ := g.Commits(ctx, repo, head, []string{"refs/heads/main"}, 6, 3, true)
	if len(rest.Items) != 1 || rest.Truncated || rest.Items[0].Subject != "work commit a" {
		t.Errorf("last page = %+v", rest)
	}
	// Without a count the total is only what was seen, and Truncated says more exists.
	nocount, _ := g.Commits(ctx, repo, head, nil, 0, 2, false)
	if len(nocount.Items) != 2 || !nocount.Truncated {
		t.Errorf("nocount = %+v", nocount)
	}
	if _, err := g.Commits(ctx, repo, "work", nil, 0, 2, false); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a bare name must be refused, got %v", err)
	}
	if _, err := g.Commits(ctx, repo, "--output=/tmp/x", nil, 0, 2, false); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an option must be refused, got %v", err)
	}

	a, b, err := g.Divergence(ctx, repo, "refs/heads/main", head)
	if err != nil || a != 0 || b != 7 {
		t.Errorf("divergence = %d, %d, %v", a, b, err)
	}
	if mb, _ := g.MergeBase(ctx, repo, "refs/heads/main", head); mb != base {
		t.Errorf("merge base = %s, want %s", mb, base)
	}
	if ok, _ := g.IsAncestor(ctx, repo, "refs/heads/main", head); !ok {
		t.Error("main is an ancestor of work")
	}
	if ok, _ := g.IsAncestor(ctx, repo, head, "refs/heads/main"); ok {
		t.Error("work is not an ancestor of main")
	}
}

func TestDiffFilesAndWindows(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	commit(t, repo, "keep.txt", "keep\n", "keep")
	commit(t, repo, "old name.txt", strings.Repeat("a line of text that is long enough\n", 20), "to rename")
	base := sha(t, repo, "HEAD")

	var big strings.Builder
	for i := 0; i < 1500; i++ {
		big.WriteString("line " + strings.Repeat("y", i%7) + "\n")
	}
	write(t, repo, "big.txt", big.String())
	write(t, repo, "bin.dat", "\x00\x01\x02binary")
	write(t, repo, "keep.txt", "keep\nchanged\n")
	run(t, repo, "mv", "old name.txt", "new name.txt")
	run(t, repo, "add", "-A")
	run(t, repo, "commit", "-q", "-m", "changes")
	head := sha(t, repo, "HEAD")

	files, trunc, err := g.DiffFiles(ctx, repo, base, head, 100)
	if err != nil || trunc {
		t.Fatalf("files = %+v trunc=%v err=%v", files, trunc, err)
	}
	by := map[string]domain.GitDiffFile{}
	for _, f := range files {
		by[f.Path] = f
	}
	if f := by["big.txt"]; f.Status != domain.FileAdded || f.Additions != 1500 || f.Deletions != 0 {
		t.Errorf("big = %+v", f)
	}
	if f := by["bin.dat"]; !f.Binary || f.Additions != 0 {
		t.Errorf("bin = %+v", f)
	}
	if f := by["keep.txt"]; f.Status != domain.FileModified || f.Additions != 1 {
		t.Errorf("keep = %+v", f)
	}
	if f := by["new name.txt"]; f.Status != domain.FileRenamed || f.OldPath != "old name.txt" || f.Additions != 0 {
		t.Errorf("rename = %+v", f)
	}
	capped, trunc, _ := g.DiffFiles(ctx, repo, base, head, 2)
	if len(capped) != 2 || !trunc {
		t.Errorf("capped = %d, trunc=%v", len(capped), trunc)
	}

	// A large file diff is never returned whole: it comes in windows.
	d, err := g.FileDiff(ctx, repo, base, head, []string{"big.txt"}, DiffWindow{Offset: 0, Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if d.Lines != 100 || !d.HasMore || d.Additions != 1500 || d.Binary || !strings.HasPrefix(d.Diff, "diff --git a/big.txt b/big.txt") {
		t.Errorf("first window = lines %d more=%v +%d head=%.40q", d.Lines, d.HasMore, d.Additions, d.Diff)
	}
	last, _ := g.FileDiff(ctx, repo, base, head, []string{"big.txt"}, DiffWindow{Offset: 1500, Lines: 2000})
	if last.HasMore || last.Offset != 1500 || last.TotalLines < 1500 || last.Lines != last.TotalLines-1500 {
		t.Errorf("last window = %+v", last.TotalLines)
	}
	past, _ := g.FileDiff(ctx, repo, base, head, []string{"big.txt"}, DiffWindow{Offset: 100000, Lines: 10})
	if past.Diff != "" || past.HasMore {
		t.Errorf("past the end = %+v", past)
	}
	bin, _ := g.FileDiff(ctx, repo, base, head, []string{"bin.dat"}, DiffWindow{})
	if !bin.Binary {
		t.Errorf("binary diff = %+v", bin)
	}
	rn, _ := g.FileDiff(ctx, repo, base, head, []string{"old name.txt", "new name.txt"}, DiffWindow{})
	if !strings.Contains(rn.Diff, "rename from old name.txt") || rn.Additions != 0 {
		t.Errorf("rename diff = %q", rn.Diff)
	}
	whole, _ := g.FileDiff(ctx, repo, base, head, nil, DiffWindow{Lines: 50})
	if whole.Lines != 50 || !whole.HasMore {
		t.Errorf("whole diff window = %+v", whole.Lines)
	}
}

// The window is clamped, and an enormous diff is cut at the byte limit rather
// than read whole.
func TestFileDiffIsBounded(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{MaxOutput: 1 << 20}
	ctx := context.Background()
	base := sha(t, repo, "HEAD")
	line := strings.Repeat("0123456789", 20) + "\n"         // 201 bytes
	write(t, repo, "huge.txt", strings.Repeat(line, 30000)) // ~6 MiB
	run(t, repo, "add", "-A")
	run(t, repo, "commit", "-q", "-m", "huge")
	head := sha(t, repo, "HEAD")

	d, err := g.FileDiff(ctx, repo, base, head, []string{"huge.txt"}, DiffWindow{Offset: 0, Lines: 999999})
	if err != nil {
		t.Fatal(err)
	}
	if d.Lines > MaxDiffLines || !d.Truncated || !d.HasMore {
		t.Errorf("lines=%d truncated=%v more=%v: an enormous diff must be clamped and flagged", d.Lines, d.Truncated, d.HasMore)
	}
	if len(d.Diff) > 2*MaxDiffLines*210 {
		t.Errorf("window is %d bytes", len(d.Diff))
	}
}

func TestWorkingDiff(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	commit(t, repo, "a.txt", "one\n", "a")
	write(t, repo, "a.txt", "one\ntwo\n")
	run(t, repo, "add", "a.txt")
	write(t, repo, "a.txt", "one\ntwo\nthree\n")
	write(t, repo, "new.txt", "fresh\nfile\n")

	st, err := g.WorkingDiff(ctx, repo, []string{"a.txt"}, WorkingStaged, DiffWindow{})
	if err != nil || st.Additions != 1 || !strings.Contains(st.Diff, "+two") || strings.Contains(st.Diff, "+three") {
		t.Errorf("staged = %+v, %v", st, err)
	}
	un, err := g.WorkingDiff(ctx, repo, []string{"a.txt"}, WorkingUnstaged, DiffWindow{})
	if err != nil || un.Additions != 1 || !strings.Contains(un.Diff, "+three") || strings.Contains(un.Diff, "+two") {
		t.Errorf("unstaged = %+v, %v", un, err)
	}
	nw, err := g.WorkingDiff(ctx, repo, []string{"new.txt"}, WorkingUntracked, DiffWindow{})
	if err != nil || nw.Additions != 2 || !strings.Contains(nw.Diff, "+fresh") || !strings.Contains(nw.Diff, "--- /dev/null") {
		t.Errorf("untracked = %+v, %v", nw, err)
	}

	// An untracked symlink is shown as a link and never followed out of the checkout.
	secret := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(secret, []byte("TOP SECRET\n"), 0o600)
	if err := os.Symlink(secret, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	ln, err := g.WorkingDiff(ctx, repo, []string{"link"}, WorkingUntracked, DiffWindow{})
	if err != nil || strings.Contains(ln.Diff, "TOP SECRET") || !strings.Contains(ln.Diff, secret) {
		t.Errorf("symlink diff = %+v, %v", ln, err)
	}
	// A symlinked DIRECTORY must not be a way out of the checkout either: "linkdir/secret.txt" is
	// outside, however the path reads.
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOP SECRET\n"), 0o600)
	if err := os.Symlink(outside, filepath.Join(repo, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if d, err := g.WorkingDiff(ctx, repo, []string{"linkdir/secret.txt"}, WorkingUntracked, DiffWindow{}); err == nil || (d != nil && strings.Contains(d.Diff, "TOP SECRET")) {
		t.Errorf("read through a symlinked directory: %+v, %v", d, err)
	}
	// Binary files are not shown.
	write(t, repo, "b.bin", "a\x00b")
	if b, _ := g.WorkingDiff(ctx, repo, []string{"b.bin"}, WorkingUntracked, DiffWindow{}); !b.Binary || b.Diff != "" {
		t.Errorf("binary = %+v", b)
	}
	// Paths that leave the checkout, or look like options, are refused.
	for _, p := range []string{"../outside", "/etc/passwd", "a/../../x", "-p", "", "x\x00y"} {
		if _, err := g.WorkingDiff(ctx, repo, []string{p}, WorkingUntracked, DiffWindow{}); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("path %q: err = %v, want invalid", p, err)
		}
	}
}

// A repository whose configuration names an external diff program or a textconv
// filter must not be made to run it by looking at a diff.
func TestDiffDoesNotRunConfiguredPrograms(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "evil.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\ncat \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "config", "diff.external", script)
	run(t, repo, "config", "diff.evil.textconv", script)
	run(t, repo, "config", "core.fsmonitor", script)
	write(t, repo, ".gitattributes", "*.txt diff=evil\n")
	run(t, repo, "add", "-A")
	run(t, repo, "commit", "-q", "-m", "attrs")
	base := sha(t, repo, "HEAD")
	commit(t, repo, "x.txt", "hello\n", "x")
	head := sha(t, repo, "HEAD")
	write(t, repo, "x.txt", "hello again\n")
	// The setup's own git commands ran with that config too; only what Werkbord's reads do counts.
	_ = os.Remove(marker)

	if _, err := g.FileDiff(ctx, repo, base, head, []string{"x.txt"}, DiffWindow{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.DiffFiles(ctx, repo, base, head, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := g.WorkingDiff(ctx, repo, []string{"x.txt"}, WorkingUnstaged, DiffWindow{}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Status(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if exists(marker) {
		t.Fatal("a configured diff, textconv or fsmonitor program was run by a read")
	}
}

func TestMergeSimulation(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	commit(t, repo, "shared.txt", "line\n", "shared")
	run(t, repo, "checkout", "-q", "-b", "clean")
	commit(t, repo, "only-here.txt", "x\n", "clean change")
	run(t, repo, "checkout", "-q", "-b", "clashing", "main")
	commit(t, repo, "shared.txt", "branch edit\n", "branch edit")
	run(t, repo, "checkout", "-q", "main")
	commit(t, repo, "shared.txt", "main edit\n", "main edit")
	mainSha := sha(t, repo, "main")

	sim, err := g.MergeSimulation(ctx, repo, mainSha, sha(t, repo, "clean"))
	if err != nil || !sim.Supported || sim.Conflicts {
		t.Errorf("clean merge: %+v, %v", sim, err)
	}
	sim, err = g.MergeSimulation(ctx, repo, mainSha, sha(t, repo, "clashing"))
	if err != nil || !sim.Supported || !sim.Conflicts || len(sim.Files) != 1 || sim.Files[0] != "shared.txt" {
		t.Errorf("conflicting merge: %+v, %v", sim, err)
	}
	// The simulation moved nothing.
	if sha(t, repo, "main") != mainSha || !isClean(t, repo) {
		t.Error("simulating a merge changed the repository")
	}
	if _, err := g.MergeSimulation(ctx, repo, "main", "clean"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("names instead of IDs must be refused: %v", err)
	}
}

// A squash merge puts a branch's content on the target under different commits, so by history the
// branch is unmerged. Merging it in memory then changes nothing, and the merged tree is the target's.
func TestMergeSimulationTreeShowsWhetherAMergeChangesAnything(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	run(t, repo, "checkout", "-q", "-b", "feature")
	commit(t, repo, "feature.txt", "one\n", "feature 1")
	commit(t, repo, "feature.txt", "one\ntwo\n", "feature 2")
	run(t, repo, "checkout", "-q", "-b", "other", "main")
	commit(t, repo, "other.txt", "x\n", "other")
	run(t, repo, "checkout", "-q", "main")
	run(t, repo, "merge", "--squash", "feature")
	run(t, repo, "commit", "-q", "-m", "squash feature")
	mainSha := sha(t, repo, "main")
	mainTree, err := g.TreeOf(ctx, repo, mainSha)
	if err != nil || mainTree == "" {
		t.Fatalf("tree of main: %q, %v", mainTree, err)
	}

	// By history the branch is not merged, yet merging it adds nothing.
	sim, err := g.MergeSimulation(ctx, repo, mainSha, sha(t, repo, "feature"))
	if err != nil || !sim.Supported || sim.Conflicts {
		t.Fatalf("squash-merged branch: %+v, %v", sim, err)
	}
	if sim.Tree != mainTree {
		t.Errorf("merged tree %s should equal the target's %s: the branch adds nothing", sim.Tree, mainTree)
	}
	// A branch with something new does change it.
	sim, err = g.MergeSimulation(ctx, repo, mainSha, sha(t, repo, "other"))
	if err != nil || sim.Tree == "" || sim.Tree == mainTree {
		t.Errorf("a branch with new content must change the tree: %+v, %v", sim, err)
	}
	if _, err := g.TreeOf(ctx, repo, "main"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a tree is read from a full commit ID: %v", err)
	}
	if tree, err := g.TreeOf(ctx, repo, strings.Repeat("0", 40)); err != nil || tree != "" {
		t.Errorf("an unknown commit has no tree: %q, %v", tree, err)
	}
}

func isClean(t *testing.T, dir string) bool {
	t.Helper()
	return gitOut(t, dir, "status", "--porcelain") == ""
}

func TestCheckRev(t *testing.T) {
	good := []string{"refs/heads/main", "refs/heads/devboard/a-b", "refs/remotes/origin/main", strings.Repeat("a", 40), strings.Repeat("0", 64)}
	bad := []string{"", "main", "HEAD", "-x", "--upload-pack=x", "refs/heads/-x", "refs/heads/a..b", "refs/heads/a b", "refs/tags/v1", "refs/heads/", "ABCDEF" + strings.Repeat("a", 34), strings.Repeat("a", 39), "refs/heads/a\nb", "refs/heads/x@{1}"}
	for _, s := range good {
		if err := checkRev(s); err != nil {
			t.Errorf("checkRev(%q) = %v", s, err)
		}
	}
	for _, s := range bad {
		if err := checkRev(s); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("checkRev(%q) = %v, want invalid", s, err)
		}
	}
}

func TestLastFetchAndNotOnAnyRemote(t *testing.T) {
	repo, _ := repoWithRemote(t)
	g := &CLI{}
	ctx := context.Background()
	common := filepath.Join(repo, ".git")
	if !g.LastFetch(filepath.Join(t.TempDir())).IsZero() {
		t.Error("a repository that never fetched has no fetch time")
	}
	if _, err := g.Fetch(ctx, repo, "origin"); err != nil {
		t.Fatal(err)
	}
	if g.LastFetch(common).IsZero() {
		t.Error("a fetch must leave a fetch time")
	}
	run(t, repo, "checkout", "-q", "-b", "local-only")
	commit(t, repo, "x", "1", "one")
	commit(t, repo, "x", "2", "two")
	if n, err := g.NotOnAnyRemote(ctx, repo, "refs/heads/local-only"); err != nil || n != 2 {
		t.Errorf("not on any remote = %d, %v", n, err)
	}
	if n, _ := g.NotOnAnyRemote(ctx, repo, "refs/heads/main"); n != 0 {
		t.Errorf("main is on the remote, got %d", n)
	}
}

func TestNeverAdvanced(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	run(t, repo, "branch", "cut", "main")
	run(t, repo, "checkout", "-q", "-b", "worked", "main")
	commit(t, repo, "w.txt", "w\n", "work")
	run(t, repo, "checkout", "-q", "main")
	commit(t, repo, "m.txt", "m\n", "main moves on")
	// A branch fast-forwarded to main has been somewhere else before: that is "advanced".
	run(t, repo, "branch", "ff", "main~1")
	run(t, repo, "branch", "-f", "ff", "main")

	for ref, want := range map[string]bool{"refs/heads/cut": true, "refs/heads/worked": false, "refs/heads/ff": false, "refs/heads/main": false} {
		got, err := g.NeverAdvanced(ctx, repo, ref)
		if err != nil || got != want {
			t.Errorf("NeverAdvanced(%s) = %v, %v; want %v", ref, got, err, want)
		}
	}
	// With no reflog it cannot be told, and "unknown" is not "never advanced".
	run(t, repo, "config", "core.logAllRefUpdates", "false")
	run(t, repo, "update-ref", "refs/heads/noreflog", sha(t, repo, "main~1"))
	if got, err := g.NeverAdvanced(ctx, repo, "refs/heads/noreflog"); err != nil || got {
		t.Errorf("no reflog: %v, %v", got, err)
	}
	if _, err := g.NeverAdvanced(ctx, repo, "cut"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a bare name must be refused: %v", err)
	}
}

package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"devboard/internal/domain"
)

// Reader answers questions about a repository without changing it. Everything
// here runs with transports off and takes no locks (see gitEnv), so looking at
// a repository never writes to it and never touches the network. The one
// exception is MergeSimulation, which writes unreferenced objects into the
// object database, as `git merge-tree --write-tree` does; no ref, index or
// working tree is touched.
//
// Revisions are always passed as full ref names (refs/heads/x) or hex commit
// IDs, never as a bare name, so nothing a user can name a branch is ever taken
// for an option or an abbreviation of something else.
type Reader interface {
	// ListRefs returns every local branch and remote-tracking branch.
	ListRefs(ctx context.Context, root string) ([]RefInfo, error)
	// ListWorktrees returns the repository's worktrees, the main one first.
	ListWorktrees(ctx context.Context, root string) ([]WorktreeEntry, error)
	// DetectTarget finds the branch work is measured against.
	DetectTarget(ctx context.Context, root string) (TargetInfo, error)
	// Status reports the staged, modified and untracked files of the checkout at dir.
	Status(ctx context.Context, dir string) (*domain.GitWorkingTree, error)
	// Commits lists commits reachable from include and from none of exclude,
	// newest first: skip of them are passed over and at most limit returned. With
	// count, Total is the number in the whole range.
	Commits(ctx context.Context, root, include string, exclude []string, skip, limit int, count bool) (domain.GitCommitPage, error)
	// CommitsNotOnRemotes lists the commits reachable from rev that no
	// remote-tracking ref has: what exists only on this computer.
	CommitsNotOnRemotes(ctx context.Context, root, rev string, limit int) (domain.GitCommitPage, error)
	// NeverAdvanced reports whether a local branch has only ever been at its current commit, by its
	// reflog: it was cut there and never committed to. False when that cannot be told (no reflog).
	NeverAdvanced(ctx context.Context, root, ref string) (bool, error)
	// Divergence counts the commits only in a, and the commits only in b.
	Divergence(ctx context.Context, root, a, b string) (aOnly, bOnly int, err error)
	// NotOnAnyRemote counts the commits reachable from rev that no remote-tracking ref has.
	NotOnAnyRemote(ctx context.Context, root, rev string) (int, error)
	// ResolveCommit returns the commit rev names, or "" when there is none.
	ResolveCommit(ctx context.Context, root, rev string) (string, error)
	IsAncestor(ctx context.Context, root, ancestor, descendant string) (bool, error)
	// MergeBase returns the best common ancestor, or "" for unrelated histories.
	MergeBase(ctx context.Context, root, a, b string) (string, error)
	// DiffFiles lists the files that differ between two commits, at most max of them.
	DiffFiles(ctx context.Context, root, from, to string, max int) (files []domain.GitDiffFile, truncated bool, err error)
	// FileDiff returns a window onto the unified diff of one file (or, with no
	// paths, of everything) between two commits.
	FileDiff(ctx context.Context, root, from, to string, paths []string, window DiffWindow) (*domain.GitFileDiff, error)
	// WorkingDiff does the same for a checkout's uncommitted changes: staged
	// ones, unstaged ones, or an untracked file. For a renamed file pass the old
	// path too.
	WorkingDiff(ctx context.Context, dir string, paths []string, kind WorkingKind, window DiffWindow) (*domain.GitFileDiff, error)
	// MergeSimulation merges two commits in memory and reports whether it conflicts.
	MergeSimulation(ctx context.Context, root, targetSha, branchSha string) (MergeSimulation, error)
	// TreeOf returns the ID of the tree a commit holds, or "" when there is no such commit.
	TreeOf(ctx context.Context, root, commit string) (string, error)
	// LastFetch is when the repository last fetched from any remote, or zero.
	LastFetch(commonDir string) time.Time
}

// RefInfo is one branch ref.
type RefInfo struct {
	Ref         string // refs/heads/x or refs/remotes/origin/x
	Name        string // x, or origin/x
	Remote      string // for a remote-tracking ref
	Sha         string
	Subject     string
	Date        time.Time
	UpstreamRef string // for a local branch: refs/remotes/origin/x
	// Track is what Git says about the upstream: "" (in sync, or no upstream),
	// "ahead 1", "behind 2", "ahead 1, behind 2" or "gone".
	Track string
}

// WorktreeEntry is one line of `git worktree list`.
type WorktreeEntry struct {
	Path       string
	Head       string
	Branch     string // short name; empty when detached
	Detached   bool
	Bare       bool
	Locked     bool
	LockReason string
	Prunable   bool
	Primary    bool // the main working tree
}

// TargetInfo is the branch work is merged into.
type TargetInfo struct {
	Name        string
	Source      string
	Ref         string // the ref to measure against: the local branch if it exists, else its remote-tracking branch
	Sha         string
	LocalExists bool
}

// Sources of the target branch, from most to least certain.
const (
	TargetFromOriginHead = "origin/HEAD"
	TargetFromConfig     = "init.defaultBranch"
	TargetFromConvention = "convention"
	TargetFromCurrent    = "current branch"
	TargetFromNone       = "none"
)

// WorkingKind says which uncommitted change of a file to show.
type WorkingKind string

const (
	WorkingStaged    WorkingKind = "staged"
	WorkingUnstaged  WorkingKind = "unstaged"
	WorkingUntracked WorkingKind = "untracked"
)

// DiffWindow is the part of a diff wanted: Lines lines from line Offset.
type DiffWindow struct {
	Offset int
	Lines  int
}

// Limits on what is read from Git, so that no repository can make a screen
// enormous or a request unbounded.
const (
	MaxDiffBytes     = 4 << 20 // the most of one diff that is ever read
	DefaultDiffLines = 400
	MaxDiffLines     = 2000
	statusListLimit  = 300
	statusMaxBytes   = 8 << 20
	logMaxBytes      = 4 << 20
	untrackedMaxRead = 512 << 10
)

var _ Reader = (*CLI)(nil)

// ---- refs ----

const refFormat = "%(refname)%1f%(objectname)%1f%(committerdate:unix)%1f%(upstream)%1f%(upstream:track,nobracket)%1f%(symref)%1f%(subject)"

// ListRefs implements Reader.
func (c *CLI) ListRefs(ctx context.Context, root string) ([]RefInfo, error) {
	remotes, err := c.remoteNames(ctx, root)
	if err != nil {
		return nil, err
	}
	out, _, err := c.run(ctx, root, runOpts{maxOut: 8 << 20}, "for-each-ref", "--format="+refFormat, "refs/heads", "refs/remotes")
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	var refs []RefInfo
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, "\x1f", 7)
		if len(f) < 7 || f[0] == "" {
			continue
		}
		if f[5] != "" { // a symbolic ref such as refs/remotes/origin/HEAD
			continue
		}
		r := RefInfo{Ref: f[0], Sha: f[1], UpstreamRef: f[3], Track: f[4], Subject: f[6]}
		if n, err := strconv.ParseInt(f[2], 10, 64); err == nil {
			r.Date = time.Unix(n, 0).UTC()
		}
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			r.Name = strings.TrimPrefix(f[0], "refs/heads/")
		case strings.HasPrefix(f[0], "refs/remotes/"):
			r.Name = strings.TrimPrefix(f[0], "refs/remotes/")
			r.Remote = remoteOf(r.Name, remotes)
		default:
			continue
		}
		refs = append(refs, r)
	}
	return refs, nil
}

func (c *CLI) remoteNames(ctx context.Context, root string) ([]string, error) {
	out, err := c.git(ctx, root, "remote")
	if err != nil {
		return nil, fmt.Errorf("list remotes: %w", err)
	}
	var names []string
	for _, n := range strings.Split(out, "\n") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return names, nil
}

// remoteOf finds which remote a remote-tracking name (origin/feature/x) belongs
// to: the longest remote name that prefixes it, since remote names may hold a '/'.
func remoteOf(name string, remotes []string) string {
	best := ""
	for _, r := range remotes {
		if strings.HasPrefix(name, r+"/") && len(r) > len(best) {
			best = r
		}
	}
	if best == "" {
		best, _, _ = strings.Cut(name, "/")
	}
	return best
}

// ---- worktrees ----

// ListWorktrees implements Reader.
func (c *CLI) ListWorktrees(ctx context.Context, root string) ([]WorktreeEntry, error) {
	out, _, err := c.run(ctx, root, runOpts{}, "worktree", "list", "--porcelain", "-z")
	sep := "\x00"
	if err != nil {
		// -z needs Git 2.36. Without it paths are newline separated, and a path
		// with a newline in it cannot be told apart from two lines; Dev Board's
		// own worktrees never have one.
		var ge *gitError
		if !errors.As(err, &ge) || ge.code != 129 {
			return nil, fmt.Errorf("list worktrees: %w", err)
		}
		if out, _, err = c.run(ctx, root, runOpts{}, "worktree", "list", "--porcelain"); err != nil {
			return nil, fmt.Errorf("list worktrees: %w", err)
		}
		sep = "\n"
	}
	return parseWorktrees(string(out), sep), nil
}

func parseWorktrees(out, sep string) []WorktreeEntry {
	var list []WorktreeEntry
	var cur *WorktreeEntry
	flush := func() {
		if cur != nil && cur.Path != "" {
			cur.Primary = len(list) == 0
			list = append(list, *cur)
		}
		cur = nil
	}
	for _, tok := range strings.Split(out, sep) {
		if tok == "" {
			flush()
			continue
		}
		key, val, _ := strings.Cut(tok, " ")
		if key == "worktree" {
			flush()
			cur = &WorktreeEntry{Path: val}
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "locked":
			cur.Locked, cur.LockReason = true, val
		case "prunable":
			cur.Prunable = true
		}
	}
	flush()
	return list
}

// ---- target ----

// DetectTarget implements Reader. The order is by how much the answer can be
// trusted: what the remote says its default branch is, then the user's
// configured default, then the usual names, then whatever is checked out.
func (c *CLI) DetectTarget(ctx context.Context, root string) (TargetInfo, error) {
	pick := func(name, source string) (TargetInfo, bool, error) {
		if domain.ValidateRefName(name) != nil {
			return TargetInfo{}, false, nil
		}
		if sha, err := c.ResolveCommit(ctx, root, "refs/heads/"+name); err != nil {
			return TargetInfo{}, false, err
		} else if sha != "" {
			return TargetInfo{Name: name, Source: source, Ref: "refs/heads/" + name, Sha: sha, LocalExists: true}, true, nil
		}
		return TargetInfo{}, false, nil
	}

	if head, err := c.optional(ctx, root, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err != nil {
		return TargetInfo{}, err
	} else if name := strings.TrimPrefix(head, "refs/remotes/origin/"); name != head && domain.ValidateRefName(name) == nil {
		if t, ok, err := pick(name, TargetFromOriginHead); err != nil || ok {
			return t, err
		}
		// The remote's default exists only as origin/<name> here. It is still the
		// target, but nothing can be merged into it on this computer.
		if sha, err := c.ResolveCommit(ctx, root, "refs/remotes/origin/"+name); err != nil {
			return TargetInfo{}, err
		} else if sha != "" {
			return TargetInfo{Name: name, Source: TargetFromOriginHead, Ref: "refs/remotes/origin/" + name, Sha: sha}, nil
		}
	}
	if name, err := c.optional(ctx, root, "config", "--get", "init.defaultBranch"); err != nil {
		return TargetInfo{}, err
	} else if name != "" {
		if t, ok, err := pick(name, TargetFromConfig); err != nil || ok {
			return t, err
		}
	}
	for _, name := range []string{"main", "master", "trunk", "develop"} {
		if t, ok, err := pick(name, TargetFromConvention); err != nil || ok {
			return t, err
		}
	}
	if name, err := c.optional(ctx, root, "symbolic-ref", "-q", "--short", "HEAD"); err != nil {
		return TargetInfo{}, err
	} else if name != "" {
		if t, ok, err := pick(name, TargetFromCurrent); err != nil || ok {
			return t, err
		}
	}
	return TargetInfo{Source: TargetFromNone}, nil
}

// ---- revisions ----

var hexRev = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// checkRev accepts only what is safe to hand to Git as a revision: a full ref
// name or a full commit ID.
func checkRev(rev string) error {
	switch {
	case hexRev.MatchString(rev):
		return nil
	case strings.HasPrefix(rev, "refs/heads/"), strings.HasPrefix(rev, "refs/remotes/"):
		if domain.ValidateRefName(strings.TrimPrefix(strings.TrimPrefix(rev, "refs/heads/"), "refs/remotes/")) != nil {
			return fmt.Errorf("%w: revision %q is not a safe ref name", domain.ErrInvalid, rev)
		}
		return nil
	}
	return fmt.Errorf("%w: revision %q must be a full ref (refs/heads/…) or a full commit ID", domain.ErrInvalid, rev)
}

// IsCommitID reports whether s is a full commit ID.
func IsCommitID(s string) bool { return hexRev.MatchString(s) }

// ResolveCommit implements Reader.
func (c *CLI) ResolveCommit(ctx context.Context, root, rev string) (string, error) {
	if err := checkRev(rev); err != nil {
		return "", err
	}
	return c.optional(ctx, root, "rev-parse", "-q", "--verify", rev+"^{commit}")
}

// HeadCommit reads the checkout's current commit without accepting a revision
// from a caller. ResolveCommit deliberately requires full refs or commit IDs.
func (c *CLI) HeadCommit(ctx context.Context, root string) (string, error) {
	return c.optional(ctx, root, "rev-parse", "-q", "--verify", "HEAD^{commit}")
}

// TreeOf implements Reader.
func (c *CLI) TreeOf(ctx context.Context, root, commit string) (string, error) {
	if !IsCommitID(commit) {
		return "", fmt.Errorf("%w: a tree is read from a full commit ID", domain.ErrInvalid)
	}
	return c.optional(ctx, root, "rev-parse", "-q", "--verify", commit+"^{tree}")
}

// IsAncestor implements Reader.
func (c *CLI) IsAncestor(ctx context.Context, root, ancestor, descendant string) (bool, error) {
	if err := checkRev(ancestor); err != nil {
		return false, err
	}
	if err := checkRev(descendant); err != nil {
		return false, err
	}
	_, err := c.git(ctx, root, "merge-base", "--is-ancestor", ancestor, descendant)
	var ge *gitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &ge) && ge.code == 1:
		return false, nil
	}
	return false, fmt.Errorf("ancestry check: %w", err)
}

// MergeBase implements Reader.
func (c *CLI) MergeBase(ctx context.Context, root, a, b string) (string, error) {
	if err := checkRev(a); err != nil {
		return "", err
	}
	if err := checkRev(b); err != nil {
		return "", err
	}
	return c.optional(ctx, root, "merge-base", a, b)
}

// Divergence implements Reader.
func (c *CLI) Divergence(ctx context.Context, root, a, b string) (int, int, error) {
	if err := checkRev(a); err != nil {
		return 0, 0, err
	}
	if err := checkRev(b); err != nil {
		return 0, 0, err
	}
	out, err := c.git(ctx, root, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0, fmt.Errorf("count commits: %w", err)
	}
	l, r, ok := strings.Cut(out, "\t")
	if !ok {
		return 0, 0, fmt.Errorf("count commits: unexpected output %q", out)
	}
	ln, err1 := strconv.Atoi(strings.TrimSpace(l))
	rn, err2 := strconv.Atoi(strings.TrimSpace(r))
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("count commits: unexpected output %q", out)
	}
	return ln, rn, nil
}

// NeverAdvanced implements Reader.
func (c *CLI) NeverAdvanced(ctx context.Context, root, ref string) (bool, error) {
	if err := checkRev(ref); err != nil {
		return false, err
	}
	out, err := c.git(ctx, root, "reflog", "show", "--no-abbrev", "--format=%H", ref)
	if err != nil {
		var ge *gitError
		if errors.As(err, &ge) {
			return false, nil // no reflog to read: unknown, which is not "never advanced"
		}
		return false, fmt.Errorf("read reflog: %w", err)
	}
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			seen[l] = true
		}
	}
	return len(seen) == 1, nil
}

// NotOnAnyRemote implements Reader.
func (c *CLI) NotOnAnyRemote(ctx context.Context, root, rev string) (int, error) {
	if err := checkRev(rev); err != nil {
		return 0, err
	}
	out, err := c.git(ctx, root, "rev-list", "--count", rev, "--not", "--remotes")
	if err != nil {
		return 0, fmt.Errorf("count unpushed commits: %w", err)
	}
	return strconv.Atoi(out)
}

// LastFetch implements Reader. FETCH_HEAD is rewritten by every fetch, so its
// modification time is when one last ran, which is as fresh as every
// remote-tracking ref can be.
func (c *CLI) LastFetch(commonDir string) time.Time {
	if commonDir == "" {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(commonDir, "FETCH_HEAD"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC()
}

// ---- commits ----

const commitFormat = "--format=%x1e%H%x1f%P%x1f%an%x1f%ct%x1f%s"

// Commits implements Reader.
func (c *CLI) Commits(ctx context.Context, root, include string, exclude []string, skip, limit int, count bool) (domain.GitCommitPage, error) {
	page := domain.GitCommitPage{Items: []domain.GitCommit{}}
	if limit <= 0 || skip < 0 {
		return page, fmt.Errorf("%w: commit window must be positive", domain.ErrInvalid)
	}
	if err := checkRev(include); err != nil {
		return page, err
	}
	revs := []string{include}
	for _, e := range exclude {
		if err := checkRev(e); err != nil {
			return page, err
		}
		revs = append(revs, "^"+e)
	}
	if err := c.readLog(ctx, root, revs, skip, limit, &page); err != nil {
		return page, err
	}
	if count {
		cargs := append([]string{"rev-list", "--count"}, revs...)
		if err := c.countInto(ctx, root, cargs, &page, skip); err != nil {
			return page, err
		}
	}
	return page, nil
}

// CommitsNotOnRemotes implements Reader.
func (c *CLI) CommitsNotOnRemotes(ctx context.Context, root, rev string, limit int) (domain.GitCommitPage, error) {
	page := domain.GitCommitPage{Items: []domain.GitCommit{}}
	if limit <= 0 {
		return page, fmt.Errorf("%w: commit window must be positive", domain.ErrInvalid)
	}
	if err := checkRev(rev); err != nil {
		return page, err
	}
	revs := []string{rev, "--not", "--remotes"}
	if err := c.readLog(ctx, root, revs, 0, limit, &page); err != nil {
		return page, err
	}
	return page, c.countInto(ctx, root, append([]string{"rev-list", "--count"}, revs...), &page, 0)
}

// readLog fills page with one window of `git log` over revs. revs are built by
// the callers from validated revisions only.
func (c *CLI) readLog(ctx context.Context, root string, revs []string, skip, limit int, page *domain.GitCommitPage) error {
	args := append([]string{"log", "--no-color", "--no-show-signature", "--max-count=" + strconv.Itoa(limit+1), "--skip=" + strconv.Itoa(skip), commitFormat}, revs...)
	args = append(args, "--")
	out, _, err := c.run(ctx, root, runOpts{maxOut: logMaxBytes, truncate: true}, args...)
	if err != nil {
		return fmt.Errorf("read history: %w", err)
	}
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.SplitN(strings.TrimSpace(rec), "\x1f", 5)
		if len(f) < 5 {
			continue
		}
		cm := domain.GitCommit{SHA: f[0], Author: f[2], Subject: clip(f[4], 200), Merge: len(strings.Fields(f[1])) > 1}
		if n, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			cm.Date = time.Unix(n, 0).UTC()
		}
		page.Items = append(page.Items, cm)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.Truncated = true
	}
	page.Total = skip + len(page.Items)
	return nil
}

// countInto sets page.Total from a `rev-list --count` and Truncated from it.
func (c *CLI) countInto(ctx context.Context, root string, args []string, page *domain.GitCommitPage, skip int) error {
	n, err := c.git(ctx, root, args...)
	if err != nil {
		return fmt.Errorf("count commits: %w", err)
	}
	if page.Total, err = strconv.Atoi(n); err != nil {
		return fmt.Errorf("count commits: unexpected output %q", n)
	}
	page.Truncated = skip+len(page.Items) < page.Total
	return nil
}

// clip shortens s to at most n runes, on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ---- working tree ----

// Status implements Reader.
func (c *CLI) Status(ctx context.Context, dir string) (*domain.GitWorkingTree, error) {
	out, truncated, err := c.run(ctx, dir, runOpts{maxOut: statusMaxBytes, truncate: true, timeout: 30 * time.Second},
		"status", "--porcelain=v2", "-z", "--branch", "--untracked-files=normal")
	if err != nil {
		return nil, fmt.Errorf("read status: %w", err)
	}
	wt := parseStatus(out)
	wt.Truncated = wt.Truncated || truncated
	op, err := c.operation(ctx, dir)
	if err != nil {
		return nil, err
	}
	wt.Operation = op
	return wt, nil
}

// operation reports a merge, rebase, cherry-pick, revert or bisect left in
// progress in the checkout at dir.
func (c *CLI) operation(ctx context.Context, dir string) (string, error) {
	gitDir, err := c.git(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", fmt.Errorf("find git dir: %w", err)
	}
	for _, m := range []struct{ file, op string }{
		{"MERGE_HEAD", "merge"}, {"rebase-merge", "rebase"}, {"rebase-apply", "rebase"},
		{"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"}, {"BISECT_LOG", "bisect"},
	} {
		if _, err := os.Lstat(filepath.Join(gitDir, m.file)); err == nil {
			return m.op, nil
		}
	}
	return "", nil
}

func statusOf(ch byte) string {
	switch ch {
	case 'M':
		return domain.FileModified
	case 'A':
		return domain.FileAdded
	case 'D':
		return domain.FileDeleted
	case 'R':
		return domain.FileRenamed
	case 'C':
		return domain.FileCopied
	case 'T':
		return domain.FileTypeChange
	case 'U':
		return domain.FileConflicted
	}
	return ""
}

// parseStatus reads `git status --porcelain=v2 -z --branch`. The lists are cut
// at statusListLimit but the counts are not.
func parseStatus(out []byte) *domain.GitWorkingTree {
	wt := &domain.GitWorkingTree{Staged: []domain.GitFileChange{}, Unstaged: []domain.GitFileChange{}, Untracked: []domain.GitFileChange{}, Conflicted: []domain.GitFileChange{}}
	toks := strings.Split(string(out), "\x00")
	add := func(list *[]domain.GitFileChange, count *int, ch domain.GitFileChange) {
		*count++
		if len(*list) < statusListLimit {
			*list = append(*list, ch)
		} else {
			wt.Truncated = true
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case strings.HasPrefix(t, "# branch.oid "):
			if v := strings.TrimPrefix(t, "# branch.oid "); v != "(initial)" {
				wt.Head = v
			}
		case strings.HasPrefix(t, "# branch.head "):
			if v := strings.TrimPrefix(t, "# branch.head "); v == "(detached)" {
				wt.Detached = true
			} else {
				wt.Branch = v
			}
		case strings.HasPrefix(t, "# branch.upstream "):
			wt.Upstream = strings.TrimPrefix(t, "# branch.upstream ")
		case strings.HasPrefix(t, "# branch.ab "):
			var a, b int
			if _, err := fmt.Sscanf(strings.TrimPrefix(t, "# branch.ab "), "+%d -%d", &a, &b); err == nil {
				wt.Ahead, wt.Behind = a, b
			}
		case strings.HasPrefix(t, "1 "):
			f := strings.SplitN(t, " ", 9)
			if len(f) < 9 || len(f[1]) < 2 {
				continue
			}
			addXY(wt, f[1], f[8], "", add)
		case strings.HasPrefix(t, "2 "):
			f := strings.SplitN(t, " ", 10)
			if len(f) < 10 || len(f[1]) < 2 {
				continue
			}
			orig := ""
			if i+1 < len(toks) {
				i++
				orig = toks[i]
			}
			addXY(wt, f[1], f[9], orig, add)
		case strings.HasPrefix(t, "u "):
			f := strings.SplitN(t, " ", 11)
			if len(f) < 11 {
				continue
			}
			add(&wt.Conflicted, &wt.Counts.Conflicted, domain.GitFileChange{Path: f[10], Status: domain.FileConflicted})
		case strings.HasPrefix(t, "? "):
			add(&wt.Untracked, &wt.Counts.Untracked, domain.GitFileChange{Path: strings.TrimPrefix(t, "? "), Status: domain.FileUntracked})
		}
	}
	wt.Clean = wt.Counts.Total() == 0
	return wt
}

func addXY(wt *domain.GitWorkingTree, xy, path, orig string, add func(*[]domain.GitFileChange, *int, domain.GitFileChange)) {
	if s := statusOf(xy[0]); xy[0] != '.' && s != "" {
		add(&wt.Staged, &wt.Counts.Staged, domain.GitFileChange{Path: path, OldPath: orig, Status: s})
	}
	if s := statusOf(xy[1]); xy[1] != '.' && s != "" {
		add(&wt.Unstaged, &wt.Counts.Unstaged, domain.GitFileChange{Path: path, OldPath: orig, Status: s})
	}
}

// ---- diffs ----

// diffFlags are on every diff. External diff programs and text conversion
// filters are named by repository configuration, and each would run a command of
// the repository's choosing; literal pathspecs stop a file name from being
// taken for pathspec magic.
var diffFlags = []string{"--no-ext-diff", "--no-textconv", "--no-color"}

var literalPathspecs = []string{"GIT_LITERAL_PATHSPECS=1"}

// DiffFiles implements Reader.
func (c *CLI) DiffFiles(ctx context.Context, root, from, to string, max int) ([]domain.GitDiffFile, bool, error) {
	if err := checkRev(from); err != nil {
		return nil, false, err
	}
	if err := checkRev(to); err != nil {
		return nil, false, err
	}
	out, truncated, err := c.run(ctx, root, runOpts{maxOut: 8 << 20, truncate: true, timeout: 30 * time.Second, env: literalPathspecs},
		"diff", "--raw", "--numstat", "-z", "-M", "--no-ext-diff", "--no-textconv", "--no-color", from, to, "--")
	if err != nil {
		return nil, false, fmt.Errorf("list changed files: %w", err)
	}
	files := parseDiffFiles(out)
	if truncated {
		// The last record may be cut mid-way; what was parsed is a prefix.
		return capFiles(files, max), true, nil
	}
	if len(files) > max {
		return files[:max], true, nil
	}
	return files, false, nil
}

func capFiles(f []domain.GitDiffFile, max int) []domain.GitDiffFile {
	if len(f) > max {
		return f[:max]
	}
	return f
}

// parseDiffFiles reads `git diff --raw --numstat -z -M`: all the raw records
// first (status and paths), then all the numstat records (counts), both in the
// same order. Counts are matched to files by path.
func parseDiffFiles(out []byte) []domain.GitDiffFile {
	toks := strings.Split(string(out), "\x00")
	var files []domain.GitDiffFile
	index := map[string]int{}
	i := 0
	for i < len(toks) {
		t := toks[i]
		if strings.HasPrefix(t, ":") {
			f := strings.Fields(t)
			if len(f) < 5 || i+1 >= len(toks) {
				break
			}
			st := f[4]
			file := domain.GitDiffFile{Path: toks[i+1], Status: diffStatus(st[0])}
			i += 2
			if st[0] == 'R' || st[0] == 'C' {
				if i >= len(toks) {
					break
				}
				file.OldPath, file.Path = file.Path, toks[i]
				i++
			}
			index[file.Path] = len(files)
			files = append(files, file)
			continue
		}
		// numstat: "<added>\t<deleted>\t<path>" or, for a rename, "<a>\t<d>\t" then old and new paths.
		parts := strings.SplitN(t, "\t", 3)
		i++
		if len(parts) < 3 {
			continue
		}
		path := parts[2]
		if path == "" {
			if i+1 >= len(toks) {
				break
			}
			path = toks[i+1]
			i += 2
		}
		idx, ok := index[path]
		if !ok {
			continue
		}
		if parts[0] == "-" || parts[1] == "-" {
			files[idx].Binary = true
			continue
		}
		files[idx].Additions, _ = strconv.Atoi(parts[0])
		files[idx].Deletions, _ = strconv.Atoi(parts[1])
	}
	return files
}

func diffStatus(ch byte) string {
	switch ch {
	case 'A':
		return domain.FileAdded
	case 'D':
		return domain.FileDeleted
	case 'R':
		return domain.FileRenamed
	case 'C':
		return domain.FileCopied
	case 'T':
		return domain.FileTypeChange
	case 'U':
		return domain.FileConflicted
	}
	return domain.FileModified
}

func (w DiffWindow) normalized() DiffWindow {
	if w.Offset < 0 {
		w.Offset = 0
	}
	if w.Lines <= 0 {
		w.Lines = DefaultDiffLines
	}
	if w.Lines > MaxDiffLines {
		w.Lines = MaxDiffLines
	}
	return w
}

// FileDiff implements Reader.
func (c *CLI) FileDiff(ctx context.Context, root, from, to string, paths []string, window DiffWindow) (*domain.GitFileDiff, error) {
	if err := checkRev(from); err != nil {
		return nil, err
	}
	if err := checkRev(to); err != nil {
		return nil, err
	}
	for _, p := range paths {
		if err := checkPath(p); err != nil {
			return nil, err
		}
	}
	args := append([]string{"diff", "--unified=3", "-M", "--numstat", "--patch"}, diffFlags...)
	args = append(args, from, to, "--")
	args = append(args, paths...)
	return c.diffWindow(ctx, root, args, window, paths)
}

// WorkingDiff implements Reader.
func (c *CLI) WorkingDiff(ctx context.Context, dir string, paths []string, kind WorkingKind, window DiffWindow) (*domain.GitFileDiff, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("%w: path is required", domain.ErrInvalid)
	}
	for _, p := range paths {
		if err := checkPath(p); err != nil {
			return nil, err
		}
	}
	switch kind {
	case WorkingUntracked:
		return untrackedDiff(dir, paths[0], window)
	case WorkingStaged, WorkingUnstaged:
	default:
		return nil, fmt.Errorf("%w: unknown kind of change %q", domain.ErrInvalid, kind)
	}
	args := []string{"diff", "--unified=3", "-M", "--numstat", "--patch"}
	args = append(args, diffFlags...)
	if kind == WorkingStaged {
		args = append(args, "--cached")
	}
	args = append(args, "--")
	args = append(args, paths...)
	return c.diffWindow(ctx, dir, args, window, paths)
}

// diffWindow runs a diff and cuts the window out of it. With --numstat and
// --patch Git prints the counts first, which gives the file's additions and
// deletions, and whether it is binary, without a second command.
func (c *CLI) diffWindow(ctx context.Context, dir string, args []string, window DiffWindow, paths []string) (*domain.GitFileDiff, error) {
	window = window.normalized()
	out, truncated, err := c.run(ctx, dir, runOpts{maxOut: MaxDiffBytes, truncate: true, timeout: 30 * time.Second, env: literalPathspecs}, args...)
	if err != nil {
		return nil, fmt.Errorf("read diff: %w", err)
	}
	d := &domain.GitFileDiff{Truncated: truncated, Offset: window.Offset}
	switch len(paths) {
	case 1:
		d.Path = paths[0]
	case 2:
		d.OldPath, d.Path = paths[0], paths[1]
	}
	text := string(out)
	// Counts come first, one per file: "<a>\t<d>\t<path>\n". They end where the patch begins.
	for {
		line, rest, ok := strings.Cut(text, "\n")
		if !ok || strings.HasPrefix(line, "diff --git ") {
			break
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			break
		}
		if parts[0] == "-" || parts[1] == "-" {
			d.Binary = true
		} else {
			a, _ := strconv.Atoi(parts[0])
			r, _ := strconv.Atoi(parts[1])
			d.Additions += a
			d.Deletions += r
		}
		text = rest
	}
	text = strings.TrimPrefix(text, "\n") // a blank line separates the counts from the patch
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if truncated {
		// The last line is probably cut; do not show half of one.
		if len(lines) > 0 {
			lines = lines[:len(lines)-1]
		}
	}
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	d.TotalLines = len(lines)
	end := min(window.Offset+window.Lines, len(lines))
	if window.Offset < len(lines) {
		d.Diff = strings.Join(lines[window.Offset:end], "\n")
		d.Lines = end - window.Offset
	}
	d.HasMore = end < len(lines)
	return d, nil
}

// untrackedDiff shows an untracked file as an addition. The file is read
// directly, within a size limit, and only if it is a regular file inside the
// checkout: an untracked symlink is shown as the link it is, never followed.
func untrackedDiff(dir, path string, window DiffWindow) (*domain.GitFileDiff, error) {
	window = window.normalized()
	// The path is checked against where it REALLY leads, not only as text: an untracked symlink to a
	// directory (link -> /etc) would otherwise let "link/passwd" read a file outside the checkout.
	// Only the final component may be a symlink, and it is then shown as the link it is.
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", domain.ErrNotFound, dir, err)
	}
	full := filepath.Join(dir, filepath.FromSlash(path))
	realParent, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", domain.ErrNotFound, path, err)
	}
	if realParent != realDir && !strings.HasPrefix(realParent, realDir+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: %q leads outside the checkout", domain.ErrInvalid, path)
	}
	full = filepath.Join(realParent, filepath.Base(full))
	d := &domain.GitFileDiff{Path: path, Status: domain.FileUntracked, Offset: window.Offset}
	info, err := os.Lstat(full)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", domain.ErrNotFound, path, err)
	}
	var body []byte
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(full)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", domain.ErrNotFound, path, err)
		}
		body = []byte(target + "\n")
	case info.Mode().IsRegular():
		f, err := os.Open(full)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", domain.ErrNotFound, path, err)
		}
		defer f.Close()
		buf := make([]byte, untrackedMaxRead+1)
		n, _ := f.Read(buf)
		body = buf[:n]
		if n > untrackedMaxRead {
			body, d.Truncated = body[:untrackedMaxRead], true
		}
	default:
		d.Binary = true
		return d, nil
	}
	if bytes.IndexByte(body, 0) >= 0 {
		d.Binary = true
		return d, nil
	}
	text := strings.TrimRight(string(body), "\n")
	lines := strings.Split(text, "\n")
	if text == "" {
		lines = nil
	}
	d.Additions = len(lines)
	all := make([]string, 0, len(lines)+4)
	all = append(all, "--- /dev/null", "+++ b/"+path)
	if len(lines) > 0 {
		all = append(all, fmt.Sprintf("@@ -0,0 +1,%d @@", len(lines)))
		for _, l := range lines {
			all = append(all, "+"+l)
		}
	}
	d.TotalLines = len(all)
	end := min(window.Offset+window.Lines, len(all))
	if window.Offset < len(all) {
		d.Diff = strings.Join(all[window.Offset:end], "\n")
		d.Lines = end - window.Offset
	}
	d.HasMore = end < len(all)
	return d, nil
}

// checkPath accepts a path inside a repository: relative, with no NUL, no '..'
// component and no leading '-' that could be taken for an option.
func checkPath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("%w: path is required", domain.ErrInvalid)
	case strings.ContainsRune(p, 0):
		return fmt.Errorf("%w: path contains a NUL byte", domain.ErrInvalid)
	case filepath.IsAbs(p) || strings.HasPrefix(p, "/"):
		return fmt.Errorf("%w: path %q must be relative to the repository", domain.ErrInvalid, p)
	case len(p) > 4096:
		return fmt.Errorf("%w: path is too long", domain.ErrInvalid)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return fmt.Errorf("%w: path %q must not contain '..'", domain.ErrInvalid, p)
		}
	}
	if strings.HasPrefix(p, "-") {
		return fmt.Errorf("%w: path %q must not start with '-'", domain.ErrInvalid, p)
	}
	return nil
}

// ---- merge simulation ----

// MergeSimulation is the result of merging two commits in memory.
type MergeSimulation struct {
	// Supported is false when this Git has no `merge-tree --write-tree` (before 2.38).
	Supported bool
	Conflicts bool
	Files     []string
	Unrelated bool
	// Tree is the ID of the tree the merge produced, when it was clean. Comparing it
	// with the target's tree says whether the merge would change anything at all.
	Tree string
}

// MergeSimulation implements Reader.
func (c *CLI) MergeSimulation(ctx context.Context, root, targetSha, branchSha string) (MergeSimulation, error) {
	var sim MergeSimulation
	if !IsCommitID(targetSha) || !IsCommitID(branchSha) {
		return sim, fmt.Errorf("%w: a merge simulation needs commit IDs", domain.ErrInvalid)
	}
	out, _, err := c.run(ctx, root, runOpts{timeout: 60 * time.Second},
		"merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", targetSha, branchSha)
	sim.Supported = true
	if err == nil {
		sim.Tree, _, _ = strings.Cut(string(out), "\x00")
		return sim, nil
	}
	var ge *gitError
	if !errors.As(err, &ge) {
		return sim, fmt.Errorf("simulate merge: %w", err)
	}
	switch {
	case ge.code == 1:
		sim.Conflicts = true
		toks := strings.Split(string(out), "\x00")
		for _, t := range toks[1:] {
			if t == "" {
				break
			}
			sim.Files = append(sim.Files, t)
		}
		return sim, nil
	case ge.code == 129 || strings.Contains(ge.stderr, "unknown option") || strings.Contains(ge.stderr, "usage:"):
		sim.Supported = false
		return sim, nil
	case strings.Contains(ge.stderr, "unrelated histories"):
		sim.Unrelated = true
		return sim, nil
	}
	return sim, fmt.Errorf("simulate merge: %w", err)
}

package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validWorktree() *Worktree {
	return &Worktree{
		ID: NewID(PrefixWorktree), ProjectID: "prj_x", Path: "/data/worktrees/wt_1",
		Branch: "devboard/task-1", BaseRef: "origin/main", State: WorktreeActive, Version: 1,
	}
}

func TestWorktreeValidate(t *testing.T) {
	if err := validWorktree().Validate(); err != nil {
		t.Fatalf("valid worktree rejected: %v", err)
	}
	cases := map[string]func(w *Worktree){
		"no project":          func(w *Worktree) { w.ProjectID = "" },
		"no id":               func(w *Worktree) { w.ID = "" },
		"unknown state":       func(w *Worktree) { w.State = "half-removed" },
		"empty path":          func(w *Worktree) { w.Path = "" },
		"relative path":       func(w *Worktree) { w.Path = "data/worktrees/x" },
		"dot-dot path":        func(w *Worktree) { w.Path = "/data/worktrees/../../etc" },
		"unclean path":        func(w *Worktree) { w.Path = "/data//worktrees/x" },
		"trailing slash":      func(w *Worktree) { w.Path = "/data/worktrees/x/" },
		"root":                func(w *Worktree) { w.Path = "/" },
		"newline in path":     func(w *Worktree) { w.Path = "/data/work\ntrees" },
		"NUL in path":         func(w *Worktree) { w.Path = "/data/work\x00trees" },
		"huge path":           func(w *Worktree) { w.Path = "/" + strings.Repeat("a/", 3000) + "a" },
		"branch is an option": func(w *Worktree) { w.Branch = "--delete" },
		"branch HEAD":         func(w *Worktree) { w.Branch = "HEAD" },
		"branch with space":   func(w *Worktree) { w.Branch = "my branch" },
		"base ref is option":  func(w *Worktree) { w.BaseRef = "--upload-pack=sh" },
		"empty base ref":      func(w *Worktree) { w.BaseRef = "" },
	}
	for name, mutate := range cases {
		w := validWorktree()
		mutate(w)
		if err := w.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestValidateRefName(t *testing.T) {
	ok := []string{"main", "feature/x", "devboard/task-12", "origin/main", "refs/heads/main", "HEAD", "v1.2.3", "a/b/c", "café", "release-1.0", "x@y", "a.b", "0123456789abcdef0123456789abcdef01234567"}
	bad := []string{
		"", "-x", "--force", "a b", "a\tb", "a\nb", "a\x00b", "a\x7fb", "a..b", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", `a\b`,
		"/a", "a/", "a//b", "a/.b", ".a", "a.", "a.lock", "a/b.lock", "a@{b", "@", "a/..", "..", "a\xffb", strings.Repeat("a", 256),
	}
	for _, n := range ok {
		if err := ValidateRefName(n); err != nil {
			t.Errorf("ValidateRefName(%q) = %v, want nil", n, err)
		}
	}
	for _, n := range bad {
		if err := ValidateRefName(n); !errors.Is(err, ErrInvalid) {
			t.Errorf("ValidateRefName(%q) = %v, want ErrInvalid", n, err)
		}
	}
}

func TestWorktreeRemovalProtocol(t *testing.T) {
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	t1 := t0.Add(time.Second)
	w := validWorktree()
	if w.Removing() {
		t.Fatal("a new worktree is not being removed")
	}
	if err := w.FinishRemoval(t0); !errors.Is(err, ErrTransition) {
		t.Fatalf("finishing before beginning: err = %v, want ErrTransition", err)
	}
	if w.State != WorktreeActive {
		t.Fatalf("a refused transition changed the state to %s", w.State)
	}

	if err := w.BeginRemoval(t0); err != nil {
		t.Fatal(err)
	}
	if !w.Removing() || w.State != WorktreeActive || w.RemovingSince == nil || !w.RemovingSince.Equal(t0) || !w.UpdatedAt.Equal(t0) {
		t.Fatalf("after BeginRemoval: %+v", w)
	}
	if err := w.BeginRemoval(t1); !errors.Is(err, ErrTransition) {
		t.Errorf("beginning twice: err = %v, want ErrTransition", err)
	}
	if !w.RemovingSince.Equal(t0) {
		t.Errorf("a refused BeginRemoval moved RemovingSince to %v", w.RemovingSince)
	}

	if err := w.FinishRemoval(t1); err != nil {
		t.Fatal(err)
	}
	if w.State != WorktreeRemoved || !w.UpdatedAt.Equal(t1) {
		t.Errorf("after FinishRemoval: %+v", w)
	}
	if err := w.FinishRemoval(t1); !errors.Is(err, ErrTransition) {
		t.Errorf("finishing twice: err = %v, want ErrTransition", err)
	}
	if err := w.BeginRemoval(t1); !errors.Is(err, ErrTransition) {
		t.Errorf("beginning on a removed worktree: err = %v, want ErrTransition", err)
	}
}

// A removed row with no removal timestamp cannot be produced by the protocol,
// but a row read back from a damaged or hand-edited database could look so; the
// state check, not just the timestamp, must keep it from restarting a removal.
func TestWorktreeRemovalChecksStateNotJustTimestamp(t *testing.T) {
	w := validWorktree()
	w.State = WorktreeRemoved
	if err := w.BeginRemoval(time.Now()); !errors.Is(err, ErrTransition) {
		t.Errorf("BeginRemoval on a removed worktree: err = %v, want ErrTransition", err)
	}
	if w.Removing() {
		t.Error("a refused BeginRemoval set RemovingSince")
	}
}

func TestWorktreeStates(t *testing.T) {
	if !WorktreeActive.CanTransitionTo(WorktreeRemoved) {
		t.Error("active -> removed should be allowed")
	}
	for _, c := range [][2]WorktreeState{{WorktreeRemoved, WorktreeActive}, {WorktreeRemoved, WorktreeRemoved}, {WorktreeActive, WorktreeActive}, {WorktreeActive, "bogus"}} {
		if c[0].CanTransitionTo(c[1]) {
			t.Errorf("%s -> %s should not be allowed", c[0], c[1])
		}
	}
}

// Each rejected case uses the smallest placement in which only the rule under
// test can reject the path, so a rule cannot be hidden by another one.
func TestWorktreePlacement(t *testing.T) {
	const root = "/data/worktrees"
	cases := []struct {
		name string
		pl   WorktreePlacement
		path string
		ok   bool
	}{
		{"plain", WorktreePlacement{Root: root}, root + "/wt_2", true},
		{"nested deeper", WorktreePlacement{Root: root}, root + "/a/b/wt_3", true},
		{"name sharing a prefix with an active worktree", WorktreePlacement{Root: root, Worktrees: []string{root + "/wt_1"}}, root + "/wt_1x", true},
		{"inside a registered repository's tree is allowed", WorktreePlacement{Root: root, RepoRoots: []string{root + "/repo"}, GitDirs: []string{"/elsewhere/.git"}}, root + "/repo/.wt/x", true},

		{"the root itself", WorktreePlacement{Root: root}, root, false},
		{"an ancestor of the root", WorktreePlacement{Root: root}, "/data", false},
		{"outside the root", WorktreePlacement{Root: root}, "/etc/wt", false},
		{"a sibling that shares the root's name as a prefix", WorktreePlacement{Root: root}, "/data/worktreesx/a", false},

		{"is a registered repository", WorktreePlacement{Root: root, RepoRoots: []string{root + "/linked"}}, root + "/linked", false},
		{"contains a registered repository", WorktreePlacement{Root: root, RepoRoots: []string{root + "/odd/repo"}}, root + "/odd", false},

		{"equals a git directory", WorktreePlacement{Root: root, GitDirs: []string{root + "/store/app/.git"}}, root + "/store/app/.git", false},
		{"inside a git directory", WorktreePlacement{Root: root, GitDirs: []string{root + "/store/app/.git"}}, root + "/store/app/.git/x", false},
		{"contains a git directory", WorktreePlacement{Root: root, GitDirs: []string{root + "/store/app/.git"}}, root + "/store", false},

		{"is an active worktree", WorktreePlacement{Root: root, Worktrees: []string{root + "/wt_1"}}, root + "/wt_1", false},
		{"inside an active worktree", WorktreePlacement{Root: root, Worktrees: []string{root + "/wt_1"}}, root + "/wt_1/inner", false},
		{"contains an active worktree", WorktreePlacement{Root: root, Worktrees: []string{root + "/group/wt_9"}}, root + "/group", false},

		{"no root configured", WorktreePlacement{}, root + "/wt_2", false},
	}
	for _, c := range cases {
		err := c.pl.Check(c.path)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s: Check(%q) = %v, want nil", c.name, c.path, err)
		case !c.ok && !errors.Is(err, ErrInvalid):
			t.Errorf("%s: Check(%q) = %v, want ErrInvalid", c.name, c.path, err)
		}
	}
}

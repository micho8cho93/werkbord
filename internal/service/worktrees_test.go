package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// pathGit treats any path as a repository rooted there, with its Git
// directory at <path>/.git. Nothing is read from disk.
type pathGit struct {
	// gitDirs puts a repository's Git directory somewhere other than
	// <path>/.git, as for a registered linked worktree, whose Git directory
	// belongs to the main checkout.
	gitDirs map[string]string
}

func (g pathGit) Inspect(_ context.Context, path string) (*domain.GitRepository, error) {
	common := path + "/.git"
	if d, ok := g.gitDirs[path]; ok {
		common = d
	}
	return &domain.GitRepository{RootPath: path, CommonDir: common, CurrentBranch: "main", Remotes: []domain.GitRemote{}, InspectedAt: time.Now().UTC()}, nil
}

type wtFixture struct {
	*fixture
	svc     *Worktrees
	base    string // canonical temporary directory holding everything
	root    string // the only place worktrees may be created
	project *ProjectDetail
}

func newWTFixture(t *testing.T) *wtFixture {
	t.Helper()
	f := newFixture(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A repository whose tree is inside the worktree directory but whose Git
	// directory is far away (as with a worktree registered by itself).
	f.projects.Git = pathGit{gitDirs: map[string]string{filepath.Join(base, "worktrees", "linked"): filepath.Join(base, "code", "other", ".git")}}
	p, err := f.projects.Register(context.Background(), filepath.Join(base, "code", "app"), "app")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "worktrees")
	return &wtFixture{fixture: f, svc: &Worktrees{Deps: f.deps, Root: root}, base: base, root: root, project: p}
}

func (f *wtFixture) create(t *testing.T, name, branch string) *domain.Worktree {
	t.Helper()
	w, err := f.svc.Create(context.Background(), NewWorktree{ProjectID: f.project.ID, Path: filepath.Join(f.root, name), Branch: branch, BaseRef: "origin/main"})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return w
}

func TestWorktreesCreate(t *testing.T) {
	f := newWTFixture(t)
	ctx := context.Background()
	sub := f.bus.Subscribe(10)

	w := f.create(t, "wt_1", "devboard/task-1")
	if w.State != domain.WorktreeActive || w.Version != 1 || w.RemovingSince != nil || w.ProjectID != f.project.ID ||
		w.Path != filepath.Join(f.root, "wt_1") || w.Branch != "devboard/task-1" || w.BaseRef != "origin/main" {
		t.Fatalf("created worktree = %+v", w)
	}
	if ev := <-sub.C; ev.Type != domain.EventWorktreeCreated || ev.ProjectID != f.project.ID || ev.Seq == 0 {
		t.Fatalf("event = %+v", ev)
	}
	if got, err := f.svc.Get(ctx, w.ID); err != nil || got.ID != w.ID {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if list, err := f.svc.ListByProject(ctx, f.project.ID); err != nil || len(list) != 1 {
		t.Errorf("ListByProject = %+v, %v", list, err)
	}
	if _, err := f.svc.Get(ctx, "wt_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get missing: err = %v", err)
	}
}

func TestWorktreesRefuseDangerousPlacement(t *testing.T) {
	f := newWTFixture(t)
	ctx := context.Background()
	// Repositories registered inside the worktree root, as a user might do.
	for _, repo := range []string{"group/repo", "store/app", "linked"} {
		if _, err := f.projects.Register(ctx, filepath.Join(f.root, repo), filepath.Base(repo)); err != nil {
			t.Fatal(err)
		}
	}
	real := filepath.Join(f.root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(f.root, "link")); err != nil {
		t.Fatal(err)
	}
	f.create(t, "taken", "taken-branch")
	f.create(t, "group2/wt_1", "g2")

	cases := []struct {
		name string
		in   NewWorktree
		want error
	}{
		{"outside the worktree directory", NewWorktree{Path: filepath.Join(f.base, "elsewhere", "wt"), Branch: "b"}, domain.ErrInvalid},
		{"the worktree directory itself", NewWorktree{Path: f.root, Branch: "b"}, domain.ErrInvalid},
		{"a relative path", NewWorktree{Path: "worktrees/wt", Branch: "b"}, domain.ErrInvalid},
		{"a path with ..", NewWorktree{Path: f.root + "/a/../../etc", Branch: "b"}, domain.ErrInvalid},
		{"a registered repository", NewWorktree{Path: filepath.Join(f.root, "store", "app"), Branch: "b"}, domain.ErrInvalid},
		// Only the repository-tree rule can reject this: its Git directory is elsewhere.
		{"a registered repository whose Git directory is elsewhere", NewWorktree{Path: filepath.Join(f.root, "linked"), Branch: "b"}, domain.ErrInvalid},
		{"a directory containing a registered repository", NewWorktree{Path: filepath.Join(f.root, "group"), Branch: "b"}, domain.ErrInvalid},
		{"inside a repository's Git directory", NewWorktree{Path: filepath.Join(f.root, "store", "app", ".git", "wt"), Branch: "b"}, domain.ErrInvalid},
		{"through a symlink", NewWorktree{Path: filepath.Join(f.root, "link", "wt"), Branch: "b"}, domain.ErrInvalid},
		{"inside an active worktree", NewWorktree{Path: filepath.Join(f.root, "taken", "inner"), Branch: "b"}, domain.ErrInvalid},
		{"a directory containing an active worktree", NewWorktree{Path: filepath.Join(f.root, "group2"), Branch: "b"}, domain.ErrInvalid},
		{"a branch that looks like an option", NewWorktree{Path: filepath.Join(f.root, "n1"), Branch: "--force"}, domain.ErrInvalid},
		{"a base ref that looks like an option", NewWorktree{Path: filepath.Join(f.root, "n2"), Branch: "b", BaseRef: "--upload-pack=sh"}, domain.ErrInvalid},
		{"the path of an active worktree", NewWorktree{Path: filepath.Join(f.root, "taken"), Branch: "b"}, domain.ErrInvalid},
		{"a branch that already has a worktree", NewWorktree{Path: filepath.Join(f.root, "n3"), Branch: "taken-branch"}, domain.ErrDuplicate},
		{"a project that does not exist", NewWorktree{ProjectID: "prj_missing", Path: filepath.Join(f.root, "n4"), Branch: "b"}, domain.ErrNotFound},
		// "Not found" must win over placement errors, so a caller with a stale
		// project id is told that, not that its path is wrong.
		{"a missing project and a bad path", NewWorktree{ProjectID: "prj_missing", Path: filepath.Join(f.base, "elsewhere", "wt"), Branch: "b"}, domain.ErrNotFound},
	}
	for _, c := range cases {
		if c.in.ProjectID == "" {
			c.in.ProjectID = f.project.ID
		}
		if c.in.BaseRef == "" {
			c.in.BaseRef = "origin/main"
		}
		if _, err := f.svc.Create(ctx, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if list, _ := f.svc.ListByProject(ctx, f.project.ID); len(list) != 2 {
		t.Errorf("refused requests left %d worktrees behind, want 2", len(list))
	}
}

// Nothing may be placed unless a worktree directory is configured: a missing
// setting must not mean "anywhere".
func TestWorktreesFailClosedWithoutARoot(t *testing.T) {
	f := newWTFixture(t)
	for _, root := range []string{"", "worktrees", f.root + "/../etc"} {
		svc := &Worktrees{Deps: f.deps, Root: root}
		_, err := svc.Create(context.Background(), NewWorktree{ProjectID: f.project.ID, Path: filepath.Join(f.root, "wt"), Branch: "b", BaseRef: "main"})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Root %q: err = %v, want ErrInvalid", root, err)
		}
	}
}

func TestWorktreeRemovalProtocol(t *testing.T) {
	f := newWTFixture(t)
	ctx := context.Background()
	w := f.create(t, "wt_1", "feature")
	sub := f.bus.Subscribe(10)

	if _, err := f.svc.FinishRemoval(ctx, w.ID, w.Version); !errors.Is(err, domain.ErrTransition) {
		t.Errorf("finishing before beginning: err = %v, want ErrTransition", err)
	}
	if _, err := f.svc.BeginRemoval(ctx, w.ID, w.Version+7); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("stale version: err = %v, want ErrConflict", err)
	}
	if _, err := f.svc.BeginRemoval(ctx, "wt_missing", 1); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing worktree: err = %v, want ErrNotFound", err)
	}

	got, err := f.svc.BeginRemoval(ctx, w.ID, w.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.RemovingSince == nil || got.State != domain.WorktreeActive || got.Version != 2 {
		t.Fatalf("after BeginRemoval: %+v", got)
	}
	if ev := <-sub.C; ev.Type != domain.EventWorktreeRemoving {
		t.Errorf("event = %+v", ev)
	}
	if _, err := f.svc.BeginRemoval(ctx, w.ID, got.Version); !errors.Is(err, domain.ErrTransition) {
		t.Errorf("beginning twice: err = %v, want ErrTransition", err)
	}

	// Until the directory is confirmed gone its place and branch stay taken.
	if _, err := f.svc.Create(ctx, NewWorktree{ProjectID: f.project.ID, Path: filepath.Join(f.root, "wt_1", "inner"), Branch: "other", BaseRef: "main"}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("creating inside a worktree being removed: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.Create(ctx, NewWorktree{ProjectID: f.project.ID, Path: filepath.Join(f.root, "wt_2"), Branch: "feature", BaseRef: "main"}); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("reusing the branch of a worktree being removed: err = %v, want ErrDuplicate", err)
	}

	done, err := f.svc.FinishRemoval(ctx, w.ID, got.Version)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != domain.WorktreeRemoved || done.Version != 3 {
		t.Fatalf("after FinishRemoval: %+v", done)
	}
	if ev := <-sub.C; ev.Type != domain.EventWorktreeRemoved {
		t.Errorf("event = %+v", ev)
	}
	if _, err := f.svc.FinishRemoval(ctx, w.ID, done.Version); !errors.Is(err, domain.ErrTransition) {
		t.Errorf("finishing twice: err = %v, want ErrTransition", err)
	}

	// Now the space and the branch are free again, but the old path is not
	// reused: its record is kept as history.
	if _, err := f.svc.Create(ctx, NewWorktree{ProjectID: f.project.ID, Path: filepath.Join(f.root, "wt_3"), Branch: "feature", BaseRef: "main"}); err != nil {
		t.Errorf("reusing the branch after removal: %v", err)
	}
	if _, err := f.svc.Create(ctx, NewWorktree{ProjectID: f.project.ID, Path: filepath.Join(f.root, "wt_1"), Branch: "again", BaseRef: "main"}); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("reusing a removed worktree's path: err = %v, want ErrDuplicate", err)
	}
}

func TestWorktreeInUseCannotBeRemoved(t *testing.T) {
	f := newWTFixture(t)
	ctx := context.Background()
	w := f.create(t, "wt_1", "feature")
	task, err := f.tasks.Create(ctx, f.project.ID, "t", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	run := &domain.Run{ID: domain.NewID(domain.PrefixRun), TaskID: task.ID, ProjectID: f.project.ID, AgentID: "x", State: domain.RunRunning, WorktreeID: w.ID, CreatedAt: now, UpdatedAt: now}
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Runs().Create(ctx, run) }); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.BeginRemoval(ctx, w.ID, w.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict: a running agent's worktree must not be deleted", err)
	}
	if got, _ := f.svc.Get(ctx, w.ID); got.RemovingSince != nil || got.Version != w.Version {
		t.Fatalf("worktree was changed: %+v", got)
	}

	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		run.State, run.UpdatedAt = domain.RunCompleted, now
		return tx.Runs().Update(ctx, run)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.BeginRemoval(ctx, w.ID, w.Version); err != nil {
		t.Errorf("after the run finished: %v", err)
	}
}

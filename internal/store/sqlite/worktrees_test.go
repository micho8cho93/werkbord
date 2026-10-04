package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// These tests pin the rules the database itself enforces about worktrees. They
// matter because deleting a worktree's directory is irreversible: whatever
// code does it will trust these records, so the records must not be able to
// say something dangerous, whoever wrote them.

var ctx = context.Background()

func newWT(projectID, path, branch string) *domain.Worktree {
	return &domain.Worktree{
		ID: domain.NewID(domain.PrefixWorktree), ProjectID: projectID, Path: path, Branch: branch,
		BaseRef: "origin/main", State: domain.WorktreeActive, CreatedAt: now(), UpdatedAt: now(),
	}
}

func createWT(db *DB, w *domain.Worktree) error {
	return db.Update(ctx, func(tx store.Tx) error { return tx.Worktrees().Create(ctx, w) })
}

func mustWT(t *testing.T, db *DB, projectID, path, branch string) *domain.Worktree {
	t.Helper()
	w := newWT(projectID, path, branch)
	if err := createWT(db, w); err != nil {
		t.Fatal(err)
	}
	return w
}

func getWT(t *testing.T, db *DB, id string) *domain.Worktree {
	t.Helper()
	var w *domain.Worktree
	if err := db.View(ctx, func(tx store.Tx) error {
		var err error
		w, err = tx.Worktrees().Get(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

// beginRemoval records that the directory is about to be deleted.
func beginRemoval(db *DB, w *domain.Worktree) error {
	return db.Update(ctx, func(tx store.Tx) error {
		t := now()
		w.RemovingSince, w.UpdatedAt = &t, t
		return tx.Worktrees().Update(ctx, w)
	})
}

// markRemoved runs the whole protocol: begin, then record the directory gone.
func markRemoved(db *DB, w *domain.Worktree) error {
	if err := beginRemoval(db, w); err != nil {
		return err
	}
	return db.Update(ctx, func(tx store.Tx) error {
		w.State = domain.WorktreeRemoved
		w.UpdatedAt = now()
		return tx.Worktrees().Update(ctx, w)
	})
}

func mustTask(t *testing.T, db *DB, projectID string) *domain.Task {
	t.Helper()
	task := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: projectID, Title: "t", State: domain.TaskDoing, Position: 1, CreatedAt: now(), UpdatedAt: now()}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Tasks().Create(ctx, task) }); err != nil {
		t.Fatal(err)
	}
	return task
}

func createRun(db *DB, projectID, taskID, worktreeID string, state domain.RunState) (*domain.Run, error) {
	r := &domain.Run{ID: domain.NewID(domain.PrefixRun), TaskID: taskID, ProjectID: projectID, AgentID: "x", State: state, WorktreeID: worktreeID, CreatedAt: now(), UpdatedAt: now()}
	return r, db.Update(ctx, func(tx store.Tx) error { return tx.Runs().Create(ctx, r) })
}

func finishRun(t *testing.T, db *DB, r *domain.Run) {
	t.Helper()
	if err := db.Update(ctx, func(tx store.Tx) error {
		r.State = domain.RunCompleted
		r.UpdatedAt = now()
		return tx.Runs().Update(ctx, r)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeRowsMustBeWellFormed(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	cases := map[string]*domain.Worktree{
		"relative path":         newWT(p.ID, "relative/dir", "b"),
		"filesystem root":       newWT(p.ID, "/", "b"),
		"trailing slash":        newWT(p.ID, "/a/b/", "b"),
		"dot-dot segment":       newWT(p.ID, "/a/../b", "b"),
		"dot segment":           newWT(p.ID, "/a/./b", "b"),
		"double slash":          newWT(p.ID, "/a//b", "b"),
		"trailing dot-dot":      newWT(p.ID, "/a/..", "b"),
		"empty branch":          newWT(p.ID, "/a/b", ""),
		"branch like an option": newWT(p.ID, "/a/b", "-D"),
		"empty base ref":        func() *domain.Worktree { w := newWT(p.ID, "/a/b", "b"); w.BaseRef = ""; return w }(),
		"base ref like option":  func() *domain.Worktree { w := newWT(p.ID, "/a/b", "b"); w.BaseRef = "--upload-pack=x"; return w }(),
	}
	for name, w := range cases {
		if err := createWT(db, w); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	// A well-formed one is still accepted.
	if err := createWT(db, newWT(p.ID, "/data/worktrees/wt_1", "devboard/task-1")); err != nil {
		t.Errorf("valid worktree rejected: %v", err)
	}
}

func TestWorktreeIdentityIsImmutable(t *testing.T) {
	db, _ := openTemp(t)
	p, other := seedProject(t, db), seedProject(t, db)
	w := mustWT(t, db, p.ID, "/data/worktrees/a", "feature")

	for name, q := range map[string]string{
		"path":       `UPDATE worktrees SET path = '/data/worktrees/elsewhere' WHERE id = ?`,
		"branch":     `UPDATE worktrees SET branch = 'main' WHERE id = ?`,
		"base ref":   `UPDATE worktrees SET base_ref = 'HEAD~5' WHERE id = ?`,
		"project":    `UPDATE worktrees SET project_id = '` + other.ID + `' WHERE id = ?`,
		"identifier": `UPDATE worktrees SET id = 'wt_other' WHERE id = ?`,
	} {
		if _, err := db.writer.ExecContext(ctx, q, w.ID); err == nil {
			t.Errorf("changing a worktree's %s was allowed", name)
		}
	}
	got := getWT(t, db, w.ID)
	if got.Path != w.Path || got.Branch != w.Branch || got.BaseRef != w.BaseRef || got.ProjectID != w.ProjectID {
		t.Errorf("worktree changed: %+v", got)
	}
}

func TestRemovedWorktreeCannotComeBack(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	w := mustWT(t, db, p.ID, "/data/worktrees/a", "feature")
	if err := markRemoved(db, w); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.ExecContext(ctx, `UPDATE worktrees SET state = 'active' WHERE id = ?`, w.ID); err == nil {
		t.Error("a removed worktree was made active again")
	}
	if got := getWT(t, db, w.ID); got.State != domain.WorktreeRemoved {
		t.Errorf("state = %s, want removed", got.State)
	}
}

// Deleting a project cascades to its worktree rows. If the directories were
// still on disk they would be orphaned: nothing would know to clean them up,
// or that a run's changes lived there.
func TestActiveWorktreeRecordsSurviveProjectDeletion(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	w := mustWT(t, db, p.ID, "/data/worktrees/a", "feature")

	if _, err := db.writer.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, p.ID); err == nil {
		t.Error("a project with an active worktree was deleted")
	}
	if _, err := db.writer.ExecContext(ctx, `DELETE FROM worktrees WHERE id = ?`, w.ID); err == nil {
		t.Error("an active worktree record was deleted")
	}
	if got := getWT(t, db, w.ID); got.State != domain.WorktreeActive {
		t.Fatalf("worktree record was damaged: %+v", got)
	}

	// Once the directory is gone and the record says so, cleanup may cascade.
	if err := markRemoved(db, w); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, p.ID); err != nil {
		t.Errorf("project with only removed worktrees could not be deleted: %v", err)
	}
}

func TestOneActiveWorktreePerBranch(t *testing.T) {
	db, _ := openTemp(t)
	a, b := seedProject(t, db), seedProject(t, db)
	first := mustWT(t, db, a.ID, "/data/worktrees/1", "feature")

	if err := createWT(db, newWT(a.ID, "/data/worktrees/2", "feature")); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("same branch twice in one project: err = %v, want ErrDuplicate (Git refuses it as well)", err)
	}
	if err := createWT(db, newWT(b.ID, "/data/worktrees/3", "feature")); err != nil {
		t.Errorf("same branch name in another project is a different branch: %v", err)
	}
	if err := markRemoved(db, first); err != nil {
		t.Fatal(err)
	}
	if err := createWT(db, newWT(a.ID, "/data/worktrees/4", "feature")); err != nil {
		t.Errorf("branch of a removed worktree should be reusable: %v", err)
	}
}

func TestWorktreeUpdatesAreCompareAndSwap(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	w := mustWT(t, db, p.ID, "/data/worktrees/a", "feature")
	if w.Version != 1 {
		t.Fatalf("new worktree version = %d, want 1", w.Version)
	}

	// Two writers read the same version; only the first may write.
	first, second := getWT(t, db, w.ID), getWT(t, db, w.ID)
	if err := beginRemoval(db, first); err != nil {
		t.Fatal(err)
	}
	if first.Version != 2 {
		t.Errorf("version after update = %d, want 2", first.Version)
	}
	if err := beginRemoval(db, second); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("stale update: err = %v, want ErrConflict", err)
	}
	ghost := newWT(p.ID, "/data/worktrees/ghost", "ghost")
	ghost.Version = 1
	if err := beginRemoval(db, ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing worktree: err = %v, want ErrNotFound", err)
	}

	// Update only ever changes state; a caller cannot smuggle in another branch.
	w2 := mustWT(t, db, p.ID, "/data/worktrees/b", "other")
	w2.Branch, w2.BaseRef = "main", "HEAD~9"
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Worktrees().Update(ctx, w2) }); err != nil {
		t.Fatal(err)
	}
	if got := getWT(t, db, w2.ID); got.Branch != "other" || got.BaseRef != "origin/main" {
		t.Errorf("Update rewrote branch/base ref: %+v", got)
	}
}

func TestRunsAndWorktreesStayConsistent(t *testing.T) {
	t.Run("a run cannot use another project's worktree", func(t *testing.T) {
		db, _ := openTemp(t)
		a, b := seedProject(t, db), seedProject(t, db)
		w := mustWT(t, db, a.ID, "/data/worktrees/a", "feature")
		task := mustTask(t, db, b.ID)
		if _, err := createRun(db, b.ID, task.ID, w.ID, domain.RunRunning); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("err = %v, want ErrInvalid", err)
		}
	})

	t.Run("two active runs cannot share a worktree", func(t *testing.T) {
		db, _ := openTemp(t)
		p := seedProject(t, db)
		w := mustWT(t, db, p.ID, "/data/worktrees/a", "feature")
		task := mustTask(t, db, p.ID)
		first, err := createRun(db, p.ID, task.ID, w.ID, domain.RunRunning)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := createRun(db, p.ID, task.ID, w.ID, domain.RunStarting); !errors.Is(err, domain.ErrDuplicate) {
			t.Errorf("second active run: err = %v, want ErrDuplicate", err)
		}
		finishRun(t, db, first)
		if _, err := createRun(db, p.ID, task.ID, w.ID, domain.RunStarting); err != nil {
			t.Errorf("a finished run must free the worktree: %v", err)
		}
	})

	t.Run("a worktree in use cannot begin removal", func(t *testing.T) {
		db, _ := openTemp(t)
		p := seedProject(t, db)
		w := mustWT(t, db, p.ID, "/data/worktrees/a", "feature")
		task := mustTask(t, db, p.ID)
		r, err := createRun(db, p.ID, task.ID, w.ID, domain.RunWaitingForUser)
		if err != nil {
			t.Fatal(err)
		}
		if err := beginRemoval(db, getWT(t, db, w.ID)); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict: deleting it would destroy a live run's uncommitted work", err)
		}
		if got := getWT(t, db, w.ID); got.RemovingSince != nil || got.State != domain.WorktreeActive {
			t.Fatalf("worktree was changed: %+v", got)
		}
		finishRun(t, db, r)
		if err := beginRemoval(db, getWT(t, db, w.ID)); err != nil {
			t.Errorf("worktree of a finished run could not begin removal: %v", err)
		}
	})

	t.Run("an active run cannot start on a removed or removing worktree", func(t *testing.T) {
		db, _ := openTemp(t)
		p := seedProject(t, db)
		task := mustTask(t, db, p.ID)
		removing := mustWT(t, db, p.ID, "/data/worktrees/a", "one")
		removed := mustWT(t, db, p.ID, "/data/worktrees/b", "two")
		if err := beginRemoval(db, removing); err != nil {
			t.Fatal(err)
		}
		if err := markRemoved(db, removed); err != nil {
			t.Fatal(err)
		}
		for name, w := range map[string]*domain.Worktree{"removing": removing, "removed": removed} {
			if _, err := createRun(db, p.ID, task.ID, w.ID, domain.RunStarting); !errors.Is(err, domain.ErrConflict) {
				t.Errorf("%s: err = %v, want ErrConflict", name, err)
			}
			// History is unaffected: a finished run may still point at it.
			if _, err := createRun(db, p.ID, task.ID, w.ID, domain.RunCompleted); err != nil {
				t.Errorf("%s: recording a finished run failed: %v", name, err)
			}
		}
	})

	t.Run("a worktree cannot be attached to a run once removal begins", func(t *testing.T) {
		db, _ := openTemp(t)
		p := seedProject(t, db)
		task := mustTask(t, db, p.ID)
		w := mustWT(t, db, p.ID, "/data/worktrees/a", "one")
		r, err := createRun(db, p.ID, task.ID, "", domain.RunStarting) // created first, attached later
		if err != nil {
			t.Fatal(err)
		}
		if err := beginRemoval(db, w); err != nil {
			t.Fatal(err)
		}
		err = db.Update(ctx, func(tx store.Tx) error {
			r.WorktreeID = w.ID
			return tx.Runs().Update(ctx, r)
		})
		if !errors.Is(err, domain.ErrConflict) {
			t.Errorf("err = %v, want ErrConflict", err)
		}
	})
}

// Removal is two steps so that the check "nothing is using this" and the
// deletion cannot be separated by a run starting.
func TestRemovalIsTwoStepAndOneWay(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	w := mustWT(t, db, p.ID, "/data/worktrees/a", "feature")

	// Straight to removed, skipping the first step, is refused.
	skip := getWT(t, db, w.ID)
	err := db.Update(ctx, func(tx store.Tx) error {
		skip.State, skip.UpdatedAt = domain.WorktreeRemoved, now()
		return tx.Worktrees().Update(ctx, skip)
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("skipping the first step: err = %v, want ErrConflict", err)
	}
	if got := getWT(t, db, w.ID); got.State != domain.WorktreeActive || got.RemovingSince != nil {
		t.Fatalf("worktree changed: %+v", got)
	}

	if err := beginRemoval(db, w); err != nil {
		t.Fatal(err)
	}
	got := getWT(t, db, w.ID)
	if got.RemovingSince == nil || got.State != domain.WorktreeActive {
		t.Fatalf("after beginning removal: %+v", got)
	}
	// It stays recorded as being on disk (so its path and branch stay reserved)
	// until the directory is confirmed gone, and the decision cannot be undone.
	if _, err := db.writer.ExecContext(ctx, `UPDATE worktrees SET removing_since = NULL WHERE id = ?`, w.ID); err == nil {
		t.Error("removal was cancelled by clearing removing_since")
	}
}

// A transaction acknowledged to the caller must survive power loss. The
// worktree engine will create or delete directories on the strength of a
// committed record, so a record that can silently roll back lets the database
// and the disk disagree.
func TestWriterSyncsEveryCommit(t *testing.T) {
	db, _ := openTemp(t)
	var mode int
	if err := db.writer.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != 2 { // 0 OFF, 1 NORMAL, 2 FULL
		t.Errorf("PRAGMA synchronous = %d, want 2 (FULL)", mode)
	}
}

// TestMigrationKeepsExistingWorktreesAndRunLinks upgrades a version-2 database
// that already has a worktree with a run on it. Schema changes that rebuild
// `worktrees` would fire ON DELETE SET NULL and silently detach that run.
func TestMigrationKeepsExistingWorktreesAndRunLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := Migrations()
	if err != nil || len(ms) < 3 {
		t.Fatalf("migrations: %d, %v", len(ms), err)
	}
	if _, err := migrate(ctx, raw, ms[:2]); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO projects VALUES ('prj_old', 'old', '/repos/old', 1, 1)`,
		`INSERT INTO tasks VALUES ('tsk_old', 'prj_old', 't', '', 'doing', 1, 1, 1, 1)`,
		`INSERT INTO worktrees VALUES ('wt_old', 'prj_old', '/data/worktrees/old', 'devboard/old', 'main', 'active', 1, 1)`,
		`INSERT INTO runs (id, task_id, project_id, agent_id, state, worktree_id, created_at, updated_at)
		 VALUES ('run_old', 'tsk_old', 'prj_old', 'x', 'running', 'wt_old', 1, 1)`,
	} {
		if _, err := raw.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_ = raw.Close()

	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w := getWT(t, db, "wt_old")
	if w.Version != 1 || w.State != domain.WorktreeActive || w.Branch != "devboard/old" {
		t.Errorf("worktree after migration = %+v", w)
	}
	if err := db.View(ctx, func(tx store.Tx) error {
		r, err := tx.Runs().Get(ctx, "run_old")
		if err != nil {
			return err
		}
		if r.WorktreeID != "wt_old" {
			t.Errorf("run lost its worktree: %+v", r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

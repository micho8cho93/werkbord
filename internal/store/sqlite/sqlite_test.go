package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func openTemp(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devboard.db")
	db, err := Open(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func seedProject(t *testing.T, db *DB) *domain.Project {
	t.Helper()
	p := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: "demo", RepoPath: "/tmp/demo-" + domain.NewID("x"), CreatedAt: now(), UpdatedAt: now()}
	if err := db.Update(context.Background(), func(tx store.Tx) error { return tx.Projects().Create(context.Background(), p) }); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMigrationsAreContiguousAndIdempotent(t *testing.T) {
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	db, path := openTemp(t)
	v, err := db.SchemaVersion(context.Background())
	if err != nil || v != len(ms) {
		t.Fatalf("schema version = %d, %v; want %d", v, err, len(ms))
	}
	_ = db.Close()

	// Reopening applies nothing and keeps the version.
	db2, err := Open(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	applied, err := migrate(context.Background(), db2.writer, ms)
	if err != nil || applied != 0 {
		t.Fatalf("second migrate applied %d, err %v", applied, err)
	}
}

func TestRefusesNewerSchema(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	if _, err := db.writer.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (999, 'future', 0)`); err != nil {
		t.Fatal(err)
	}
	ms, _ := Migrations()
	if _, err := migrate(ctx, db.writer, ms); err == nil {
		t.Fatal("expected an error for a database newer than the build")
	}
}

func TestStatePersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	db, path := openTemp(t)
	p := seedProject(t, db)
	task := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: p.ID, Title: "t", State: domain.TaskDoing, Position: 1, CreatedAt: now(), UpdatedAt: now()}
	wt := &domain.Worktree{ID: domain.NewID(domain.PrefixWorktree), ProjectID: p.ID, Path: "/tmp/wt1", Branch: "devboard/t", BaseRef: "origin/main", State: domain.WorktreeActive, CreatedAt: now(), UpdatedAt: now()}
	run := &domain.Run{ID: domain.NewID(domain.PrefixRun), TaskID: task.ID, ProjectID: p.ID, AgentID: "claude-code", State: domain.RunWaitingForUser, Waiting: domain.WaitQuestion, WorktreeID: wt.ID, CreatedAt: now(), UpdatedAt: now()}
	q := &domain.Question{ID: domain.NewID(domain.PrefixQuestion), RunID: run.ID, TaskID: task.ID, ProjectID: p.ID, Kind: domain.QuestionDecision, Prompt: "Push the tag?", Context: "v1.2.0 on main", Options: []string{"Yes", "No"}, State: domain.QuestionPending, AskedAt: now()}
	ev, _ := domain.NewEvent(domain.EventAgentQuestion, map[string]string{"questionId": q.ID})
	ev.RunID = run.ID

	err := db.Update(ctx, func(tx store.Tx) error {
		return errors.Join(
			tx.Tasks().Create(ctx, task),
			tx.Worktrees().Create(ctx, wt),
			tx.Runs().Create(ctx, run),
			tx.Questions().Create(ctx, q),
			tx.Events().Append(ctx, &ev),
		)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	err = db2.View(ctx, func(tx store.Tx) error {
		if got, err := tx.Projects().Get(ctx, p.ID); err != nil || got.RepoPath != p.RepoPath {
			t.Errorf("project: %+v, %v", got, err)
		}
		if got, err := tx.Tasks().Get(ctx, task.ID); err != nil || got.State != domain.TaskDoing || got.Version != 1 {
			t.Errorf("task: %+v, %v", got, err)
		}
		if got, err := tx.Runs().ListActive(ctx); err != nil || len(got) != 1 || got[0].WorktreeID != wt.ID {
			t.Errorf("active runs: %+v, %v", got, err)
		}
		if got, err := tx.Questions().ListPending(ctx); err != nil || len(got) != 1 || len(got[0].Options) != 2 {
			t.Errorf("pending questions: %+v, %v", got, err)
		}
		if got, err := tx.Worktrees().ListByProject(ctx, p.ID); err != nil || len(got) != 1 {
			t.Errorf("worktrees: %+v, %v", got, err)
		}
		if got, err := tx.Events().ListAfter(ctx, 0, 10); err != nil || len(got) != 1 || got[0].RunID != run.ID {
			t.Errorf("events: %+v, %v", got, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTaskUpdateIsCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: p.ID, Title: "t", State: domain.TaskBacklog, Position: 1, CreatedAt: now(), UpdatedAt: now()}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Tasks().Create(ctx, task) })

	stale := *task
	task.State = domain.TaskDoing
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Tasks().Update(ctx, task) })
	if task.Version != 2 {
		t.Fatalf("version after update = %d, want 2", task.Version)
	}

	stale.Title = "overwrite"
	err := db.Update(ctx, func(tx store.Tx) error { return tx.Tasks().Update(ctx, &stale) })
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale update: err = %v, want ErrConflict", err)
	}

	missing := domain.Task{ID: "tsk_missing", Version: 1}
	err = db.Update(ctx, func(tx store.Tx) error { return tx.Tasks().Update(ctx, &missing) })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing update: err = %v, want ErrNotFound", err)
	}
}

func TestDuplicateProjectPath(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	p := seedProject(t, db)
	dup := *p
	dup.ID = domain.NewID(domain.PrefixProject)
	err := db.Update(ctx, func(tx store.Tx) error { return tx.Projects().Create(ctx, &dup) })
	if !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

func TestDatabaseRejectsUnknownStates(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: p.ID, Title: "t", State: "in_progress", CreatedAt: now(), UpdatedAt: now()}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Tasks().Create(ctx, task) }); err == nil {
		t.Fatal("expected CHECK constraint to reject an unknown task state")
	}
}

func TestRollbackOnError(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	boom := errors.New("boom")
	err := db.Update(ctx, func(tx store.Tx) error {
		ev := domain.Event{Type: "test"}
		if err := tx.Events().Append(ctx, &ev); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	_ = db.View(ctx, func(tx store.Tx) error {
		if seq, _ := tx.Events().LatestSeq(ctx); seq != 0 {
			t.Errorf("event survived rollback: seq %d", seq)
		}
		return nil
	})
}

func TestEventsListAfter(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	mustUpdate(t, db, func(tx store.Tx) error {
		for i := 0; i < 5; i++ {
			e := domain.Event{Type: "test"}
			if err := tx.Events().Append(ctx, &e); err != nil {
				return err
			}
		}
		return nil
	})
	_ = db.View(ctx, func(tx store.Tx) error {
		got, err := tx.Events().ListAfter(ctx, 2, 2)
		if err != nil || len(got) != 2 || got[0].Seq != 3 || got[1].Seq != 4 {
			t.Errorf("ListAfter(2, 2) = %+v, %v", got, err)
		}
		return nil
	})
}

func mustUpdate(t *testing.T, db *DB, fn func(store.Tx) error) {
	t.Helper()
	if err := db.Update(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func snapshot(projectID, commonDir string) *domain.GitRepository {
	return &domain.GitRepository{ProjectID: projectID, RootPath: "/r/" + projectID, CommonDir: commonDir, Remotes: []domain.GitRemote{}, InspectedAt: now()}
}

func TestRepositoryCommonDirIsUnique(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	a, b, c := seedProject(t, db), seedProject(t, db), seedProject(t, db)

	err := db.Update(ctx, func(tx store.Tx) error {
		if err := tx.Repositories().Upsert(ctx, snapshot(a.ID, "/repos/x/.git")); err != nil {
			return err
		}
		// Refreshing a project's own snapshot is not a conflict.
		if err := tx.Repositories().Upsert(ctx, snapshot(a.ID, "/repos/x/.git")); err != nil {
			return err
		}
		// Unknown ('') identities may repeat: rows from before the migration.
		if err := tx.Repositories().Upsert(ctx, snapshot(b.ID, "")); err != nil {
			return err
		}
		return tx.Repositories().Upsert(ctx, snapshot(c.ID, ""))
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.Update(ctx, func(tx store.Tx) error { return tx.Repositories().Upsert(ctx, snapshot(b.ID, "/repos/x/.git")) })
	if !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("second project with the same common dir: err = %v, want ErrDuplicate", err)
	}

	_ = db.View(ctx, func(tx store.Tx) error {
		got, err := tx.Repositories().GetByCommonDir(ctx, "/repos/x/.git")
		if err != nil || got.ProjectID != a.ID {
			t.Errorf("GetByCommonDir = %+v, %v; want project %s", got, err, a.ID)
		}
		if _, err := tx.Repositories().GetByCommonDir(ctx, "/repos/none/.git"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("unknown common dir: err = %v, want ErrNotFound", err)
		}
		if _, err := tx.Repositories().GetByCommonDir(ctx, ""); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("empty common dir must never match: err = %v", err)
		}
		return nil
	})
}

// TestMigrationKeepsExistingRepositorySnapshots upgrades a version-1 database
// that already has data, as a user's database would be.
func TestMigrationKeepsExistingRepositorySnapshots(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := Migrations()
	if err != nil || len(ms) < 2 {
		t.Fatalf("migrations: %v, %v", len(ms), err)
	}
	if _, err := migrate(ctx, raw, ms[:1]); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO projects VALUES ('prj_old', 'old', '/repos/old', 1, 1)`,
		`INSERT INTO git_repositories (project_id, root_path, current_branch, head_commit, default_branch, remotes, inspected_at)
		 VALUES ('prj_old', '/repos/old', 'main', 'abc', 'main', '[]', 1)`,
	} {
		if _, err := raw.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	_ = raw.Close()

	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.View(ctx, func(tx store.Tx) error {
		g, err := tx.Repositories().Get(ctx, "prj_old")
		if err != nil {
			return err
		}
		if g.HeadCommit != "abc" || g.CurrentBranch != "main" || g.CommonDir != "" {
			t.Errorf("snapshot after migration = %+v", g)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

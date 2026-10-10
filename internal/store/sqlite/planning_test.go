package sqlite

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/planning"
	"devboard/internal/store"
)

func TestPlanningUpgradeKeepsEveryExistingTaskAsItWas(t *testing.T) {
	// A database from before this migration, with a project and a task in it.
	path := oldDatabase(t, 12)
	old, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.ExecContext(ctx, `INSERT INTO tasks (id, project_id, title, description, state, position, created_at, updated_at) VALUES ('tsk_1', 'prj_1', 'old task', '', 'doing', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.View(ctx, func(tx store.Tx) error {
		p, err := tx.Projects().Get(ctx, "prj_1")
		if err != nil {
			return err
		}
		if p.Kind != domain.ProjectRepository || p.RepoPath != "/repos/keep" || !p.HasRepository() {
			t.Errorf("an existing project must stay a repository project: %+v", p)
		}
		task, err := tx.Tasks().Get(ctx, "tsk_1")
		if err != nil {
			return err
		}
		if task.WorkMode != planning.ModeAgent || task.Mode() != planning.ModeAgent || len(task.LabelIDs) != 0 || !task.Plan.IsZero() || task.State != domain.TaskDoing {
			t.Errorf("an existing task must stay agent work with no labels or plan: %+v", task)
		}
		if task.LabelIDs == nil {
			t.Error("a task with no labels has an empty list, not null")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func openRaw(path string) (*sql.DB, error) {
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(1)
	return raw, nil
}

func newLabel(id, name string) *domain.Label {
	return &domain.Label{ID: id, Name: name, Color: "#336699", CreatedAt: time.UnixMilli(1000), UpdatedAt: time.UnixMilli(1000)}
}

func seedTask(t *testing.T, tx store.Tx, projectID, id string) *domain.Task {
	t.Helper()
	task := &domain.Task{ID: id, ProjectID: projectID, Title: id, State: domain.TaskBacklog, Position: 1, CreatedAt: time.UnixMilli(1), UpdatedAt: time.UnixMilli(1), LabelIDs: []string{}}
	if err := tx.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestLabelsAreUniqueByNameIgnoringCaseAndSpacing(t *testing.T) {
	db, _ := openTemp(t)
	err := db.Update(ctx, func(tx store.Tx) error {
		if err := tx.Labels().Create(ctx, newLabel("lbl_1", "Design")); err != nil {
			return err
		}
		for _, name := range []string{"design", "  DESIGN ", "Design"} {
			if err := tx.Labels().Create(ctx, newLabel("lbl_x", name)); !errors.Is(err, domain.ErrDuplicate) {
				t.Errorf("%q: err = %v, want a duplicate", name, err)
			}
		}
		other := newLabel("lbl_2", "Marketing")
		if err := tx.Labels().Create(ctx, other); err != nil {
			return err
		}
		other.Name = "design"
		if err := tx.Labels().Update(ctx, other); !errors.Is(err, domain.ErrDuplicate) {
			t.Errorf("renaming onto another label: err = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLabelUpdateIsCompareAndSwap(t *testing.T) {
	db, _ := openTemp(t)
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Labels().Create(ctx, newLabel("lbl_1", "Ops")) }); err != nil {
		t.Fatal(err)
	}
	err := db.Update(ctx, func(tx store.Tx) error {
		l, _ := tx.Labels().Get(ctx, "lbl_1")
		stale := *l
		l.Color = "#ff0000"
		if err := tx.Labels().Update(ctx, l); err != nil || l.Version != 2 {
			t.Fatalf("update: %v, version %d", err, l.Version)
		}
		stale.Color = "#00ff00"
		if err := tx.Labels().Update(ctx, &stale); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("stale update: err = %v", err)
		}
		if err := tx.Labels().Update(ctx, &domain.Label{ID: "lbl_none", Name: "x", Color: "#000000", Version: 1}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("missing: err = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTasksCarryLabelsInOrderAndLoseThemWhenALabelIsDeleted(t *testing.T) {
	db, _ := openTemp(t)
	err := db.Update(ctx, func(tx store.Tx) error {
		if err := tx.Projects().Create(ctx, &domain.Project{ID: "prj_1", Name: "p", RepoPath: "/repos/p", CreatedAt: time.UnixMilli(1), UpdatedAt: time.UnixMilli(1)}); err != nil {
			return err
		}
		for _, l := range []*domain.Label{newLabel("lbl_b", "Bravo"), newLabel("lbl_a", "Alpha"), newLabel("lbl_c", "Charlie")} {
			if err := tx.Labels().Create(ctx, l); err != nil {
				return err
			}
		}
		task := seedTask(t, tx, "prj_1", "tsk_1")
		task.LabelIDs = []string{"lbl_c", "lbl_a", "lbl_b"} // deliberately not alphabetical
		if err := tx.Tasks().Update(ctx, task); err != nil {
			return err
		}
		got, err := tx.Tasks().Get(ctx, "tsk_1")
		if err != nil {
			return err
		}
		if strings.Join(got.LabelIDs, ",") != "lbl_c,lbl_a,lbl_b" {
			t.Errorf("labels = %v, want the order they were put on", got.LabelIDs)
		}
		// The listing carries them too, in one pass.
		list, _ := tx.Tasks().ListByProject(ctx, "prj_1")
		if len(list) != 1 || strings.Join(list[0].LabelIDs, ",") != "lbl_c,lbl_a,lbl_b" {
			t.Errorf("list = %+v", list)
		}
		// A label that does not exist cannot be put on a task.
		got.LabelIDs = []string{"lbl_missing"}
		if err := tx.Tasks().Update(ctx, got); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("unknown label: err = %v", err)
		}
		return errRollbackOnly
	})
	if !errors.Is(err, errRollbackOnly) {
		t.Fatal(err)
	}

	if err := db.Update(ctx, func(tx store.Tx) error {
		if err := tx.Projects().Create(ctx, &domain.Project{ID: "prj_1", Name: "p", RepoPath: "/repos/p", CreatedAt: time.UnixMilli(1), UpdatedAt: time.UnixMilli(1)}); err != nil {
			return err
		}
		for _, l := range []*domain.Label{newLabel("lbl_a", "Alpha"), newLabel("lbl_b", "Bravo")} {
			if err := tx.Labels().Create(ctx, l); err != nil {
				return err
			}
		}
		one, two := seedTask(t, tx, "prj_1", "tsk_1"), seedTask(t, tx, "prj_1", "tsk_2")
		one.LabelIDs, two.LabelIDs = []string{"lbl_a", "lbl_b"}, []string{"lbl_a"}
		if err := tx.Tasks().Update(ctx, one); err != nil {
			return err
		}
		if err := tx.Tasks().Update(ctx, two); err != nil {
			return err
		}
		// Archived tasks do not count as use.
		now := time.UnixMilli(5)
		two, _ = tx.Tasks().Get(ctx, "tsk_2")
		two.ArchivedAt = &now
		if err := tx.Tasks().Update(ctx, two); err != nil {
			return err
		}
		use, err := tx.Labels().Usage(ctx)
		if err != nil || use["lbl_a"] != 1 || use["lbl_b"] != 1 {
			t.Errorf("usage = %v, %v", use, err)
		}
		ids, _ := tx.Labels().TaskIDs(ctx, "lbl_a")
		if strings.Join(ids, ",") != "tsk_1,tsk_2" {
			t.Errorf("task ids = %v (archived tasks carry labels too)", ids)
		}
		if err := tx.Labels().Delete(ctx, "lbl_a"); err != nil {
			return err
		}
		got, _ := tx.Tasks().Get(ctx, "tsk_1")
		if strings.Join(got.LabelIDs, ",") != "lbl_b" {
			t.Errorf("after deleting a label the task keeps the others: %v", got.LabelIDs)
		}
		if err := tx.Labels().Delete(ctx, "lbl_a"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("deleting twice: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

var errRollbackOnly = errors.New("roll back")

func TestWorkProjectsNeedNoRepository(t *testing.T) {
	db, _ := openTemp(t)
	err := db.Update(ctx, func(tx store.Tx) error {
		now := time.UnixMilli(1)
		a := &domain.Project{ID: "prj_a", Name: "Marketing", Kind: domain.ProjectWork, CreatedAt: now, UpdatedAt: now}
		b := &domain.Project{ID: "prj_b", Name: "Ops", Kind: domain.ProjectWork, CreatedAt: now, UpdatedAt: now}
		for _, p := range []*domain.Project{a, b} { // two of them: the placeholder path is unique per project
			if err := tx.Projects().Create(ctx, p); err != nil {
				return err
			}
		}
		got, err := tx.Projects().Get(ctx, "prj_a")
		if err != nil {
			return err
		}
		if got.Kind != domain.ProjectWork || got.RepoPath != "" || got.HasRepository() {
			t.Errorf("a work project reads back with no path: %+v", got)
		}
		all, _ := tx.Projects().List(ctx)
		for _, p := range all {
			if p.RepoPath != "" {
				t.Errorf("the placeholder leaked into %+v", p)
			}
		}
		if err := tx.Projects().Create(ctx, &domain.Project{ID: "prj_c", Name: "x", Kind: domain.ProjectWork, RepoPath: "/repos/x", CreatedAt: now, UpdatedAt: now}); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("a work project with a path: err = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseKeepsProjectKindsApartWhoeverWrites(t *testing.T) {
	db, _ := openTemp(t)
	exec := func(q string, args ...any) error {
		return db.Update(ctx, func(tx store.Tx) error {
			_, err := tx.(*txn).q.ExecContext(ctx, q, args...)
			return err
		})
	}
	// A repository project may not borrow the placeholder, and a work project may not claim a real path.
	if err := exec(`INSERT INTO projects (id, name, repo_path, kind, created_at, updated_at) VALUES ('p1', 'x', 'work:p1', 'repository', 1, 1)`); err == nil {
		t.Error("a repository project with the placeholder path was accepted")
	}
	if err := exec(`INSERT INTO projects (id, name, repo_path, kind, created_at, updated_at) VALUES ('p2', 'x', '/repos/p2', 'work', 1, 1)`); err == nil {
		t.Error("a work project with a real path was accepted")
	}
	if err := exec(`INSERT INTO projects (id, name, repo_path, kind, created_at, updated_at) VALUES ('p3', 'x', 'work:p3', 'work', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := exec(`UPDATE projects SET kind = 'repository' WHERE id = 'p3'`); err == nil {
		t.Error("a project changed kind")
	}
	if err := exec(`INSERT INTO projects (id, name, repo_path, kind, created_at, updated_at) VALUES ('p4', 'x', '/repos/p4', 'folder', 1, 1)`); err == nil {
		t.Error("an unknown kind was accepted")
	}
	// Likewise a label colour and a work mode.
	if err := exec(`INSERT INTO labels (id, name, name_key, color, created_at, updated_at) VALUES ('l1', 'x', 'x', 'red', 1, 1)`); err == nil {
		t.Error("a label colour that is not #rrggbb was accepted")
	}
	if err := exec(`INSERT INTO tasks (id, project_id, title, state, position, created_at, updated_at, work_mode) VALUES ('t1', 'p3', 't', 'backlog', 1, 1, 1, 'robot')`); err == nil {
		t.Error("an unknown work mode was accepted")
	}
}

func TestTaskPlanAndModeRoundTrip(t *testing.T) {
	db, _ := openTemp(t)
	if err := db.Update(ctx, func(tx store.Tx) error {
		if err := tx.Projects().Create(ctx, &domain.Project{ID: "prj_1", Name: "w", Kind: domain.ProjectWork, CreatedAt: time.UnixMilli(1), UpdatedAt: time.UnixMilli(1)}); err != nil {
			return err
		}
		task := seedTask(t, tx, "prj_1", "tsk_1")
		task.WorkMode = planning.ModeHybrid
		task.Plan = domain.Plan{Start: "2026-10-05", End: "2026-10-09"}
		if err := tx.Tasks().Update(ctx, task); err != nil {
			return err
		}
		ms := seedTask(t, tx, "prj_1", "tsk_2")
		ms.Plan = domain.Plan{Start: "2026-10-12", Milestone: true}
		if err := tx.Tasks().Update(ctx, ms); err != nil {
			return err
		}
		got, _ := tx.Tasks().Get(ctx, "tsk_1")
		if got.WorkMode != planning.ModeHybrid || got.Plan != (domain.Plan{Start: "2026-10-05", End: "2026-10-09"}) {
			t.Errorf("got %+v", got)
		}
		got, _ = tx.Tasks().Get(ctx, "tsk_2")
		if !got.Plan.Milestone || got.Plan.Start != "2026-10-12" || got.WorkMode != planning.ModeAgent {
			t.Errorf("milestone: %+v", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

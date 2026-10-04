package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
	"devboard/internal/store/sqlite"
)

// fakeGit treats any path under /repos/ as a repository rooted at that path.
type fakeGit struct{}

func (fakeGit) Inspect(_ context.Context, path string) (*domain.GitRepository, error) {
	if filepath.Dir(path) != "/repos" {
		return nil, gitrepo.ErrNotRepository
	}
	return &domain.GitRepository{RootPath: path, CurrentBranch: "main", Remotes: []domain.GitRemote{}, InspectedAt: time.Now().UTC()}, nil
}

type fixture struct {
	deps     Deps
	bus      *events.Broker
	projects *Projects
	tasks    *Tasks
	runs     *Runs
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.NewBroker()
	deps := Deps{Store: db, Bus: bus}
	return &fixture{deps: deps, bus: bus, projects: &Projects{Deps: deps, Git: fakeGit{}}, tasks: &Tasks{deps}, runs: &Runs{deps}}
}

func TestRegisterProject(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sub := f.bus.Subscribe(10)

	p, err := f.projects.Register(ctx, "/repos/api", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "api" || p.RepoPath != "/repos/api" || p.Repository == nil || p.Repository.ProjectID != p.ID {
		t.Fatalf("unexpected project: %+v", p)
	}
	if ev := <-sub.C; ev.Type != domain.EventProjectRegistered || ev.ProjectID != p.ID || ev.Seq == 0 {
		t.Fatalf("unexpected event: %+v", ev)
	}

	if _, err := f.projects.Register(ctx, "/repos/api", "again"); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("duplicate: err = %v", err)
	}
	if _, err := f.projects.Register(ctx, "/elsewhere/x", ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("non-repo: err = %v", err)
	}
	list, err := f.projects.List(ctx)
	if err != nil || len(list) != 1 || list[0].Repository == nil {
		t.Fatalf("List = %+v, %v", list, err)
	}
}

func TestTaskLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/web", "Web")
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.tasks.Create(ctx, p.ID, "First", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.tasks.Create(ctx, p.ID, "Second", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.State != domain.TaskBacklog || b.Position <= a.Position {
		t.Fatalf("new tasks should append to Backlog: a=%+v b=%+v", a, b)
	}

	doing := domain.TaskDoing
	moved, err := f.tasks.Update(ctx, a.ID, TaskPatch{State: &doing, Version: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	if moved.State != domain.TaskDoing || moved.Version != a.Version+1 {
		t.Fatalf("moved = %+v", moved)
	}

	title := "stale write"
	if _, err := f.tasks.Update(ctx, a.ID, TaskPatch{Title: &title, Version: a.Version}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale update: err = %v", err)
	}
	bogus := domain.TaskState("in_progress")
	if _, err := f.tasks.Update(ctx, a.ID, TaskPatch{State: &bogus, Version: moved.Version}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("bad state: err = %v", err)
	}
	if _, err := f.tasks.Create(ctx, "prj_missing", "x", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing project: err = %v", err)
	}
	if _, err := f.tasks.Create(ctx, p.ID, "  ", ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank title: err = %v", err)
	}
}

func TestRecoverAfterRestart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.projects.Register(ctx, "/repos/svc", "")
	task, _ := f.tasks.Create(ctx, p.ID, "t", "")

	now := time.Now().UTC().Truncate(time.Millisecond)
	mk := func(state domain.RunState) *domain.Run {
		return &domain.Run{ID: domain.NewID(domain.PrefixRun), TaskID: task.ID, ProjectID: p.ID, AgentID: "x", State: state, CreatedAt: now, UpdatedAt: now}
	}
	running, waiting, done := mk(domain.RunRunning), mk(domain.RunWaitingForUser), mk(domain.RunCompleted)
	err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		return errors.Join(tx.Runs().Create(ctx, running), tx.Runs().Create(ctx, waiting), tx.Runs().Create(ctx, done))
	})
	if err != nil {
		t.Fatal(err)
	}

	n, err := f.runs.RecoverAfterRestart(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recovered %d, err %v; want 1", n, err)
	}
	active, _ := f.runs.ListActive(ctx)
	if len(active) != 1 || active[0].ID != waiting.ID {
		t.Fatalf("active after recovery = %+v", active)
	}
	_ = f.deps.Store.View(ctx, func(tx store.Tx) error {
		r, _ := tx.Runs().Get(ctx, running.ID)
		if r.State != domain.RunFailed || r.EndedAt == nil || r.Reason != interruptedReason {
			t.Errorf("interrupted run = %+v", r)
		}
		return nil
	})
}

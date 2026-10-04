package runner

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/fake"
	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
	"devboard/internal/store"
	"devboard/internal/store/sqlite"
)

var ctx = context.Background()

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// newRepo makes a repository with one commit, canonical path.
func newRepo(t *testing.T, commits bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	git(t, dir, "init", "-q", "-b", "main")
	if commits {
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-q", "-m", "init")
	}
	return dir
}

// env is everything a runner test needs, over a real database, a real Git
// repository and a fake agent.
type env struct {
	t         *testing.T
	dbPath    string
	db        *sqlite.DB
	bus       *events.Broker
	projects  *service.Projects
	settings  *service.Settings
	tasks     *service.Tasks
	runs      *service.Runs
	worktrees *service.Worktrees
	agents    *agent.Registry
	adapter   *fake.Adapter
	git       gitrepo.Worktrees
	repo      string
	root      string
	project   *service.ProjectDetail
	mgr       *Manager
	sub       *events.Subscription
	evMu      sync.Mutex
	seen      []domain.Event
}

type envOpt func(*env, *Options)

func newEnv(t *testing.T, opts ...envOpt) *env {
	t.Helper()
	e := &env{t: t, repo: newRepo(t, true)}
	e.root, _ = filepath.EvalSymlinks(t.TempDir())
	e.dbPath = filepath.Join(t.TempDir(), "devboard.db")
	e.open()
	e.adapter = &fake.Adapter{}
	e.agents = agent.NewRegistry()
	if err := e.agents.Register(e.adapter); err != nil {
		t.Fatal(err)
	}
	e.projects.Catalog, e.settings.Catalog, e.tasks.Catalog = e.agents, e.agents, e.agents
	e.git = &gitrepo.CLI{}
	p, err := e.projects.Register(ctx, e.repo, "demo")
	if err != nil {
		t.Fatal(err)
	}
	e.project = p
	for _, o := range opts {
		o(e, nil)
	}
	e.mgr = e.newManager(opts...)
	return e
}

// open (re)opens the database and the services over it, as a restart does.
func (e *env) open() {
	e.t.Helper()
	db, err := sqlite.Open(ctx, e.dbPath, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = db.Close() })
	e.db = db
	e.bus = events.NewBroker()
	deps := service.Deps{Store: db, Bus: e.bus}
	var cat service.AgentCatalog
	if e.agents != nil {
		cat = e.agents // after a restart; the first time the registry does not exist yet
	}
	e.projects = &service.Projects{Deps: deps, Git: &gitrepo.CLI{}, Catalog: cat}
	e.settings = &service.Settings{Deps: deps, Catalog: cat}
	e.tasks = &service.Tasks{Deps: deps, Catalog: cat}
	e.runs = &service.Runs{Deps: deps}
	e.worktrees = &service.Worktrees{Deps: deps, Root: e.root}
	e.sub = e.bus.Subscribe(100000)
}

func (e *env) newManager(opts ...envOpt) *Manager {
	o := Options{
		Runs: e.runs, Tasks: e.tasks, Projects: e.projects, Settings: e.settings, Worktrees: e.worktrees, Git: e.git, Agents: e.agents,
		WorktreeRoot: e.root, FlushInterval: 10 * time.Millisecond, SetupTimeout: 30 * time.Second,
		StopTimeout: 5 * time.Second, FinishTimeout: 2 * time.Second, ReapGrace: time.Second,
	}
	for _, f := range opts {
		f(nil, &o)
	}
	m := New(o)
	e.t.Cleanup(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_ = m.Shutdown(c)
	})
	return m
}

func withOptions(f func(*Options)) envOpt {
	return func(_ *env, o *Options) {
		if o != nil {
			f(o)
		}
	}
}

func (e *env) task(title string) *domain.Task {
	e.t.Helper()
	tk, err := e.tasks.Create(ctx, e.project.ID, title, "Do the thing carefully.")
	if err != nil {
		e.t.Fatal(err)
	}
	return tk
}

func (e *env) start(task *domain.Task) *domain.Run {
	e.t.Helper()
	r, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"})
	if err != nil {
		e.t.Fatalf("Start: %v", err)
	}
	return r
}

func (e *env) run(id string) *domain.Run {
	e.t.Helper()
	r, err := e.runs.Get(ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *env) taskState(id string) domain.TaskState {
	e.t.Helper()
	tk, err := e.tasks.Get(ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return tk.State
}

// session is the fake agent session of the most recent Start.
func (e *env) session() *fake.Session {
	e.t.Helper()
	s := e.adapter.Last()
	if s == nil {
		e.t.Fatal("no agent session was started")
	}
	return s
}

// eventually polls until cond holds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (e *env) waitState(runID string, state domain.RunState) *domain.Run {
	e.t.Helper()
	eventually(e.t, "run to be "+string(state), func() bool { return e.run(runID).State == state })
	return e.run(runID)
}

func (e *env) waitWaiting(runID string, kind domain.WaitingKind) *domain.Run {
	e.t.Helper()
	eventually(e.t, "run to wait for "+string(kind), func() bool {
		r := e.run(runID)
		return r.State == domain.RunWaitingForUser && r.Waiting == kind
	})
	return e.run(runID)
}

// log returns the persisted events of a run, oldest first.
func (e *env) log(runID string) []domain.Event {
	e.t.Helper()
	var out []domain.Event
	if err := e.db.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Events().ListByRun(ctx, runID, 0, 100000)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func logTypes(evs []domain.Event) []domain.EventType {
	out := make([]domain.EventType, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

// agentTypes is the run's agent.* events only: the activity timeline.
func (e *env) agentTypes(runID string) []domain.EventType {
	var out []domain.EventType
	for _, ev := range e.log(runID) {
		switch ev.Type {
		case domain.EventAgentStarted, domain.EventAgentQuestion, domain.EventAgentWaiting, domain.EventAgentResumed,
			domain.EventAgentCompleted, domain.EventAgentFailed, domain.EventAgentStopped:
			out = append(out, ev.Type)
		}
	}
	return out
}

func (e *env) outputs(runID string) []domain.AgentOutput {
	var out []domain.AgentOutput
	for _, ev := range e.log(runID) {
		if ev.Type == domain.EventAgentOutput {
			var o domain.AgentOutput
			if err := json.Unmarshal(ev.Payload, &o); err != nil {
				e.t.Fatal(err)
			}
			out = append(out, o)
		}
	}
	return out
}

func sameTypes(a, b []domain.EventType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (e *env) worktreeOf(run *domain.Run) *domain.Worktree {
	e.t.Helper()
	w, err := e.worktrees.Get(ctx, run.WorktreeID)
	if err != nil {
		e.t.Fatal(err)
	}
	return w
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func service_patch(state *domain.TaskState, version int64) service.TaskPatch {
	return service.TaskPatch{State: state, Version: version}
}

type fakeSession = fake.Session

func removeAll(path string) error { return os.RemoveAll(path) }

type storeTx = store.Tx

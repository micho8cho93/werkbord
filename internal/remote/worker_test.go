package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/fake"
	"devboard/internal/api"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/runnerwire"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
)

var testCtx = context.Background()

type workerEnv struct {
	t       *testing.T
	worker  *Worker
	db      *sqlite.DB
	svc     *service.Runners
	runs    *service.Runs
	tasks   *service.Tasks
	project *service.ProjectDetail
	runner  *domain.Runner
	fake    *fake.Adapter
	clock   atomic.Int64
	handler atomic.Value
	server  *httptest.Server
}

func workerFixture(t *testing.T) *workerEnv {
	t.Helper()
	env := &workerEnv{t: t}
	env.clock.Store(time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC).UnixMilli())
	now := func() time.Time { return time.UnixMilli(env.clock.Load()).UTC() }
	db, e := sqlite.Open(testCtx, filepath.Join(t.TempDir(), "controller.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	env.db = db
	t.Cleanup(func() { db.Close() })
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "test@example.test")
	runGit(t, root, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(root, "test.txt"), []byte("base"), 0600)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	deps := service.Deps{Store: db, Now: now}
	projects := &service.Projects{Deps: deps, Git: &gitrepo.CLI{}}
	env.project, e = projects.Register(testCtx, root, "Project")
	if e != nil {
		t.Fatal(e)
	}
	env.tasks = &service.Tasks{Deps: deps}
	env.runs = &service.Runs{Deps: deps}
	env.svc = &service.Runners{Deps: deps, Runs: env.runs}
	env.handler.Store(api.New(api.Options{Store: db, Runs: env.runs, Projects: projects, Tasks: env.tasks, Distributed: env.svc, AuthRequired: true, Token: "owner-only"}).Handler())
	env.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { env.handler.Load().(http.Handler).ServeHTTP(w, r) }))
	t.Cleanup(env.server.Close)
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	pairing, e := env.svc.Pair(testCtx, env.server.URL, []string{env.project.ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	_, secret, _ := runnerwire.DecodeCode(pairing.Code)
	env.runner, e = env.svc.Join(testCtx, runnerwire.Join{Name: "Simulated runner", PublicKey: base64.RawURLEncoding.EncodeToString(pub), Secret: secret})
	if e != nil {
		t.Fatal(e)
	}
	env.runner.Automatic = true
	env.runner.Capacity = 2
	env.runner, e = env.svc.Manage(testCtx, env.runner.ID, *env.runner, false)
	if e != nil {
		t.Fatal(e)
	}
	registry := agent.NewRegistry()
	env.fake = &fake.Adapter{Name: "codex"}
	registry.Register(env.fake)
	env.worker = &Worker{Dir: t.TempDir(), Identity: Identity{Controller: env.server.URL, RunnerID: env.runner.ID, PrivateKey: key}, Agents: registry, Client: env.server.Client(), Now: now, Bindings: map[string]string{env.project.ID: root}, Capabilities: func(context.Context) domain.RunnerCapabilities {
		return domain.RunnerCapabilities{CPU: 4, Agents: registry.Detect(testCtx)}
	}}
	env.worker.Prepare = func(ctx context.Context, j runnerwire.Job) (string, string, string, error) {
		return root, "devboard/" + j.Run.RunnerID + "/" + j.Run.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	}
	if e := env.worker.Load(testCtx); e != nil {
		t.Fatal(e)
	}
	if e := env.worker.Tick(testCtx); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		env.worker.mu.Lock()
		for _, s := range env.worker.sessions {
			go s.Stop(testCtx)
		}
		env.worker.mu.Unlock()
		env.worker.wg.Wait()
	})
	return env
}
func (e *workerEnv) start() *domain.Run {
	e.t.Helper()
	task, err := e.tasks.Create(testCtx, e.project.ID, "Task", "Work")
	if err != nil {
		e.t.Fatal(err)
	}
	selected, res, err := e.svc.Route(testCtx, task, domain.Resolved{Runner: e.runner.ID, Agent: "codex"})
	if err != nil {
		e.t.Fatal(err)
	}
	run, err := e.runs.Create(testCtx, service.NewRun{TaskID: task.ID, AgentID: res.Agent, RunnerID: e.runner.ID, Remote: true, Prompt: "work", Claim: e.svc.Claim(selected, &runnerwire.Job{TargetBranch: "main"})})
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.worker.Tick(testCtx); err != nil {
		e.t.Fatal(err)
	}
	waitFor(e.t, func() bool {
		e.worker.mu.Lock()
		defer e.worker.mu.Unlock()
		return e.worker.records[run.ID].Phase == "active"
	})
	if err := e.worker.Tick(testCtx); err != nil {
		e.t.Fatal(err)
	}
	return run
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("timed out waiting for simulated runner")
}
func waitPending(t *testing.T, w *Worker, id string, kind string) {
	waitFor(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, ob := range w.records[id].Pending {
			if ob.Kind == kind {
				return true
			}
		}
		return false
	})
}

func TestWorkerReconnectAndControllerRestartNeverLaunchDuplicate(t *testing.T) {
	e := workerFixture(t)
	run := e.start()
	for i := 0; i < 3; i++ {
		if err := e.worker.Tick(testCtx); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.fake.Sessions()) != 1 {
		t.Fatal("job launched more than once")
	}
	// Simulate controller process recovery and replace its handler using the same SQLite.
	if n, err := e.runs.RecoverAfterRestart(testCtx); err != nil || n != 0 {
		t.Fatalf("recovery %d %v", n, err)
	}
	restarted := &service.Runners{Deps: e.svc.Deps, Runs: e.runs}
	e.handler.Store(api.New(api.Options{Store: e.db, Distributed: restarted, AuthRequired: true, Token: "new-owner-key"}).Handler())
	e.fake.Last().Assistant("after reconnect")
	waitPending(t, e.worker, run.ID, "event")
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Sessions()) != 1 {
		t.Fatal("restart duplicated execution")
	}
	e.fake.Last().Exit(0, "")
	waitPending(t, e.worker, run.ID, "ended")
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	got, err := e.runs.Get(testCtx, run.ID)
	if err != nil || got.State != domain.RunCompleted {
		t.Fatalf("reconciled %+v %v", got, err)
	}
	// Ended journal tombstones protect against a controller replaying the old job.
	reloaded := &Worker{Dir: e.worker.Dir, Identity: e.worker.Identity}
	if err := reloaded.Load(testCtx); err != nil {
		t.Fatal(err)
	}
	if reloaded.records[run.ID].Phase != "ended" {
		t.Fatal("terminal ownership not durable")
	}
}
func TestWorkerLeaseExpiryStopsProcessAndReportsWhenNetworkReturns(t *testing.T) {
	e := workerFixture(t)
	run := e.start()
	e.clock.Add(int64((runnerwire.Lease + time.Second) / time.Millisecond))
	e.handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	if err := e.worker.Tick(testCtx); err == nil {
		t.Fatal("network failure was hidden")
	}
	waitFor(t, func() bool { return e.fake.Last().StopRequested() })
	waitPending(t, e.worker, run.ID, "ended")
	// No terminal state reaches the controller during the partition.
	got, _ := e.runs.Get(testCtx, run.ID)
	if got.State.Terminal() {
		t.Fatal("controller released unknown ownership")
	}
	e.handler.Store(api.New(api.Options{Store: e.db, Distributed: e.svc, AuthRequired: true, Token: "owner"}).Handler())
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	got, _ = e.runs.Get(testCtx, run.ID)
	if got.State != domain.RunStopped || !strings.Contains(got.Reason, "lease expired") {
		t.Fatalf("lease reconciliation: %+v", got)
	}
}
func TestWorkerCommandsAreAcknowledgedAndNotRepeated(t *testing.T) {
	e := workerFixture(t)
	run := e.start()
	e.fake.Last().TurnEnd()
	waitPending(t, e.worker, run.ID, "event")
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Command(testCtx, run.ID, "send", "Next turn"); err != nil {
		t.Fatal(err)
	}
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	waitPending(t, e.worker, run.ID, "command")
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Last().Sent()) != 1 {
		t.Fatalf("send delivered %d times", len(e.fake.Last().Sent()))
	}
	got, _ := e.runs.Get(testCtx, run.ID)
	if got.State != domain.RunRunning {
		t.Fatalf("send did not resume: %s", got.State)
	}
	// Approvals are never granted by autonomous policy.
	e.fake.Last().RequestApproval("ref", "Execute a tool?")
	waitPending(t, e.worker, run.ID, "event")
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	qs, _ := e.runs.ListPendingQuestions(testCtx)
	if len(qs) != 1 {
		t.Fatal("approval unavailable")
	}
	if _, err := e.svc.Answer(testCtx, qs[0].ID, "Allow"); err != nil {
		t.Fatal(err)
	}
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	waitPending(t, e.worker, run.ID, "command")
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Last().Responses()) != 1 {
		t.Fatal("approval duplicated")
	}
	if _, err := e.svc.Command(testCtx, run.ID, "stop", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	waitPending(t, e.worker, run.ID, "ended")
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	got, _ = e.runs.Get(testCtx, run.ID)
	if got.State != domain.RunStopped {
		t.Fatalf("stop state: %s", got.State)
	}
}
func TestRunnerKeyHasNoOwnerAPIPrivileges(t *testing.T) {
	e := workerFixture(t)
	for _, path := range []string{"/api/projects", "/api/runners", "/api/settings", "/api/control-center"} {
		req, _ := http.NewRequest("GET", e.server.URL+path, nil)
		req.Header.Set("X-Runner-Signature", runnerwire.Signature(ed25519.PrivateKey(e.worker.Identity.PrivateKey), nil))
		res, err := e.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Fatalf("runner can access %s: %d", path, res.StatusCode)
		}
	}
	// A signed key can't use the owner token slot either.
	req, _ := http.NewRequest("PUT", e.server.URL+"/api/runners/"+e.runner.ID, strings.NewReader(`{"name":"forged","capacity":128}`))
	req.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(e.worker.Identity.PrivateKey))
	res, err := e.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("runner became admin")
	}
}
func TestRunnerCrashJournalNeverRelaunchesUncertainJob(t *testing.T) {
	e := workerFixture(t)
	id := domain.NewID(domain.PrefixRun)
	record := &Record{Job: runnerwire.Job{Run: domain.Run{ID: id, RunnerID: e.runner.ID}}, Phase: "launching", Commands: map[string]bool{}}
	if err := AtomicJSON(e.worker.journalPath(id), record); err != nil {
		t.Fatal(err)
	}
	w := &Worker{Dir: e.worker.Dir, Identity: e.worker.Identity}
	if err := w.Load(testCtx); err != nil {
		t.Fatal(err)
	}
	if w.records[id].Phase != "uncertain" || len(w.records[id].Pending) != 0 {
		t.Fatal("ambiguous execution automatically released")
	}
	if err := w.Resolve(id); err != nil {
		t.Fatal(err)
	}
	if w.records[id].Phase != "ended" {
		t.Fatal("owner resolution not persisted")
	}
	raw, err := os.ReadFile(w.journalPath(id))
	if err != nil {
		t.Fatal(err)
	}
	var saved Record
	json.Unmarshal(raw, &saved)
	if saved.Phase != "ended" {
		t.Fatal("journal not durable")
	}
}

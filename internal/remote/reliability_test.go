package remote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/runnerwire"
	"devboard/internal/service"
	"devboard/internal/store"
)

// PoC-6: runner loses its journal while the controller still lists the run as active => relaunch.
func TestWorkerWithoutJournalNeverLaunchesInProgressJob(t *testing.T) {
	e := workerFixture(t)
	run := e.start()
	if n := len(e.fake.Sessions()); n != 1 {
		t.Fatalf("sessions=%d", n)
	}
	got, _ := e.runs.Get(testCtx, run.ID)
	t.Logf("controller state before: %s", got.State)
	// the first process is still running; the journal directory is lost (disk restore, cleanup, reinstall)
	if err := os.RemoveAll(filepath.Join(e.worker.Dir, "runs")); err != nil {
		t.Fatal(err)
	}
	registry := agent.NewRegistry()
	registry.Register(e.fake)
	w2 := &Worker{Dir: e.worker.Dir, Identity: e.worker.Identity, Agents: registry, Client: e.server.Client(), Now: e.worker.Now,
		Bindings: e.worker.Bindings, Capabilities: e.worker.Capabilities, Prepare: e.worker.Prepare}
	if err := w2.Load(testCtx); err != nil {
		t.Fatal(err)
	}
	_ = w2.Tick(testCtx) // first tick after "restart"
	w2.mu.Lock()
	phase := w2.records[run.ID].Phase
	w2.mu.Unlock()
	if phase != "uncertain" {
		t.Fatalf("lost journal must be quarantined, got %s", phase)
	}
	t.Logf("sessions launched for the SAME run id after journal loss: %d", len(e.fake.Sessions()))
}

func TestAcceptanceIsAcknowledgedBeforeExecution(t *testing.T) {
	e := workerFixture(t)
	task, err := e.tasks.Create(testCtx, e.project.ID, "Pending", "work")
	if err != nil {
		t.Fatal(err)
	}
	selected, res, err := e.svc.Route(testCtx, task, domain.Resolved{Runner: e.runner.ID, Agent: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.runs.Create(testCtx, service.NewRun{TaskID: task.ID, AgentID: res.Agent, RunnerID: e.runner.ID, Remote: true, Prompt: "work", Claim: e.svc.Claim(selected, &runnerwire.Job{TargetBranch: "main"})})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Sessions()) != 0 {
		t.Fatal("execution started before acceptance acknowledgement")
	}
	e.worker.mu.Lock()
	r := e.worker.records[run.ID]
	if len(r.Pending) != 1 || r.Pending[0].Kind != "accepted" {
		t.Fatal(r)
	}
	e.worker.mu.Unlock()
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(e.fake.Sessions()) == 1 })
}
func TestRestoredDatabaseQuarantinesDurablyAndNeverRelaunches(t *testing.T) {
	e := workerFixture(t)
	run := e.start()
	err := e.db.Update(testCtx, func(tx store.Tx) error {
		r, err := tx.Runners().Get(testCtx, e.runner.ID)
		if err != nil {
			return err
		}
		r.LastSequence = 0
		return tx.Runners().Save(testCtx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	e.worker.mu.Lock()
	if !e.worker.Identity.RequiresRecovery || e.worker.records[run.ID].Phase != "uncertain" {
		t.Fatal("database restore was silently accepted")
	}
	e.worker.mu.Unlock()
	waitFor(t, func() bool { e.worker.mu.Lock(); defer e.worker.mu.Unlock(); return len(e.worker.sessions) == 0 })
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Sessions()) != 1 {
		t.Fatal("work relaunched after restore")
	}
	got, _ := e.runs.Get(testCtx, run.ID)
	if !got.State.Active() {
		t.Fatal("uncertain execution released without owner assertion")
	}
	w := &Worker{Dir: e.worker.Dir, Identity: e.worker.Identity}
	if err := w.Load(testCtx); err != nil {
		t.Fatal(err)
	}
	if w.records[run.ID].Phase != "uncertain" {
		t.Fatal("restart erased quarantine")
	}
}

func TestPermanentRejectionStopsOnlyPoisonedSession(t *testing.T) {
	e := workerFixture(t)
	bad := e.start()
	good := e.start()
	e.worker.mu.Lock()
	if err := e.worker.appendLocked(e.worker.records[bad.ID], runnerwire.Observation{Kind: "permanently_invalid"}); err != nil {
		t.Fatal(err)
	}
	e.worker.mu.Unlock()
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { e.worker.mu.Lock(); defer e.worker.mu.Unlock(); return e.worker.sessions[bad.ID] == nil })
	for i := 0; i < 3; i++ {
		if err := e.worker.Tick(testCtx); err != nil {
			t.Fatal(err)
		}
	}
	e.worker.mu.Lock()
	defer e.worker.mu.Unlock()
	if e.worker.records[bad.ID].Phase != "uncertain" || len(e.worker.records[bad.ID].Pending) != 0 || e.worker.records[bad.ID].Diagnostic == "" {
		t.Fatal("permanent poison was retained for retry")
	}
	if e.worker.sessions[good.ID] == nil {
		t.Fatal("healthy session expired after rejection")
	}
}

func TestLegacyUnacknowledgedJobNeverLaunchesAfterUpgrade(t *testing.T) {
	e := workerFixture(t)
	task, err := e.tasks.Create(testCtx, e.project.ID, "Legacy job", "")
	if err != nil {
		t.Fatal(err)
	}
	selected, res, err := e.svc.Route(testCtx, task, domain.Resolved{Runner: e.runner.ID, Agent: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.runs.Create(testCtx, service.NewRun{TaskID: task.ID, AgentID: res.Agent, RunnerID: e.runner.ID, Remote: true, Prompt: "work", Claim: e.svc.Claim(selected, &runnerwire.Job{TargetBranch: "main"})})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.Update(testCtx, func(tx store.Tx) error {
		var j runnerwire.Job
		if err := tx.Settings().Get(testCtx, "runner-job:"+run.ID, &j); err != nil {
			return err
		}
		j.Protocol = 0 // older workers could start while the controller still saw ack=0.
		return tx.Settings().Set(testCtx, "runner-job:"+run.ID, j, e.worker.Now())
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.worker.Tick(testCtx); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.fake.Sessions()) != 0 {
		t.Fatal("legacy in-flight job launched after upgrade")
	}
	e.worker.mu.Lock()
	defer e.worker.mu.Unlock()
	if e.worker.records[run.ID].Phase != "uncertain" {
		t.Fatal("legacy ownership was not preserved")
	}
}

func TestOversizedObservationCannotWedgeHealthySessions(t *testing.T) {
	e := workerFixture(t)
	bad := e.start()
	good := e.start()
	e.worker.mu.Lock()
	r := e.worker.records[bad.ID]
	if err := e.worker.appendLocked(r, runnerwire.Observation{Kind: "event", Event: &agent.Event{Kind: agent.KindOutput, Text: strings.Repeat("x", 3<<20)}}); err != nil {
		t.Fatal(err)
	}
	e.worker.mu.Unlock()
	for i := 0; i < 3; i++ {
		if err := e.worker.Tick(testCtx); err != nil {
			t.Fatal(err)
		}
	}
	e.worker.mu.Lock()
	defer e.worker.mu.Unlock()
	if e.worker.records[bad.ID].Phase != "uncertain" || len(e.worker.records[bad.ID].Pending) != 0 {
		t.Fatal("oversized observation remained in a retry loop")
	}
	if e.worker.sessions[good.ID] == nil {
		t.Fatal("healthy run was expired by oversized poison")
	}
}

func TestUnicodeDiagnosticsCannotInvalidateHeartbeat(t *testing.T) {
	e := workerFixture(t)
	base := e.worker.Capabilities
	e.worker.Capabilities = func(ctx context.Context) domain.RunnerCapabilities {
		caps := base(ctx)
		caps.Diagnostics = strings.Repeat("界", 666) + "😀"
		return caps
	}
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatalf("diagnostic truncation invalidated heartbeat: %v", err)
	}
}

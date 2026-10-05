package runner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/runnerwire"
	"devboard/internal/service"
)

// PoC-2: an unreachable remote silently stalls every scheduled task, with one fetch per tick.
func TestScheduledStartDoesNotFetchOrRetryBlindly(t *testing.T) {
	var fetches atomic.Int64
	repoPath := ""
	e := newEnv(t, withOptions(func(o *Options) {
		o.RefreshRemotes = func(ctx context.Context, id string) error { // identical to controller.go:152-173
			fetches.Add(1)
			g := &gitrepo.CLI{}
			for _, name := range []string{"origin"} {
				result, err := g.Fetch(ctx, repoPath, name)
				_ = result
				if err != nil {
					return err
				}
				if result.Outcome != domain.OutcomeDone {
					return fmt.Errorf("%w: cannot refresh remote %s: %s", domain.ErrConflict, name, result.Message)
				}
			}
			return nil
		}
	}))
	repoPath = e.repo
	git(t, e.repo, "remote", "add", "origin", "https://127.0.0.1:1/x.git")
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.arm(e.task("nightly"), domain.Orchestration{})
	var last error
	start := time.Now()
	for i := 0; i < 5; i++ {
		last = e.mgr.ScheduleOnce(ctx)
	}
	t.Logf("5 ticks in %v, ScheduleOnce err=%v, fetch attempts=%d", time.Since(start), last, fetches.Load())
	plan, _ := e.mgr.opt.Scheduler.Plan(ctx, e.project.ID)
	t.Logf("plan decision shown to user: %+v", plan)
	tk, _ := e.tasks.Get(ctx, task.ID)
	t.Logf("task.Orchestration.Error=%q Missed=%v RunID=%q", tk.Orchestration.Error, tk.Orchestration.Missed, tk.Orchestration.RunID)
	rs, _ := e.runs.ListByTask(ctx, task.ID)
	if fetches.Load() > 0 || len(rs) != 1 {
		t.Fatalf("scheduled start fetched %d times and created %d runs", fetches.Load(), len(rs))
	}
}

// PoC-3: user (or a cleaner) deletes the task's worktree directory: task can never start again.
func TestDeletedWorktreeCanBeRecovered(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.task("feature")
	r1 := e.start(task)
	e.session().Close(ctx)
	e.waitState(r1.ID, domain.RunCompleted)
	wt, _ := e.worktrees.Get(ctx, e.run(r1.ID).WorktreeID)
	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatal(err)
	}
	t.Logf("removed worktree dir %s", filepath.Base(wt.Path))
	_, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"})
	if err != nil {
		t.Fatalf("deleted worktree cannot recover: %v", err)
	}
	task, _ = e.tasks.Get(ctx, task.ID)
	task = e.arm(task, domain.Orchestration{})
	for i := 0; i < 3; i++ {
		_ = e.mgr.ScheduleOnce(ctx)
	}
	plan, _ := e.mgr.opt.Scheduler.Plan(ctx, e.project.ID)
	t.Logf("plan: %+v", plan)
	_ = service.Scheduler{}
	_ = runnerwire.Lease
}

// PoC-4: untracked secret-bearing files are copied into the next agent's prompt.
func TestHandoffOmitsUntrackedSecrets(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.task("feature")
	r1 := e.start(task)
	wt, _ := e.worktrees.Get(ctx, e.run(r1.ID).WorktreeID)
	_ = os.WriteFile(filepath.Join(wt.Path, ".env.local"), []byte("STRIPE_SECRET_KEY=sk_live_PoC1234567890\n"), 0o600)
	e.session().Close(ctx)
	e.waitState(r1.ID, domain.RunCompleted)
	time.Sleep(300 * time.Millisecond)
	out, err := e.mgr.opt.Handoffs.Context(ctx, r1.ID, "review", "")
	if err != nil || strings.Contains(out, "sk_live_PoC1234567890") {
		t.Fatalf("handoff leaked secret or failed: %v", err)
	}
	if i := strings.Index(out, "sk_live"); i >= 0 {
		t.Logf("...%s...", out[max(0, i-80):min(len(out), i+40)])
	}
}

// PoC-7: cost of one Plan() (== one Control Center / schedule API read, and one scheduler tick) with many runnable tasks.
type countingSchedulerGit struct {
	gitrepo.Reader
	statuses int
}

func (g *countingSchedulerGit) Status(ctx context.Context, path string) (*domain.GitWorkingTree, error) {
	g.statuses++
	return g.Reader.Status(ctx, path)
}
func TestPlanQueueScaling(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	g := &countingSchedulerGit{Reader: &gitrepo.CLI{}}
	e.mgr.opt.Scheduler.Git = g
	for _, n := range []int{1, 10, 40, 100} {
		g.statuses = 0
		for i := len(mustList(e)); i < n; i++ {
			e.arm(e.task(fmt.Sprintf("queued-%d", i)), domain.Orchestration{})
		}
		start := time.Now()
		plan, err := e.mgr.opt.Scheduler.Plan(ctx, e.project.ID)
		if err != nil {
			t.Fatal(err)
		}
		runnable := 0
		for _, d := range plan {
			if d.State == "runnable" {
				runnable++
			}
		}
		t.Logf("tasks=%3d runnable=%3d Git inspections=%d Plan() took %v", n, runnable, g.statuses, time.Since(start).Round(time.Millisecond))
		if runnable != 1 || g.statuses != 1 {
			t.Fatalf("capacity filtering inspected %d tasks and allowed %d", g.statuses, runnable)
		}
	}
}

func mustList(e *env) []domain.Task {
	ts, _ := e.tasks.List(ctx, e.project.ID)
	return ts
}

// A metadata refresh during Git inspection must not replace a concurrent note.
type concurrentHandoffGit struct {
	gitrepo.Reader
	inspect func()
}

func (g concurrentHandoffGit) Status(ctx context.Context, path string) (*domain.GitWorkingTree, error) {
	g.inspect()
	return g.Reader.Status(ctx, path)
}
func TestHandoffGenerateRetainsConcurrentUserNotes(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	run := e.start(e.task("handoff race"))
	e.session().Close(ctx)
	e.waitState(run.ID, domain.RunCompleted)
	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	hCopy := *e.mgr.opt.Handoffs
	h := &hCopy
	captured := false
	h.Git = concurrentHandoffGit{Reader: &gitrepo.CLI{}, inspect: func() {
		if captured {
			return
		}
		captured = true
		latest, err := e.runs.Get(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		draft := *latest.Handoff
		draft.Summary = "user's concurrent note"
		draft.NextAction = "preserve me"
		if _, err = h.Save(ctx, run.ID, latest.Version, draft); err != nil {
			t.Fatal(err)
		}
	}}
	got, err := h.Generate(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Handoff.Summary != "user's concurrent note" || got.Handoff.NextAction != "preserve me" {
		t.Fatalf("lost note: %+v", got.Handoff)
	}
}
func TestImportedTaskStartsOnIntendedBaseAndBranch(t *testing.T) {
	e := newEnv(t)
	git(t, e.repo, "checkout", "-b", "release")
	os.WriteFile(filepath.Join(e.repo, "release.txt"), []byte("release base"), 0600)
	git(t, e.repo, "add", ".")
	git(t, e.repo, "commit", "-m", "release base")
	git(t, e.repo, "checkout", "main")
	task, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: e.project.ID, Title: "imported", SourceRef: "https://origin.test/1", WorkBranch: "ticket-1", BaseBranch: "release"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	e.orchestrate(&now)
	task = e.arm(task, domain.Orchestration{})
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	rs, err := e.runs.ListByTask(ctx, task.ID)
	if err != nil || len(rs) != 1 {
		t.Fatalf("intended base was rejected by scheduler: %v %v", rs, err)
	}
	run := &rs[0]
	wt, _ := e.worktrees.Get(ctx, e.run(run.ID).WorktreeID)
	if wt.Branch != "ticket-1" {
		t.Fatal(wt.Branch)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "release.txt")); err != nil {
		t.Fatal("intended base not used:", err)
	}
}

func configureSharedRunner(t *testing.T, e *env, projects []string) *service.Runners {
	t.Helper()
	d := &service.Runners{Deps: e.tasks.Deps, Runs: e.runs}
	for i := 0; i < 2; i++ {
		pair, err := d.Pair(ctx, "http://controller.test", projects, false)
		if err != nil {
			t.Fatal(err)
		}
		_, secret, _ := runnerwire.DecodeCode(pair.Code)
		pub, key, _ := ed25519.GenerateKey(rand.Reader)
		r, err := d.Join(ctx, runnerwire.Join{Secret: secret, PublicKey: base64.RawURLEncoding.EncodeToString(pub), Name: fmt.Sprintf("peer %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		r.Automatic = true
		r.Capacity = 1
		if _, err = d.Manage(ctx, r.ID, *r, false); err != nil {
			t.Fatal(err)
		}
		sync := runnerwire.Sync{Protocol: runnerwire.Protocol, RunnerID: r.ID, Sequence: 1, At: time.Now().UTC(), Capabilities: domain.RunnerCapabilities{Agents: []domain.Agent{{ID: "fake", Available: true}}, Repositories: projects}}
		b, _ := json.Marshal(sync)
		if _, err = d.Sync(ctx, b, runnerwire.Signature(key, b)); err != nil {
			t.Fatal(err)
		}
	}
	e.mgr.opt.Distributed = d
	e.mgr.opt.Scheduler.Runners = d
	return d
}
func TestPlanReservesSharedCapacityAcrossRunnerCandidates(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	configureSharedRunner(t, e, []string{e.project.ID})
	if err := e.mgr.opt.Scheduler.SetSettings(ctx, e.project.ID, service.OrchestrationSettings{ConcurrencyLimit: 4}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		e.arm(e.task(fmt.Sprint(i)), domain.Orchestration{ExpectedPaths: []string{fmt.Sprintf("file-%d", i)}})
	}
	g := &countingSchedulerGit{Reader: &gitrepo.CLI{}}
	e.mgr.opt.Scheduler.Git = g
	plan, err := e.mgr.opt.Scheduler.Plan(ctx, e.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range plan {
		if d.State == "runnable" {
			n++
		}
	}
	if n != 2 || g.statuses != 2 {
		t.Fatalf("two one-slot runners: runnable=%d Git inspections=%d plan=%+v", n, g.statuses, plan)
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := e.runs.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 || active[0].RunnerID == active[1].RunnerID {
		t.Fatalf("runner capacity not shared: %+v", active)
	}
}
func TestScheduleRotatesProjectsWhenCapacityIsShared(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	p, err := e.projects.Register(ctx, newRepo(t, true), "second")
	if err != nil {
		t.Fatal(err)
	}
	d := configureSharedRunner(t, e, []string{e.project.ID, p.ID})
	peers, err := d.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Restrict automatic execution to a single slot shared by both projects.
	for _, peer := range peers {
		if peer.Kind == domain.RunnerRemote {
			peer.Disabled = true
			if _, err := d.Manage(ctx, peer.ID, peer, false); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	for i := 0; i < 3; i++ {
		e.arm(e.task(fmt.Sprintf("first %d", i)), domain.Orchestration{})
		task, err := e.tasks.Create(ctx, p.ID, fmt.Sprintf("second %d", i), "")
		if err != nil {
			t.Fatal(err)
		}
		e.arm(task, domain.Orchestration{})
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := e.runs.ListActive(ctx)
	if err != nil || len(first) != 1 {
		t.Fatalf("first dispatch: %v %v", first, err)
	}
	if _, err = e.runs.End(ctx, first[0].ID, service.Ended{State: domain.RunStopped}); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := e.runs.ListActive(ctx)
	if err != nil || len(second) != 1 || second[0].ProjectID == first[0].ProjectID {
		t.Fatalf("project starved: first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestImportedBaseCanUseFetchedRemoteBranch(t *testing.T) {
	e := newEnv(t)
	git(t, e.repo, "checkout", "-b", "release")
	if err := os.WriteFile(filepath.Join(e.repo, "release.txt"), []byte("remote release base"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, e.repo, "add", ".")
	git(t, e.repo, "commit", "-m", "release base")
	git(t, e.repo, "update-ref", "refs/remotes/origin/release", "HEAD")
	git(t, e.repo, "checkout", "main")
	git(t, e.repo, "branch", "-D", "release")
	task, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: e.project.ID, Title: "imported remote base", WorkBranch: "ticket-remote-base", BaseBranch: "release"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	e.orchestrate(&now)
	run := e.start(task)
	wt, err := e.worktrees.Get(ctx, e.run(run.ID).WorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "release.txt")); err != nil {
		t.Fatal("fetched intended base was not used:", err)
	}
}

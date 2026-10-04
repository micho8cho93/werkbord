package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
)

func TestStartRunsTheAgentInAFreshWorktree(t *testing.T) {
	e := newEnv(t)
	task := e.task("Fix the login bug")

	run := e.start(task)
	if run.State != domain.RunRunning || run.AgentID != "fake" || run.PID == 0 {
		t.Fatalf("run = %+v", run)
	}
	if got := e.taskState(task.ID); got != domain.TaskDoing {
		t.Fatalf("the card is in %s; a running agent belongs in Doing", got)
	}
	if !e.mgr.IsLive(run.ID) || e.mgr.LiveCount() != 1 {
		t.Fatal("the session should be live")
	}

	// The agent got the task as its prompt, in a worktree of its own.
	s := e.session()
	if !strings.HasPrefix(s.Req.Prompt, "Fix the login bug\n\nDo the thing carefully.") {
		t.Fatalf("prompt = %q", s.Req.Prompt)
	}
	wt := e.worktreeOf(run)
	if s.Req.WorkDir != wt.Path || s.Req.RunID != run.ID || s.Req.ResumeRef != "" {
		t.Fatalf("start request = %+v, worktree %s", s.Req, wt.Path)
	}
	if !strings.HasPrefix(wt.Path, e.root+"/") || !exists(filepath.Join(wt.Path, "README.md")) {
		t.Fatalf("worktree %s is not a checkout inside the data directory", wt.Path)
	}
	if got := strings.TrimSpace(git(t, wt.Path, "rev-parse", "--abbrev-ref", "HEAD")); got != wt.Branch || !strings.HasPrefix(got, "devboard/fix-the-login-bug-") {
		t.Fatalf("worktree is on %q, record says %q", got, wt.Branch)
	}
	if got := strings.TrimSpace(git(t, e.repo, "rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Fatalf("the user's own checkout was moved to %q", got)
	}
	if wt.BaseRef != "main" {
		t.Fatalf("base ref = %q", wt.BaseRef)
	}

	// What is persisted matches: the process, the prompt, the run's worktree.
	stored := e.run(run.ID)
	if stored.PID != run.PID || stored.ProcessID != run.ProcessID || stored.Prompt != s.Req.Prompt || stored.WorktreeID != wt.ID {
		t.Fatalf("stored = %+v", stored)
	}
	// Order on the wire: the run exists as starting, then the card moves and the agent starts.
	want := []domain.EventType{domain.EventRunStateChanged, domain.EventRunStateChanged, domain.EventAgentStarted}
	var got []domain.EventType
	for _, ev := range e.log(run.ID) {
		got = append(got, ev.Type)
	}
	if !sameTypes(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestStartRefusesWhatCannotRun(t *testing.T) {
	cases := []struct {
		name  string
		setup func(e *env, task *domain.Task) StartInput
		want  error
	}{
		{"unknown task", func(e *env, _ *domain.Task) StartInput { return StartInput{TaskID: "tsk_nope", AgentID: "fake"} }, domain.ErrNotFound},
		{"unknown agent", func(e *env, t *domain.Task) StartInput { return StartInput{TaskID: t.ID, AgentID: "gemini"} }, domain.ErrNotFound},
		{"a model without an agent", func(e *env, t *domain.Task) StartInput { return StartInput{TaskID: t.ID, Model: "opus"} }, domain.ErrInvalid},
		{"no agent can be used", func(e *env, t *domain.Task) StartInput {
			e.adapter.Info = domain.Agent{ID: "fake", Name: "Fake", Detail: "not installed"}
			return StartInput{TaskID: t.ID}
		}, domain.ErrConflict},
		{"agent unavailable", func(e *env, t *domain.Task) StartInput {
			e.adapter.Info = domain.Agent{ID: "fake", Name: "Fake", Detail: "not signed in"}
			return StartInput{TaskID: t.ID, AgentID: "fake"}
		}, domain.ErrConflict},
		{"task is done", func(e *env, t *domain.Task) StartInput {
			done := domain.TaskDone
			if _, err := e.tasks.Update(ctx, t.ID, service_patch(&done, t.Version)); err != nil {
				panic(err)
			}
			return StartInput{TaskID: t.ID, AgentID: "fake"}
		}, domain.ErrInvalid},
		{"nothing to resume", func(e *env, t *domain.Task) StartInput {
			return StartInput{TaskID: t.ID, AgentID: "fake", Resume: true}
		}, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			task := e.task("A task")
			in := tc.setup(e, task)
			before := e.taskState(task.ID)
			if _, err := e.mgr.Start(ctx, in); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if got := e.taskState(task.ID); got != before {
				t.Errorf("a refused start moved the card: %s -> %s", before, got)
			}
			if rs, _ := e.runs.ListByTask(ctx, task.ID); len(rs) != 0 {
				t.Errorf("a refused start left runs behind: %+v", rs)
			}
			if entries, _ := os.ReadDir(e.root); len(entries) != 0 {
				t.Errorf("a refused start made a worktree: %v", entries)
			}
			if len(e.adapter.Sessions()) != 0 {
				t.Error("a refused start launched an agent")
			}
		})
	}
}

func TestStartRefusesARepositoryWithoutCommits(t *testing.T) {
	e := newEnv(t)
	empty := newRepo(t, false)
	p, err := e.projects.Register(ctx, empty, "empty")
	if err != nil {
		t.Fatal(err)
	}
	task, _ := e.tasks.Create(ctx, p.ID, "x", "")
	_, err = e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "no commits") {
		t.Fatalf("err = %v", err)
	}
}

func TestStartRefusesASecondActiveRunOnATask(t *testing.T) {
	e := newEnv(t)
	task := e.task("One at a time")
	e.start(task)
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if len(e.adapter.Sessions()) != 1 {
		t.Fatal("a second agent was launched")
	}
}

func TestConcurrentStartsOfOneTaskLaunchExactlyOneAgent(t *testing.T) {
	e := newEnv(t)
	task := e.task("Race")
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var ok, conflict int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrConflict):
			conflict++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || conflict != 7 || len(e.adapter.Sessions()) != 1 {
		t.Fatalf("%d started, %d refused, %d agents launched", ok, conflict, len(e.adapter.Sessions()))
	}
	if ws, _ := e.worktrees.ListByProject(ctx, e.project.ID); len(ws) != 1 {
		t.Fatalf("%d worktrees for one task", len(ws))
	}
}

// ---- setup failures must not look like a running agent ----

func TestAgentThatFailsToStartLeavesNoTrace(t *testing.T) {
	e := newEnv(t)
	e.adapter.StartFunc = func(agent.StartRequest) error { return errors.New("claude exited immediately: unknown option") }
	task := e.task("Doomed")

	_, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"})
	if !errors.Is(err, domain.ErrAgent) || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("err = %v; the user must be told why, in the agent's words", err)
	}
	if got := e.taskState(task.ID); got != domain.TaskBacklog {
		t.Fatalf("the card moved to %s though no agent ever ran", got)
	}
	runs, _ := e.runs.ListByTask(ctx, task.ID)
	if len(runs) != 1 || runs[0].State != domain.RunFailed || !strings.Contains(runs[0].Reason, "unknown option") || runs[0].PID != 0 {
		t.Fatalf("runs = %+v; the attempt is on record, as failed", runs)
	}
	types := e.agentTypes(runs[0].ID)
	if !sameTypes(types, []domain.EventType{domain.EventAgentFailed}) {
		t.Fatalf("agent events = %v; there must be a failure and never a start", types)
	}
	if e.mgr.LiveCount() != 0 {
		t.Fatal("a session is live for an agent that never started")
	}
	// The worktree made for the attempt, and its branch, are taken away again.
	w := e.worktreeOf(&runs[0])
	if w.State != domain.WorktreeRemoved || exists(w.Path) {
		t.Fatalf("worktree = %+v; nothing ran in it, so it should be gone", w)
	}
	if got := git(t, e.repo, "branch", "--list", w.Branch); strings.TrimSpace(got) != "" {
		t.Fatalf("branch %s survived", w.Branch)
	}
	if got := git(t, e.repo, "worktree", "list", "--porcelain"); strings.Contains(got, w.Path) {
		t.Fatalf("git still lists the worktree:\n%s", got)
	}

	// And the task can be run again once the agent works.
	e.adapter.StartFunc = nil
	run := e.start(task)
	if run.State != domain.RunRunning || e.taskState(task.ID) != domain.TaskDoing {
		t.Fatalf("retry: %+v", run)
	}
}

type failingGit struct {
	gitrepo.Worktrees
	addErr error
}

func (g failingGit) AddWorktree(ctx context.Context, repo, path, branch, start string) (gitrepo.AddedWorktree, error) {
	if g.addErr != nil {
		return gitrepo.AddedWorktree{}, g.addErr
	}
	return g.Worktrees.AddWorktree(ctx, repo, path, branch, start)
}

func TestWorktreeFailureStopsBeforeAnyRunExists(t *testing.T) {
	e := newEnv(t)
	e.git = failingGit{Worktrees: e.git, addErr: errors.New("git: disk full")}
	e.mgr = e.newManager()
	task := e.task("No room")

	if _, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"}); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	if runs, _ := e.runs.ListByTask(ctx, task.ID); len(runs) != 0 {
		t.Fatalf("runs = %+v; a run that never had a workspace must not exist", runs)
	}
	if len(e.adapter.Sessions()) != 0 || e.taskState(task.ID) != domain.TaskBacklog {
		t.Fatal("an agent was launched, or the card moved, though the workspace could not be made")
	}
	ws, _ := e.worktrees.ListByProject(ctx, e.project.ID)
	if len(ws) != 1 || ws[0].State != domain.WorktreeRemoved {
		t.Fatalf("worktree records = %+v; the half-made one must be retired, not left reserving its path", ws)
	}

	// The next attempt is not blocked by the failed one.
	e.git = failingGit{Worktrees: &gitrepo.CLI{}}
	e.mgr = e.newManager()
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"}); err != nil {
		t.Fatalf("retry after a failed worktree: %v", err)
	}
}

func TestStartSurvivesTheCallerGivingUp(t *testing.T) {
	// A phone that drops off mid-request must not abort setup half-way, nor
	// leave an agent that nothing tracks.
	e := newEnv(t)
	task := e.task("Hang up")
	gone, cancel := context.WithCancel(ctx)
	cancel()

	run, err := e.mgr.Start(gone, StartInput{TaskID: task.ID, AgentID: "fake"})
	if err != nil {
		t.Fatalf("Start with a cancelled context: %v", err)
	}
	if !e.mgr.IsLive(run.ID) || e.run(run.ID).State != domain.RunRunning {
		t.Fatal("the session should be running regardless of the caller")
	}
}

// ---- a task keeps its worktree ----

func TestRetryReusesTheTasksWorktree(t *testing.T) {
	e := newEnv(t)
	task := e.task("Keep my work")
	first := e.start(task)
	wt := e.worktreeOf(first)
	if err := os.WriteFile(filepath.Join(wt.Path, "work.txt"), []byte("half done"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.session().Exit(1, "crashed")
	e.waitState(first.ID, domain.RunFailed)

	second := e.start(task)
	if second.WorktreeID != first.WorktreeID {
		t.Fatalf("a retry made a new worktree (%s then %s); it must continue in the same one", first.WorktreeID, second.WorktreeID)
	}
	if b, err := os.ReadFile(filepath.Join(wt.Path, "work.txt")); err != nil || string(b) != "half done" {
		t.Fatalf("the earlier work is gone: %v %q", err, b)
	}
	if e.session().Req.WorkDir != wt.Path {
		t.Fatalf("the agent started in %s", e.session().Req.WorkDir)
	}
	if ws, _ := e.worktrees.ListByProject(ctx, e.project.ID); len(ws) != 1 {
		t.Fatalf("%d worktrees", len(ws))
	}
}

func TestWorktreeDeletedByHandIsReplacedOnTheSameBranch(t *testing.T) {
	e := newEnv(t)
	task := e.task("Vanishing")
	first := e.start(task)
	old := e.worktreeOf(first)
	e.session().Exit(0, "")
	e.waitState(first.ID, domain.RunCompleted)

	if err := os.RemoveAll(old.Path); err != nil {
		t.Fatal(err)
	}
	second := e.start(task)
	fresh := e.worktreeOf(second)
	if fresh.ID == old.ID || fresh.Path == old.Path || !exists(fresh.Path) {
		t.Fatalf("old %+v, new %+v", old, fresh)
	}
	if fresh.Branch != old.Branch {
		t.Fatalf("branch %q then %q; the new worktree should pick up the task's branch", old.Branch, fresh.Branch)
	}
	if got := e.worktreeOf(&domain.Run{WorktreeID: old.ID}); got.State != domain.WorktreeRemoved {
		t.Fatalf("the record of the vanished worktree is %+v", got)
	}
}

func TestResumeContinuesTheEarlierSession(t *testing.T) {
	e := newEnv(t)
	task := e.task("Pick up where we left off")
	first := e.start(task)
	e.session().Ref("sess-abc")
	eventually(t, "the session ref to be stored", func() bool { return e.run(first.ID).SessionRef == "sess-abc" })
	e.session().Exit(1, "interrupted")
	e.waitState(first.ID, domain.RunFailed)

	second, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake", Resume: true, Instructions: "Now add tests."})
	if err != nil {
		t.Fatal(err)
	}
	req := e.session().Req
	if req.ResumeRef != "sess-abc" || req.Prompt != "Now add tests." || second.WorktreeID != first.WorktreeID {
		t.Fatalf("resume request = %+v", req)
	}
}

func TestInstructionsAreAddedToTheTask(t *testing.T) {
	e := newEnv(t)
	task := e.task("Refactor")
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake", Instructions: "  Do not touch the tests. "}); err != nil {
		t.Fatal(err)
	}
	if p := e.session().Req.Prompt; p != "Refactor\n\nDo the thing carefully.\n\nDo not touch the tests." {
		t.Fatalf("prompt = %q", p)
	}
}

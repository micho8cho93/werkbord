package runner

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

// shellAdapter is an agent that is a real shell script, run through the same
// process machinery as Claude Code and Codex. The script speaks a tiny line
// protocol: lines it writes are "say:<text>", "turn", "ask:<ref>:<prompt>" and
// "child:<pid>"; lines it reads are messages, or "answer:<ref>:<text>".
type shellAdapter struct {
	script string
	mu     sync.Mutex
	last   *shellSession
}

const agentScript = `
sleep 300 &
echo "child:$!"
echo "say:ready"
while read line; do
  case "$line" in
    crash) echo "boom" >&2; exit 3 ;;
    ask) echo "ask:q1:Proceed?" ;;
    answer:*) echo "say:got $line" ; echo turn ;;
    *) echo "say:heard $line"; echo turn ;;
  esac
done
`

func (a *shellAdapter) ID() string { return "shell" }
func (a *shellAdapter) Detect(context.Context) domain.Agent {
	return domain.Agent{ID: "shell", Name: "Shell", Available: true}
}

type shellSession struct {
	*agent.Base
	mu    sync.Mutex
	child int
}

func (a *shellAdapter) Start(_ context.Context, req agent.StartRequest) (agent.Session, error) {
	script := a.script
	if script == "" {
		script = agentScript
	}
	s := &shellSession{Base: agent.NewBase(agent.ProcSpec{Command: "/bin/sh", Args: []string{"-c", script}, Dir: req.WorkDir, Env: os.Environ()})}
	s.OnLine = func(line []byte, _ bool) {
		text := string(line)
		switch {
		case strings.HasPrefix(text, "say:"):
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamAssistant, Text: strings.TrimPrefix(text, "say:")})
		case text == "turn":
			s.Emit(agent.Event{Kind: agent.KindTurnEnd})
		case strings.HasPrefix(text, "ask:"):
			parts := strings.SplitN(text, ":", 3)
			s.Emit(agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{Ref: parts[1], Kind: domain.QuestionAsk, Prompt: parts[2], Options: []string{"Yes", "No"}}})
		case strings.HasPrefix(text, "child:"):
			pid, _ := strconv.Atoi(strings.TrimPrefix(text, "child:"))
			s.mu.Lock()
			s.child = pid
			s.mu.Unlock()
		}
	}
	if err := s.Launch(); err != nil {
		return nil, err
	}
	s.Emit(agent.Event{Kind: agent.KindSessionRef, SessionRef: "shell-session"})
	a.mu.Lock()
	a.last = s
	a.mu.Unlock()
	return s, nil
}

func (s *shellSession) Send(_ context.Context, text string) error { return s.WriteLine([]byte(text)) }
func (s *shellSession) Respond(_ context.Context, ref, answer string) error {
	return s.WriteLine([]byte("answer:" + ref + ":" + answer))
}
func (s *shellSession) Close(context.Context) error { return s.CloseInput() }
func (s *shellSession) childPID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.child
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func waitGone(t *testing.T, what string, pid int) {
	t.Helper()
	eventually(t, what+" to be gone", func() bool { return !alive(pid) })
}

// shellEnv is an env whose agent is the shell script, and a way to reach its session.
func shellEnv(t *testing.T, opts ...envOpt) (*env, *shellAdapter) {
	t.Helper()
	e := newEnv(t, opts...)
	sh := &shellAdapter{}
	if err := e.agents.Register(sh); err != nil {
		t.Fatal(err)
	}
	return e, sh
}

func (e *env) startShell(task *domain.Task) (*domain.Run, *shellSession) {
	e.t.Helper()
	r, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "shell"})
	if err != nil {
		e.t.Fatal(err)
	}
	adapter, _ := e.agents.Get("shell")
	sh := adapter.(*shellAdapter)
	sh.mu.Lock()
	s := sh.last
	sh.mu.Unlock()
	eventually(e.t, "the agent to report its child", func() bool { return s.childPID() > 0 })
	return r, s
}

func TestRealProcessConversation(t *testing.T) {
	e, _ := shellEnv(t)
	run, s := e.startShell(e.task("Real process"))
	if run.PID != s.Process().PID || run.PID <= 1 || run.ProcessID == "" {
		t.Fatalf("run = %+v, process = %+v", run, s.Process())
	}

	eventually(t, "the agent's greeting", func() bool {
		o := e.outputs(run.ID)
		return len(o) > 0 && o[0].Text == "ready"
	})
	if err := e.mgr.Send(ctx, run.ID, "hello there"); err != nil {
		t.Fatal(err)
	}
	e.waitWaiting(run.ID, domain.WaitIdle)

	if err := e.mgr.Send(ctx, run.ID, "ask"); err != nil {
		t.Fatal(err)
	}
	e.waitWaiting(run.ID, domain.WaitQuestion)
	qs, _ := e.runs.ListPendingQuestions(ctx)
	if err := e.mgr.Answer(ctx, qs[0].ID, "Yes"); err != nil {
		t.Fatal(err)
	}
	e.waitWaiting(run.ID, domain.WaitIdle)

	var said []string
	for _, o := range e.outputs(run.ID) {
		if o.Stream == domain.StreamAssistant {
			said = append(said, o.Text)
		}
	}
	if got := strings.Join(said, "|"); got != "ready|heard hello there|got answer:q1:Yes" {
		t.Fatalf("said %q", got)
	}

	done, err := e.mgr.Finish(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != domain.RunCompleted || done.ExitCode == nil || *done.ExitCode != 0 {
		t.Fatalf("closing stdin should end the shell cleanly: %+v", done)
	}
	waitGone(t, "the agent's child", s.childPID())
}

func TestStopKillsTheAgentAndEverythingItStarted(t *testing.T) {
	e, _ := shellEnv(t)
	run, s := e.startShell(e.task("Kill the tree"))
	if !alive(s.childPID()) || !alive(run.PID) {
		t.Fatal("the agent or its child is not running")
	}
	stopped, err := e.mgr.Stop(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != domain.RunStopped {
		t.Fatalf("run = %+v", stopped)
	}
	waitGone(t, "the agent", run.PID)
	waitGone(t, "the agent's child", s.childPID())
}

func TestCrashingAgentFailsTheRunAndCleansUp(t *testing.T) {
	e, _ := shellEnv(t)
	run, s := e.startShell(e.task("Crash for real"))
	if err := e.mgr.Send(ctx, run.ID, "crash"); err != nil {
		t.Fatal(err)
	}
	got := e.waitState(run.ID, domain.RunFailed)
	if got.ExitCode == nil || *got.ExitCode != 3 || !strings.Contains(got.Reason, "status 3") || !strings.Contains(got.Reason, "boom") {
		t.Fatalf("run = %+v", got)
	}
	waitGone(t, "the agent's child", s.childPID())
	var sawStderr bool
	for _, o := range e.outputs(run.ID) {
		sawStderr = sawStderr || (o.Stream == domain.StreamStderr && o.Text == "boom")
	}
	if !sawStderr {
		t.Fatal("stderr was not recorded")
	}
}

func TestShutdownKillsRealProcesses(t *testing.T) {
	e, _ := shellEnv(t)
	run, s := e.startShell(e.task("Shut down"))
	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	waitGone(t, "the agent", run.PID)
	waitGone(t, "the agent's child", s.childPID())
	if got := e.run(run.ID); got.State != domain.RunFailed || got.PID != 0 {
		t.Fatalf("run = %+v", got)
	}
}

// A controller that dies without cleaning up leaves its agents running. The next
// one finds them from what the database recorded, and stops them.
func TestOrphanedAgentIsStoppedAfterACrash(t *testing.T) {
	e, _ := shellEnv(t)
	run, s := e.startShell(e.task("Orphan"))
	pid, child := run.PID, s.childPID()
	if !alive(pid) || !alive(child) {
		t.Fatal("setup: the agent is not running")
	}

	// "Crash": a new controller opens the database while the old process lives on.
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	waitGone(t, "the orphaned agent", pid)
	waitGone(t, "the orphan's child", child)
	got := e.run(run.ID)
	if got.State != domain.RunFailed || got.Reason != "interrupted: controller restarted" || got.PID != 0 {
		t.Fatalf("run = %+v", got)
	}
}

func TestRecoveryNeverKillsAProcessItCannotVouchFor(t *testing.T) {
	e, _ := shellEnv(t)
	run, s := e.startShell(e.task("Reused pid"))

	// The recorded identity no longer matches whatever has this pid: as happens
	// when the agent died and an unrelated program was given its number.
	other := exec.Command("sleep", "300")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })

	_ = e.db.Close()
	e.open()
	if err := e.db.Update(ctx, func(tx storeTx) error {
		r, err := tx.Runs().Get(ctx, run.ID)
		if err != nil {
			return err
		}
		r.PID, r.ProcessID = other.Process.Pid, "Thu Jan  1 00:00:00 1970"
		return tx.Runs().Update(ctx, r)
	}); err != nil {
		t.Fatal(err)
	}
	e.mgr = e.newManager()
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if !alive(other.Process.Pid) {
		t.Fatal("recovery killed a process that merely had the recorded pid")
	}
	if got := e.run(run.ID); got.State != domain.RunFailed {
		t.Fatalf("run = %+v", got)
	}
	// The real agent is still ours to clean up; do so.
	_ = s.Stop(ctx)
}

func TestTheAgentStartsInItsWorktree(t *testing.T) {
	e, sh := shellEnv(t)
	sh.script = `pwd; echo "say:$(pwd)"; while read l; do :; done`
	run := func() *domain.Run {
		r, err := e.mgr.Start(ctx, StartInput{TaskID: e.task("Where am I").ID, AgentID: "shell"})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}()
	wt := e.worktreeOf(run)
	eventually(t, "the working directory", func() bool {
		for _, o := range e.outputs(run.ID) {
			if o.Text == wt.Path {
				return true
			}
		}
		return false
	})
}

package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

// ---- streaming ----

func TestOutputIsStreamedPersistedAndSummarised(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Stream"))
	s := e.session()

	s.Say(domain.StreamSystem, "Session started")
	s.Assistant("I'll start with the parser.")
	s.Say(domain.StreamTool, "Edit internal/parser.go")
	s.Say(domain.StreamStderr, "warning: something")
	for i := 0; i < 200; i++ { // a burst, as a chatty agent makes
		s.Say(domain.StreamTool, fmt.Sprintf("Bash: step %d", i))
	}
	eventually(t, "all output to be recorded", func() bool { return len(e.outputs(run.ID)) == 204 })

	out := e.outputs(run.ID)
	if out[0].Stream != domain.StreamSystem || out[1].Text != "I'll start with the parser." || out[3].Stream != domain.StreamStderr {
		t.Fatalf("output = %+v", out[:4])
	}
	for i := 0; i < 200; i++ {
		if want := fmt.Sprintf("Bash: step %d", i); out[4+i].Text != want {
			t.Fatalf("output %d = %q, want %q (order must be preserved)", 4+i, out[4+i].Text, want)
		}
	}
	eventually(t, "the card's activity to follow", func() bool { return e.run(run.ID).Activity == "Bash: step 199" })
	if got := e.run(run.ID); got.ActivityAt == nil || got.State != domain.RunRunning {
		t.Fatalf("run = %+v", got)
	}
}

func TestOutputReachesLiveSubscribersInOrder(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Live"))
	e.session().Assistant("one")
	e.session().Assistant("two")
	eventually(t, "output events on the bus", func() bool {
		n := 0
	drain:
		for {
			select {
			case ev := <-e.sub.C:
				e.evMu.Lock()
				e.seen = append(e.seen, ev)
				e.evMu.Unlock()
			default:
				break drain
			}
		}
		e.evMu.Lock()
		defer e.evMu.Unlock()
		for _, ev := range e.seen {
			if ev.Type == domain.EventAgentOutput && ev.RunID == run.ID {
				n++
			}
		}
		return n == 2
	})
	e.evMu.Lock()
	defer e.evMu.Unlock()
	var last int64
	for _, ev := range e.seen {
		if ev.Seq <= last {
			t.Fatalf("events out of order: %d after %d", ev.Seq, last)
		}
		last = ev.Seq
	}
}

func TestOutputBudgetIsEnforced(t *testing.T) {
	e := newEnv(t, withOptions(func(o *Options) { o.MaxOutputBytes = 1 << 20 }))
	run := e.start(e.task("Chatty"))
	chunk := strings.Repeat("x", 100<<10)
	for i := 0; i < 15; i++ {
		e.session().Assistant(chunk)
	}
	e.session().Assistant("the last word")
	eventually(t, "the notice", func() bool {
		for _, o := range e.outputs(run.ID) {
			if strings.Contains(o.Text, "Further output is not kept") {
				return true
			}
		}
		return false
	})
	// Everything else still works: the agent can finish its turn and be answered.
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
	total := 0
	for _, o := range e.outputs(run.ID) {
		total += len(o.Text)
		if o.Text == "the last word" {
			t.Fatal("output past the budget was kept")
		}
	}
	if total > 2<<20 {
		t.Fatalf("%d bytes kept against a 1 MiB budget", total)
	}
}

// ---- turns and messages ----

func TestAgentWaitsForTheNextMessageAndIsResumedByIt(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Chat"))
	s := e.session()

	s.Assistant("Done with the first part.")
	s.TurnEnd()
	idle := e.waitWaiting(run.ID, domain.WaitIdle)
	if idle.PID == 0 {
		t.Fatal("an idle session still has its process")
	}

	if err := e.mgr.Send(ctx, run.ID, "  Now do the second part.  "); err != nil {
		t.Fatal(err)
	}
	if got := s.Sent(); len(got) != 1 || got[0] != "Now do the second part." {
		t.Fatalf("the agent was sent %q", got)
	}
	if got := e.run(run.ID); got.State != domain.RunRunning || got.Waiting != domain.WaitNone {
		t.Fatalf("run = %+v; a message resumes the agent", got)
	}

	s.Assistant("Second part done.")
	s.TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)

	want := []domain.EventType{domain.EventAgentStarted, domain.EventAgentWaiting, domain.EventAgentResumed, domain.EventAgentWaiting}
	if got := e.agentTypes(run.ID); !sameTypes(got, want) {
		t.Fatalf("timeline = %v, want %v", got, want)
	}
	var users []string
	for _, o := range e.outputs(run.ID) {
		if o.Stream == domain.StreamUser {
			users = append(users, o.Text)
		}
	}
	if len(users) != 1 || users[0] != "Now do the second part." {
		t.Fatalf("the user's message should be in the activity: %v", users)
	}
}

func TestMessageWhileTheAgentWorksIsQueued(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Busy"))
	if err := e.mgr.Send(ctx, run.ID, "also check the docs"); err != nil {
		t.Fatal(err)
	}
	if got := e.session().Sent(); len(got) != 1 {
		t.Fatalf("sent = %v", got)
	}
	if e.run(run.ID).State != domain.RunRunning {
		t.Fatal("queueing a message must not change the state")
	}
}

func TestMessageRefusals(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Refusals"))
	if err := e.mgr.Send(ctx, run.ID, "   "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank: err = %v", err)
	}
	if err := e.mgr.Send(ctx, "run_missing", "hi"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown run: err = %v", err)
	}

	e.session().Ask("q1", "Which one?", "A", "B")
	e.waitWaiting(run.ID, domain.WaitQuestion)
	if err := e.mgr.Send(ctx, run.ID, "never mind"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("while a question is open: err = %v", err)
	}
	if len(e.session().Sent()) != 0 {
		t.Fatal("a refused message reached the agent")
	}

	e.session().OnSend = func(string) error { return errors.New("pipe broke") }
	qs, _ := e.runs.ListPendingQuestions(ctx)
	if err := e.mgr.Answer(ctx, qs[0].ID, "A"); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Send(ctx, run.ID, "will fail"); err == nil || !strings.Contains(err.Error(), "pipe broke") {
		t.Fatalf("a failed delivery must be reported: err = %v", err)
	}
	var users int
	for _, o := range e.outputs(run.ID) {
		if o.Stream == domain.StreamUser && o.Text == "will fail" {
			users++
		}
	}
	if users != 0 {
		t.Fatal("a message the agent never got was recorded as sent")
	}
}

// ---- questions ----

func TestQuestionAndAnswer(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Ask"))
	s := e.session()

	s.Assistant("I need a decision.")
	s.Ask("ref-1", "Use Postgres or SQLite?", "Postgres", "SQLite")
	waiting := e.waitWaiting(run.ID, domain.WaitQuestion)
	pending, err := e.runs.ListPendingQuestions(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	q := pending[0]
	if q.RunID != run.ID || q.Prompt != "Use Postgres or SQLite?" || q.Kind != domain.QuestionAsk || len(q.Options) != 2 {
		t.Fatalf("question = %+v", q)
	}
	_ = waiting

	if err := e.mgr.Answer(ctx, q.ID, "SQLite"); err != nil {
		t.Fatal(err)
	}
	if got := s.Responses(); len(got) != 1 || got[0].Ref != "ref-1" || got[0].Answer != "SQLite" {
		t.Fatalf("the agent was given %+v", got)
	}
	got := e.run(run.ID)
	if got.State != domain.RunRunning {
		t.Fatalf("run = %+v; the answer unblocks the agent", got)
	}
	if left, _ := e.runs.ListPendingQuestions(ctx); len(left) != 0 {
		t.Fatalf("pending = %+v", left)
	}

	// Said before it asked: the question comes after the words that led to it.
	out := e.outputs(run.ID)
	if out[0].Text != "I need a decision." || out[len(out)-1].Stream != domain.StreamUser || out[len(out)-1].Text != "SQLite" {
		t.Fatalf("output = %+v", out)
	}
	want := []domain.EventType{domain.EventAgentStarted, domain.EventAgentQuestion, domain.EventAgentResumed}
	if got := e.agentTypes(run.ID); !sameTypes(got, want) {
		t.Fatalf("timeline = %v", got)
	}

	if err := e.mgr.Answer(ctx, q.ID, "again"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("answering twice: err = %v", err)
	}
	if err := e.mgr.Answer(ctx, "qst_nope", "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown question: err = %v", err)
	}
}

func TestParallelApprovalsEachNeedAnAnswer(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Parallel tools"))
	s := e.session()
	s.RequestApproval("a", "Run npm test?")
	s.RequestApproval("b", "Edit main.go?")
	eventually(t, "both questions", func() bool { qs, _ := e.runs.ListPendingQuestions(ctx); return len(qs) == 2 })
	qs, _ := e.runs.ListPendingQuestions(ctx)
	if qs[0].Kind != domain.QuestionApproval {
		t.Fatalf("kind = %s", qs[0].Kind)
	}

	if err := e.mgr.Answer(ctx, qs[0].ID, "Allow"); err != nil {
		t.Fatal(err)
	}
	if got := e.run(run.ID); got.State != domain.RunWaitingForUser || got.Waiting != domain.WaitQuestion {
		t.Fatalf("run = %+v; one approval is still open", got)
	}
	if err := e.mgr.Answer(ctx, qs[1].ID, "Deny"); err != nil {
		t.Fatal(err)
	}
	if e.run(run.ID).State != domain.RunRunning {
		t.Fatal("the last answer should resume the run")
	}
	if got := s.Responses(); len(got) != 2 || got[0].Answer != "Allow" || got[1].Answer != "Deny" {
		t.Fatalf("responses = %+v", got)
	}
}

func TestQuestionWithdrawnByTheAgent(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Changed my mind"))
	e.session().Ask("r", "Proceed?")
	e.waitWaiting(run.ID, domain.WaitQuestion)
	e.session().WithdrawQuestion("r")
	e.waitState(run.ID, domain.RunRunning)
	if qs, _ := e.runs.ListPendingQuestions(ctx); len(qs) != 0 {
		t.Fatalf("pending = %+v", qs)
	}
}

func TestAnswerThatTheAgentRejectsIsNotRecorded(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Stale"))
	e.session().Ask("r", "Proceed?")
	e.waitWaiting(run.ID, domain.WaitQuestion)
	qs, _ := e.runs.ListPendingQuestions(ctx)

	// The agent forgot the question (its own bookkeeping, or a protocol hiccup).
	if err := e.session().Respond(ctx, "r", "x"); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Answer(ctx, qs[0].ID, "Yes"); !errors.Is(err, agent.ErrUnknownQuestion) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := e.runs.GetQuestion(ctx, qs[0].ID); got.Status != domain.QuestionPending {
		t.Fatalf("question = %+v; an answer the agent did not receive is not an answer", got)
	}
}

// ---- ending ----

func TestFinishCompletesAnIdleSession(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Wrap up"))
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)

	done, err := e.mgr.Finish(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != domain.RunCompleted || done.EndedAt == nil || done.PID != 0 || done.Reason != "" {
		t.Fatalf("run = %+v", done)
	}
	if !e.session().CloseRequested() || e.session().StopRequested() {
		t.Fatal("finishing is a graceful end, not a kill")
	}
	if e.mgr.IsLive(run.ID) || e.mgr.LiveCount() != 0 {
		t.Fatal("the session is still registered")
	}
	want := []domain.EventType{domain.EventAgentStarted, domain.EventAgentWaiting, domain.EventAgentCompleted}
	if got := e.agentTypes(run.ID); !sameTypes(got, want) {
		t.Fatalf("timeline = %v", got)
	}
	if _, err := e.mgr.Finish(ctx, run.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("finishing twice: err = %v", err)
	}
	if err := e.mgr.Send(ctx, run.ID, "late"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("message to a finished run: err = %v", err)
	}
	if got := e.taskState(e.run(run.ID).TaskID); got != domain.TaskDoing {
		t.Fatalf("task = %s; moving cards is the user's call, not a side effect of finishing", got)
	}
}

func TestFinishIsRefusedWhileAQuestionIsOpen(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Blocked"))
	e.session().Ask("r", "Proceed?")
	e.waitWaiting(run.ID, domain.WaitQuestion)
	if _, err := e.mgr.Finish(ctx, run.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if e.session().CloseRequested() || e.run(run.ID).State != domain.RunWaitingForUser {
		t.Fatal("a refused finish changed something")
	}
}

func TestFinishStopsAnAgentThatWillNotExit(t *testing.T) {
	e := newEnv(t, withOptions(func(o *Options) { o.FinishTimeout = 150 * time.Millisecond }))
	run := e.start(e.task("Stubborn"))
	e.session().OnClose = func(*fakeSession) {} // ignores the request
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)

	done, err := e.mgr.Finish(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != domain.RunStopped || !strings.Contains(done.Reason, "did not exit when asked to finish") {
		t.Fatalf("run = %+v", done)
	}
	if !e.session().StopRequested() {
		t.Fatal("the agent was never stopped")
	}
}

func TestStopCancelsARunningSession(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Cancel me"))
	e.session().Ask("r", "Proceed?")
	e.waitWaiting(run.ID, domain.WaitQuestion)

	stopped, err := e.mgr.Stop(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != domain.RunStopped || stopped.Reason != "stopped by user" || stopped.EndedAt == nil || stopped.PID != 0 {
		t.Fatalf("run = %+v; a stop is reported as stopped, not as the failure a killed process looks like", stopped)
	}
	if !e.session().StopRequested() {
		t.Fatal("the process was not stopped")
	}
	if e.mgr.IsLive(run.ID) {
		t.Fatal("still registered")
	}
	if qs, _ := e.runs.ListPendingQuestions(ctx); len(qs) != 0 {
		t.Fatalf("a question nobody can answer any more is still pending: %+v", qs)
	}
	want := []domain.EventType{domain.EventAgentStarted, domain.EventAgentQuestion, domain.EventAgentStopped}
	if got := e.agentTypes(run.ID); !sameTypes(got, want) {
		t.Fatalf("timeline = %v", got)
	}
	if _, err := e.mgr.Stop(ctx, run.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stopping twice: err = %v", err)
	}
	// Stopping is not undoing: the work is still in the worktree, the card where it was.
	if !exists(e.worktreeOf(stopped).Path) || e.taskState(stopped.TaskID) != domain.TaskDoing {
		t.Fatal("stopping must not delete the work or move the card")
	}
	// And the task can be started again.
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: stopped.TaskID, AgentID: "fake"}); err != nil {
		t.Fatalf("restart after a stop: %v", err)
	}
}

func TestStopWhileAMessageIsBeingSent(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Race"))
	release := make(chan struct{})
	e.session().OnSend = func(string) error { <-release; return nil }
	sent := make(chan error, 1)
	go func() { sent <- e.mgr.Send(ctx, run.ID, "slow") }()
	time.Sleep(50 * time.Millisecond)

	stopped := make(chan error, 1)
	go func() { _, err := e.mgr.Stop(ctx, run.ID); stopped <- err }()
	if err := <-stopped; err != nil {
		t.Fatalf("a stop must not wait for a stuck send: %v", err)
	}
	close(release)
	<-sent
	e.waitState(run.ID, domain.RunStopped)
}

func TestAgentThatExitsCleanlyCompletes(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Self-finishing"))
	e.session().Assistant("All done.")
	e.session().Exit(0, "")
	got := e.waitState(run.ID, domain.RunCompleted)
	if got.ExitCode == nil || *got.ExitCode != 0 || got.Reason != "" || got.PID != 0 {
		t.Fatalf("run = %+v", got)
	}
	if out := e.outputs(run.ID); len(out) != 1 || out[0].Text != "All done." {
		t.Fatalf("output written just before the exit was lost: %+v", out)
	}
	if e.mgr.LiveCount() != 0 {
		t.Fatal("still registered")
	}
}

func TestAgentFailureIsRecordedWithItsReason(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Crash"))
	e.session().Assistant("Working on it.")
	e.session().Exit(2, "exited with status 2: out of memory")
	got := e.waitState(run.ID, domain.RunFailed)
	if got.Reason != "exited with status 2: out of memory" || got.ExitCode == nil || *got.ExitCode != 2 {
		t.Fatalf("run = %+v", got)
	}
	want := []domain.EventType{domain.EventAgentStarted, domain.EventAgentFailed}
	if got := e.agentTypes(run.ID); !sameTypes(got, want) {
		t.Fatalf("timeline = %v", got)
	}
	if e.taskState(got.TaskID) != domain.TaskDoing {
		t.Fatal("a failed run leaves its card in Doing: the user decides what happens to it")
	}
}

func TestEverythingTheAgentSaidBeforeDyingIsKept(t *testing.T) {
	e := newEnv(t, withOptions(func(o *Options) { o.FlushInterval = time.Hour })) // only the end flushes
	run := e.start(e.task("Last words"))
	e.session().Assistant("one")
	e.session().Say(domain.StreamStderr, "fatal: boom")
	e.session().Exit(1, "boom")
	e.waitState(run.ID, domain.RunFailed)
	if out := e.outputs(run.ID); len(out) != 2 || out[1].Stream != domain.StreamStderr {
		t.Fatalf("output = %+v", out)
	}
}

// ---- the controller stopping ----

func TestShutdownEndsSessionsAndRecordsWhatBecameOfThem(t *testing.T) {
	e := newEnv(t)
	working := e.start(e.task("Working"))
	workingSession := e.session()
	idle := e.start(e.task("Idle"))
	idleSession := e.session()
	idleSession.Ref("sess-idle")
	eventually(t, "the session ref", func() bool { return e.run(idle.ID).SessionRef == "sess-idle" })
	idleSession.TurnEnd()
	e.waitWaiting(idle.ID, domain.WaitIdle)
	asking := e.start(e.task("Asking"))
	askingSession := e.session()
	askingSession.Ref("sess-ask")
	askingSession.Ask("r", "Proceed?")
	e.waitWaiting(asking.ID, domain.WaitQuestion)
	eventually(t, "the session ref", func() bool { return e.run(asking.ID).SessionRef == "sess-ask" })

	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if !workingSession.StopRequested() || !idleSession.StopRequested() || !askingSession.StopRequested() {
		t.Fatal("every process must be stopped before the controller exits")
	}
	if e.mgr.LiveCount() != 0 {
		t.Fatal("sessions are still registered")
	}
	if r := e.run(working.ID); r.State != domain.RunFailed || r.Reason != "interrupted: controller shut down" || r.PID != 0 {
		t.Fatalf("a run that was working: %+v", r)
	}
	if r := e.run(idle.ID); r.State != domain.RunWaitingForUser || r.Waiting != domain.WaitIdle || r.PID != 0 {
		t.Fatalf("an idle run is kept, to be resumed by a message: %+v", r)
	}
	if r := e.run(asking.ID); r.State != domain.RunWaitingForUser || r.Waiting != domain.WaitIdle {
		t.Fatalf("a run that was asking is kept too, with its question gone: %+v", r)
	}
	if qs, _ := e.runs.ListPendingQuestions(ctx); len(qs) != 0 {
		t.Fatalf("the question died with the process: %+v", qs)
	}

	// No new work once shutting down.
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: e.run(working.ID).TaskID, AgentID: "fake"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Start during shutdown: err = %v", err)
	}
	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown twice: %v", err)
	}
}

func TestShutdownDoesNotWaitForeverOnAProcessThatWillNotDie(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Immortal"))
	release := make(chan struct{})
	e.session().OnStop = func(*fakeSession) { <-release }
	defer close(release)
	c, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	err := e.mgr.Shutdown(c)
	if err == nil || !strings.Contains(err.Error(), run.ID) {
		t.Fatalf("err = %v; shutdown must give up, and say which run it could not stop", err)
	}
}

func TestUnattendedSessionKeepsWorkingAndRecording(t *testing.T) {
	// The runner has no notion of a browser: a disconnected frontend is simply
	// the absence of requests. The session keeps working and keeps recording, and
	// a client that comes back later reads it all from the log.
	e := newEnv(t)
	run := e.start(e.task("Unattended"))
	e.session().Assistant("working...")
	time.Sleep(100 * time.Millisecond) // nobody is watching
	e.session().Ask("r", "Proceed?")
	e.waitWaiting(run.ID, domain.WaitQuestion)
	if out := e.outputs(run.ID); len(out) < 1 || out[0].Text != "working..." {
		t.Fatalf("output = %+v", out)
	}
	// A later client sees everything from the log.
	if evs, err := e.runs.Events(ctx, run.ID, 0, 100); err != nil || len(evs) < 4 {
		t.Fatalf("history = %d events, %v", len(evs), err)
	}
}

// ---- restart ----

func TestConversationSurvivesAControllerRestart(t *testing.T) {
	e := newEnv(t)
	task := e.task("Long conversation")
	run := e.start(task)
	e.session().Ref("sess-1")
	eventually(t, "the session ref", func() bool { return e.run(run.ID).SessionRef == "sess-1" })
	e.session().Assistant("Finished the first step.")
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
	wt := e.worktreeOf(run)

	// The controller stops and starts again over the same database and disk.
	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	got := e.run(run.ID)
	if got.State != domain.RunWaitingForUser || got.Waiting != domain.WaitIdle || got.SessionRef != "sess-1" {
		t.Fatalf("after restart: %+v", got)
	}
	if e.mgr.IsLive(run.ID) {
		t.Fatal("no process survives a restart")
	}
	if out := e.outputs(run.ID); len(out) == 0 || out[0].Text != "Finished the first step." {
		t.Fatalf("history after restart: %+v", out)
	}

	// A message continues the same conversation in the same worktree.
	if err := e.mgr.Send(ctx, run.ID, "Continue with step two."); err != nil {
		t.Fatal(err)
	}
	if e.mgr.LiveCount() != 1 {
		t.Fatal("a message to a waiting run must start a process for it")
	}
	req := e.session().Req
	if req.ResumeRef != "sess-1" || req.Prompt != "Continue with step two." || req.WorkDir != wt.Path || req.RunID != run.ID {
		t.Fatalf("resume request = %+v", req)
	}
	resumed := e.run(run.ID)
	if resumed.State != domain.RunRunning || resumed.PID == 0 || resumed.ID != run.ID {
		t.Fatalf("resumed run = %+v; the same run continues, not a new one", resumed)
	}
	if got := e.agentTypes(run.ID); got[len(got)-1] != domain.EventAgentStarted {
		t.Fatalf("timeline = %v", got)
	}

	// It is a normal session from here.
	e.session().Assistant("Step two done.")
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
	if _, err := e.mgr.Finish(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCrashRecoveryMarksInterruptedRunsFailed(t *testing.T) {
	e := newEnv(t)
	working := e.start(e.task("Was working"))
	idleTask := e.task("Was idle")
	idle := e.start(idleTask)
	e.session().Ref("sess-i")
	eventually(t, "the session ref", func() bool { return e.run(idle.ID).SessionRef == "sess-i" })
	e.session().TurnEnd()
	e.waitWaiting(idle.ID, domain.WaitIdle)

	// The controller dies: no shutdown, nothing recorded. A new one starts on the same database.
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if r := e.run(working.ID); r.State != domain.RunFailed || r.Reason != "interrupted: controller restarted" || r.PID != 0 {
		t.Fatalf("a run that was working when the controller died: %+v", r)
	}
	if r := e.run(idle.ID); r.State != domain.RunWaitingForUser || r.PID != 0 {
		t.Fatalf("an idle run survives a crash: %+v", r)
	}
	// And the failed one can simply be started again.
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: e.run(working.ID).TaskID, AgentID: "fake"}); err != nil {
		t.Fatalf("restart a run after a crash: %v", err)
	}
}

func TestMessageToARunWhoseWorktreeIsGone(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Gone"))
	e.session().Ref("s")
	eventually(t, "ref", func() bool { return e.run(run.ID).SessionRef == "s" })
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
	wt := e.worktreeOf(run)
	_ = e.mgr.Shutdown(ctx)
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	_ = e.mgr.Recover(ctx)
	if err := removeAll(wt.Path); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Send(ctx, run.ID, "hello?"); !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "worktree") {
		t.Fatalf("err = %v", err)
	}
	if e.mgr.LiveCount() != 0 || len(e.adapter.Sessions()) != 1 {
		t.Fatal("an agent was started in a directory that is not there")
	}
	// The run can still be ended by the user.
	if got, err := e.mgr.Stop(ctx, run.ID); err != nil || got.State != domain.RunStopped {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
}

func TestResumeThatFailsToLaunchKeepsTheRunWaiting(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Fragile"))
	e.session().Ref("s")
	eventually(t, "ref", func() bool { return e.run(run.ID).SessionRef == "s" })
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
	_ = e.mgr.Shutdown(ctx)
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	_ = e.mgr.Recover(ctx)

	e.adapter.StartFunc = func(agent.StartRequest) error { return errors.New("codex: not signed in") }
	err := e.mgr.Send(ctx, run.ID, "hello")
	if !errors.Is(err, domain.ErrAgent) || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("err = %v", err)
	}
	if got := e.run(run.ID); got.State != domain.RunWaitingForUser {
		t.Fatalf("run = %+v; a failed resume must not claim the agent is running, nor throw the conversation away", got)
	}
	e.adapter.StartFunc = nil
	if err := e.mgr.Send(ctx, run.ID, "hello again"); err != nil {
		t.Fatalf("a later attempt should work: %v", err)
	}
}

// ---- persistence ----

func TestRunHistorySurvivesReopeningTheDatabase(t *testing.T) {
	e := newEnv(t)
	task := e.task("History")
	run := e.start(task)
	e.session().Assistant("hello")
	e.session().Ask("r", "Proceed?")
	e.waitWaiting(run.ID, domain.WaitQuestion)
	qs, _ := e.runs.ListPendingQuestions(ctx)
	if err := e.mgr.Answer(ctx, qs[0].ID, "yes"); err != nil {
		t.Fatal(err)
	}
	e.session().Exit(0, "")
	e.waitState(run.ID, domain.RunCompleted)

	_ = e.db.Close()
	e.open()

	got, err := e.runs.Get(ctx, run.ID)
	if err != nil || got.State != domain.RunCompleted || got.Prompt == "" || got.WorktreeID == "" || got.ExitCode == nil {
		t.Fatalf("run = %+v, %v", got, err)
	}
	q, _ := e.runs.GetQuestion(ctx, qs[0].ID)
	if q.Status != domain.QuestionAnswered || q.Answer != "yes" || q.RunID != run.ID {
		t.Fatalf("question = %+v", q)
	}
	evs, err := e.runs.Events(ctx, run.ID, 0, 1000)
	if err != nil || len(evs) < 6 {
		t.Fatalf("history = %d events, %v", len(evs), err)
	}
	var last int64
	for _, ev := range evs {
		if ev.Seq <= last || ev.RunID != run.ID {
			t.Fatalf("history out of order or mixed: %+v", ev)
		}
		last = ev.Seq
	}
	latest, _ := e.runs.ListLatestByProject(ctx, e.project.ID)
	if len(latest) != 1 || latest[0].ID != run.ID {
		t.Fatalf("latest = %+v", latest)
	}
}

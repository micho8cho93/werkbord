package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/store"
)

// runFixture is a project with one task and a subscription to everything published.
type runFixture struct {
	*fixture
	project *ProjectDetail
	task    *domain.Task
	sub     *events.Subscription
}

func newRunFixture(t *testing.T) *runFixture {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/app", "")
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.tasks.Create(ctx, p.ID, "Fix the bug", "details")
	if err != nil {
		t.Fatal(err)
	}
	rf := &runFixture{fixture: f, project: p, task: task, sub: f.bus.Subscribe(1000)}
	rf.drain()
	return rf
}

// drain returns the events published since the last call.
func (f *runFixture) drain() []domain.Event {
	var out []domain.Event
	for {
		select {
		case ev := <-f.sub.C:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func types(evs []domain.Event) []domain.EventType {
	out := make([]domain.EventType, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

func wantTypes(t *testing.T, got []domain.Event, want ...domain.EventType) {
	t.Helper()
	g := types(got)
	if len(g) != len(want) {
		t.Fatalf("events = %v, want %v", g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Fatalf("events = %v, want %v", g, want)
		}
	}
}

func (f *runFixture) newRun(t *testing.T) *domain.Run {
	t.Helper()
	r, err := f.runs.Create(context.Background(), NewRun{TaskID: f.task.ID, AgentID: "fake", Prompt: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *runFixture) running(t *testing.T) *domain.Run {
	t.Helper()
	r := f.newRun(t)
	r, err := f.runs.MarkStarted(context.Background(), r.ID, Started{PID: 4242, ProcessID: "tok", SessionRef: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	f.drain()
	return r
}

func (f *runFixture) taskState(t *testing.T) domain.TaskState {
	t.Helper()
	ts, err := f.tasks.List(context.Background(), f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ts[0].State
}

func payload[T any](t *testing.T, ev domain.Event) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(ev.Payload, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCreateRunRules(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()

	r := f.newRun(t)
	if r.State != domain.RunStarting || r.ProjectID != f.project.ID || r.Prompt != "do it" || r.PID != 0 {
		t.Fatalf("run = %+v", r)
	}
	evs := f.drain()
	wantTypes(t, evs, domain.EventRunStateChanged)
	if evs[0].RunID != r.ID || evs[0].TaskID != f.task.ID || evs[0].ProjectID != f.project.ID {
		t.Fatalf("event ids = %+v", evs[0])
	}
	if f.taskState(t) != domain.TaskBacklog {
		t.Fatal("creating a run is not starting an agent: the card must not move yet")
	}

	if _, err := f.runs.Create(ctx, NewRun{TaskID: f.task.ID, AgentID: "fake", Prompt: "again"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second active run: err = %v, want ErrConflict", err)
	}
	if len(f.drain()) != 0 {
		t.Fatal("a refused run must publish nothing")
	}
	if _, err := f.runs.End(ctx, r.ID, Ended{State: domain.RunFailed, Reason: "setup failed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.Create(ctx, NewRun{TaskID: f.task.ID, AgentID: "fake", Prompt: "retry"}); err != nil {
		t.Fatalf("a task can run again once its run has ended: %v", err)
	}

	for _, bad := range []NewRun{
		{TaskID: f.task.ID, AgentID: "", Prompt: "p"},
		{TaskID: f.task.ID, AgentID: "fake", Prompt: "  "},
		{TaskID: f.task.ID, AgentID: "fake", Prompt: strings.Repeat("x", maxPromptBytes+1)},
	} {
		if _, err := f.runs.Create(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Create(%+v): err = %v, want ErrInvalid", bad, err)
		}
	}
	if _, err := f.runs.Create(ctx, NewRun{TaskID: "tsk_missing", AgentID: "fake", Prompt: "p"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing task: err = %v", err)
	}
}

func TestMarkStartedMovesTheCardAndRecordsTheProcess(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.newRun(t)
	f.drain()

	started, err := f.runs.MarkStarted(ctx, r.ID, Started{PID: 4242, ProcessID: "Sat Oct 4", SessionRef: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	if started.State != domain.RunRunning || started.PID != 4242 || started.ProcessID != "Sat Oct 4" || started.SessionRef != "sess-1" {
		t.Fatalf("started = %+v", started)
	}
	if f.taskState(t) != domain.TaskDoing {
		t.Fatalf("the task is %s; a running agent belongs in Doing", f.taskState(t))
	}
	evs := f.drain()
	wantTypes(t, evs, domain.EventTaskUpdated, domain.EventRunStateChanged, domain.EventAgentStarted)
	if ch := payload[struct {
		Run  domain.Run
		From domain.RunState
	}](t, evs[1]); ch.Run.State != domain.RunRunning || ch.From != domain.RunStarting {
		t.Fatalf("state change = %+v", ch)
	}
	stored, _ := f.runs.Get(ctx, r.ID)
	if stored.PID != 4242 || stored.ProcessID != "Sat Oct 4" {
		t.Fatalf("the process was not persisted: %+v", stored)
	}

	if _, err := f.runs.MarkStarted(ctx, r.ID, Started{PID: 1}); !errors.Is(err, domain.ErrTransition) {
		t.Fatalf("starting twice: err = %v", err)
	}
	if _, err := f.runs.MarkStarted(ctx, r.ID, Started{PID: 1, Resumed: true}); !errors.Is(err, domain.ErrTransition) {
		t.Fatalf("resuming a run that is not waiting: err = %v", err)
	}
}

func TestMarkStartedLeavesDoneCardsAndMovesReviewCards(t *testing.T) {
	for _, tc := range []struct {
		from, want domain.TaskState
	}{{domain.TaskReview, domain.TaskDoing}, {domain.TaskDone, domain.TaskDone}, {domain.TaskDoing, domain.TaskDoing}} {
		f := newRunFixture(t)
		ctx := context.Background()
		to := tc.from
		if _, err := f.tasks.Update(ctx, f.task.ID, TaskPatch{State: &to, Version: f.task.Version}); err != nil {
			t.Fatal(err)
		}
		r := f.newRun(t)
		if _, err := f.runs.MarkStarted(ctx, r.ID, Started{PID: 1}); err != nil {
			t.Fatal(err)
		}
		if got := f.taskState(t); got != tc.want {
			t.Errorf("task in %s after its agent started: now %s, want %s", tc.from, got, tc.want)
		}
	}
}

func TestQuestionsAndAnswers(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)

	q1, run, err := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Kind: domain.QuestionApproval, Prompt: "Run npm test?", Options: []string{"Allow", " ", "Deny"}})
	if err != nil {
		t.Fatal(err)
	}
	if run.State != domain.RunWaitingForUser || run.Waiting != domain.WaitQuestion || q1.Status != domain.QuestionPending || len(q1.Options) != 2 {
		t.Fatalf("run = %+v, q = %+v", run, q1)
	}
	evs := f.drain()
	wantTypes(t, evs, domain.EventRunStateChanged, domain.EventAgentQuestion)

	// Parallel tool calls ask for several approvals at once.
	q2, _, err := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "Which branch?"})
	if err != nil {
		t.Fatal(err)
	}
	if q2.Kind != domain.QuestionAsk {
		t.Fatalf("kind defaults to ask: %+v", q2)
	}
	wantTypes(t, f.drain(), domain.EventAgentQuestion) // no state change: it was already waiting

	if _, _, err := f.runs.AnswerQuestion(ctx, q1.ID, "   "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank answer: err = %v", err)
	}
	if _, run, err = f.runs.AnswerQuestion(ctx, q1.ID, "Allow"); err != nil {
		t.Fatal(err)
	}
	if run.State != domain.RunWaitingForUser {
		t.Fatalf("one question is still open, the run must keep waiting: %+v", run)
	}
	wantTypes(t, f.drain(), domain.EventQuestionAnswered, domain.EventAgentOutput)

	if _, _, err := f.runs.AnswerQuestion(ctx, q1.ID, "Allow"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("answering twice: err = %v", err)
	}
	answered, run, err := f.runs.AnswerQuestion(ctx, q2.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if answered.Status != domain.QuestionAnswered || answered.Answer != "main" || answered.AnsweredAt == nil {
		t.Fatalf("answered = %+v", answered)
	}
	if run.State != domain.RunRunning || run.Waiting != domain.WaitNone {
		t.Fatalf("the last answer resumes the run: %+v", run)
	}
	evs = f.drain()
	wantTypes(t, evs, domain.EventQuestionAnswered, domain.EventAgentOutput, domain.EventRunStateChanged, domain.EventAgentResumed)
	if out := payload[domain.AgentOutput](t, evs[1]); out.Stream != domain.StreamUser || out.Text != "main" {
		t.Fatalf("the answer should appear in the activity as the user's words: %+v", out)
	}
	if left, _ := f.runs.ListPendingQuestions(ctx); len(left) != 0 {
		t.Fatalf("pending = %+v", left)
	}
	if _, _, err := f.runs.AnswerQuestion(ctx, "qst_missing", "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown question: err = %v", err)
	}
}

func TestQuestionRefusedWhenNotRunning(t *testing.T) {
	f := newRunFixture(t)
	r := f.newRun(t) // still starting
	if _, _, err := f.runs.RecordQuestion(context.Background(), r.ID, NewQuestion{Prompt: "?"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
}

func TestWithdrawnQuestionReleasesTheRun(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	q, _, err := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "?"})
	if err != nil {
		t.Fatal(err)
	}
	f.drain()
	if err := f.runs.CancelQuestion(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.runs.Get(ctx, r.ID)
	if got.State != domain.RunRunning {
		t.Fatalf("the agent gave up waiting, so it is working again: %+v", got)
	}
	if stored, _ := f.runs.GetQuestion(ctx, q.ID); stored.Status != domain.QuestionCancelled {
		t.Fatalf("question = %+v", stored)
	}
	wantTypes(t, f.drain(), domain.EventQuestionAnswered, domain.EventRunStateChanged, domain.EventAgentResumed)
	if err := f.runs.CancelQuestion(ctx, q.ID); err != nil {
		t.Fatalf("cancelling twice is harmless: %v", err)
	}
}

func TestIdleAndMessages(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)

	// A message while the agent works is queued by it and changes no state.
	if _, err := f.runs.Message(ctx, r.ID, "also add tests"); err != nil {
		t.Fatal(err)
	}
	wantTypes(t, f.drain(), domain.EventAgentOutput)

	idle, err := f.runs.MarkIdle(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if idle.State != domain.RunWaitingForUser || idle.Waiting != domain.WaitIdle {
		t.Fatalf("idle = %+v", idle)
	}
	evs := f.drain()
	wantTypes(t, evs, domain.EventRunStateChanged, domain.EventAgentWaiting)
	if why := payload[map[string]string](t, evs[1])["reason"]; why != "turn_complete" {
		t.Fatalf("waiting reason = %q", why)
	}
	if again, err := f.runs.MarkIdle(ctx, r.ID); err != nil || again.State != domain.RunWaitingForUser || len(f.drain()) != 0 {
		t.Fatal("a second turn end while idle is a no-op")
	}

	// The card was moved on by the user while the agent was idle.
	review := domain.TaskReview
	ts, _ := f.tasks.List(ctx, f.project.ID)
	if _, err := f.tasks.Update(ctx, f.task.ID, TaskPatch{State: &review, Version: ts[0].Version}); err != nil {
		t.Fatal(err)
	}
	f.drain()

	resumed, err := f.runs.Message(ctx, r.ID, "looks good, one more thing")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.State != domain.RunRunning {
		t.Fatalf("a message to an idle agent resumes it: %+v", resumed)
	}
	if f.taskState(t) != domain.TaskDoing {
		t.Fatal("work resumed, so the card goes back to Doing")
	}
	evs = f.drain()
	wantTypes(t, evs, domain.EventAgentOutput, domain.EventTaskUpdated, domain.EventRunStateChanged, domain.EventAgentResumed)
	if out := payload[domain.AgentOutput](t, evs[0]); out.Stream != domain.StreamUser || out.Text != "looks good, one more thing" {
		t.Fatalf("output = %+v", out)
	}

	// No message while a question is open, or after the end.
	if _, _, err := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.Message(ctx, r.ID, "hello?"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("message while a question is open: err = %v", err)
	}
	if err := f.runs.CheckMessageable(ctx, r.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("CheckMessageable: err = %v", err)
	}
	if _, err := f.runs.MarkIdle(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.runs.Get(ctx, r.ID); got.Waiting != domain.WaitQuestion {
		t.Fatalf("a turn end must not hide an open question: %+v", got)
	}
	if _, err := f.runs.End(ctx, r.ID, Ended{State: domain.RunStopped, Reason: "stopped"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.Message(ctx, r.ID, "late"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("message after the end: err = %v", err)
	}
}

func TestEnd(t *testing.T) {
	cases := []struct {
		state domain.RunState
		event domain.EventType
	}{
		{domain.RunCompleted, domain.EventAgentCompleted},
		{domain.RunFailed, domain.EventAgentFailed},
		{domain.RunStopped, domain.EventAgentStopped},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			f := newRunFixture(t)
			ctx := context.Background()
			r := f.running(t)
			q, _, err := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "?"})
			if err != nil {
				t.Fatal(err)
			}
			f.drain()

			code := 3
			ended, err := f.runs.End(ctx, r.ID, Ended{State: tc.state, Reason: "because", ExitCode: &code})
			if err != nil {
				t.Fatal(err)
			}
			if ended.State != tc.state || ended.Reason != "because" || ended.EndedAt == nil || ended.PID != 0 || ended.ProcessID != "" ||
				ended.ExitCode == nil || *ended.ExitCode != 3 || ended.Waiting != domain.WaitNone {
				t.Fatalf("ended = %+v", ended)
			}
			if stored, _ := f.runs.GetQuestion(ctx, q.ID); stored.Status != domain.QuestionCancelled {
				t.Fatalf("a question nobody can answer any more must be cancelled: %+v", stored)
			}
			wantTypes(t, f.drain(), domain.EventQuestionAnswered, domain.EventRunStateChanged, tc.event)

			if _, err := f.runs.End(ctx, r.ID, Ended{State: domain.RunFailed}); !errors.Is(err, domain.ErrTransition) {
				t.Fatalf("ending twice: err = %v", err)
			}
		})
	}
	f := newRunFixture(t)
	if _, err := f.runs.End(context.Background(), f.running(t).ID, Ended{State: domain.RunRunning}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("End to a non-final state: err = %v", err)
	}
}

func TestAppendOutput(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)

	long := strings.Repeat("é", maxOutputBytes) // 2 bytes each: must be cut on a character boundary
	if err := f.runs.AppendOutput(ctx, r.ID, []OutputItem{
		{domain.StreamAssistant, "I'll start with the\n  parser."},
		{domain.StreamStderr, "warning: noisy"},
		{domain.StreamTool, "Edit   internal/parser.go"},
		{domain.StreamSystem, "   "},
		{domain.OutputStream("bogus"), long},
	}); err != nil {
		t.Fatal(err)
	}
	evs := f.drain()
	wantTypes(t, evs, domain.EventAgentOutput, domain.EventAgentOutput, domain.EventAgentOutput, domain.EventAgentOutput)
	if o := payload[domain.AgentOutput](t, evs[3]); o.Stream != domain.StreamSystem || len(o.Text) > maxOutputBytes || !strings.HasSuffix(o.Text, "(cut)") {
		t.Fatalf("an unknown stream becomes a system notice and long text is cut: stream %s, %d bytes", o.Stream, len(o.Text))
	}
	if !json.Valid(evs[3].Payload) {
		t.Fatal("a cut must not split a multi-byte character")
	}

	got, _ := f.runs.Get(ctx, r.ID)
	if got.Activity != "Edit internal/parser.go" || got.ActivityAt == nil {
		t.Fatalf("activity = %q; the card shows the latest thing the agent did, not stderr or notices", got.Activity)
	}
	if got.Version != r.Version {
		t.Fatalf("version %d -> %d: activity is not a state change and must not bump it", r.Version, got.Version)
	}

	if err := f.runs.AppendOutput(ctx, r.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.runs.AppendOutput(ctx, "run_missing", []OutputItem{{domain.StreamAssistant, "x"}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown run: err = %v", err)
	}
}

func TestSetSessionRef(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	if err := f.runs.SetSessionRef(ctx, r.ID, "sess-2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.runs.Get(ctx, r.ID); got.SessionRef != "sess-2" {
		t.Fatalf("ref = %q", got.SessionRef)
	}
	if err := f.runs.SetSessionRef(ctx, r.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.runs.Get(ctx, r.ID); got.SessionRef != "sess-2" {
		t.Fatal("an empty ref must not erase the real one")
	}
}

func TestInterrupt(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()

	t.Run("working run fails", func(t *testing.T) {
		r := f.running(t)
		got, err := f.runs.Interrupt(ctx, r.ID, "interrupted: controller shut down")
		if err != nil {
			t.Fatal(err)
		}
		if got.State != domain.RunFailed || got.Reason != "interrupted: controller shut down" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("idle run with a session is kept", func(t *testing.T) {
		r := f.running(t)
		if _, err := f.runs.MarkIdle(ctx, r.ID); err != nil {
			t.Fatal(err)
		}
		f.drain()
		got, err := f.runs.Interrupt(ctx, r.ID, "interrupted: controller shut down")
		if err != nil {
			t.Fatal(err)
		}
		if got.State != domain.RunWaitingForUser || got.Waiting != domain.WaitIdle || got.PID != 0 || got.ProcessID != "" {
			t.Fatalf("got %+v", got)
		}
		evs := f.drain()
		wantTypes(t, evs, domain.EventRunStateChanged, domain.EventAgentWaiting)
		if why := payload[map[string]string](t, evs[1])["reason"]; why != "interrupted" {
			t.Fatalf("reason = %q", why)
		}
		// A message to it later resumes it (the runtime starts a process for that).
		if _, err := f.runs.Interrupt(ctx, r.ID, "again"); err != nil {
			t.Fatalf("interrupting twice is harmless: %v", err)
		}
		if _, err := f.runs.End(ctx, r.ID, Ended{State: domain.RunStopped}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestEventsPageThroughARunsHistory(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	for i := 0; i < 5; i++ {
		if err := f.runs.AppendOutput(ctx, r.ID, []OutputItem{{domain.StreamAssistant, strings.Repeat("a", i+1)}}); err != nil {
			t.Fatal(err)
		}
	}
	newest, err := f.runs.Events(ctx, r.ID, 0, 2)
	if err != nil || len(newest) != 2 {
		t.Fatalf("newest = %+v, %v", newest, err)
	}
	if o := payload[domain.AgentOutput](t, newest[1]); o.Text != "aaaaa" {
		t.Fatalf("the newest page ends with the latest event: %+v", o)
	}
	older, _ := f.runs.Events(ctx, r.ID, newest[0].Seq, 100)
	if len(older) < 3 || older[len(older)-1].Seq >= newest[0].Seq {
		t.Fatalf("older = %+v", older)
	}
	if _, err := f.runs.Events(ctx, "run_missing", 0, 10); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown run: err = %v", err)
	}
}

func TestLatestRunsAndHistoryByTask(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	other, _ := f.tasks.Create(ctx, f.project.ID, "Other", "")

	first := f.newRun(t)
	if _, err := f.runs.End(ctx, first.ID, Ended{State: domain.RunFailed, Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	second := f.newRun(t)

	latest, err := f.runs.ListLatestByProject(ctx, f.project.ID)
	if err != nil || len(latest) != 1 || latest[0].ID != second.ID {
		t.Fatalf("latest = %+v, %v; want only the newest run of the one task that has run", latest, err)
	}
	history, err := f.runs.ListByTask(ctx, f.task.ID)
	if err != nil || len(history) != 2 || history[0].ID != first.ID {
		t.Fatalf("history = %+v, %v", history, err)
	}
	if h, err := f.runs.ListByTask(ctx, other.ID); err != nil || len(h) != 0 {
		t.Fatalf("a task that never ran: %+v, %v", h, err)
	}
	if _, err := f.runs.ListByTask(ctx, "tsk_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown task: err = %v", err)
	}
	if _, err := f.runs.ListLatestByProject(ctx, "prj_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown project: err = %v", err)
	}
}

func TestNothingIsPublishedWhenATransactionFails(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	// Ending a run twice fails after the first End has committed; the failed
	// attempt must not publish anything.
	if _, err := f.runs.End(ctx, r.ID, Ended{State: domain.RunCompleted}); err != nil {
		t.Fatal(err)
	}
	f.drain()
	_, _ = f.runs.End(ctx, r.ID, Ended{State: domain.RunFailed})
	if got := f.drain(); len(got) != 0 {
		t.Fatalf("published %v for a failed transaction", types(got))
	}
	// And the log agrees with what was published.
	var logged int
	_ = f.deps.Store.View(ctx, func(tx store.Tx) error {
		evs, _ := tx.Events().ListByRun(ctx, r.ID, 0, 1000)
		logged = len(evs)
		return nil
	})
	if logged == 0 {
		t.Fatal("events were not logged")
	}
}

package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

// These tests drive the whole question/answer lifecycle through the Manager
// with a scripted agent (internal/agent/fake), a real database and a real Git
// worktree. Nothing in them depends on timing beyond waiting for the
// controller to record what the agent did: races are staged with barriers and
// the adapter's OnRespond hook, not with sleeps.

func (e *env) pending() []domain.Question {
	e.t.Helper()
	qs, err := e.runs.ListPendingQuestions(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return qs
}

// waitPending waits until exactly n questions are pending and returns them, oldest first.
func (e *env) waitPending(n int) []domain.Question {
	e.t.Helper()
	eventually(e.t, fmt.Sprintf("%d pending question(s)", n), func() bool { return len(e.pending()) == n })
	return e.pending()
}

func (e *env) question(id string) *domain.Question {
	e.t.Helper()
	q, err := e.runs.GetQuestion(ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return q
}

// eventsOf returns the run's persisted events of one type, oldest first.
func (e *env) eventsOf(runID string, t domain.EventType) []domain.Event {
	var out []domain.Event
	for _, ev := range e.log(runID) {
		if ev.Type == t {
			out = append(out, ev)
		}
	}
	return out
}

// questionEventTypes is the run's question lifecycle as the log tells it.
func (e *env) questionEventTypes(runID string) []domain.EventType {
	var out []domain.EventType
	for _, ev := range e.log(runID) {
		switch ev.Type {
		case domain.EventAgentQuestion, domain.EventQuestionAnswered, domain.EventQuestionCancelled:
			out = append(out, ev.Type)
		}
	}
	return out
}

// restart stops the controller cleanly, or, if crash is set, not at all, and starts a new one over the same data.
func (e *env) restart(crash bool) {
	e.t.Helper()
	if !crash {
		if err := e.mgr.Shutdown(ctx); err != nil {
			e.t.Fatal(err)
		}
	}
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	if err := e.mgr.Recover(ctx); err != nil {
		e.t.Fatal(err)
	}
}

func asClosed(t *testing.T, err error) *domain.QuestionClosedError {
	t.Helper()
	var closed *domain.QuestionClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("err = %v, want a *domain.QuestionClosedError", err)
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	return closed
}

// ---- the lifecycle ----

func TestQuestionLifecycleFromAskToResume(t *testing.T) {
	e := newEnv(t)
	task := e.task("Pick a database")
	run := e.start(task)
	s := e.session()

	// While the answer is being handed to the agent, look at what is stored: the
	// answer must already be durable, and the run must not claim to be running yet.
	var qid string // set before the answer is given
	var atDelivery struct {
		q   *domain.Question
		run *domain.Run
	}
	s.OnRespond = func(string, string) error {
		atDelivery.q, atDelivery.run = e.question(qid), e.run(run.ID)
		return nil
	}

	s.Assistant("I found two candidates.")
	s.AskQuestion(agent.Question{
		Ref: "ref-1", Kind: domain.QuestionDecision, Prompt: "Use Postgres or SQLite?",
		Context: "Postgres: robust, needs a server.\nSQLite: embedded, single writer.",
		Options: []string{"Postgres", "SQLite"}, AllowFreeText: true,
	})
	waiting := e.waitWaiting(run.ID, domain.WaitQuestion)

	// The controller persisted a self-contained question and the run is waiting for the user.
	qs := e.waitPending(1)
	q := qs[0]
	if q.RunID != run.ID || q.TaskID != task.ID || q.ProjectID != e.project.ID || q.Kind != domain.QuestionDecision ||
		q.Prompt != "Use Postgres or SQLite?" || !strings.Contains(q.Context, "single writer") ||
		len(q.Options) != 2 || !q.AllowFreeText || q.State != domain.QuestionPending || q.AskedAt.IsZero() ||
		q.Answer != "" || q.AnsweredAt != nil || q.DeliveredAt != nil {
		t.Fatalf("question = %+v", q)
	}

	// The task stays in Doing: there is no "Needs input" column.
	if got := e.taskState(task.ID); got != domain.TaskDoing {
		t.Fatalf("task = %s while its agent waits for an answer", got)
	}
	if len(waiting.Prompt) == 0 || waiting.Waiting != domain.WaitQuestion {
		t.Fatalf("run = %+v", waiting)
	}

	// Connected clients are told, with the whole question, by an event that is in the durable log.
	asked := e.eventsOf(run.ID, domain.EventAgentQuestion)
	if len(asked) != 1 || asked[0].TaskID != task.ID || asked[0].ProjectID != e.project.ID || asked[0].Seq == 0 {
		t.Fatalf("agent.question events = %+v", asked)
	}

	// The user answers.
	qid = q.ID
	got, err := e.mgr.Answer(ctx, q.ID, "SQLite")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.QuestionAnswered || got.Answer != "SQLite" || got.AnsweredAt == nil || got.DeliveredAt == nil {
		t.Fatalf("returned = %+v", got)
	}
	if atDelivery.q == nil || atDelivery.q.State != domain.QuestionAnswered || atDelivery.q.Answer != "SQLite" || atDelivery.q.DeliveredAt != nil {
		t.Fatalf("at delivery the answer was %+v; it must be persisted before the agent sees it", atDelivery.q)
	}
	if atDelivery.run.State != domain.RunWaitingForUser {
		t.Fatalf("at delivery the run was %s; it only runs again once the agent has the answer", atDelivery.run.State)
	}

	// It went to the same session, once, and the run works again.
	if n := len(e.adapter.Sessions()); n != 1 {
		t.Fatalf("%d sessions; the answer must reach the session that asked", n)
	}
	if r := s.Responses(); len(r) != 1 || r[0].Ref != "ref-1" || r[0].Answer != "SQLite" {
		t.Fatalf("the agent was given %+v", r)
	}
	if r := e.run(run.ID); r.State != domain.RunRunning || r.Waiting != domain.WaitNone {
		t.Fatalf("run = %+v", r)
	}
	if got := e.taskState(task.ID); got != domain.TaskDoing {
		t.Fatalf("task = %s", got)
	}

	// The log tells the whole story, in order, and the agent carries on.
	want := []domain.EventType{domain.EventAgentQuestion, domain.EventQuestionAnswered}
	if got := e.questionEventTypes(run.ID); !sameTypes(got, want) {
		t.Fatalf("question events = %v", got)
	}
	if got := e.agentTypes(run.ID); !sameTypes(got, []domain.EventType{domain.EventAgentStarted, domain.EventAgentQuestion, domain.EventAgentResumed}) {
		t.Fatalf("timeline = %v", got)
	}
	s.Assistant("SQLite it is.")
	s.TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
	if out := e.outputs(run.ID); out[len(out)-1].Text != "SQLite it is." {
		t.Fatalf("output = %+v", out)
	}
}

// Every kind of request an agent can make is asked, validated and answered the same way.
func TestEveryKindOfQuestionCanBeAskedAndAnswered(t *testing.T) {
	for _, tc := range []struct {
		name    string
		q       agent.Question
		refused []string // answers the controller must turn away
		answer  string
		want    string
	}{
		{"clarification", agent.Question{Kind: domain.QuestionClarification, Prompt: "Which table holds the users?"},
			[]string{"", "   "}, "  the accounts table ", "the accounts table"},
		{"decision", agent.Question{Kind: domain.QuestionDecision, Prompt: "Ship it?", Options: []string{"Yes", "No"}, AllowFreeText: true},
			[]string{""}, "yes, but behind a flag", "yes, but behind a flag"},
		{"approval", agent.Question{Kind: domain.QuestionApproval, Prompt: "Run this command?", Context: "$ rm -rf build", Options: []string{"Allow", "Deny"}},
			[]string{"", "Maybe", "yes please"}, "deny", "Deny"},
		{"selection", agent.Question{Kind: domain.QuestionSelection, Prompt: "Which of these?", Options: []string{"A", "B", "C"}},
			[]string{"D", ""}, "b", "B"},
		{"instruction", agent.Question{Kind: domain.QuestionInstruction, Prompt: "What should I do next?"},
			[]string{" "}, "Write the migration", "Write the migration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := e.start(e.task(tc.name))
			s := e.session()
			tc.q.Ref = "ref"
			s.AskQuestion(tc.q)
			q := e.waitPending(1)[0]
			if q.Kind != tc.q.Kind || q.Prompt != tc.q.Prompt || q.Context != tc.q.Context {
				t.Fatalf("recorded %+v", q)
			}

			for _, bad := range tc.refused {
				if _, err := e.mgr.Answer(ctx, q.ID, bad); !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("answer %q: err = %v", bad, err)
				}
			}
			if got := e.question(q.ID); !got.Pending() || len(s.Responses()) != 0 || e.run(run.ID).State != domain.RunWaitingForUser {
				t.Fatalf("a refused answer must change nothing, and the agent must not hear of it: %+v", got)
			}

			if _, err := e.mgr.Answer(ctx, q.ID, tc.answer); err != nil {
				t.Fatal(err)
			}
			if r := s.Responses(); len(r) != 1 || r[0].Answer != tc.want {
				t.Fatalf("the agent was given %+v, want %q", r, tc.want)
			}
			if e.run(run.ID).State != domain.RunRunning {
				t.Fatal("the run should be working again")
			}
		})
	}
}

// ---- no client needs to be connected ----

func TestPendingQuestionIsRecoverableFromControllerState(t *testing.T) {
	e := newEnv(t)
	task := e.task("Needs a human")
	run := e.start(task)

	// Nobody has the app open. The log is the only thing a client that connects
	// later has, and the last event it saw is its resume point.
	var before int64
	if evs := e.log(run.ID); len(evs) > 0 {
		before = evs[len(evs)-1].Seq
	}
	e.session().Ask("r", "Overwrite config.yaml?", "Overwrite", "Keep")
	e.waitPending(1)

	// A browser opening now (or a second one) finds it in controller state...
	for client := 1; client <= 2; client++ {
		qs := e.pending()
		if len(qs) != 1 || qs[0].Prompt != "Overwrite config.yaml?" || qs[0].RunID != run.ID {
			t.Fatalf("client %d sees pending = %+v", client, qs)
		}
	}
	// ...and a client that was connected before, and missed it, finds it by resuming from its last event.
	var missed []domain.Event
	if err := e.db.View(ctx, func(tx storeTx) error {
		var err error
		missed, err = tx.Events().ListAfter(ctx, before, 100)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ev := range missed {
		if ev.Type == domain.EventAgentQuestion && ev.RunID == run.ID && ev.TaskID == task.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("resuming after seq %d replays %d events but not the question", before, len(missed))
	}

	// It is still there however long it takes, and a different client can answer it.
	if _, err := e.mgr.Answer(ctx, e.pending()[0].ID, "Keep"); err != nil {
		t.Fatal(err)
	}
	if len(e.pending()) != 0 {
		t.Fatal("answered questions are no longer pending, for every client")
	}
}

// A restart takes the agent's process with it. What was asked can no longer be
// answered, the controller says so, and the conversation can be resumed.
func TestControllerRestartWhileAQuestionIsOpen(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash=%v", crash), func(t *testing.T) {
			e := newEnv(t)
			run := e.start(e.task("Interrupted"))
			e.session().Ref("sess-q")
			eventually(t, "the session ref", func() bool { return e.run(run.ID).SessionRef == "sess-q" })
			e.session().Ask("r", "Proceed?")
			q := e.waitPending(1)[0]

			e.restart(crash)

			got := e.question(q.ID)
			if got.State != domain.QuestionCancelled || got.CancelReason != domain.CancelInterrupted || got.ClosedAt == nil {
				t.Fatalf("question = %+v", got)
			}
			if len(e.pending()) != 0 {
				t.Fatal("a question nobody can answer is not pending")
			}
			if r := e.run(run.ID); r.State != domain.RunWaitingForUser || r.Waiting != domain.WaitIdle || r.PID != 0 {
				t.Fatalf("run = %+v; it waits for a message to resume it", r)
			}

			// An answer from a phone that still showed the question is told exactly that.
			_, err := e.mgr.Answer(ctx, q.ID, "Yes")
			closed := asClosed(t, err)
			if closed.AlreadyAnswered() || closed.Question.CancelReason != domain.CancelInterrupted || !strings.Contains(err.Error(), "Send a message to continue") {
				t.Fatalf("err = %v", err)
			}
			if len(e.adapter.Sessions()) != 1 && !crash {
				t.Fatal("answering a closed question must not start an agent")
			}

			// The way forward is a message, which resumes the very same session.
			if err := e.mgr.Send(ctx, run.ID, "Yes, proceed."); err != nil {
				t.Fatal(err)
			}
			if req := e.session().Req; req.ResumeRef != "sess-q" || req.RunID != run.ID {
				t.Fatalf("resume request = %+v", req)
			}
			if e.run(run.ID).State != domain.RunRunning {
				t.Fatal("the run should be working again")
			}
		})
	}
}

// The user's answer is recorded before it is handed to the agent. If the
// controller dies in between, the answer is not lost: it is kept on a question
// that says it never reached the agent.
func TestAnswerRecordedButNotDeliveredSurvivesACrash(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Crash in the middle"))
	e.session().Ref("sess-x")
	eventually(t, "the session ref", func() bool { return e.run(run.ID).SessionRef == "sess-x" })
	e.session().Ask("r", "Which branch?")
	q := e.waitPending(1)[0]

	// What the controller had written when it died: step 1 of answering.
	if _, err := e.runs.AcceptAnswer(ctx, q.ID, "release/2"); err != nil {
		t.Fatal(err)
	}
	if got := e.question(q.ID); got.DeliveredAt != nil || got.State != domain.QuestionAnswered {
		t.Fatalf("question = %+v", got)
	}
	e.restart(true)

	got := e.question(q.ID)
	if got.State != domain.QuestionCancelled || got.CancelReason != domain.CancelInterrupted || got.Answer != "release/2" || got.AnsweredAt == nil || got.DeliveredAt != nil {
		t.Fatalf("question = %+v; the answer is kept but was never delivered", got)
	}
	if r := e.run(run.ID); r.State != domain.RunWaitingForUser || r.Waiting != domain.WaitIdle {
		t.Fatalf("run = %+v", r)
	}
}

// ---- races ----

func TestDuplicateAnswerIsIdempotent(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Retried"))
	s := e.session()
	s.Ask("r", "Proceed?", "Yes", "No")
	q := e.waitPending(1)[0]

	first, err := e.mgr.Answer(ctx, q.ID, "Yes")
	if err != nil {
		t.Fatal(err)
	}
	// The reply was lost; the phone tries again, with the same answer typed in another case.
	again, err := e.mgr.Answer(ctx, q.ID, "yes")
	if err != nil {
		t.Fatalf("a repeat of the answer already given must succeed: %v", err)
	}
	if again.ID != first.ID || again.Answer != "Yes" || again.State != domain.QuestionAnswered || again.DeliveredAt == nil {
		t.Fatalf("again = %+v", again)
	}
	if n := len(s.Responses()); n != 1 {
		t.Fatalf("the agent was told %d times; an answer is delivered once", n)
	}
	if n := len(e.eventsOf(run.ID, domain.EventQuestionAnswered)); n != 1 {
		t.Fatalf("%d question.answered events; a repeat must not announce itself again", n)
	}

	// A different answer is a different request, and loses.
	_, err = e.mgr.Answer(ctx, q.ID, "No")
	closed := asClosed(t, err)
	if !closed.AlreadyAnswered() || closed.Question.Answer != "Yes" || !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("err = %v", err)
	}
	if n := len(s.Responses()); n != 1 {
		t.Fatalf("the agent was told %d times", n)
	}
	if e.run(run.ID).State != domain.RunRunning {
		t.Fatal("the run should be working")
	}
}

func TestSimultaneousAnswersFromManyClientsHaveOneWinner(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Crowded"))
	s := e.session()
	s.Ask("r", "Which one?")
	q := e.waitPending(1)[0]

	const clients = 24
	var (
		wg      sync.WaitGroup
		ready   sync.WaitGroup
		start   = make(chan struct{})
		mu      sync.Mutex
		winners []string
		losers  []*domain.QuestionClosedError
		other   []error
	)
	ready.Add(clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			answer := fmt.Sprintf("answer from client %d", i)
			ready.Done()
			<-start
			got, err := e.mgr.Answer(ctx, q.ID, answer)
			mu.Lock()
			defer mu.Unlock()
			var closed *domain.QuestionClosedError
			switch {
			case err == nil:
				winners = append(winners, got.Answer)
			case errors.As(err, &closed):
				losers = append(losers, closed)
			default:
				other = append(other, err)
			}
		}(i)
	}
	ready.Wait()
	close(start)
	wg.Wait()

	if len(other) != 0 {
		t.Fatalf("unexpected errors: %v", other)
	}
	if len(winners) != 1 || len(losers) != clients-1 {
		t.Fatalf("%d winners and %d losers among %d clients", len(winners), len(losers), clients)
	}
	for _, l := range losers {
		if !l.AlreadyAnswered() || l.Question.Answer != winners[0] {
			t.Fatalf("a loser was told %+v, but the winning answer is %q", l.Question, winners[0])
		}
	}
	// Exactly one answer reached the agent, and it is the one that was recorded.
	if r := s.Responses(); len(r) != 1 || r[0].Answer != winners[0] {
		t.Fatalf("the agent was given %+v, the winner was %q", r, winners[0])
	}
	if got := e.question(q.ID); got.Answer != winners[0] || got.DeliveredAt == nil {
		t.Fatalf("question = %+v", got)
	}
	if n := len(e.eventsOf(run.ID, domain.EventQuestionAnswered)); n != 1 {
		t.Fatalf("%d question.answered events", n)
	}
	if e.run(run.ID).State != domain.RunRunning {
		t.Fatal("the run should be working")
	}
}

// Several clients all sending the same answer (a double tap on a flaky link) all succeed.
func TestSimultaneousIdenticalAnswersAllSucceed(t *testing.T) {
	e := newEnv(t)
	e.start(e.task("Double tap"))
	s := e.session()
	s.RequestApproval("r", "Run the tests?")
	q := e.waitPending(1)[0]

	const clients = 12
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	start := make(chan struct{})
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.mgr.Answer(ctx, q.ID, "Allow")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a duplicate of the winning answer failed: %v", err)
		}
	}
	if n := len(s.Responses()); n != 1 {
		t.Fatalf("the agent was told %d times", n)
	}
}

func TestMultipleQuestionsAreAnsweredIndependently(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Parallel"))
	s := e.session()
	s.RequestApproval("a", "Run npm test?")
	s.Ask("b", "Which port?", "3000", "8080")
	s.RequestApproval("c", "Edit main.go?")
	qs := e.waitPending(3)
	if qs[0].Prompt != "Run npm test?" || qs[1].Prompt != "Which port?" || qs[2].Prompt != "Edit main.go?" {
		t.Fatalf("questions come back in the order they were asked: %+v", qs)
	}

	// Answered in a different order, by two clients at once; each answer reaches its own question.
	var wg sync.WaitGroup
	for _, tc := range []struct {
		i      int
		answer string
	}{{2, "Deny"}, {1, "8080"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.mgr.Answer(ctx, qs[tc.i].ID, tc.answer); err != nil {
				t.Errorf("answering %q: %v", qs[tc.i].Prompt, err)
			}
		}()
	}
	wg.Wait()
	if r := e.run(run.ID); r.State != domain.RunWaitingForUser || r.Waiting != domain.WaitQuestion {
		t.Fatalf("run = %+v; one question is still open", r)
	}
	if left := e.pending(); len(left) != 1 || left[0].ID != qs[0].ID {
		t.Fatalf("pending = %+v", left)
	}
	if _, err := e.mgr.Answer(ctx, qs[0].ID, "Allow"); err != nil {
		t.Fatal(err)
	}
	if e.run(run.ID).State != domain.RunRunning {
		t.Fatal("answering the last question resumes the run")
	}
	byRef := map[string]string{}
	for _, r := range s.Responses() {
		byRef[r.Ref] = r.Answer
	}
	if len(byRef) != 3 || byRef["a"] != "Allow" || byRef["b"] != "8080" || byRef["c"] != "Deny" {
		t.Fatalf("answers by question = %v", byRef)
	}
}

func TestQuestionThatBecomesInvalidWhileTheUserIsAnswering(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Changed its mind"))
	s := e.session()
	s.Ask("r", "Delete the old tests?", "Yes", "No")
	q := e.waitPending(1)[0]

	// The user is reading it when the agent works it out for itself.
	s.WithdrawQuestion("r")
	eventually(t, "the question to be withdrawn", func() bool { return e.question(q.ID).State == domain.QuestionCancelled })
	if got := e.question(q.ID); got.CancelReason != domain.CancelWithdrawn {
		t.Fatalf("question = %+v", got)
	}
	if e.run(run.ID).State != domain.RunRunning {
		t.Fatal("nothing holds the agent up any more")
	}

	_, err := e.mgr.Answer(ctx, q.ID, "Yes")
	closed := asClosed(t, err)
	if closed.AlreadyAnswered() || closed.Question.CancelReason != domain.CancelWithdrawn || !strings.Contains(err.Error(), "no longer needs an answer") {
		t.Fatalf("err = %v", err)
	}
	if len(s.Responses()) != 0 {
		t.Fatal("the agent was given an answer to a question it had withdrawn")
	}
	want := []domain.EventType{domain.EventAgentQuestion, domain.EventQuestionCancelled}
	if got := e.questionEventTypes(run.ID); !sameTypes(got, want) {
		t.Fatalf("question events = %v", got)
	}
}

// The agent does not recognise the question when the answer arrives, though it never withdrew it.
func TestAnswerToAQuestionTheAgentNoLongerHasIsCancelledAndKept(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Forgetful"))
	s := e.session()
	s.Ask("r", "Proceed?")
	q := e.waitPending(1)[0]
	if err := s.Respond(ctx, "r", "from elsewhere"); err != nil { // the agent's side forgets it
		t.Fatal(err)
	}

	_, err := e.mgr.Answer(ctx, q.ID, "Yes")
	asClosed(t, err)
	got := e.question(q.ID)
	if got.State != domain.QuestionCancelled || got.CancelReason != domain.CancelWithdrawn || got.Answer != "Yes" || got.DeliveredAt != nil {
		t.Fatalf("question = %+v; the user's words are kept, marked undelivered", got)
	}
	if e.run(run.ID).State != domain.RunRunning {
		t.Fatal("the run is not held up by a question the agent does not have")
	}
	want := []domain.EventType{domain.EventAgentQuestion, domain.EventQuestionAnswered, domain.EventQuestionCancelled}
	if got := e.questionEventTypes(run.ID); !sameTypes(got, want) {
		t.Fatalf("question events = %v", got)
	}
}

// ---- the agent goes away ----

func TestAgentExitsWhileAQuestionIsOpen(t *testing.T) {
	for _, tc := range []struct {
		name  string
		end   func(e *env, s *fakeSession, run *domain.Run)
		state domain.RunState
	}{
		{"exits cleanly", func(_ *env, s *fakeSession, _ *domain.Run) { s.Exit(0, "") }, domain.RunCompleted},
		{"crashes", func(_ *env, s *fakeSession, _ *domain.Run) { s.Exit(2, "exited with status 2") }, domain.RunFailed},
		{"is stopped by the user", func(e *env, _ *fakeSession, run *domain.Run) {
			if _, err := e.mgr.Stop(ctx, run.ID); err != nil {
				e.t.Fatal(err)
			}
		}, domain.RunStopped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := e.start(e.task("Gone"))
			s := e.session()
			s.Ask("a", "First?")
			s.RequestApproval("b", "Second?")
			qs := e.waitPending(2)

			tc.end(e, s, run)
			e.waitState(run.ID, tc.state)

			// Both questions were closed, the world was told, and nothing is pending.
			for _, q := range qs {
				got := e.question(q.ID)
				if got.State != domain.QuestionCancelled || got.CancelReason != domain.CancelRunEnded || got.ClosedAt == nil {
					t.Fatalf("question = %+v", got)
				}
			}
			if len(e.pending()) != 0 {
				t.Fatal("a question nobody can answer must not be pending")
			}
			if n := len(e.eventsOf(run.ID, domain.EventQuestionCancelled)); n != 2 {
				t.Fatalf("%d question.cancelled events, want 2", n)
			}

			// The user, who still sees the questions on a phone, answers anyway.
			for _, q := range qs {
				_, err := e.mgr.Answer(ctx, q.ID, "Allow")
				closed := asClosed(t, err)
				if closed.AlreadyAnswered() || closed.Question.CancelReason != domain.CancelRunEnded || !strings.Contains(err.Error(), "agent stopped") {
					t.Fatalf("err = %v", err)
				}
			}
			if len(s.Responses()) != 0 {
				t.Fatal("an answer was given to an agent that is gone")
			}
			if got := e.run(run.ID); got.State != tc.state {
				t.Fatalf("run = %+v; an answer must not change how it ended", got)
			}
		})
	}
}

// The agent's process exits in the very moment an answer is being delivered.
func TestAgentExitsWhileTheAnswerIsBeingDelivered(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Dies mid-answer"))
	s := e.session()
	s.OnRespond = func(string, string) error { s.Exit(1, "killed"); return nil }
	s.Ask("r", "Proceed?")
	q := e.waitPending(1)[0]

	_, err := e.mgr.Answer(ctx, q.ID, "Yes")
	closed := asClosed(t, err)
	if closed.AlreadyAnswered() || !strings.Contains(err.Error(), "Your answer was not delivered") {
		t.Fatalf("err = %v; the user must be told that the answer did not get through", err)
	}
	got := e.question(q.ID)
	if got.State != domain.QuestionCancelled || got.CancelReason != domain.CancelRunEnded || got.Answer != "Yes" || got.DeliveredAt != nil {
		t.Fatalf("question = %+v", got)
	}
	e.waitState(run.ID, domain.RunFailed)
	if r := e.run(run.ID); r.Reason != "killed" {
		t.Fatalf("run = %+v", r)
	}
	if len(s.Responses()) != 0 {
		t.Fatal("the agent cannot have received it")
	}
}

// If the agent is still alive but cannot be given the answer, it is stuck on a
// request that will never be answered. The run is ended so that it is not left
// waiting for a user who has already replied.
func TestDeliveryFailureThatLeavesTheAgentStuckEndsTheRun(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Broken pipe"))
	s := e.session()
	s.OnRespond = func(string, string) error { return errors.New("write |1: broken pipe") }
	s.Ask("r", "Proceed?")
	q := e.waitPending(1)[0]

	_, err := e.mgr.Answer(ctx, q.ID, "Yes")
	if !errors.Is(err, domain.ErrAgent) || !strings.Contains(err.Error(), "broken pipe") {
		t.Fatalf("err = %v", err)
	}
	r := e.waitState(run.ID, domain.RunFailed)
	if !strings.Contains(r.Reason, "could not deliver an answer") {
		t.Fatalf("run = %+v", r)
	}
	got := e.question(q.ID)
	if got.State != domain.QuestionCancelled || got.CancelReason != domain.CancelRunEnded || got.Answer != "Yes" || got.DeliveredAt != nil {
		t.Fatalf("question = %+v", got)
	}
	if len(e.pending()) != 0 {
		t.Fatal("nothing should be left pending")
	}
}

// A user's answer made while the run is being stopped is not delivered.
func TestAnswerWhileTheRunIsEnding(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Stopping"))
	s := e.session()
	s.Ask("r", "Proceed?")
	q := e.waitPending(1)[0]

	stopping := make(chan struct{})
	release := make(chan struct{})
	s.OnStop = func(s *fakeSession) {
		close(stopping)
		<-release
		s.Exit(1, "terminated")
	}
	done := make(chan error, 1)
	go func() { _, err := e.mgr.Stop(ctx, run.ID); done <- err }()
	<-stopping // the stop is under way and the process has not gone yet

	if _, err := e.mgr.Answer(ctx, q.ID, "Yes"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if len(s.Responses()) != 0 {
		t.Fatal("an answer was delivered to a session that is being stopped")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.waitState(run.ID, domain.RunStopped)
	if got := e.question(q.ID); got.State != domain.QuestionCancelled || got.CancelReason != domain.CancelRunEnded {
		t.Fatalf("question = %+v", got)
	}
}

// ---- what the log offers a future notifier ----

func TestQuestionEventsAreSelfContained(t *testing.T) {
	e := newEnv(t)
	task := e.task("Notify me")
	run := e.start(task)
	s := e.session()
	s.AskQuestion(agent.Question{Ref: "a", Kind: domain.QuestionApproval, Prompt: "Run this?", Context: "$ make deploy", Options: []string{"Allow", "Deny"}})
	s.Ask("b", "Anything else?")
	qs := e.waitPending(2)
	if _, err := e.mgr.Answer(ctx, qs[0].ID, "Allow"); err != nil {
		t.Fatal(err)
	}
	s.Exit(1, "boom")
	e.waitState(run.ID, domain.RunFailed)

	type payload struct{ Question domain.Question }
	var asked, answered, cancelled []domain.Question
	for _, ev := range e.log(run.ID) {
		var p payload
		switch ev.Type {
		case domain.EventAgentQuestion, domain.EventQuestionAnswered, domain.EventQuestionCancelled:
			if ev.ProjectID != e.project.ID || ev.TaskID != task.ID || ev.RunID != run.ID || ev.Seq == 0 || ev.CreatedAt.IsZero() {
				t.Fatalf("%s: the envelope must say where it belongs: %+v", ev.Type, ev)
			}
			if err := json.Unmarshal(ev.Payload, &p); err != nil || p.Question.ID == "" || p.Question.TaskID != task.ID || p.Question.Prompt == "" {
				t.Fatalf("%s: payload %s must carry the question: %v", ev.Type, ev.Payload, err)
			}
		}
		switch ev.Type {
		case domain.EventAgentQuestion:
			asked = append(asked, p.Question)
		case domain.EventQuestionAnswered:
			answered = append(answered, p.Question)
		case domain.EventQuestionCancelled:
			cancelled = append(cancelled, p.Question)
		}
	}
	if len(asked) != 2 || len(answered) != 1 || len(cancelled) != 1 {
		t.Fatalf("asked %d, answered %d, cancelled %d", len(asked), len(answered), len(cancelled))
	}
	if asked[0].Context != "$ make deploy" || answered[0].Answer != "Allow" || cancelled[0].CancelReason != domain.CancelRunEnded || cancelled[0].ID != qs[1].ID {
		t.Fatalf("asked %+v, answered %+v, cancelled %+v", asked[0], answered[0], cancelled[0])
	}
}

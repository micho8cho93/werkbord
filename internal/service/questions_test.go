package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devboard/internal/domain"
)

// The three steps of answering are accept (record), deliver (the runtime's
// part) and confirm. These tests follow a question through all of them.
func TestQuestionsAndAnswers(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)

	q1, run, err := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{
		Kind: domain.QuestionApproval, Prompt: "Run this command?", Context: "$ npm test", Options: []string{"Allow", " ", "Deny"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.State != domain.RunWaitingForUser || run.Waiting != domain.WaitQuestion || q1.State != domain.QuestionPending || len(q1.Options) != 2 {
		t.Fatalf("run = %+v, q = %+v", run, q1)
	}
	if q1.TaskID != f.task.ID || q1.ProjectID != f.project.ID || q1.Context != "$ npm test" || q1.AllowFreeText || q1.AskedAt.IsZero() {
		t.Fatalf("a question is self-contained: %+v", q1)
	}
	evs := f.drain()
	wantTypes(t, evs, domain.EventRunStateChanged, domain.EventAgentQuestion)
	if got := payload[struct{ Question domain.Question }](t, evs[1]).Question; got.ID != q1.ID || got.TaskID != f.task.ID || evs[1].TaskID != f.task.ID {
		t.Fatalf("the event must carry the whole question: %+v", got)
	}

	// Parallel tool calls ask for several approvals at once.
	q2, _, err := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "Which branch?"})
	if err != nil {
		t.Fatal(err)
	}
	if q2.Kind != domain.QuestionClarification || !q2.AllowFreeText {
		t.Fatalf("a question without options is a clarification that takes text: %+v", q2)
	}
	wantTypes(t, f.drain(), domain.EventAgentQuestion) // no state change: it was already waiting

	if _, err := f.runs.AcceptAnswer(ctx, q1.ID, "   "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank answer: err = %v", err)
	}
	if _, err := f.runs.AcceptAnswer(ctx, q1.ID, "Maybe"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("an approval takes only its options: err = %v", err)
	}
	if got, _ := f.runs.GetQuestion(ctx, q1.ID); !got.Pending() {
		t.Fatalf("a refused answer changes nothing: %+v", got)
	}
	wantTypes(t, f.drain())

	// Step 1: the answer is recorded. The run does not move until the agent has it.
	acc, err := f.runs.AcceptAnswer(ctx, q1.ID, "allow")
	if err != nil {
		t.Fatal(err)
	}
	if acc.State != domain.QuestionAnswered || acc.Answer != "Allow" || acc.AnsweredAt == nil || acc.DeliveredAt != nil {
		t.Fatalf("accepted = %+v; the choice is recorded as spelled, and is not yet delivered", acc)
	}
	if got, _ := f.runs.Get(ctx, r.ID); got.State != domain.RunWaitingForUser {
		t.Fatalf("run = %+v; it waits until the agent has the answer", got)
	}
	wantTypes(t, f.drain(), domain.EventQuestionAnswered, domain.EventAgentOutput)

	// Step 3: delivery is confirmed. One question is still open, so the run keeps waiting.
	if _, err := f.runs.ConfirmDelivery(ctx, q1.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.runs.Get(ctx, r.ID); got.State != domain.RunWaitingForUser {
		t.Fatalf("one question is still open, the run must keep waiting: %+v", got)
	}
	wantTypes(t, f.drain())

	var closed *domain.QuestionClosedError
	if _, err := f.runs.AcceptAnswer(ctx, q1.ID, "Allow"); !errors.As(err, &closed) || !closed.AlreadyAnswered() || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("answering twice: err = %v", err)
	}

	if _, err := f.runs.AcceptAnswer(ctx, q2.ID, "main"); err != nil {
		t.Fatal(err)
	}
	wantTypes(t, f.drain(), domain.EventQuestionAnswered, domain.EventAgentOutput)
	answered, err := f.runs.ConfirmDelivery(ctx, q2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if answered.State != domain.QuestionAnswered || answered.Answer != "main" || answered.DeliveredAt == nil {
		t.Fatalf("answered = %+v", answered)
	}
	got, _ := f.runs.Get(ctx, r.ID)
	if got.State != domain.RunRunning || got.Waiting != domain.WaitNone {
		t.Fatalf("delivering the last answer resumes the run: %+v", got)
	}
	wantTypes(t, f.drain(), domain.EventRunStateChanged, domain.EventAgentResumed)
	if again, err := f.runs.ConfirmDelivery(ctx, q2.ID); err != nil || again.DeliveredAt == nil {
		t.Fatalf("confirming twice is harmless: %+v, %v", again, err)
	}
	wantTypes(t, f.drain())
	if left, _ := f.runs.ListPendingQuestions(ctx); len(left) != 0 {
		t.Fatalf("pending = %+v", left)
	}
	if _, err := f.runs.AcceptAnswer(ctx, "qst_missing", "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown question: err = %v", err)
	}
}

func TestRecordedQuestionsAreAlwaysAnswerable(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	for _, tc := range []struct {
		name string
		in   NewQuestion
		kind domain.QuestionKind
		opts []string
		free bool
	}{
		{"no kind, no options", NewQuestion{Prompt: "a"}, domain.QuestionClarification, nil, true},
		{"no kind, options", NewQuestion{Prompt: "b", Options: []string{"x", "y"}}, domain.QuestionSelection, []string{"x", "y"}, false},
		{"unknown kind", NewQuestion{Kind: "ask", Prompt: "c", Options: []string{"x"}, AllowFreeText: true}, domain.QuestionSelection, []string{"x"}, true},
		{"approval without options", NewQuestion{Kind: domain.QuestionApproval, Prompt: "d"}, domain.QuestionApproval, []string{"Allow", "Deny"}, false},
		{"options nobody can pick", NewQuestion{Kind: domain.QuestionDecision, Prompt: "e", Options: []string{" ", ""}}, domain.QuestionDecision, nil, true},
		{"instruction", NewQuestion{Kind: domain.QuestionInstruction, Prompt: "What next?"}, domain.QuestionInstruction, nil, true},
		{"blank prompt", NewQuestion{Prompt: "  "}, domain.QuestionClarification, nil, true},
	} {
		q, _, err := f.runs.RecordQuestion(ctx, r.ID, tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if q.Kind != tc.kind || q.AllowFreeText != tc.free || strings.Join(q.Options, ",") != strings.Join(tc.opts, ",") || q.Prompt == "" {
			t.Errorf("%s: recorded %+v", tc.name, q)
		}
	}
}

func TestQuestionLimitsAreEnforced(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	many := make([]string, 30)
	for i := range many {
		many[i] = strings.Repeat("o", 500)
	}
	q, _, err := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: strings.Repeat("p", 100<<10), Context: strings.Repeat("c", 100<<10), Options: many})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Prompt) > maxQuestionBytes || len(q.Context) > maxContextBytes || len(q.Options) != maxOptions || len(q.Options[0]) > maxOptionBytes {
		t.Fatalf("an agent must not make the controller store unbounded text: prompt %d, context %d, %d options", len(q.Prompt), len(q.Context), len(q.Options))
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
	if _, err := f.runs.CancelQuestion(ctx, q.ID, domain.CancelWithdrawn); err != nil {
		t.Fatal(err)
	}
	got, _ := f.runs.Get(ctx, r.ID)
	if got.State != domain.RunRunning {
		t.Fatalf("the agent gave up waiting, so it is working again: %+v", got)
	}
	stored, _ := f.runs.GetQuestion(ctx, q.ID)
	if stored.State != domain.QuestionCancelled || stored.CancelReason != domain.CancelWithdrawn || stored.ClosedAt == nil || stored.AnsweredAt != nil {
		t.Fatalf("question = %+v", stored)
	}
	wantTypes(t, f.drain(), domain.EventQuestionCancelled, domain.EventRunStateChanged, domain.EventAgentResumed)
	if _, err := f.runs.CancelQuestion(ctx, q.ID, domain.CancelRunEnded); err != nil {
		t.Fatalf("cancelling twice is harmless: %v", err)
	}
	if again, _ := f.runs.GetQuestion(ctx, q.ID); again.CancelReason != domain.CancelWithdrawn {
		t.Fatalf("the first reason stands: %+v", again)
	}
	wantTypes(t, f.drain())

	// A late answer is told what became of the question.
	var closed *domain.QuestionClosedError
	if _, err := f.runs.AcceptAnswer(ctx, q.ID, "yes"); !errors.As(err, &closed) || closed.AlreadyAnswered() || closed.Question.CancelReason != domain.CancelWithdrawn {
		t.Fatalf("err = %v", err)
	}
}

// An answer that was recorded but never reached the agent is cancelled with its
// answer kept, and does not bring the run back to life when the session is gone.
func TestUndeliveredAnswerIsCancelledAndKept(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	q, _, _ := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "?"})
	if _, err := f.runs.AcceptAnswer(ctx, q.ID, "use sqlite"); err != nil {
		t.Fatal(err)
	}
	f.drain()
	if _, err := f.runs.CancelQuestion(ctx, q.ID, domain.CancelRunEnded); err != nil {
		t.Fatal(err)
	}
	stored, _ := f.runs.GetQuestion(ctx, q.ID)
	if stored.State != domain.QuestionCancelled || stored.Answer != "use sqlite" || stored.AnsweredAt == nil || stored.DeliveredAt != nil {
		t.Fatalf("question = %+v", stored)
	}
	if got, _ := f.runs.Get(ctx, r.ID); got.State != domain.RunWaitingForUser {
		t.Fatalf("run = %+v; the session is ending, not resuming", got)
	}
	wantTypes(t, f.drain(), domain.EventQuestionCancelled)

	// Once delivered, a question is settled for good.
	q2, _, _ := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "again?"})
	_, _ = f.runs.AcceptAnswer(ctx, q2.ID, "yes")
	_, _ = f.runs.ConfirmDelivery(ctx, q2.ID)
	if _, err := f.runs.CancelQuestion(ctx, q2.ID, domain.CancelWithdrawn); !errors.Is(err, domain.ErrTransition) {
		t.Fatalf("cancelling a delivered answer: err = %v", err)
	}
}

// Ending a run closes every question that still holds the agent up, answered
// or not, each with a reason and an event so that clients drop it.
func TestEndingARunClosesItsQuestions(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	open, _, _ := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "open?"})
	inFlight, _, _ := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "in flight?"})
	settled, _, _ := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "settled?"})
	_, _ = f.runs.AcceptAnswer(ctx, inFlight.ID, "yes")
	_, _ = f.runs.AcceptAnswer(ctx, settled.ID, "yes")
	_, _ = f.runs.ConfirmDelivery(ctx, settled.ID)
	f.drain()

	if _, err := f.runs.End(ctx, r.ID, Ended{State: domain.RunFailed, Reason: "crashed"}); err != nil {
		t.Fatal(err)
	}
	get := func(id string) *domain.Question { q, _ := f.runs.GetQuestion(ctx, id); return q }
	if q := get(open.ID); q.State != domain.QuestionCancelled || q.CancelReason != domain.CancelRunEnded || q.Answer != "" {
		t.Errorf("open: %+v", q)
	}
	if q := get(inFlight.ID); q.State != domain.QuestionCancelled || q.Answer != "yes" || q.DeliveredAt != nil {
		t.Errorf("an answer the agent never got is cancelled, and keeps the user's words: %+v", q)
	}
	if q := get(settled.ID); q.State != domain.QuestionAnswered || q.DeliveredAt == nil {
		t.Errorf("a delivered answer is history, not cancelled: %+v", q)
	}
	wantTypes(t, f.drain(), domain.EventQuestionCancelled, domain.EventQuestionCancelled, domain.EventRunStateChanged, domain.EventAgentFailed)
}

// A restart cancels what was open, for a reason that says the session can be
// resumed, and leaves the run waiting for a message.
func TestInterruptCancelsQuestionsAsInterrupted(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t) // has a session ref
	q, _, _ := f.runs.RecordQuestion(ctx, r.ID, NewQuestion{Prompt: "?"})
	f.drain()

	if _, err := f.runs.Interrupt(ctx, r.ID, "interrupted: controller shut down"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.runs.GetQuestion(ctx, q.ID)
	if got.State != domain.QuestionCancelled || got.CancelReason != domain.CancelInterrupted {
		t.Fatalf("question = %+v", got)
	}
	if run, _ := f.runs.Get(ctx, r.ID); run.State != domain.RunWaitingForUser || run.Waiting != domain.WaitIdle {
		t.Fatalf("run = %+v; a message resumes it", run)
	}
	var closed *domain.QuestionClosedError
	_, err := f.runs.AcceptAnswer(ctx, q.ID, "yes")
	if !errors.As(err, &closed) || !strings.Contains(err.Error(), "Send a message to continue") {
		t.Fatalf("a late answer should be told how to carry on: %v", err)
	}
}

package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func pendingQuestion(opts []string, free bool) *Question {
	return &Question{ID: "qst_1", RunID: "run_1", Kind: QuestionDecision, Prompt: "Which?", Options: opts, AllowFreeText: free, State: QuestionPending, AskedAt: t0}
}

func TestCheckAnswer(t *testing.T) {
	choice := pendingQuestion([]string{"Postgres", "SQLite"}, false)
	choiceOrText := pendingQuestion([]string{"Postgres", "SQLite"}, true)
	text := pendingQuestion(nil, true)
	textNoFlag := pendingQuestion(nil, false) // a question with no options can only take text

	for _, tc := range []struct {
		name    string
		q       *Question
		in      string
		want    string
		invalid bool
	}{
		{"a choice, as spelled", choice, "SQLite", "SQLite", false},
		{"a choice, in another case and with space", choice, "  sqlite ", "SQLite", false},
		{"text where only choices are allowed", choice, "MySQL", "", true},
		{"text beside choices", choiceOrText, "MySQL, then", "MySQL, then", false},
		{"a choice typed into a free-text question is still the choice", choiceOrText, "postgres", "Postgres", false},
		{"free text", text, "  use the existing table  ", "use the existing table", false},
		{"no options means text", textNoFlag, "anything", "anything", false},
		{"blank", text, "   \n", "", true},
		{"blank choice", choice, "", "", true},
	} {
		got, err := tc.q.CheckAnswer(tc.in)
		if tc.invalid != (err != nil) || (err != nil && !errors.Is(err, ErrInvalid)) || got != tc.want {
			t.Errorf("%s: CheckAnswer(%q) = %q, %v", tc.name, tc.in, got, err)
		}
	}
	if _, err := choice.CheckAnswer("MySQL"); err == nil || !strings.Contains(err.Error(), "Postgres, SQLite") {
		t.Errorf("the refusal should list what is allowed: %v", err)
	}
}

func TestQuestionLifecycle(t *testing.T) {
	q := pendingQuestion([]string{"Allow", "Deny"}, false)
	if !q.Pending() || !q.Blocking() {
		t.Fatal("a new question is pending and holds the agent up")
	}

	if err := q.Accept("nope", t0); !errors.Is(err, ErrInvalid) || !q.Pending() {
		t.Fatalf("a refused answer changes nothing: %v, %+v", err, q)
	}
	if err := q.Accept("allow", t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if q.State != QuestionAnswered || q.Answer != "Allow" || q.AnsweredAt == nil || q.DeliveredAt != nil || !q.Blocking() {
		t.Fatalf("accepted = %+v; until delivered the answer still holds the agent up", q)
	}
	if q.SameAnswer("deny") || !q.SameAnswer(" ALLOW ") {
		t.Fatal("SameAnswer must match the recorded answer after checking, and nothing else")
	}

	var closed *QuestionClosedError
	if err := q.Accept("Deny", t0); !errors.As(err, &closed) || !closed.AlreadyAnswered() || !errors.Is(err, ErrConflict) {
		t.Fatalf("a second answer: %v", err)
	}
	if closed.Question == q || closed.Question.Answer != "Allow" {
		t.Fatal("the error carries a snapshot of the question as it is")
	}

	if err := q.MarkDelivered(t0.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if q.DeliveredAt == nil || q.Blocking() {
		t.Fatalf("delivered = %+v", q)
	}
	first := *q.DeliveredAt
	_ = q.MarkDelivered(t0.Add(time.Hour))
	if !q.DeliveredAt.Equal(first) {
		t.Fatal("delivery is recorded once")
	}
	if err := q.Cancel(CancelRunEnded, t0); !errors.Is(err, ErrTransition) {
		t.Fatalf("a delivered answer cannot be cancelled: %v", err)
	}
}

func TestQuestionCancel(t *testing.T) {
	q := pendingQuestion(nil, true)
	if err := q.Cancel("nonsense", t0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown reason: %v", err)
	}
	if err := q.MarkDelivered(t0); !errors.Is(err, ErrTransition) {
		t.Fatalf("nothing to deliver for a pending question: %v", err)
	}
	if err := q.Cancel(CancelWithdrawn, t0); err != nil {
		t.Fatal(err)
	}
	if q.State != QuestionCancelled || q.CancelReason != CancelWithdrawn || q.ClosedAt == nil || q.Blocking() || q.Pending() {
		t.Fatalf("cancelled = %+v", q)
	}
	if err := q.Cancel(CancelRunEnded, t0.Add(time.Hour)); err != nil || q.CancelReason != CancelWithdrawn {
		t.Fatalf("cancelling twice keeps the first reason: %v, %+v", err, q)
	}
	var closed *QuestionClosedError
	if err := q.Accept("late", t0); !errors.As(err, &closed) || closed.AlreadyAnswered() {
		t.Fatalf("a late answer: %v", err)
	}
	if q.SameAnswer("late") {
		t.Fatal("a cancelled question has no answer to repeat")
	}

	// An answer that was recorded but not delivered is cancelled, and keeps the answer.
	a := pendingQuestion(nil, true)
	_ = a.Accept("use sqlite", t0)
	if err := a.Cancel(CancelRunEnded, t0.Add(time.Second)); err != nil || a.State != QuestionCancelled || a.Answer != "use sqlite" || a.AnsweredAt == nil {
		t.Fatalf("answered, then cancelled: %v, %+v", err, a)
	}
}

func TestQuestionClosedErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		reason   CancelReason
		answered bool
		contains string
	}{
		{CancelRunEnded, false, "agent stopped"},
		{CancelInterrupted, false, "Send a message to continue"},
		{CancelWithdrawn, false, "no longer needs an answer"},
		{CancelRunEnded, true, "Your answer was not delivered"},
	} {
		q := pendingQuestion(nil, true)
		if tc.answered {
			_ = q.Accept("x", t0)
		}
		_ = q.Cancel(tc.reason, t0)
		err := (&QuestionClosedError{Question: q}).Error()
		if !strings.Contains(err, tc.contains) {
			t.Errorf("%s (answered=%v): %q does not mention %q", tc.reason, tc.answered, err, tc.contains)
		}
	}
	done := pendingQuestion(nil, true)
	_ = done.Accept("blue", t0)
	if msg := (&QuestionClosedError{Question: done}).Error(); !strings.Contains(msg, "already answered") || !strings.Contains(msg, "blue") {
		t.Errorf("message = %q", msg)
	}
}

func TestQuestionVocabularyIsClosed(t *testing.T) {
	for _, k := range []QuestionKind{QuestionClarification, QuestionDecision, QuestionApproval, QuestionSelection, QuestionInstruction} {
		if !k.Valid() {
			t.Errorf("%s should be valid", k)
		}
	}
	for _, k := range []QuestionKind{"", "ask", "Approval"} {
		if k.Valid() {
			t.Errorf("%q should not be valid", k)
		}
	}
	if !QuestionPending.Valid() || QuestionState("closed").Valid() {
		t.Error("question states")
	}
	if !CancelWithdrawn.Valid() || CancelReason("").Valid() {
		t.Error("cancel reasons")
	}
}

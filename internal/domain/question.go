package domain

import (
	"fmt"
	"strings"
	"time"
)

// QuestionState says where a question is in its life.
//
//	pending ──▶ answered      the user's answer is recorded (and then delivered)
//	   │           │
//	   └──▶ cancelled ◀──────┘ the agent could not use an answer: see CancelReason
//
// An answered question has been delivered once DeliveredAt is set. Until then
// the controller is still handing the answer to the agent, and if that fails
// the question is cancelled, keeping the answer so nothing the user wrote is lost.
type QuestionState string

const (
	QuestionPending   QuestionState = "pending"
	QuestionAnswered  QuestionState = "answered"
	QuestionCancelled QuestionState = "cancelled"
)

// Valid reports whether s is a known question state.
func (s QuestionState) Valid() bool {
	return s == QuestionPending || s == QuestionAnswered || s == QuestionCancelled
}

// CancelReason says why a question was closed without its answer reaching the agent.
type CancelReason string

const (
	// CancelRunEnded: the session ended (the process exited, failed or was
	// stopped) while the question was open.
	CancelRunEnded CancelReason = "run_ended"
	// CancelInterrupted: the controller restarted or shut down. The agent's
	// request died with its process; the run can be resumed with a message.
	CancelInterrupted CancelReason = "interrupted"
	// CancelWithdrawn: the agent no longer waits for it. It gave up, moved on, or
	// does not recognise the question any more.
	CancelWithdrawn CancelReason = "withdrawn"
)

// Valid reports whether r is a known reason.
func (r CancelReason) Valid() bool {
	return r == CancelRunEnded || r == CancelInterrupted || r == CancelWithdrawn
}

// QuestionKind says what the agent wants from the user, so that the interface
// can word it and the right way to answer it.
type QuestionKind string

const (
	// QuestionClarification: the agent needs information to go on. Free text.
	QuestionClarification QuestionKind = "clarification"
	// QuestionDecision: the agent needs the user to decide something.
	QuestionDecision QuestionKind = "decision"
	// QuestionApproval: the agent wants permission to run a tool or change files.
	QuestionApproval QuestionKind = "approval"
	// QuestionSelection: choose between alternatives the agent put forward.
	QuestionSelection QuestionKind = "selection"
	// QuestionInstruction: the agent asks what to do next, or for more instructions.
	QuestionInstruction QuestionKind = "instruction"
)

// Valid reports whether k is a known question kind.
func (k QuestionKind) Valid() bool {
	switch k {
	case QuestionClarification, QuestionDecision, QuestionApproval, QuestionSelection, QuestionInstruction:
		return true
	}
	return false
}

// Default answers to an approval.
const (
	AnswerAllow = "Allow"
	AnswerDeny  = "Deny"
)

// AnsweredBy says who gave a question's answer.
type AnsweredBy string

const (
	// AnsweredByUser: the user typed or chose it. The default.
	AnsweredByUser AnsweredBy = "user"
	// AnsweredByPolicy: the run's interaction policy said not to put the question
	// to the user, so the controller replied for them. The answer is the reply
	// the agent was given. Policies never answer an approval.
	AnsweredByPolicy AnsweredBy = "policy"
)

// Valid reports whether b names a known answerer.
func (b AnsweredBy) Valid() bool { return b == AnsweredByUser || b == AnsweredByPolicy }

// Question is something an agent asked the user during a run. While a
// question is pending its run is in RunWaitingForUser, waiting for a question.
//
// A question is self-contained: it carries the project, task and run it
// belongs to, so a client (or, later, a notification) can show and answer it
// without looking anything else up.
type Question struct {
	ID        string `json:"id"`
	RunID     string `json:"runId"`
	TaskID    string `json:"taskId"`
	ProjectID string `json:"projectId"`

	Kind QuestionKind `json:"kind"`
	// Prompt is the question itself, in a sentence or two.
	Prompt string `json:"prompt"`
	// Context is what the user needs to judge it: the command to be run, the
	// plan to be approved, what the agent has found. Plain text, optional.
	Context string `json:"context,omitempty"`
	// Options are the suggested answers, shown as one-tap choices.
	Options []string `json:"options,omitempty"`
	// AllowFreeText says whether a typed answer is accepted. When false the
	// answer must be one of Options. A question without options always allows it.
	AllowFreeText bool `json:"allowFreeText"`

	State        QuestionState `json:"state"`
	Answer       string        `json:"answer,omitempty"`
	AnsweredBy   AnsweredBy    `json:"answeredBy,omitempty"` // set once answered
	CancelReason CancelReason  `json:"cancelReason,omitempty"`

	AskedAt     time.Time  `json:"askedAt"`
	AnsweredAt  *time.Time `json:"answeredAt,omitempty"`  // when the user's answer was recorded
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"` // when the agent received it
	ClosedAt    *time.Time `json:"closedAt,omitempty"`    // when it was cancelled
}

// Pending reports whether the question still needs the user.
func (q *Question) Pending() bool { return q.State == QuestionPending }

// Blocking reports whether the agent is still held up by the question: it is
// unanswered, or answered but the answer has not reached the agent yet.
func (q *Question) Blocking() bool {
	return q.State == QuestionPending || (q.State == QuestionAnswered && q.DeliveredAt == nil)
}

// CheckAnswer validates an answer against the question and returns it in the
// form to record: trimmed, and for a choice, spelled as the option is.
func (q *Question) CheckAnswer(answer string) (string, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("%w: an answer is required", ErrInvalid)
	}
	for _, o := range q.Options {
		if strings.EqualFold(answer, o) {
			return o, nil
		}
	}
	if q.AllowFreeText || len(q.Options) == 0 {
		return answer, nil
	}
	return "", fmt.Errorf("%w: the answer must be one of: %s", ErrInvalid, strings.Join(q.Options, ", "))
}

// SameAnswer reports whether answer is, once checked, the answer already given.
// It is how a retried request is told from a competing one.
func (q *Question) SameAnswer(answer string) bool {
	got, err := q.CheckAnswer(answer)
	return err == nil && q.State == QuestionAnswered && got == q.Answer
}

// Accept records the user's answer. It is the first step of answering: the
// agent has not been given it yet. It returns a *QuestionClosedError if the
// question is no longer pending, so a duplicate or late answer can be told what
// happened to it.
func (q *Question) Accept(answer string, now time.Time) error {
	if !q.Pending() {
		return &QuestionClosedError{Question: q.clone()}
	}
	checked, err := q.CheckAnswer(answer)
	if err != nil {
		return err
	}
	q.State, q.Answer, q.AnsweredAt, q.AnsweredBy = QuestionAnswered, checked, &now, AnsweredByUser
	return nil
}

// AcceptFromPolicy records the reply the controller gives on the user's behalf
// because the run's policy does not put this question to them. The reply is
// not one of the options and is not checked against them. An approval can never
// be answered this way: permission is the user's to give.
func (q *Question) AcceptFromPolicy(reply string, now time.Time) error {
	if q.Kind == QuestionApproval {
		return fmt.Errorf("%w: a policy cannot answer an approval", ErrInvalid)
	}
	if !q.Pending() {
		return &QuestionClosedError{Question: q.clone()}
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return fmt.Errorf("%w: a reply is required", ErrInvalid)
	}
	q.State, q.Answer, q.AnsweredAt, q.AnsweredBy = QuestionAnswered, reply, &now, AnsweredByPolicy
	return nil
}

// MarkDelivered records that the agent received the answer.
func (q *Question) MarkDelivered(now time.Time) error {
	if q.State != QuestionAnswered {
		return fmt.Errorf("%w: question %s is %s, so it has no answer to deliver", ErrTransition, q.ID, q.State)
	}
	if q.DeliveredAt == nil {
		q.DeliveredAt = &now
	}
	return nil
}

// Cancel closes a question that can no longer be answered. A question whose
// answer already reached the agent is settled and cannot be cancelled; one that
// was answered but not delivered keeps its answer. Cancelling twice is not an
// error: the first reason stands.
func (q *Question) Cancel(reason CancelReason, now time.Time) error {
	if !reason.Valid() {
		return fmt.Errorf("%w: unknown cancel reason %q", ErrInvalid, reason)
	}
	switch {
	case q.State == QuestionCancelled:
		return nil
	case q.State == QuestionAnswered && q.DeliveredAt != nil:
		return fmt.Errorf("%w: question %s was already answered and delivered", ErrTransition, q.ID)
	}
	q.State, q.CancelReason, q.ClosedAt = QuestionCancelled, reason, &now
	return nil
}

func (q *Question) clone() *Question {
	c := *q
	c.Options = append([]string(nil), q.Options...)
	return &c
}

// QuestionClosedError is returned when someone answers a question that is not
// pending: another client got there first, or the agent has moved on. It carries
// the question as it now stands, so the caller can show what actually happened.
// It is a conflict.
type QuestionClosedError struct{ Question *Question }

// Unwrap lets errors.Is(err, ErrConflict) hold.
func (e *QuestionClosedError) Unwrap() error { return ErrConflict }

func (e *QuestionClosedError) Error() string {
	q := e.Question
	switch q.State {
	case QuestionAnswered:
		return fmt.Sprintf("this question was already answered: %q", q.Answer)
	case QuestionCancelled:
		return q.CancelReason.Explain(q.Answer != "")
	}
	return "this question is still open"
}

// AlreadyAnswered reports whether the question was answered (as opposed to cancelled).
func (e *QuestionClosedError) AlreadyAnswered() bool { return e.Question.State == QuestionAnswered }

// Explain says in a sentence why a question could not be answered. answered
// tells whether the user had given an answer that then could not be used.
func (r CancelReason) Explain(answered bool) string {
	var s string
	switch r {
	case CancelRunEnded:
		s = "The agent stopped, so this question can no longer be answered."
	case CancelInterrupted:
		s = "The session was interrupted before this was answered. Send a message to continue it."
	case CancelWithdrawn:
		s = "The agent no longer needs an answer to this."
	default:
		s = "This question is no longer open."
	}
	if answered {
		s += " Your answer was not delivered."
	}
	return s
}

package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// Runs owns the state of runs and their questions: every change to a run is a
// method here, written in one transaction with the events that describe it.
// It does not start or talk to processes; the runtime does that and reports
// what happened.
//
// Every state change emits a run.state_changed event carrying the run, which is
// what clients use to keep their copy current, and an agent.* event saying what
// it meant. See domain.EventAgentStarted and its neighbours.
type Runs struct {
	Deps
	Handoffs *Handoffs
}

// Limits on what an agent can make the controller store.
const (
	maxOutputBytes   = 16 << 10 // one output event
	maxPromptBytes   = 512 << 10
	maxQuestionBytes = 8 << 10
	maxContextBytes  = 16 << 10
	maxOptions       = 10
	maxOptionBytes   = 200
	maxActivityRunes = 160
)

// interruptedReason is recorded on runs that were in flight when the
// controller stopped without being able to say so.
const interruptedReason = "interrupted: controller restarted"

// NewRun describes a run to record.
type NewRun struct {
	TaskID     string
	AgentID    string
	Prompt     string
	WorktreeID string
	// Policy is the execution policy the run is started with: the task's, unless
	// the user chose another for this run. Unset means interactive.
	Policy domain.ExecutionPolicy
	// Model and Reasoning are what the agent is started with; empty means the
	// agent's own default.
	Model        string
	Reasoning    string
	ParentRunID  string
	Purpose      string
	ScheduleKey  string
	EnforceGates bool
	Manual       bool
	RunnerID     string
	Remote       bool
	Claim        func(context.Context, store.Tx, *domain.Run) error
}

// Create records a new run in the starting state. It does not mean an agent is
// running: that is MarkStarted, after the process is up. A task has at most one
// active run, so this returns domain.ErrConflict if it already has one.
func (s *Runs) Create(ctx context.Context, in NewRun) (*domain.Run, error) {
	if strings.TrimSpace(in.AgentID) == "" {
		return nil, fmt.Errorf("%w: a run needs an agent", domain.ErrInvalid)
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return nil, fmt.Errorf("%w: a run needs a prompt", domain.ErrInvalid)
	}
	if len(in.Prompt) > maxPromptBytes {
		return nil, fmt.Errorf("%w: the prompt is longer than %d bytes", domain.ErrInvalid, maxPromptBytes)
	}
	if err := in.Policy.Validate(); err != nil {
		return nil, err
	}
	now := s.now()
	r := &domain.Run{
		ID: domain.NewID(domain.PrefixRun), TaskID: in.TaskID, AgentID: in.AgentID, State: domain.RunStarting,
		WorktreeID: in.WorktreeID, Prompt: in.Prompt, Policy: in.Policy.Normalized(), Model: in.Model, Reasoning: in.Reasoning,
		ParentRunID: in.ParentRunID, Purpose: in.Purpose, ScheduleKey: in.ScheduleKey,
		RunnerID: in.RunnerID, Remote: in.Remote, Usage: domain.Usage{CostKind: "usage_only"},
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		task, err := tx.Tasks().Get(ctx, in.TaskID)
		if err != nil {
			return err
		}
		r.ProjectID = task.ProjectID
		r.Handoff = &domain.Handoff{Objective: task.Title + "\n" + task.Description}
		r.Handoff.Normalize()
		if in.EnforceGates {
			x, e := scheduleRead(ctx, tx, task.ProjectID)
			if e != nil {
				return e
			}
			d := baseDecision(*task, x, now, in.Manual)
			if d.State != "runnable" {
				return fmt.Errorf("%w: %s", domain.ErrConflict, d.Reason)
			}
		}
		if in.ParentRunID != "" {
			parent, e := tx.Runs().Get(ctx, in.ParentRunID)
			if e != nil {
				return e
			}
			if parent.TaskID != task.ID || !parent.State.Terminal() {
				return fmt.Errorf("%w: continuation requires a terminal run of this task", domain.ErrInvalid)
			}
		}
		if in.ScheduleKey != "" {
			o := &task.Orchestration
			if !o.Enabled || o.Key != in.ScheduleKey || o.RunID != "" || o.Missed || o.Error != "" {
				return fmt.Errorf("%w: schedule already claimed or changed", domain.ErrConflict)
			}
			x, e := scheduleRead(ctx, tx, task.ProjectID)
			if e != nil {
				return e
			}
			d := baseDecision(*task, x, now, in.Manual)
			if d.State != "runnable" {
				return fmt.Errorf("%w: %s", domain.ErrConflict, d.Reason)
			}
			o.RunID = r.ID
			o.DispatchedAt = &now
			task.UpdatedAt = now
			if e := tx.Tasks().Update(ctx, task); e != nil {
				return e
			}
			ev := newEvent(domain.EventTaskUpdated, task)
			ev.ProjectID, ev.TaskID = task.ProjectID, task.ID
			if e := em.emit(ev); e != nil {
				return e
			}
		}
		prior, err := tx.Runs().ListByTask(ctx, task.ID)
		if err != nil {
			return err
		}
		r.Attempt = len(prior) + 1
		for _, p := range prior {
			if !p.State.Terminal() {
				return fmt.Errorf("task %s already has an active run (%s, %s): %w", task.ID, p.ID, p.State, domain.ErrConflict)
			}
		}
		if in.Claim != nil {
			if err := in.Claim(ctx, tx, r); err != nil {
				return err
			}
		}
		if err := tx.Runs().Create(ctx, r); err != nil {
			return err
		}
		return emitRun(em, r, "")
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Started describes the process that now backs a run.
type Started struct {
	UsagePartial bool
	PID          int
	ProcessID    string // identifies the process across restarts; see domain.Run.ProcessID
	SessionRef   string // the agent's resumable handle, if already known
	// Resumed is set when the process continues an existing run (a waiting run
	// whose process was lost) rather than beginning one.
	Resumed bool
}

// MarkStarted records that the agent process is up: the run goes from starting
// (or, for a resumed run, from waiting or blocked) to running, and the task moves to Doing.
// Both happen in one transaction, so the board never shows a running agent on a
// card that is not in Doing, nor a Doing card whose agent never started.
func (s *Runs) MarkStarted(ctx context.Context, id string, in Started) (*domain.Run, error) {
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, id); err != nil {
			return err
		}
		from := r.State
		if in.Resumed != (from == domain.RunWaitingForUser || from == domain.RunBlocked) {
			return fmt.Errorf("%w: run %s is %s, cannot be started (resumed=%v)", domain.ErrTransition, id, from, in.Resumed)
		}
		if err := r.Transition(domain.RunRunning, "", s.now()); err != nil {
			return err
		}
		r.PID, r.ProcessID = in.PID, in.ProcessID
		if in.Resumed || in.UsagePartial {
			r.Usage.Partial = true
		}
		if in.SessionRef != "" {
			r.SessionRef = in.SessionRef
		}
		if err := tx.Runs().Update(ctx, r); err != nil {
			return err
		}
		if err := moveTaskToDoing(ctx, tx, em, r.TaskID, s.now()); err != nil {
			return err
		}
		if err := emitRun(em, r, from); err != nil {
			return err
		}
		return emitAgent(em, domain.EventAgentStarted, r, map[string]any{"run": r, "resumed": in.Resumed})
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Ended describes how a run ended.
type Ended struct {
	State    domain.RunState // completed, failed or stopped
	Reason   string
	ExitCode *int
}

// End moves a run to a terminal state. Questions still pending are cancelled,
// since nobody can answer them any more. It returns domain.ErrTransition if the
// run has already ended.
func (s *Runs) End(ctx context.Context, id string, in Ended) (*domain.Run, error) {
	if !in.State.Terminal() {
		return nil, fmt.Errorf("%w: %s is not a final run state", domain.ErrInvalid, in.State)
	}
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, id); err != nil {
			return err
		}
		return s.end(ctx, tx, em, r, in, domain.CancelRunEnded)
	})
	if err != nil {
		return nil, err
	}
	if s.Handoffs != nil {
		if captured, e := s.Handoffs.Generate(ctx, r.ID); e == nil {
			r = captured
		} else {
			s.log().Warn("handoff capture failed", "run", r.ID, "err", e)
		}
	}
	return r, nil
}

// end moves a run to its final state. Its open questions are cancelled for
// the reason given: nobody can answer them any more.
func (s *Runs) end(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, in Ended, why domain.CancelReason) error {
	from := r.State
	if err := r.Transition(in.State, truncate(in.Reason, 1000), s.now()); err != nil {
		return err
	}
	r.ExitCode = in.ExitCode
	{
		h, err := basicHandoff(ctx, tx, r, s.now())
		if err != nil {
			return err
		}
		r.Handoff = h
	}
	if err := cancelOpen(ctx, tx, em, r, why, s.now()); err != nil {
		return err
	}
	if err := tx.Runs().Update(ctx, r); err != nil {
		return err
	}
	if err := emitRun(em, r, from); err != nil {
		return err
	}
	if r.ScheduleKey != "" && r.State == domain.RunCompleted {
		t, e := tx.Tasks().Get(ctx, r.TaskID)
		if e != nil {
			return e
		}
		if t.State != domain.TaskDone && t.State != domain.TaskReview {
			max, e := tx.Tasks().MaxPosition(ctx, t.ProjectID, domain.TaskReview)
			if e != nil {
				return e
			}
			t.State = domain.TaskReview
			t.Position = max + 1
			t.UpdatedAt = s.now()
			if e := tx.Tasks().Update(ctx, t); e != nil {
				return e
			}
			ev := newEvent(domain.EventTaskUpdated, t)
			ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
			if e := em.emit(ev); e != nil {
				return e
			}
		}
	}
	kind := map[domain.RunState]domain.EventType{
		domain.RunCompleted: domain.EventAgentCompleted,
		domain.RunFailed:    domain.EventAgentFailed,
		domain.RunStopped:   domain.EventAgentStopped,
	}[in.State]
	return emitAgent(em, kind, r, map[string]any{"run": r, "reason": r.Reason})
}

// cancelOpen cancels every question of the run that still holds the agent up:
// the unanswered ones, and any whose answer was recorded but never reached the
// agent. Each is announced, so clients drop it.
func cancelOpen(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, why domain.CancelReason, now time.Time) error {
	qs, err := tx.Questions().ListByRun(ctx, r.ID)
	if err != nil {
		return err
	}
	for i := range qs {
		q := &qs[i]
		if !q.Blocking() {
			continue
		}
		if err := q.Cancel(why, now); err != nil {
			return err
		}
		if err := tx.Questions().Update(ctx, q); err != nil {
			return err
		}
		if err := emitClosed(em, r, q); err != nil {
			return err
		}
	}
	return nil
}

// stillBlocked reports whether any of the run's questions still holds the agent up.
func stillBlocked(ctx context.Context, tx store.Tx, runID string) (bool, error) {
	qs, err := tx.Questions().ListByRun(ctx, runID)
	if err != nil {
		return false, err
	}
	for i := range qs {
		if qs[i].Blocking() {
			return true, nil
		}
	}
	return false, nil
}

// NewQuestion is something the agent asked, as the adapter reports it.
type NewQuestion struct {
	Kind          domain.QuestionKind // derived from the options if unset or unknown
	Prompt        string
	Context       string
	Options       []string
	AllowFreeText bool
}

// RecordQuestion stores a question the agent is blocked on and makes the run
// wait for the user. An agent can have several questions open at once (parallel
// tool calls each ask for permission); the run waits until all are answered.
//
// What is recorded is made consistent first: an approval always has options,
// and a question with none always takes a typed answer, so that nothing is
// recorded that the user could not answer.
func (s *Runs) RecordQuestion(ctx context.Context, runID string, in NewQuestion) (*domain.Question, *domain.Run, error) {
	q := s.newQuestion(runID, in)
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, runID); err != nil {
			return err
		}
		q.TaskID, q.ProjectID = r.TaskID, r.ProjectID
		from := r.State
		switch {
		case from == domain.RunRunning:
			if err := r.WaitFor(domain.WaitQuestion, s.now()); err != nil {
				return err
			}
		case from == domain.RunWaitingForUser && r.Waiting == domain.WaitQuestion:
			// A second question while the first is open: nothing changes but the count.
		default:
			return fmt.Errorf("%w: run %s is %s and cannot ask a question", domain.ErrConflict, runID, from)
		}
		if err := tx.Questions().Create(ctx, q); err != nil {
			return err
		}
		if from != r.State {
			if err := tx.Runs().Update(ctx, r); err != nil {
				return err
			}
			if err := emitRun(em, r, from); err != nil {
				return err
			}
		}
		return emitAgent(em, domain.EventAgentQuestion, r, map[string]any{"question": q})
	})
	if err != nil {
		return nil, nil, err
	}
	return q, r, nil
}

// newQuestion builds the question to store for what the agent asked, made
// consistent first: an approval always has options, and a question with none
// always takes a typed answer, so that nothing is recorded that the user could
// not answer.
func (s *Runs) newQuestion(runID string, in NewQuestion) *domain.Question {
	q := &domain.Question{
		ID: domain.NewID(domain.PrefixQuestion), RunID: runID, Kind: in.Kind,
		Prompt: truncate(strings.TrimSpace(in.Prompt), maxQuestionBytes), Context: truncate(strings.TrimSpace(in.Context), maxContextBytes),
		Options: cleanOptions(in.Options), AllowFreeText: in.AllowFreeText,
		State: domain.QuestionPending, AskedAt: s.now(),
	}
	if q.Prompt == "" {
		q.Prompt = "The agent needs your input."
	}
	if !q.Kind.Valid() {
		q.Kind = domain.QuestionClarification
		if len(q.Options) > 0 {
			q.Kind = domain.QuestionSelection
		}
	}
	if q.Kind == domain.QuestionApproval && len(q.Options) == 0 {
		q.Options = []string{domain.AnswerAllow, domain.AnswerDeny}
	}
	if len(q.Options) == 0 {
		q.AllowFreeText = true
	}
	return q
}

// Replied is how the controller dealt with a question on the user's behalf.
type Replied struct {
	// Reply is what the agent is being told, recorded as the answer.
	Reply string
	// Blocker, if set, stops the run: it becomes blocked, for this reason.
	Blocker *domain.Blocker
}

// RecordReplied records a question that the run's policy did not put to the user,
// already answered by the policy with in.Reply. The run keeps working, or, with a
// blocker, becomes blocked (a run that is already blocked stays so, with its
// first blocker). The question is recorded as asked and answered in the same
// transaction, so the activity feed shows what the agent asked and how it was
// dealt with. It is not delivered yet: the caller gives the reply to the agent
// and then calls ConfirmDelivery, or CancelQuestion if the agent cannot take it.
//
// An approval is refused: permission is only ever the user's to give. A run that
// is waiting for the user, or has ended, is a conflict, and nothing is recorded.
func (s *Runs) RecordReplied(ctx context.Context, runID string, in NewQuestion, rep Replied) (*domain.Question, *domain.Run, error) {
	q := s.newQuestion(runID, in)
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, runID); err != nil {
			return err
		}
		q.TaskID, q.ProjectID = r.TaskID, r.ProjectID
		from := r.State
		if from != domain.RunRunning && from != domain.RunBlocked {
			return fmt.Errorf("%w: run %s is %s, so a policy cannot answer its question", domain.ErrConflict, runID, from)
		}
		if err := q.AcceptFromPolicy(rep.Reply, s.now()); err != nil {
			return err
		}
		if err := tx.Questions().Create(ctx, q); err != nil {
			return err
		}
		if err := emitAgent(em, domain.EventAgentQuestion, r, map[string]any{"question": q}); err != nil {
			return err
		}
		if err := emitAnswered(em, r, q); err != nil {
			return err
		}
		if rep.Blocker == nil || from == domain.RunBlocked {
			return nil
		}
		return s.block(ctx, tx, em, r, *rep.Blocker)
	})
	if err != nil {
		return nil, nil, err
	}
	return q, r, nil
}

// Block stops a run that has reached a decision it cannot safely make: it
// becomes blocked, with the blocker persisted on it, and the task stays where it
// is. It is for a run whose agent reported the blocker itself, at the end of its
// turn; one that asked a question goes through RecordReplied. Blocking a run
// that is already blocked changes nothing. A run that is not running (it is
// waiting, or over) is a conflict.
func (s *Runs) Block(ctx context.Context, runID string, b domain.Blocker) (*domain.Run, error) {
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, runID); err != nil {
			return err
		}
		if r.State == domain.RunBlocked {
			return nil
		}
		return s.block(ctx, tx, em, r, b)
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Runs) block(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, b domain.Blocker) error {
	if r.State != domain.RunRunning {
		return fmt.Errorf("%w: run %s is %s and cannot be blocked", domain.ErrConflict, r.ID, r.State)
	}
	b.Summary = truncate(strings.TrimSpace(b.Summary), maxQuestionBytes)
	b.Detail = truncate(strings.TrimSpace(b.Detail), maxContextBytes)
	b.Options = cleanOptions(b.Options)
	from := r.State
	if err := r.Block(b, s.now()); err != nil {
		return err
	}
	if err := tx.Runs().Update(ctx, r); err != nil {
		return err
	}
	if err := emitRun(em, r, from); err != nil {
		return err
	}
	return emitAgent(em, domain.EventAgentBlocked, r, map[string]any{"blocker": r.Blocker})
}

// AcceptAnswer records the user's answer to a pending question. It is the
// first of three steps, and deliberately only that: the answer is durable
// before the agent has seen it, so a crash cannot lose what the user said, but
// the run goes on waiting until the agent has it (ConfirmDelivery).
//
// It returns a *domain.QuestionClosedError, a conflict carrying the question
// as it now is, if the question is not pending: someone else answered first, or
// the agent has moved on. An answer that is not one of the allowed choices is
// domain.ErrInvalid.
func (s *Runs) AcceptAnswer(ctx context.Context, questionID, answer string) (*domain.Question, error) {
	var q *domain.Question
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if q, err = tx.Questions().Get(ctx, questionID); err != nil {
			return err
		}
		r, err := tx.Runs().Get(ctx, q.RunID)
		if err != nil {
			return err
		}
		if err := q.Accept(answer, s.now()); err != nil {
			return err
		}
		if err := tx.Questions().Update(ctx, q); err != nil {
			return err
		}
		if err := emitAnswered(em, r, q); err != nil {
			return err
		}
		return emitOutput(em, r, domain.AgentOutput{Stream: domain.StreamUser, Text: q.Answer})
	})
	if err != nil {
		return nil, err
	}
	return q, nil
}

// ConfirmDelivery records that the agent received an answer. When it was the
// last question holding the agent up, the run goes back to running (and its
// task to Doing). It does nothing for a question that is not an undelivered
// answer, so confirming twice is harmless.
func (s *Runs) ConfirmDelivery(ctx context.Context, questionID string) (*domain.Question, error) {
	var q *domain.Question
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if q, err = tx.Questions().Get(ctx, questionID); err != nil {
			return err
		}
		if q.State != domain.QuestionAnswered || q.DeliveredAt != nil {
			return nil
		}
		if err := q.MarkDelivered(s.now()); err != nil {
			return err
		}
		if err := tx.Questions().Update(ctx, q); err != nil {
			return err
		}
		r, err := tx.Runs().Get(ctx, q.RunID)
		if err != nil {
			return err
		}
		return s.resumeIfUnblocked(ctx, tx, em, r, "answer")
	})
	if err != nil {
		return nil, err
	}
	return q, nil
}

// CancelQuestion closes a question that can no longer be answered, for the
// reason given. It works on an unanswered question and on one whose answer was
// recorded but not delivered, which then keeps its answer. A question the agent
// withdrew lets the run carry on if nothing else holds it up; for a run whose
// session ended or was interrupted nothing is resumed here. It does nothing if
// the question is already cancelled, and is refused for one that was delivered.
func (s *Runs) CancelQuestion(ctx context.Context, questionID string, why domain.CancelReason) (*domain.Question, error) {
	var q *domain.Question
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if q, err = tx.Questions().Get(ctx, questionID); err != nil {
			return err
		}
		if q.State == domain.QuestionCancelled {
			return nil
		}
		r, err := tx.Runs().Get(ctx, q.RunID)
		if err != nil {
			return err
		}
		if err := q.Cancel(why, s.now()); err != nil {
			return err
		}
		if err := tx.Questions().Update(ctx, q); err != nil {
			return err
		}
		if err := emitClosed(em, r, q); err != nil {
			return err
		}
		if why != domain.CancelWithdrawn {
			return nil
		}
		return s.resumeIfUnblocked(ctx, tx, em, r, "question_withdrawn")
	})
	if err != nil {
		return nil, err
	}
	return q, nil
}

// resumeIfUnblocked moves a run that waits for a question back to running once
// no question holds the agent up any more.
func (s *Runs) resumeIfUnblocked(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, via string) error {
	if r.State != domain.RunWaitingForUser || r.Waiting != domain.WaitQuestion {
		return nil
	}
	blocked, err := stillBlocked(ctx, tx, r.ID)
	if err != nil || blocked {
		return err
	}
	return s.resume(ctx, tx, em, r, via)
}

// resume moves a waiting or blocked run back to running, on behalf of the user.
// Leaving the blocked state clears the blocker (the event log keeps it).
func (s *Runs) resume(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, via string) error {
	from := r.State
	if err := r.Transition(domain.RunRunning, "", s.now()); err != nil {
		return err
	}
	if err := tx.Runs().Update(ctx, r); err != nil {
		return err
	}
	if err := moveTaskToDoing(ctx, tx, em, r.TaskID, s.now()); err != nil {
		return err
	}
	if err := emitRun(em, r, from); err != nil {
		return err
	}
	return emitAgent(em, domain.EventAgentResumed, r, map[string]any{"via": via})
}

// Message records a message the user sent to a live agent and that the agent
// accepted. If the run was waiting for one, or blocked (the message is how the
// user settles the blocker), it is running again. A message sent
// while the agent works is queued by the agent and changes no state. It returns
// domain.ErrConflict if the run is waiting for an answer, or has ended.
func (s *Runs) Message(ctx context.Context, runID, text string) (*domain.Run, error) {
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, runID); err != nil {
			return err
		}
		if err := checkMessageable(r); err != nil {
			return err
		}
		if err := emitOutput(em, r, domain.AgentOutput{Stream: domain.StreamUser, Text: truncate(text, maxOutputBytes)}); err != nil {
			return err
		}
		if r.State == domain.RunWaitingForUser || r.State == domain.RunBlocked {
			return s.resume(ctx, tx, em, r, "message")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// checkMessageable reports whether a user message makes sense for the run now.
func checkMessageable(r *domain.Run) error {
	switch {
	case r.State == domain.RunRunning, r.State == domain.RunBlocked, r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitIdle:
		return nil
	case r.State == domain.RunWaitingForUser:
		return fmt.Errorf("run %s is waiting for an answer to a question: %w", r.ID, domain.ErrConflict)
	}
	return fmt.Errorf("run %s is %s and cannot take a message: %w", r.ID, r.State, domain.ErrConflict)
}

// CheckMessageable returns domain.ErrConflict unless a message to the run would
// be accepted now. It lets the runtime refuse before bothering the agent.
func (s *Runs) CheckMessageable(ctx context.Context, runID string) error {
	r, err := s.Get(ctx, runID)
	if err != nil {
		return err
	}
	return checkMessageable(r)
}

// MarkIdle records that the agent finished its turn and waits for a message:
// running becomes waiting. It does nothing if the run is not running (it may be
// waiting on a question, which takes precedence, or already over).
func (s *Runs) MarkIdle(ctx context.Context, runID string) (*domain.Run, error) {
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, runID); err != nil {
			return err
		}
		if r.State != domain.RunRunning {
			return nil
		}
		from := r.State
		if err := r.WaitFor(domain.WaitIdle, s.now()); err != nil {
			return err
		}
		if err := tx.Runs().Update(ctx, r); err != nil {
			return err
		}
		if err := emitRun(em, r, from); err != nil {
			return err
		}
		return emitAgent(em, domain.EventAgentWaiting, r, map[string]any{"reason": "turn_complete"})
	})
	return r, err
}

// SetSessionRef records the agent's resumable session handle.
func (s *Runs) SetSessionRef(ctx context.Context, runID, ref string) error {
	if ref == "" {
		return nil
	}
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		r, err := tx.Runs().Get(ctx, runID)
		if err != nil {
			return err
		}
		if r.SessionRef == ref || r.State.Terminal() {
			return nil
		}
		r.SessionRef, r.UpdatedAt = ref, s.now()
		return tx.Runs().Update(ctx, r)
	})
}

// OutputItem is one piece of agent output to record.
type OutputItem struct {
	Stream domain.OutputStream
	Text   string
}

// AppendOutput records a batch of agent output in one transaction, and updates
// the run's latest activity from it. Text over the size limit is cut.
func (s *Runs) AppendOutput(ctx context.Context, runID string, items []OutputItem) error {
	if len(items) == 0 {
		return nil
	}
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		r, err := tx.Runs().Get(ctx, runID)
		if err != nil {
			return err
		}
		activity := ""
		for _, it := range items {
			if !it.Stream.Valid() {
				it.Stream = domain.StreamSystem
			}
			text := truncate(it.Text, maxOutputBytes)
			if strings.TrimSpace(text) == "" {
				continue
			}
			if err := emitOutput(em, r, domain.AgentOutput{Stream: it.Stream, Text: text}); err != nil {
				return err
			}
			if it.Stream == domain.StreamAssistant || it.Stream == domain.StreamTool {
				activity = oneLine(text)
			}
		}
		if activity != "" && !r.State.Terminal() {
			return tx.Runs().TouchActivity(ctx, runID, activity, s.now())
		}
		return nil
	})
}

// Interrupt handles a run whose process is gone without having finished: the
// controller is shutting down, or has just started and found the run left over.
//
// A run that was working has lost whatever it was doing and fails with reason.
// A run that was waiting for the user or blocked, and has a session the agent
// can resume, is kept: it waits for a message, which will start a new process that
// continues the conversation. Its pending questions are cancelled, because the
// requests they answered died with the process.
func (s *Runs) Interrupt(ctx context.Context, runID, reason string) (*domain.Run, error) {
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, runID); err != nil {
			return err
		}
		return s.interrupt(ctx, tx, em, r, reason)
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Runs) interrupt(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, reason string) error {
	if r.State.Terminal() {
		return nil
	}
	resumable := (r.State == domain.RunWaitingForUser || r.State == domain.RunBlocked) && r.SessionRef != ""
	if !resumable {
		return s.end(ctx, tx, em, r, Ended{State: domain.RunFailed, Reason: reason}, domain.CancelInterrupted)
	}
	now := s.now()
	if err := cancelOpen(ctx, tx, em, r, domain.CancelInterrupted, now); err != nil {
		return err
	}
	r.PID, r.ProcessID, r.UpdatedAt = 0, "", now
	if r.State == domain.RunWaitingForUser {
		r.Waiting = domain.WaitIdle // a blocked run stays blocked: it still has its blocker
	}
	r.Activity, r.ActivityAt = oneLine(reason+"; send a message to resume"), &now
	if err := tx.Runs().Update(ctx, r); err != nil {
		return err
	}
	if err := emitRun(em, r, r.State); err != nil {
		return err
	}
	return emitAgent(em, domain.EventAgentWaiting, r, map[string]any{"reason": "interrupted", "detail": reason})
}

// RecoverAfterRestart reconciles persisted runs with reality at startup. The
// runtime has already stopped any agent process the previous controller left
// behind (Manager.Recover), so every run that was starting or running is over,
// and a run that was waiting has no process. See Interrupt for what becomes of each.
func (s *Runs) RecoverAfterRestart(ctx context.Context) (int, error) {
	recovered := 0
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		active, err := tx.Runs().ListActive(ctx)
		if err != nil {
			return err
		}
		for i := range active {
			r := &active[i]
			if r.Remote {
				continue
			}
			// A run that is already idle or blocked, with no process, needs nothing.
			idle := r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitIdle
			if (idle || r.State == domain.RunBlocked) && r.SessionRef != "" && r.PID == 0 {
				continue
			}
			if err := s.interrupt(ctx, tx, em, r, interruptedReason); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	if recovered > 0 {
		s.log().Warn("recovered runs interrupted by a controller restart", "count", recovered)
	}
	return recovered, err
}

// ---- reads ----

// Get returns a run.
func (s *Runs) Get(ctx context.Context, id string) (*domain.Run, error) {
	var r *domain.Run
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		r, err = tx.Runs().Get(ctx, id)
		return err
	})
	return r, err
}

// GetIn returns a run of the given project; one of another project is reported
// as not found, as if it did not exist.
func (s *Runs) GetIn(ctx context.Context, projectID, id string) (*domain.Run, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.ProjectID != projectID {
		return nil, fmt.Errorf("run %s: %w", id, domain.ErrNotFound)
	}
	return r, nil
}

// History returns up to limit of the project's runs, newest first: what its
// Activity view lists.
func (s *Runs) History(ctx context.Context, projectID string, limit int) ([]domain.Run, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var out []domain.Run
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		var err error
		out, err = tx.Runs().ListByProject(ctx, projectID, limit)
		return err
	})
	return out, err
}

// ListByTask returns every run of a task, oldest first.
func (s *Runs) ListByTask(ctx context.Context, taskID string) ([]domain.Run, error) {
	var out []domain.Run
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Tasks().Get(ctx, taskID); err != nil {
			return err
		}
		var err error
		out, err = tx.Runs().ListByTask(ctx, taskID)
		return err
	})
	return out, err
}

// ListLatestByProject returns the most recent run of each of the project's
// tasks that has run. It is what the board shows on its cards.
func (s *Runs) ListLatestByProject(ctx context.Context, projectID string) ([]domain.Run, error) {
	var out []domain.Run
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		var err error
		out, err = tx.Runs().ListLatestByProject(ctx, projectID)
		return err
	})
	return out, err
}

// ListActive returns runs that have not reached a terminal state.
func (s *Runs) ListActive(ctx context.Context) ([]domain.Run, error) {
	var out []domain.Run
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Runs().ListActive(ctx)
		return err
	})
	return out, err
}

// Events returns up to limit of the run's events that precede seq before (0
// for the newest), oldest first. It is the run's activity history.
func (s *Runs) Events(ctx context.Context, runID string, before int64, limit int) ([]domain.Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out []domain.Event
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Runs().Get(ctx, runID); err != nil {
			return err
		}
		var err error
		out, err = tx.Events().ListByRun(ctx, runID, before, limit)
		return err
	})
	return out, err
}

// GetQuestion returns a question.
func (s *Runs) GetQuestion(ctx context.Context, id string) (*domain.Question, error) {
	var q *domain.Question
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		q, err = tx.Questions().Get(ctx, id)
		return err
	})
	return q, err
}

// GetQuestionIn returns a question of the given project; one of another project
// is reported as not found, as if it did not exist.
func (s *Runs) GetQuestionIn(ctx context.Context, projectID, id string) (*domain.Question, error) {
	q, err := s.GetQuestion(ctx, id)
	if err != nil {
		return nil, err
	}
	if q.ProjectID != projectID {
		return nil, fmt.Errorf("question %s: %w", id, domain.ErrNotFound)
	}
	return q, nil
}

// ListPendingQuestionsIn returns one project's questions that are waiting for
// the user. The database does the filtering: a project's list never contains
// another's.
func (s *Runs) ListPendingQuestionsIn(ctx context.Context, projectID string) ([]domain.Question, error) {
	var out []domain.Question
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		var err error
		out, err = tx.Questions().ListPendingByProject(ctx, projectID)
		return err
	})
	return out, err
}

// ListPendingQuestions returns questions, across every project, still waiting
// for the user. It is for the Control Center; everything else is project-scoped.
func (s *Runs) ListPendingQuestions(ctx context.Context) ([]domain.Question, error) {
	var out []domain.Question
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Questions().ListPending(ctx)
		return err
	})
	return out, err
}

// ---- events and helpers ----

func emitRun(em *emitter, r *domain.Run, from domain.RunState) error {
	snapshot := *r
	snapshot.Prompt = ""
	if !r.State.Terminal() || from == r.State {
		snapshot.Handoff = nil
	}
	ev := newEvent(domain.EventRunStateChanged, map[string]any{"run": &snapshot, "from": from, "compact": true})
	runIDs(&ev, r)
	return em.emit(ev)
}

func emitAgent(em *emitter, t domain.EventType, r *domain.Run, payload any) error {
	ev := newEvent(t, payload)
	runIDs(&ev, r)
	return em.emit(ev)
}

func emitOutput(em *emitter, r *domain.Run, out domain.AgentOutput) error {
	return emitAgent(em, domain.EventAgentOutput, r, out)
}

func emitAnswered(em *emitter, r *domain.Run, q *domain.Question) error {
	ev := newEvent(domain.EventQuestionAnswered, map[string]any{"question": q})
	runIDs(&ev, r)
	return em.emit(ev)
}

func emitClosed(em *emitter, r *domain.Run, q *domain.Question) error {
	ev := newEvent(domain.EventQuestionCancelled, map[string]any{"question": q})
	runIDs(&ev, r)
	return em.emit(ev)
}

func runIDs(ev *domain.Event, r *domain.Run) {
	ev.ProjectID, ev.TaskID, ev.RunID = r.ProjectID, r.TaskID, r.ID
}

func cleanOptions(opts []string) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		if o = truncate(strings.TrimSpace(o), maxOptionBytes); o != "" && len(out) < maxOptions {
			out = append(out, o)
		}
	}
	return out
}

// truncate cuts s to at most max bytes without splitting a character.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const mark = "\n… (cut)"
	cut := max - len(mark)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + mark
}

// oneLine reduces text to a short single line for display on a card.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > maxActivityRunes {
		return string(r[:maxActivityRunes-1]) + "…"
	}
	return s
}

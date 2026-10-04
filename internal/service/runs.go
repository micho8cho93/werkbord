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
}

// Limits on what an agent can make the controller store.
const (
	maxOutputBytes   = 16 << 10 // one output event
	maxPromptBytes   = 100 << 10
	maxQuestionBytes = 8 << 10
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
	now := s.now()
	r := &domain.Run{
		ID: domain.NewID(domain.PrefixRun), TaskID: in.TaskID, AgentID: in.AgentID, State: domain.RunStarting,
		WorktreeID: in.WorktreeID, Prompt: in.Prompt, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		task, err := tx.Tasks().Get(ctx, in.TaskID)
		if err != nil {
			return err
		}
		r.ProjectID = task.ProjectID
		prior, err := tx.Runs().ListByTask(ctx, task.ID)
		if err != nil {
			return err
		}
		for _, p := range prior {
			if !p.State.Terminal() {
				return fmt.Errorf("task %s already has an active run (%s, %s): %w", task.ID, p.ID, p.State, domain.ErrConflict)
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
	PID        int
	ProcessID  string // identifies the process across restarts; see domain.Run.ProcessID
	SessionRef string // the agent's resumable handle, if already known
	// Resumed is set when the process continues an existing run (a waiting run
	// whose process was lost) rather than beginning one.
	Resumed bool
}

// MarkStarted records that the agent process is up: the run goes from starting
// (or, for a resumed run, from waiting) to running, and the task moves to Doing.
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
		if in.Resumed != (from == domain.RunWaitingForUser) {
			return fmt.Errorf("%w: run %s is %s, cannot be started (resumed=%v)", domain.ErrTransition, id, from, in.Resumed)
		}
		if err := r.Transition(domain.RunRunning, "", s.now()); err != nil {
			return err
		}
		r.PID, r.ProcessID = in.PID, in.ProcessID
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
		return s.end(ctx, tx, em, r, in)
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Runs) end(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, in Ended) error {
	from := r.State
	if err := r.Transition(in.State, truncate(in.Reason, 1000), s.now()); err != nil {
		return err
	}
	r.ExitCode = in.ExitCode
	if err := cancelPending(ctx, tx, em, r, s.now()); err != nil {
		return err
	}
	if err := tx.Runs().Update(ctx, r); err != nil {
		return err
	}
	if err := emitRun(em, r, from); err != nil {
		return err
	}
	kind := map[domain.RunState]domain.EventType{
		domain.RunCompleted: domain.EventAgentCompleted,
		domain.RunFailed:    domain.EventAgentFailed,
		domain.RunStopped:   domain.EventAgentStopped,
	}[in.State]
	return emitAgent(em, kind, r, map[string]any{"run": r, "reason": r.Reason})
}

// cancelPending cancels the run's pending questions.
func cancelPending(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, now time.Time) error {
	qs, err := tx.Questions().ListByRun(ctx, r.ID)
	if err != nil {
		return err
	}
	for i := range qs {
		q := &qs[i]
		if q.Status != domain.QuestionPending {
			continue
		}
		q.Status, q.AnsweredAt = domain.QuestionCancelled, &now
		if err := tx.Questions().Update(ctx, q); err != nil {
			return err
		}
		if err := emitAnswered(em, r, q); err != nil {
			return err
		}
	}
	return nil
}

// NewQuestion is something the agent asked.
type NewQuestion struct {
	Kind    domain.QuestionKind
	Prompt  string
	Options []string
}

// RecordQuestion stores a question the agent is blocked on and makes the run
// wait for the user. An agent can have several questions open at once (parallel
// tool calls each ask for permission); the run waits until all are answered.
func (s *Runs) RecordQuestion(ctx context.Context, runID string, in NewQuestion) (*domain.Question, *domain.Run, error) {
	if !in.Kind.Valid() {
		in.Kind = domain.QuestionAsk
	}
	q := &domain.Question{
		ID: domain.NewID(domain.PrefixQuestion), RunID: runID, Kind: in.Kind,
		Prompt: truncate(strings.TrimSpace(in.Prompt), maxQuestionBytes), Options: cleanOptions(in.Options),
		Status: domain.QuestionPending, CreatedAt: s.now(),
	}
	if q.Prompt == "" {
		q.Prompt = "The agent needs your input."
	}
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if r, err = tx.Runs().Get(ctx, runID); err != nil {
			return err
		}
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

// AnswerQuestion records the user's answer. When it was the last question open
// the run goes back to running (and its task to Doing). It returns
// domain.ErrConflict if the question is not pending.
func (s *Runs) AnswerQuestion(ctx context.Context, questionID, answer string) (*domain.Question, *domain.Run, error) {
	answer = truncate(strings.TrimSpace(answer), maxQuestionBytes)
	if answer == "" {
		return nil, nil, fmt.Errorf("%w: an answer is required", domain.ErrInvalid)
	}
	var q *domain.Question
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if q, err = tx.Questions().Get(ctx, questionID); err != nil {
			return err
		}
		if q.Status != domain.QuestionPending {
			return fmt.Errorf("question %s is already %s: %w", q.ID, q.Status, domain.ErrConflict)
		}
		if r, err = tx.Runs().Get(ctx, q.RunID); err != nil {
			return err
		}
		now := s.now()
		q.Status, q.Answer, q.AnsweredAt = domain.QuestionAnswered, answer, &now
		if err := tx.Questions().Update(ctx, q); err != nil {
			return err
		}
		if err := emitAnswered(em, r, q); err != nil {
			return err
		}
		if err := emitOutput(em, r, domain.AgentOutput{Stream: domain.StreamUser, Text: answer}); err != nil {
			return err
		}

		qs, err := tx.Questions().ListByRun(ctx, r.ID)
		if err != nil {
			return err
		}
		for _, other := range qs {
			if other.Status == domain.QuestionPending {
				return nil // still waiting on another one
			}
		}
		if r.State != domain.RunWaitingForUser {
			return nil
		}
		return s.resume(ctx, tx, em, r, "answer")
	})
	if err != nil {
		return nil, nil, err
	}
	return q, r, nil
}

// resume moves a waiting run back to running, on behalf of the user.
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
// accepted. If the run was waiting for one, it is running again. A message sent
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
		if r.State == domain.RunWaitingForUser {
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
	case r.State == domain.RunRunning, r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitIdle:
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

// CancelQuestion cancels one pending question the agent withdrew. If it was the
// last one the run goes back to running: the agent is working again.
func (s *Runs) CancelQuestion(ctx context.Context, questionID string) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		q, err := tx.Questions().Get(ctx, questionID)
		if err != nil {
			return err
		}
		if q.Status != domain.QuestionPending {
			return nil
		}
		r, err := tx.Runs().Get(ctx, q.RunID)
		if err != nil {
			return err
		}
		now := s.now()
		q.Status, q.AnsweredAt = domain.QuestionCancelled, &now
		if err := tx.Questions().Update(ctx, q); err != nil {
			return err
		}
		if err := emitAnswered(em, r, q); err != nil {
			return err
		}
		qs, err := tx.Questions().ListByRun(ctx, r.ID)
		if err != nil {
			return err
		}
		for _, other := range qs {
			if other.Status == domain.QuestionPending {
				return nil
			}
		}
		if r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitQuestion {
			return s.resume(ctx, tx, em, r, "question_withdrawn")
		}
		return nil
	})
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
// A run that was waiting for the user, and has a session the agent can resume,
// is kept: it waits for a message, which will start a new process that
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
	if r.State != domain.RunWaitingForUser || r.SessionRef == "" {
		return s.end(ctx, tx, em, r, Ended{State: domain.RunFailed, Reason: reason})
	}
	now := s.now()
	if err := cancelPending(ctx, tx, em, r, now); err != nil {
		return err
	}
	r.PID, r.ProcessID, r.UpdatedAt = 0, "", now
	r.Waiting = domain.WaitIdle
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
			// A waiting run that is already idle, with no process, needs nothing.
			if r.State == domain.RunWaitingForUser && r.SessionRef != "" && r.Waiting == domain.WaitIdle && r.PID == 0 {
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

// ListPendingQuestions returns questions still waiting for the user.
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
	ev := newEvent(domain.EventRunStateChanged, map[string]any{"run": r, "from": from})
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

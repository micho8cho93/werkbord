package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/service"
)

// How a live run is being ended, if it is. The first request wins, except that
// a stop overrides a finish that is taking too long, and shutdown and abort
// override anything.
const (
	modeNone int32 = iota
	modeFinish
	modeStop
	modeShutdown
	modeAbort
)

// live is one run that has a process. A single goroutine (loop) reads the
// session's events and is the only one to turn them into state; the methods
// the user's actions call take mu, which that goroutine also holds while it
// records a question or the end of a turn, so an answer and the next question,
// or a message and the end of the turn it caused, are recorded in the order
// they happened.
type live struct {
	m         *Manager
	runID     string
	sess      agent.Session
	done      chan struct{}
	closeOnce sync.Once

	policy domain.ExecutionPolicy // the run's, fixed when it started

	mu        sync.Mutex
	questions map[string]string // question ID -> the adapter's ref for it
	refs      map[string]string // the adapter's ref -> question ID
	sessRef   string
	replies   int // questions the policy has answered for the user in this session

	mode        atomic.Int32
	rmu         sync.Mutex // guards the two below
	abortReason string
	forced      bool // the agent had to be stopped after being asked to finish

	// owned by loop
	outBytes int64
	capped   bool
	turn     []byte // what the agent said in the turn in progress, for a blocker report at its end
}

// maxTurnText bounds how much of a turn's speech is kept to look for a blocker
// report, which is always at the end of it.
const maxTurnText = 16 << 10

func newLive(m *Manager, run *domain.Run, sess agent.Session) *live {
	return &live{
		m: m, runID: run.ID, sess: sess, policy: run.Policy.Normalized(), done: make(chan struct{}),
		questions: map[string]string{}, refs: map[string]string{},
	}
}

func (l *live) start() { go l.loop() }

func (l *live) closeDone() { l.closeOnce.Do(func() { close(l.done) }) }

// ---- the event loop ----

func (l *live) loop() {
	opt := l.m.opt
	tick := time.NewTicker(opt.FlushInterval)
	defer tick.Stop()

	var pending []service.OutputItem
	size := 0
	flush := func() {
		if len(pending) == 0 {
			return
		}
		batch := pending
		pending, size = nil, 0
		if err := opt.Runs.AppendOutput(bg(), l.runID, batch); err != nil {
			l.m.log().Warn("cannot record agent output", "run", l.runID, "lines", len(batch), "err", err)
		}
	}

	events := l.sess.Events()
	for events != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			switch ev.Kind {
			case agent.KindOutput:
				l.hear(ev)
				if item, ok := l.admit(ev); ok {
					pending = append(pending, item)
					size += len(item.Text)
					if size >= opt.FlushBytes {
						flush()
					}
				}
			case agent.KindSessionRef:
				l.onSessionRef(ev.SessionRef)
			case agent.KindQuestion:
				flush() // what the agent said before asking comes before the question
				l.onQuestion(ev.Question)
			case agent.KindQuestionClosed:
				flush()
				l.onQuestionClosed(ev.Ref)
			case agent.KindTurnEnd:
				flush()
				l.onTurnEnd()
			}
		case <-tick.C:
			flush()
		}
	}
	flush()
	l.conclude(l.sess.Wait())
}

// hear keeps the end of what the agent says during a turn. Only a run that must
// stop when blocked has any use for it.
func (l *live) hear(ev agent.Event) {
	if ev.Stream != domain.StreamAssistant || l.policy.Interaction != domain.InteractionAutonomousStopIfBlocked {
		return
	}
	l.turn = append(append(l.turn, ev.Text...), '\n')
	if over := len(l.turn) - maxTurnText; over > 0 {
		l.turn = append(l.turn[:0], l.turn[over:]...)
	}
}

// admit applies the per-run output budget: past it, output is dropped, once
// with a notice, so one runaway agent cannot fill the disk.
func (l *live) admit(ev agent.Event) (service.OutputItem, bool) {
	if l.capped {
		return service.OutputItem{}, false
	}
	l.outBytes += int64(len(ev.Text))
	if l.outBytes > l.m.opt.MaxOutputBytes {
		l.capped = true
		return service.OutputItem{Stream: domain.StreamSystem,
			Text: fmt.Sprintf("This run has produced more than %d MiB of output. Further output is not kept.", l.m.opt.MaxOutputBytes>>20)}, true
	}
	return service.OutputItem{Stream: ev.Stream, Text: ev.Text}, true
}

func (l *live) onSessionRef(ref string) {
	l.mu.Lock()
	if ref == "" || ref == l.sessRef {
		l.mu.Unlock()
		return
	}
	l.sessRef = ref
	l.mu.Unlock()
	if err := l.m.opt.Runs.SetSessionRef(bg(), l.runID, ref); err != nil {
		l.m.log().Warn("cannot record the session reference", "run", l.runID, "err", err)
	}
}

func (l *live) onQuestion(q *agent.Question) {
	if q == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// The run's policy decides whether this goes to the user at all. It never
	// swallows an approval: see agent.HandleQuestion.
	if h := agent.HandleQuestion(l.policy, *q, l.replies); h.Action != agent.ActionAsk && l.reply(q, h) {
		return
	}

	rec, _, err := l.m.opt.Runs.RecordQuestion(bg(), l.runID, service.NewQuestion{
		Kind: q.Kind, Prompt: q.Prompt, Context: q.Context, Options: q.Options, AllowFreeText: q.AllowFreeText,
	})
	if err != nil {
		if l.mode.Load() == modeNone {
			// An agent blocked on a question nobody can see would hang for good.
			l.abort("could not record a question from the agent: " + err.Error())
		}
		return
	}
	l.questions[rec.ID], l.refs[q.Ref] = q.Ref, rec.ID
}

// reply deals with a question on the user's behalf, as its run's policy says:
// the agent is told to decide for itself, or, if the run must stop when blocked,
// to stop, and the run becomes blocked. The question is recorded, as answered by
// the policy, so the activity feed shows what was asked and what was done.
//
// It reports false if the policy could not deal with it here (the run is not in
// a state to be answered for, because it is already waiting on an approval), and
// the question should be put to the user after all. The caller holds l.mu.
func (l *live) reply(q *agent.Question, h agent.Handling) bool {
	runs := l.m.opt.Runs
	rec, _, err := runs.RecordReplied(bg(), l.runID, service.NewQuestion{
		Kind: q.Kind, Prompt: q.Prompt, Context: q.Context, Options: q.Options, AllowFreeText: q.AllowFreeText,
	}, service.Replied{Reply: h.Reply, Blocker: h.Blocker})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return false
		}
		if l.mode.Load() == modeNone {
			l.abort("could not record a question from the agent: " + err.Error())
		}
		return true
	}
	if h.Action == agent.ActionReply {
		l.replies++
	}
	l.m.log().Info("answered an agent's question for the user", "run", l.runID, "policy", l.policy.Interaction, "blocked", h.Action == agent.ActionBlock)

	dctx, cancel := context.WithTimeout(bg(), deliverTimeout)
	defer cancel()
	if err := l.sess.Respond(dctx, q.Ref, rec.Answer); err != nil {
		_ = l.undelivered(rec, q.Ref, err)
		return true
	}
	if _, err := runs.ConfirmDelivery(bg(), rec.ID); err != nil {
		l.m.log().Warn("cannot record that a reply reached the agent", "run", l.runID, "question", rec.ID, "err", err)
	}
	return true
}

func (l *live) onQuestionClosed(ref string) {
	l.mu.Lock()
	id := l.refs[ref]
	delete(l.refs, ref)
	delete(l.questions, id)
	l.mu.Unlock()
	if id == "" {
		return
	}
	if _, err := l.m.opt.Runs.CancelQuestion(bg(), id, domain.CancelWithdrawn); err != nil {
		l.m.log().Warn("cannot cancel a withdrawn question", "run", l.runID, "question", id, "err", err)
	}
}

func (l *live) onTurnEnd() {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := string(l.turn)
	l.turn = l.turn[:0]

	// A run that must stop when blocked ends its turn with a report when it cannot
	// go on: the run is blocked, not idle.
	if l.policy.Interaction == domain.InteractionAutonomousStopIfBlocked {
		if b, ok := agent.ParseBlocker(text); ok {
			if _, err := l.m.opt.Runs.Block(bg(), l.runID, b); err == nil {
				return
			}
			// Not running (it is waiting on an approval, or over): an ordinary end of turn.
		}
	}
	if _, err := l.m.opt.Runs.MarkIdle(bg(), l.runID); err != nil {
		l.m.log().Warn("cannot record the end of a turn", "run", l.runID, "err", err)
	}
}

// conclude records how the session ended, once the process is gone and all of
// its output has been written.
func (l *live) conclude(res agent.Result) {
	defer l.closeDone()
	defer l.m.unregister(l.runID)

	ctx, cancel := context.WithTimeout(bg(), 30*time.Second)
	defer cancel()
	code := res.ExitCode
	ended := service.Ended{State: res.State, Reason: res.Reason, ExitCode: &code}

	l.rmu.Lock()
	forced, abortReason := l.forced, l.abortReason
	l.rmu.Unlock()

	var err error
	mode := l.mode.Load()
	switch mode {
	case modeShutdown:
		_, err = l.m.opt.Runs.Interrupt(ctx, l.runID, "interrupted: controller shut down")
	default:
		switch {
		case mode == modeStop:
			ended.State, ended.Reason = domain.RunStopped, "stopped by user"
		case mode == modeAbort:
			ended.State, ended.Reason = domain.RunFailed, abortReason
		case mode == modeFinish && forced:
			ended.State, ended.Reason = domain.RunStopped, "the agent did not exit when asked to finish, so it was stopped"
		}
		_, err = l.m.opt.Runs.End(ctx, l.runID, ended)
	}
	if err != nil {
		// The run stays as it is in the database; recovery deals with it on the next start.
		l.m.log().Error("cannot record the end of a run", "run", l.runID, "err", err)
		return
	}
	if mode == modeShutdown {
		l.m.log().Info("agent process stopped for controller shutdown", "run", l.runID, "exit", res.ExitCode)
		return
	}
	l.m.log().Info("agent session ended", "run", l.runID, "state", ended.State, "exit", res.ExitCode, "reason", ended.Reason)
}

// ---- user actions ----

func (l *live) ending() error {
	if l.mode.Load() != modeNone {
		return fmt.Errorf("run %s is ending: %w", l.runID, domain.ErrConflict)
	}
	return nil
}

func (l *live) send(ctx context.Context, text string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ending(); err != nil {
		return err
	}
	if err := l.m.opt.Runs.CheckMessageable(ctx, l.runID); err != nil {
		return err
	}
	if err := l.sess.Send(ctx, text); err != nil {
		return err
	}
	// The agent has it. Recording failing is worth reporting, but the message is delivered.
	if _, err := l.m.opt.Runs.Message(bg(), l.runID, text); err != nil {
		return fmt.Errorf("the message reached the agent but could not be recorded: %w", err)
	}
	return nil
}

// deliverTimeout bounds handing an answer to the agent. It does not depend on
// the request that carried the answer: a client that disconnects must not leave
// an answer half-delivered.
const deliverTimeout = 30 * time.Second

// answer gives the user's answer to the agent: record it, deliver it to this
// session, then let the run go on.
//
//  1. The answer is recorded, so a crash cannot lose it.
//  2. It is delivered to the session that asked.
//  3. Delivery is recorded; when no other question holds the agent up, the run
//     is running again.
//
// All three happen under the run's lock, so competing answers are decided one
// at a time: the first is recorded, and each later one finds the question
// settled and is told how (see settled). If the agent cannot take the answer
// the question is cancelled, keeping what the user wrote, and the caller gets a
// *domain.QuestionClosedError saying what became of it.
func (l *live) answer(ctx context.Context, questionID, answer string) (*domain.Question, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	runs := l.m.opt.Runs

	// Read it under the lock: whoever answered first has finished by now.
	q, err := runs.GetQuestion(ctx, questionID)
	if err != nil {
		return nil, err
	}
	if !q.Pending() {
		return settled(q, answer)
	}
	if err := l.ending(); err != nil {
		return nil, err
	}
	ref, open := l.questions[questionID]
	if !open {
		// Recorded as open, but this session does not have it: the agent has moved on.
		cancelled, err := runs.CancelQuestion(bg(), questionID, domain.CancelWithdrawn)
		if err != nil {
			return nil, err
		}
		return nil, &domain.QuestionClosedError{Question: cancelled}
	}

	accepted, err := runs.AcceptAnswer(bg(), questionID, answer)
	if err != nil {
		return nil, err
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliverTimeout)
	defer cancel()
	if err := l.sess.Respond(dctx, ref, accepted.Answer); err != nil {
		return nil, l.undelivered(accepted, ref, err)
	}
	delete(l.questions, questionID)
	delete(l.refs, ref)
	delivered, err := runs.ConfirmDelivery(bg(), questionID)
	if err != nil {
		return nil, fmt.Errorf("the answer reached the agent but could not be recorded: %w", err)
	}
	return delivered, nil
}

// undelivered deals with an answer that was recorded but that the agent did not
// take. The question is cancelled, keeping the answer, unless the agent is stuck
// waiting for it and cannot be unblocked; then the run is ended, which cancels it.
func (l *live) undelivered(q *domain.Question, ref string, err error) error {
	var why domain.CancelReason
	switch {
	case errors.Is(err, agent.ErrEnded):
		why = domain.CancelRunEnded
	case errors.Is(err, agent.ErrUnknownQuestion):
		why = domain.CancelWithdrawn
	default:
		// The agent still waits on a request it will not get an answer to.
		l.abort("could not deliver an answer to the agent: " + err.Error())
		return fmt.Errorf("%w: could not deliver your answer: %v", domain.ErrAgent, err)
	}
	delete(l.questions, q.ID)
	delete(l.refs, ref)
	cancelled, cerr := l.m.opt.Runs.CancelQuestion(bg(), q.ID, why)
	if cerr != nil {
		return fmt.Errorf("the agent did not take the answer (%v) and the question could not be closed: %w", err, cerr)
	}
	return &domain.QuestionClosedError{Question: cancelled}
}

// settled answers a request to answer a question that is no longer pending. A
// repeat of the answer already given is a success, so a client that never heard
// the first reply can simply retry. Anything else is a conflict that carries the
// question as it now is.
func settled(q *domain.Question, answer string) (*domain.Question, error) {
	if q.DeliveredAt != nil && q.SameAnswer(answer) {
		return q, nil
	}
	return nil, &domain.QuestionClosedError{Question: q}
}

func (l *live) finish(ctx context.Context) (*domain.Run, error) {
	l.mu.Lock()
	if len(l.questions) > 0 {
		l.mu.Unlock()
		return nil, fmt.Errorf("run %s is waiting for an answer; answer it, or stop the run: %w", l.runID, domain.ErrConflict)
	}
	if !l.mode.CompareAndSwap(modeNone, modeFinish) {
		l.mu.Unlock()
		return nil, fmt.Errorf("run %s is already ending: %w", l.runID, domain.ErrConflict)
	}
	err := l.sess.Close(ctx)
	l.mu.Unlock()
	if err != nil {
		return nil, err
	}

	// An agent that ignores the request to finish is stopped, not waited on.
	go func() {
		select {
		case <-l.done:
		case <-time.After(l.m.opt.FinishTimeout):
			l.rmu.Lock()
			l.forced = true
			l.rmu.Unlock()
			_ = l.sess.Stop(bg())
		}
	}()
	return l.await(ctx, l.m.opt.FinishTimeout+l.m.opt.StopTimeout)
}

func (l *live) stop(ctx context.Context) (*domain.Run, error) {
	for {
		cur := l.mode.Load()
		if cur >= modeStop || l.mode.CompareAndSwap(cur, modeStop) {
			break
		}
	}
	sctx, cancel := context.WithTimeout(ctx, l.m.opt.StopTimeout)
	defer cancel()
	if err := l.sess.Stop(sctx); err != nil {
		return nil, fmt.Errorf("the agent did not stop: %w", err)
	}
	return l.await(ctx, l.m.opt.StopTimeout)
}

// await waits for the run to be fully ended and recorded, and returns it.
func (l *live) await(ctx context.Context, max time.Duration) (*domain.Run, error) {
	select {
	case <-l.done:
	case <-time.After(max):
		return nil, fmt.Errorf("run %s did not end in time", l.runID)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return l.m.opt.Runs.Get(ctx, l.runID)
}

// shutdown ends the session because the controller is going away.
func (l *live) shutdown() {
	l.mode.Store(modeShutdown)
	go func() { _ = l.sess.Stop(bg()) }()
}

// abort ends the session because the controller can no longer keep track of it.
func (l *live) abort(reason string) {
	if !l.mode.CompareAndSwap(modeNone, modeAbort) && !l.mode.CompareAndSwap(modeFinish, modeAbort) {
		return
	}
	l.rmu.Lock()
	l.abortReason = reason
	l.rmu.Unlock()
	l.m.log().Error("ending a run the controller cannot track", "run", l.runID, "reason", reason)
	go func() { _ = l.sess.Stop(bg()) }()
}

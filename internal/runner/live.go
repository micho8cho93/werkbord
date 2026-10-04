package runner

import (
	"context"
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

	mu        sync.Mutex
	questions map[string]string // question ID -> the adapter's ref for it
	refs      map[string]string // the adapter's ref -> question ID
	sessRef   string

	mode        atomic.Int32
	rmu         sync.Mutex // guards the two below
	abortReason string
	forced      bool // the agent had to be stopped after being asked to finish

	// owned by loop
	outBytes int64
	capped   bool
}

func newLive(m *Manager, run *domain.Run, sess agent.Session) *live {
	return &live{
		m: m, runID: run.ID, sess: sess, done: make(chan struct{}),
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
	rec, _, err := l.m.opt.Runs.RecordQuestion(bg(), l.runID, service.NewQuestion{Kind: q.Kind, Prompt: q.Prompt, Options: q.Options})
	if err != nil {
		if l.mode.Load() == modeNone {
			// An agent blocked on a question nobody can see would hang for good.
			l.abort("could not record a question from the agent: " + err.Error())
		}
		return
	}
	l.questions[rec.ID], l.refs[q.Ref] = q.Ref, rec.ID
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
	if err := l.m.opt.Runs.CancelQuestion(bg(), id); err != nil {
		l.m.log().Warn("cannot cancel a withdrawn question", "run", l.runID, "question", id, "err", err)
	}
}

func (l *live) onTurnEnd() {
	l.mu.Lock()
	defer l.mu.Unlock()
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

func (l *live) answer(ctx context.Context, questionID, answer string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ending(); err != nil {
		return err
	}
	ref, ok := l.questions[questionID]
	if !ok {
		return fmt.Errorf("question %s is not open: %w", questionID, domain.ErrConflict)
	}
	if err := l.sess.Respond(ctx, ref, answer); err != nil {
		return err
	}
	delete(l.questions, questionID)
	delete(l.refs, ref)
	if _, _, err := l.m.opt.Runs.AnswerQuestion(bg(), questionID, answer); err != nil {
		return fmt.Errorf("the answer reached the agent but could not be recorded: %w", err)
	}
	return nil
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

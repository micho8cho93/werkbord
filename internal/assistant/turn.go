package assistant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"devboard/internal/appops"
	"devboard/internal/assistant/provider"
	"devboard/internal/domain"
	"devboard/internal/store"
)

// turnError is a failure that is ready to be told to the person.
type turnError struct{ info ErrorInfo }

func (t *turnError) Error() string { return t.info.Message }

func failure(code, message string, retryable bool) *turnError {
	return &turnError{ErrorInfo{Code: code, Message: message, Retryable: retryable}}
}

// turnRun is the state of one turn.
type turnRun struct {
	e      *Engine
	h      *hub
	prov   provider.Provider
	turnID string
	p      appops.Principal

	mu       sync.Mutex
	sess     domain.AssistantSession // the session as this turn sees it
	attempts int
	// told is when the provider was last told what became of the changes it proposed.
	told time.Time
	// preamble is said to the provider once, with the next message, when its conversation had to be started again.
	preamble string
}

func (t *turnRun) publish(e Event) {
	e.SessionID, e.TurnID = t.sess.ID, t.turnID
	t.h.publish(e)
}

func (t *turnRun) ref() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sess.ProviderRef
}

func (t *turnRun) setRef(ref string) {
	t.mu.Lock()
	if t.sess.ProviderRef == ref {
		t.mu.Unlock()
		return
	}
	t.sess.ProviderRef = ref
	t.mu.Unlock()
	// Recorded at once: a turn that dies a moment later can still be continued.
	if _, err := t.e.update(context.Background(), t.sess.ID, func(s *domain.AssistantSession) { s.ProviderRef = ref }); err != nil {
		t.e.log.Error("could not record an assistant conversation handle", "session", t.sess.ID, "err", err)
	}
}

func (e *Engine) workDir() string {
	e.workOnce.Do(func() {
		dir := e.cfg.WorkDir
		if dir == "" {
			d, err := os.MkdirTemp("", "werkbord-assistant-")
			if err != nil {
				e.log.Error("no private directory for the assistant", "err", err)
				return
			}
			dir = d
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			e.log.Error("no private directory for the assistant", "dir", dir, "err", err)
			return
		}
		e.work = dir
	})
	return e.work
}

// runTurn is a turn from start to end. It runs on its own goroutine, owned by the engine.
func (e *Engine) runTurn(ctx context.Context, prov provider.Provider, s *domain.AssistantSession, run *activeTurn, text string) {
	defer e.wg.Done()
	t := &turnRun{e: e, h: e.hub(s.ID), prov: prov, turnID: run.id, p: e.principal(s), sess: *s}
	t.publish(Event{Type: EventTurnStarted})

	tctx, stop := context.WithTimeoutCause(ctx, e.cfg.TurnTimeout, errTurnTimeout)
	var usage Usage
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("assistant turn panicked", "session", s.ID, "panic", r)
				err = failure("internal", "The assistant hit an internal error. Nothing was changed.", true)
			}
		}()
		usage, err = t.conversation(tctx, text)
	}()
	cause := context.Cause(tctx)
	stop()

	// How did it end?
	var end Event
	switch {
	case err == nil:
		end = Event{Type: EventCompleted}
		if usage != (Usage{}) {
			end.Usage = &usage
		}
	case errors.Is(cause, errCancelled) || errors.Is(cause, errShutdown):
		end = Event{Type: EventCancelled, Text: cause.Error()}
	case errors.Is(cause, errTurnTimeout):
		end = Event{Type: EventFailed, Error: &ErrorInfo{Code: "timeout", Retryable: true,
			Message: fmt.Sprintf("The assistant did not finish within %s. Nothing was changed without your confirmation; try again, or ask for less at once.", e.cfg.TurnTimeout.Round(time.Second))}}
	default:
		var te *turnError
		if !errors.As(err, &te) {
			e.log.Error("assistant turn failed", "session", s.ID, "err", err)
			te = failure("internal", "The assistant hit an internal error.", true)
		}
		end = Event{Type: EventFailed, Error: &te.info}
	}

	// Record where the conversation stands, whatever happened, even if the turn was cancelled.
	bg := context.WithoutCancel(ctx)
	pending, perr := e.ops.Pending(bg, s.ID, t.p)
	if perr != nil {
		e.log.Error("could not read the changes waiting after an assistant turn", "session", s.ID, "err", perr)
	}
	state := domain.AssistantIdle
	if len(pending) > 0 {
		state = domain.AssistantAwaiting
	}
	now := e.now()
	_, uerr := e.update(bg, s.ID, func(s *domain.AssistantSession) {
		s.State, s.Turns = state, s.Turns+1
		s.LastError = ""
		if end.Type == EventFailed {
			s.LastError = end.Error.Code
		}
		if end.Type == EventCompleted {
			s.ReportedAt = t.reportedAt(now)
		}
		if ref := t.ref(); ref != "" {
			s.ProviderRef = ref
		} else {
			s.ProviderRef = ""
		}
	})
	if uerr != nil && !errors.Is(uerr, domain.ErrNotFound) { // not found: the conversation was deleted meanwhile
		e.log.Error("could not record the end of an assistant turn", "session", s.ID, "err", uerr)
	}
	e.release(s.ID, run)
	end.State = state
	t.publish(end)
}

// reportedAt is the point up to which outcomes have now been told to the provider: the time this turn began to be
// assembled, so an outcome that landed while it ran is told next time.
func (t *turnRun) reportedAt(now time.Time) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.told.IsZero() {
		return now
	}
	return t.told
}

// conversation is a message, and as many trips to the provider as it takes to get an answer in words.
func (t *turnRun) conversation(ctx context.Context, text string) (Usage, error) {
	e := t.e
	// Anything that expired while nobody looked is settled first, so that what the provider is told is true.
	if _, err := e.ops.Pending(ctx, t.sess.ID, t.p); err != nil {
		return Usage{}, failure("audit_unavailable", "The assistant's records could not be written, so it did not start.", true)
	}
	notices, told, err := t.outcomes(ctx)
	if err != nil {
		return Usage{}, err
	}
	t.mu.Lock()
	t.told = told
	t.mu.Unlock()
	prompt := formatNotices(notices) + text
	system := SystemPrompt(e.ops.Catalog(t.p), e.cfg.Now())

	var total Usage
	for step := 1; ; step++ {
		if step > e.cfg.MaxSteps {
			return total, failure("too_many_steps", fmt.Sprintf("The assistant kept asking for more information (%d rounds) without answering. Try a more specific question.", e.cfg.MaxSteps), true)
		}
		r, err := t.round(ctx, system, prompt)
		total.InputTokens += r.res.Usage.InputTokens
		total.OutputTokens += r.res.Usage.OutputTokens
		if err != nil {
			return total, err
		}
		if len(r.calls) == 0 {
			return total, nil
		}
		results, err := t.runCalls(ctx, r.calls)
		if err != nil {
			return total, err
		}
		prompt = formatResults(results)
	}
}

// outcomes tells what became of the changes the assistant proposed since it was last told.
func (t *turnRun) outcomes(ctx context.Context) (notices []string, told time.Time, err error) {
	var acts []domain.AssistantAction
	if err := t.e.st.View(ctx, func(tx store.Tx) (err error) { acts, err = tx.Assistant().ListActions(ctx, t.sess.ID); return }); err != nil {
		return nil, time.Time{}, failure("internal", "The conversation could not be read.", true)
	}
	told = t.e.now()
	for _, a := range acts {
		if !a.State.Settled() || a.ResolvedAt == nil || a.ResolvedAt.Before(t.sess.ReportedAt) {
			continue
		}
		sum := clipText(a.Summary, 300)
		switch a.State {
		case domain.ActionExecuted:
			notices = append(notices, fmt.Sprintf("The person confirmed this change and it was carried out: %s Result: %s", sum, clipText(a.Outcome, 200)))
		case domain.ActionFailed:
			notices = append(notices, fmt.Sprintf("The person confirmed this change but it could not be carried out: %s Reason: %s", sum, clipText(a.Outcome, 300)))
		case domain.ActionRejected:
			notices = append(notices, fmt.Sprintf("The person declined this change, so nothing was done: %s", sum))
		case domain.ActionExpired:
			notices = append(notices, fmt.Sprintf("This change was not confirmed in time and was dropped; nothing was done: %s", sum))
		}
		if len(notices) == 10 {
			break
		}
	}
	return notices, told, nil
}

type roundResult struct {
	text  string
	calls []call
	res   provider.Result
}

// round is one trip to the provider, with the retrying and recovering that make it dependable.
func (t *turnRun) round(ctx context.Context, system, prompt string) (roundResult, error) {
	e := t.e
	restarted := false
	for try := 0; ; try++ {
		if ctx.Err() != nil {
			return roundResult{}, ctx.Err()
		}
		t.mu.Lock()
		p := prompt
		if t.preamble != "" {
			p = t.preamble + p
		}
		t.mu.Unlock()
		r, err := t.attempt(ctx, system, p)
		if err == nil {
			t.mu.Lock()
			t.preamble = ""
			t.mu.Unlock()
			return r, nil
		}
		if ctx.Err() != nil {
			return roundResult{}, ctx.Err()
		}
		kind := provider.KindOf(err)
		switch {
		case kind == provider.KindSessionLost && t.ref() != "" && !restarted:
			// The provider no longer has the conversation. Start a fresh one, and say so, rather than fail.
			restarted = true
			lost := t.sess.Turns > 0
			t.setRef("")
			if lost {
				t.mu.Lock()
				t.preamble = "<werkbord-notice>\nYour earlier conversation with this person could not be restored, so this is a fresh start. You remember nothing from before. If you need something from earlier, ask them.\n</werkbord-notice>\n\n"
				t.mu.Unlock()
			}
			t.publish(Event{Type: EventNotice, Text: "The earlier conversation could not be continued, so a new one was started."})
			t.retryEvent()
			try--
			continue
		case provider.Retryable(err) && try < e.cfg.MaxRetries:
			wait := e.cfg.Backoff(try + 1)
			t.publish(Event{Type: EventNotice, Text: fmt.Sprintf("%s Trying again (%d of %d).", sentence(err.Error()), try+2, e.cfg.MaxRetries+1)})
			t.retryEvent()
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return roundResult{}, ctx.Err()
			}
			continue
		}
		return roundResult{}, toTurnError(err)
	}
}

func (t *turnRun) retryEvent() {
	t.mu.Lock()
	next := t.attempts + 1
	t.mu.Unlock()
	t.publish(Event{Type: EventRetry, Attempt: next})
}

// attempt is one run of the provider. The reply is streamed to the person as it arrives, minus any operation calls.
func (t *turnRun) attempt(ctx context.Context, system, prompt string) (r roundResult, err error) {
	e := t.e
	t.mu.Lock()
	t.attempts++
	n := t.attempts
	t.mu.Unlock()

	actx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	idle := time.AfterFunc(e.cfg.IdleTimeout, func() { cancel(errStalled) })
	defer idle.Stop()

	var visible strings.Builder
	f := newFilter(func(s string) {
		visible.WriteString(s)
		t.publish(Event{Type: EventDelta, Text: s, Attempt: n})
	})
	turn := provider.Turn{Ref: t.ref(), System: system, Prompt: prompt, Model: t.sess.Model, Reasoning: t.sess.Reasoning, WorkDir: e.workDir()}

	defer func() {
		if rec := recover(); rec != nil {
			e.log.Error("assistant provider panicked", "provider", t.prov.ID(), "panic", rec)
			err = provider.Errorf(provider.KindFailed, false, "The provider adapter failed unexpectedly.")
		}
	}()
	res, err := t.prov.Run(actx, turn, func(ev provider.Event) {
		idle.Reset(e.cfg.IdleTimeout)
		switch ev.Kind {
		case provider.EventText:
			f.Write(ev.Text)
		case provider.EventRef:
			if ev.Ref != "" {
				t.setRef(ev.Ref)
			}
		}
	})
	if err != nil {
		if errors.Is(context.Cause(actx), errStalled) && ctx.Err() == nil {
			return roundResult{}, provider.Errorf(provider.KindTransient, true, "The provider stopped answering for %s.", e.cfg.IdleTimeout.Round(time.Second))
		}
		return roundResult{}, err
	}
	f.Flush()
	if res.Ref != "" {
		t.setRef(res.Ref)
	}
	if strings.TrimSpace(visible.String()) == "" && len(f.calls) == 0 {
		return roundResult{}, provider.Errorf(provider.KindTransient, true, "The provider sent an empty reply.")
	}
	return roundResult{text: visible.String(), calls: f.calls, res: res}, nil
}

// runCalls runs the operations a reply asked for, as the session's principal, and returns what to tell the provider.
func (t *turnRun) runCalls(ctx context.Context, calls []call) ([]callResult, error) {
	var results []callResult
	for i, c := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := c.ID
		if id == "" {
			id = fmt.Sprintf("call-%d", i+1)
		}
		fail := func(code, msg string) {
			t.publish(Event{Type: EventToolResult, Tool: &ToolEvent{CallID: id, Name: c.Name, Status: "error", Code: code, Detail: clipText(msg, 300)}})
			results = append(results, callResult{ID: id, Name: c.Name, Status: "error", Body: map[string]any{"error": map[string]string{"code": code, "message": msg}}})
		}
		switch {
		case i >= maxCallsPerMessage:
			fail("too_many_calls", fmt.Sprintf("at most %d calls per message: this one was not run; ask for it in your next message", maxCallsPerMessage))
			continue
		case c.problem != "":
			fail("invalid_call", c.problem)
			continue
		}
		t.publish(Event{Type: EventToolCall, Tool: &ToolEvent{CallID: id, Name: c.Name, Detail: clipText(string(c.Arguments), 300)}})
		res, err := t.e.ops.Call(ctx, t.p, c.Name, c.Arguments)
		switch {
		case err != nil && appops.CodeOf(err) == appops.CodeAuditUnavailable:
			// Nothing may happen off the record, and no more may be asked of a store that cannot be written.
			return nil, failure("audit_unavailable", "The assistant's records could not be written, so it stopped. Nothing was changed.", true)
		case err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			fail(appops.CodeOf(err), err.Error())
		case res.Pending != nil:
			pr := res.Pending
			t.publish(Event{Type: EventToolResult, Tool: &ToolEvent{CallID: id, Name: c.Name, Status: "pending_confirmation", Detail: clipText(pr.Summary, 300)}})
			t.publish(Event{Type: EventConfirmationRequired, Proposal: pr, ActionID: pr.ActionID})
			results = append(results, callResult{ID: id, Name: c.Name, Status: "pending_confirmation", Body: map[string]any{
				"status": "pending_confirmation", "actionId": pr.ActionID, "summary": pr.Summary, "expiresAt": pr.ExpiresAt,
				"note": "Nothing has changed yet. The person has been asked to confirm. Tell them what you proposed.",
			}})
		default:
			t.publish(Event{Type: EventToolResult, Tool: &ToolEvent{CallID: id, Name: c.Name, Status: "ok"}})
			results = append(results, callResult{ID: id, Name: c.Name, Status: "ok", Body: res.Data})
		}
	}
	return results, nil
}

// toTurnError turns a provider's failure into what the person is told.
func toTurnError(err error) error {
	var pe *provider.Error
	if errors.As(err, &pe) {
		return failure(string(pe.Kind), pe.Message, pe.Retry || pe.Kind == provider.KindTransient)
	}
	return failure("internal", "The assistant hit an internal error.", true)
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s != "" && !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

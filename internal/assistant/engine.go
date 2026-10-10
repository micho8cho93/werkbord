// Package assistant is the conversational assistant's engine: the part that lets a person talk to their own coding
// agent runtime (Claude Code or Codex, signed in as they are) about their board, and have it look things up and prepare
// changes through the same application operations (internal/appops) that an MCP server will offer.
//
// The engine owns the conversation, not the provider. A turn is: the person's message goes to the provider, the
// reply streams back, any operation the reply asks for is run as the session's principal, and the results go back
// until the provider answers in words. A change an operation proposes is never carried out here; it waits for the
// person (Resolve). The providers are run with every tool turned off (internal/assistant/provider), so the only
// effects a model can have on the world are the ones appops allows and the person confirms.
//
// What it keeps is small: per conversation, the provider's handle for it, the model chosen and the changes waiting for
// confirmation, in the controller's database; and the audit of every operation. It keeps no transcript, and offers no
// way to browse one. The conversation itself lives with the provider.
//
// A turn belongs to the engine, not to whoever started it: closing the page that sent a message does not stop the
// reply, and a client that comes back resumes from the last event it saw. Cancel is explicit.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"devboard/internal/appops"
	"devboard/internal/assistant/provider"
	"devboard/internal/domain"
	"devboard/internal/store"
)

// Config is what an Engine needs.
type Config struct {
	Providers *provider.Registry
	Ops       *appops.Service
	Store     store.Store
	Log       *slog.Logger
	Now       func() time.Time
	// WorkDir is an empty directory private to the assistant, where the providers are run.
	WorkDir string
	// Grants and Projects are what every session's principal may do and see. Default: every permission, every project.
	Grants   []appops.Permission
	Projects []string

	// TurnTimeout bounds one message from start to finish, including every provider call and operation. Default 5m.
	TurnTimeout time.Duration
	// IdleTimeout is how long a provider may say nothing at all before the attempt is given up. Default 90s.
	IdleTimeout time.Duration
	// MaxSteps bounds how many times one message may go to the provider and back with results. Default 6.
	MaxSteps int
	// MaxRetries is how many times a failed attempt that may work again is repeated. Default 2.
	MaxRetries int
	// Backoff is the wait before a retry. Default 400ms, doubling.
	Backoff func(attempt int) time.Duration
	// MaxSessions bounds how many conversations exist at once. Default 50.
	MaxSessions int
	// AuditRetention is how long audit entries are kept. Default 400 days; at least 30; negative keeps them for ever.
	AuditRetention time.Duration
}

// DefaultAuditRetention is a little over a year: long enough to look back a full cycle of work, short enough that the
// audit stays a few tens of megabytes however much the assistant is used.
const DefaultAuditRetention = 400 * 24 * time.Hour

// Limits.
const (
	maxMessageChars = 20000
	turnPrefix      = "trn"
)

// Engine runs the conversations.
type Engine struct {
	cfg   Config
	log   *slog.Logger
	st    store.Store
	ops   *appops.Service
	provs *provider.Registry

	base       context.Context
	cancelBase context.CancelCauseFunc
	wg         sync.WaitGroup

	mu   sync.Mutex
	runs map[string]*activeTurn
	hubs map[string]*hub

	workOnce sync.Once
	work     string
}

type activeTurn struct {
	id     string
	cancel context.CancelCauseFunc
	done   chan struct{}
}

// The reasons a turn ends before it is done, as the cause of its context.
var (
	errCancelled   = errors.New("cancelled by the person")
	errShutdown    = errors.New("the controller is shutting down")
	errTurnTimeout = errors.New("the turn took too long")
	errStalled     = errors.New("the provider stopped answering")
)

// New builds an Engine. Call Recover before serving.
func New(cfg Config) *Engine {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Grants == nil {
		cfg.Grants = appops.AllPermissions
	}
	if cfg.TurnTimeout <= 0 {
		cfg.TurnTimeout = 5 * time.Minute
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 90 * time.Second
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 6
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	} else if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 2
	}
	if cfg.Backoff == nil {
		cfg.Backoff = func(attempt int) time.Duration { return 400 * time.Millisecond << (attempt - 1) }
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 50
	}
	e := &Engine{cfg: cfg, log: cfg.Log, st: cfg.Store, ops: cfg.Ops, provs: cfg.Providers, runs: map[string]*activeTurn{}, hubs: map[string]*hub{}}
	e.base, e.cancelBase = context.WithCancelCause(context.Background())
	return e
}

func (e *Engine) now() time.Time { return e.cfg.Now().UTC().Truncate(time.Millisecond) }

// principal is who a session acts as. It is rebuilt from configuration each time, so a narrowed grant takes effect on
// a conversation already under way.
func (e *Engine) principal(s *domain.AssistantSession) appops.Principal {
	return appops.Principal{
		ID: "assistant:" + s.ID, Actor: "assistant:" + s.Provider, Via: "assistant", SessionID: s.ID,
		Grants: e.cfg.Grants, Projects: e.cfg.Projects,
	}
}

func (e *Engine) hub(id string) *hub {
	e.mu.Lock()
	defer e.mu.Unlock()
	h := e.hubs[id]
	if h == nil {
		h = newHub()
		e.hubs[id] = h
	}
	return h
}

// ---- providers ----

// ProviderStatus is a provider as the person sees it when choosing one.
type ProviderStatus struct {
	provider.Info
	Models provider.Models `json:"models"`
}

// Providers reports what can be used on this computer.
func (e *Engine) Providers(ctx context.Context) []ProviderStatus {
	var out []ProviderStatus
	for _, p := range e.provs.All() {
		st := ProviderStatus{Info: p.Detect(ctx)}
		if st.Installed {
			st.Models = p.Models(ctx)
		} else {
			st.Models = provider.Models{ProviderID: p.ID(), Models: []provider.Model{}, Source: "builtin", Custom: true}
		}
		out = append(out, st)
	}
	return out
}

var (
	modelRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]-]{0,99}$`)
	reasoningRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,19}$`)
)

func checkChoice(model, reasoning string) error {
	// A name that starts with a dash would be read by the provider's command line as an option.
	if model != "" && !modelRE.MatchString(model) {
		return fmt.Errorf("%w: that is not a model name", domain.ErrInvalid)
	}
	if reasoning != "" && !reasoningRE.MatchString(reasoning) {
		return fmt.Errorf("%w: that is not a reasoning level", domain.ErrInvalid)
	}
	return nil
}

// ---- sessions ----

// CreateRequest starts a conversation.
type CreateRequest struct {
	Provider  string
	Model     string
	Reasoning string
}

// SessionView is a conversation as a client needs it: its state and the changes waiting for the person. There is no
// history in it, because there is none to give.
type SessionView struct {
	domain.AssistantSession
	// Running says a turn is in progress.
	Running bool `json:"running"`
	// Pending are the changes waiting for the person to confirm or decline.
	Pending []domain.AssistantAction `json:"pending"`
	// LastEvent is the newest event's Seq, for resuming a stream.
	LastEvent int64 `json:"lastEvent"`
}

// CreateSession starts a conversation with a provider. It fails at once, with what to do about it, if the provider is
// not installed or not signed in.
func (e *Engine) CreateSession(ctx context.Context, req CreateRequest) (*SessionView, error) {
	prov, ok := e.provs.Get(req.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: there is no assistant provider %q", domain.ErrInvalid, clipText(req.Provider, 40))
	}
	if err := checkChoice(req.Model, req.Reasoning); err != nil {
		return nil, err
	}
	if info := prov.Detect(ctx); !info.Available {
		return nil, unavailable(info)
	}
	if err := checkReasoning(ctx, prov, req.Model, req.Reasoning); err != nil {
		return nil, err
	}
	now := e.now()
	s := &domain.AssistantSession{ID: domain.NewID(domain.PrefixAssistantSession), Provider: req.Provider, Model: req.Model, Reasoning: req.Reasoning,
		State: domain.AssistantIdle, CreatedAt: now, UpdatedAt: now, ReportedAt: now}
	var tooMany bool
	err := e.st.Update(ctx, func(tx store.Tx) error {
		all, err := tx.Assistant().ListSessions(ctx)
		if err != nil {
			return err
		}
		if len(all) >= e.cfg.MaxSessions {
			tooMany = true
			return nil
		}
		return tx.Assistant().CreateSession(ctx, s)
	})
	if err != nil {
		return nil, err
	}
	if tooMany {
		return nil, fmt.Errorf("%w: there are already %d conversations: delete one first", domain.ErrConflict, e.cfg.MaxSessions)
	}
	if err := e.ops.Note(ctx, e.principal(s), "session_started", "provider "+s.Provider+modelNote(s)); err != nil {
		_ = e.st.Update(ctx, func(tx store.Tx) error { return tx.Assistant().DeleteSession(ctx, s.ID) })
		return nil, err
	}
	return e.view(ctx, s)
}

func modelNote(s *domain.AssistantSession) string {
	if s.Model == "" {
		return ""
	}
	return ", model " + s.Model
}

func unavailable(info provider.Info) error {
	return fmt.Errorf("%w: %s is not ready: %s. %s", domain.ErrAgent, info.Name, info.Detail, info.Guidance)
}

func (e *Engine) loadSession(ctx context.Context, id string) (*domain.AssistantSession, error) {
	var s *domain.AssistantSession
	err := e.st.View(ctx, func(tx store.Tx) (err error) { s, err = tx.Assistant().GetSession(ctx, id); return })
	return s, err
}

func (e *Engine) view(ctx context.Context, s *domain.AssistantSession) (*SessionView, error) {
	pending, err := e.ops.Pending(ctx, s.ID, e.principal(s))
	if err != nil {
		return nil, err
	}
	// Reload: expiring a stale change may have moved the session out of awaiting_confirmation.
	e.mu.Lock()
	_, running := e.runs[s.ID]
	e.mu.Unlock()
	if s2, err := e.loadSession(ctx, s.ID); err == nil {
		s = s2
	}
	if pending == nil {
		pending = []domain.AssistantAction{}
	}
	return &SessionView{AssistantSession: *s, Running: running, Pending: pending, LastEvent: e.hub(s.ID).last()}, nil
}

// Session returns a conversation.
func (e *Engine) Session(ctx context.Context, id string) (*SessionView, error) {
	s, err := e.loadSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return e.view(ctx, s)
}

// Sessions lists the conversations, most recently active first.
func (e *Engine) Sessions(ctx context.Context) ([]SessionView, error) {
	var all []domain.AssistantSession
	if err := e.st.View(ctx, func(tx store.Tx) (err error) { all, err = tx.Assistant().ListSessions(ctx); return }); err != nil {
		return nil, err
	}
	out := make([]SessionView, 0, len(all))
	for i := range all {
		v, err := e.view(ctx, &all[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// Configure changes which provider, model and reasoning level the turns that follow use. A person whose plan cannot use a
// model chooses another here, or moves to the other provider. Moving to another provider starts a fresh conversation
// with it (each provider keeps its own), which the assistant is told; changes waiting for confirmation are unaffected.
// Because a model belongs to its provider, the model and level must be given again when the provider changes.
func (e *Engine) Configure(ctx context.Context, id, providerID, model, reasoning string) (*SessionView, error) {
	if err := checkChoice(model, reasoning); err != nil {
		return nil, err
	}
	cur, err := e.loadSession(ctx, id)
	if err != nil {
		return nil, err
	}
	prov, ok := e.provs.Get(providerID)
	if !ok {
		return nil, fmt.Errorf("%w: there is no assistant provider %q", domain.ErrInvalid, clipText(providerID, 40))
	}
	switched := providerID != cur.Provider
	if switched {
		if info := prov.Detect(ctx); !info.Available {
			return nil, unavailable(info)
		}
	}
	if err := checkReasoning(ctx, prov, model, reasoning); err != nil {
		return nil, err
	}
	e.mu.Lock()
	_, running := e.runs[id]
	e.mu.Unlock()
	if running {
		return nil, fmt.Errorf("%w: wait for the reply to finish before changing the model", domain.ErrConflict)
	}
	s, err := e.update(ctx, id, func(s *domain.AssistantSession) {
		s.Model, s.Reasoning = model, reasoning
		if switched {
			s.Provider, s.ProviderRef, s.LastError = providerID, "", ""
		}
	})
	if err != nil {
		return nil, err
	}
	note := "model " + orDefault(model) + ", reasoning " + orDefault(reasoning)
	if switched {
		note = "provider " + cur.Provider + " -> " + providerID + ", " + note
	}
	if err := e.ops.Note(ctx, e.principal(s), "session_configured", note); err != nil {
		return nil, err
	}
	return e.view(ctx, s)
}

// checkReasoning refuses a reasoning level the chosen model does not take, and says which it does. Where the provider
// cannot say (a model it does not list, or no levels at all) the choice is let through and the provider has the last word.
func checkReasoning(ctx context.Context, prov provider.Provider, model, reasoning string) error {
	if reasoning == "" {
		return nil
	}
	m := prov.Models(ctx)
	var allowed []string
	for _, x := range m.Models {
		if (model != "" && x.ID == model) || (model == "" && x.Default) {
			allowed = x.Reasoning
		}
	}
	if len(allowed) == 0 {
		allowed = m.Reasoning
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, l := range allowed {
		if l == reasoning {
			return nil
		}
	}
	which := "that model"
	if model == "" {
		which = "the default model"
	}
	return fmt.Errorf("%w: %q is not a reasoning level %s takes (it takes: %s)", domain.ErrInvalid, reasoning, which, strings.Join(allowed, ", "))
}

func orDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

// update changes a session with compare-and-swap, trying again if another writer got in first. It uses a context that
// outlives the caller's, so the state is recorded even when a turn is being cancelled.
func (e *Engine) update(ctx context.Context, id string, mutate func(*domain.AssistantSession)) (*domain.AssistantSession, error) {
	ctx = context.WithoutCancel(ctx)
	var last error
	for try := 0; try < 4; try++ {
		var s *domain.AssistantSession
		err := e.st.Update(ctx, func(tx store.Tx) error {
			var err error
			if s, err = tx.Assistant().GetSession(ctx, id); err != nil {
				return err
			}
			mutate(s)
			s.UpdatedAt = e.now()
			return tx.Assistant().UpdateSession(ctx, s)
		})
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
		last = err
	}
	return nil, last
}

// DeleteSession ends a conversation: a turn in progress is stopped, changes it left waiting are dropped, and its
// record is removed. The audit of what it did stays. The provider keeps its own copy of the conversation in its own
// storage, which Werkbord does not reach into.
func (e *Engine) DeleteSession(ctx context.Context, id string) error {
	s, err := e.loadSession(ctx, id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	run := e.runs[id]
	e.mu.Unlock()
	if run != nil {
		run.cancel(errCancelled)
		select {
		case <-run.done:
		case <-time.After(10 * time.Second):
			return fmt.Errorf("%w: the reply could not be stopped; try again", domain.ErrConflict)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p := e.principal(s)
	n, err := e.ops.Withdraw(ctx, id, "the conversation was deleted", p)
	if err != nil {
		return err
	}
	if err := e.ops.Note(ctx, p, "session_deleted", fmt.Sprintf("%d waiting change(s) dropped", n)); err != nil {
		return err
	}
	if err := e.st.Update(ctx, func(tx store.Tx) error { return tx.Assistant().DeleteSession(ctx, id) }); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.hubs, id)
	e.mu.Unlock()
	return nil
}

// ---- sending ----

// Send starts a turn: the person's message goes to the provider and the reply streams as events. It returns as soon
// as the turn has begun. A conversation has one turn at a time.
func (e *Engine) Send(ctx context.Context, sessionID, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("%w: there is nothing to send", domain.ErrInvalid)
	}
	if len([]rune(text)) > maxMessageChars {
		return "", fmt.Errorf("%w: a message is at most %d characters", domain.ErrInvalid, maxMessageChars)
	}
	s, err := e.loadSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	prov, ok := e.provs.Get(s.Provider)
	if !ok {
		return "", fmt.Errorf("%w: this conversation's provider %q is not available in this version", domain.ErrInvalid, s.Provider)
	}
	if info := prov.Detect(ctx); !info.Available {
		return "", unavailable(info)
	}

	e.mu.Lock()
	if e.base.Err() != nil {
		e.mu.Unlock()
		return "", fmt.Errorf("%w: the controller is shutting down", domain.ErrConflict)
	}
	if _, busy := e.runs[sessionID]; busy {
		e.mu.Unlock()
		return "", fmt.Errorf("%w: the assistant is still replying to the last message", domain.ErrConflict)
	}
	turnID := domain.NewID(turnPrefix)
	tctx, cancel := context.WithCancelCause(e.base)
	run := &activeTurn{id: turnID, cancel: cancel, done: make(chan struct{})}
	e.runs[sessionID] = run
	e.mu.Unlock()

	s, err = e.update(ctx, sessionID, func(s *domain.AssistantSession) { s.State, s.LastError = domain.AssistantRunning, "" })
	if err != nil {
		e.release(sessionID, run)
		return "", err
	}
	e.wg.Add(1)
	go e.runTurn(tctx, prov, s, run, text)
	return turnID, nil
}

func (e *Engine) release(sessionID string, run *activeTurn) {
	e.mu.Lock()
	if e.runs[sessionID] == run {
		delete(e.runs, sessionID)
	}
	e.mu.Unlock()
	run.cancel(nil)
	select {
	case <-run.done:
	default:
		close(run.done)
	}
}

// Cancel stops the turn in progress, if any. The provider's process is stopped, with anything it started.
func (e *Engine) Cancel(sessionID string) (bool, error) {
	e.mu.Lock()
	run := e.runs[sessionID]
	e.mu.Unlock()
	if run == nil {
		if _, err := e.loadSession(context.Background(), sessionID); err != nil {
			return false, err
		}
		return false, nil
	}
	run.cancel(errCancelled)
	return true, nil
}

// Wait blocks until the conversation has no turn in progress, or ctx ends.
func (e *Engine) Wait(ctx context.Context, sessionID string) error {
	e.mu.Lock()
	run := e.runs[sessionID]
	e.mu.Unlock()
	if run == nil {
		return nil
	}
	select {
	case <-run.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Subscribe returns the conversation's events after seq, then live ones. A client that was disconnected passes the
// Seq of the last event it saw; the turn went on without it.
func (e *Engine) Subscribe(ctx context.Context, sessionID string, after int64) (<-chan Event, error) {
	if _, err := e.loadSession(ctx, sessionID); err != nil {
		return nil, err
	}
	return e.hub(sessionID).subscribe(ctx, sessionID, after), nil
}

// Shutdown stops every turn and waits for them to finish recording where they ended.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.cancelBase(errShutdown)
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---- confirming ----

// Resolve is the person's answer to a change the assistant proposed. It is the only way a proposal is carried out.
func (e *Engine) Resolve(ctx context.Context, sessionID, actionID string, approve bool) (*appops.Result, error) {
	s, err := e.loadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	p := e.principal(s)
	res, err := e.ops.Resolve(ctx, appops.Decision{SessionID: sessionID, ActionID: actionID, Approve: approve, By: "owner", Executor: p})
	h := e.hub(sessionID)
	switch {
	case err == nil:
		ev := Event{SessionID: sessionID, Type: EventConfirmationResolved, ActionID: actionID}
		if approve {
			if ch, ok := res.Data.(*appops.Changed); ok {
				ev.Outcome = ch.Message
			}
			ev.Text = "confirmed"
		} else {
			ev.Text, ev.Outcome = "declined", "declined by the person"
		}
		h.publish(ev)
	case appops.CodeOf(err) == appops.CodeExpired:
		h.publish(Event{SessionID: sessionID, Type: EventConfirmationResolved, ActionID: actionID, Text: "expired", Outcome: "nobody answered in time"})
	case approve && appops.CodeOf(err) != appops.CodeNotFound && appops.CodeOf(err) != appops.CodeNotPending && appops.CodeOf(err) != appops.CodeAuditUnavailable:
		// Confirmed, and then it could not be carried out.
		h.publish(Event{SessionID: sessionID, Type: EventConfirmationResolved, ActionID: actionID, Text: "failed", Outcome: err.Error()})
	}
	e.settle(ctx, s)
	return res, mapOpsError(err)
}

// settle brings an idle conversation's state in line with whether changes are waiting.
func (e *Engine) settle(ctx context.Context, s *domain.AssistantSession) {
	e.mu.Lock()
	_, running := e.runs[s.ID]
	e.mu.Unlock()
	if running {
		return
	}
	pending, err := e.ops.Pending(ctx, s.ID, e.principal(s))
	if err != nil {
		return
	}
	want := domain.AssistantIdle
	if len(pending) > 0 {
		want = domain.AssistantAwaiting
	}
	_, _ = e.update(ctx, s.ID, func(s *domain.AssistantSession) {
		if s.State != domain.AssistantRunning {
			s.State = want
		}
	})
}

// mapOpsError makes an operation's error one the API can answer with.
func mapOpsError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// Start begins the engine's housekeeping: the audit is pruned to its retention period now and once a day while the
// controller runs. Call it after Recover. Shutdown stops it.
func (e *Engine) Start() {
	keep := e.cfg.AuditRetention
	if keep < 0 {
		return
	}
	if keep == 0 {
		keep = DefaultAuditRetention
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for {
			if n, err := e.ops.PruneAudit(e.base, keep); err != nil && e.base.Err() == nil {
				e.log.Error("could not prune the assistant audit", "err", err)
			} else if n > 0 {
				e.log.Info("assistant audit pruned", "entries", n, "kept_days", int(keep.Hours()/24))
			}
			select {
			case <-time.After(24 * time.Hour):
			case <-e.base.Done():
				return
			}
		}
	}()
}

// ---- recovery ----

// Recover settles what a restart left in doubt: turns that were in progress ended with the process, changes that were
// being carried out are marked failed, changes whose time ran out are expired. Changes still waiting, and the
// conversations they belong to, are untouched, so a restart does not lose a proposal the person has not seen yet.
func (e *Engine) Recover(ctx context.Context) error {
	expired, interrupted, err := e.ops.Recover(ctx)
	if err != nil {
		return err
	}
	var all []domain.AssistantSession
	if err := e.st.View(ctx, func(tx store.Tx) (err error) { all, err = tx.Assistant().ListSessions(ctx); return }); err != nil {
		return err
	}
	fixed := 0
	for i := range all {
		s := &all[i]
		pending, err := e.ops.Pending(ctx, s.ID, e.principal(s))
		if err != nil {
			return err
		}
		want := domain.AssistantIdle
		if len(pending) > 0 {
			want = domain.AssistantAwaiting
		}
		if s.State == want {
			continue
		}
		lost := s.State == domain.AssistantRunning
		if _, err := e.update(ctx, s.ID, func(s *domain.AssistantSession) {
			s.State = want
			if lost {
				s.LastError = "interrupted"
			}
		}); err != nil {
			return err
		}
		fixed++
	}
	if expired+interrupted+fixed == 0 {
		return nil
	}
	return e.ops.Note(ctx, appops.Principal{Actor: "assistant:recovery"}, "recovered",
		fmt.Sprintf("%d conversation(s) reset, %d waiting change(s) expired, %d interrupted change(s) marked failed", fixed, expired, interrupted))
}

// Audit returns the audit trail, newest first, optionally for one conversation.
func (e *Engine) Audit(ctx context.Context, sessionID string, before int64, limit int) ([]domain.AssistantAuditEntry, error) {
	return e.ops.Audit(ctx, sessionID, before, limit)
}

// VerifyAudit checks the audit trail's chain.
func (e *Engine) VerifyAudit(ctx context.Context) (appops.AuditReport, error) {
	return e.ops.VerifyAudit(ctx)
}

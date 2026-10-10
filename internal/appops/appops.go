// Package appops is the one set of application operations through which anything other than the person's own hands
// looks at or changes Werkbord's board: the conversational assistant today, an MCP server later.
//
// Each operation has a name, a description, an input schema, a permission, and whether it only reads or changes
// something. They are the same for every caller; what differs is the Principal, who may do which of them and in which
// projects. The rules that make this safe live here, not in any caller:
//
//   - An operation reaches the board only through the existing domain services (internal/service), so every rule they
//     already enforce (validation, compare-and-swap versions, what a human-only task may become, who may answer which
//     question) is enforced here too. Nothing here writes to the store except the assistant's own records.
//   - A caller needs the operation's permission, and its projects are limited to the principal's. A project the
//     principal may not see is "not found", exactly like one that does not exist.
//   - A mutation is never carried out by the call that asks for it. The call checks it, works out exactly what would
//     change, writes that down as a proposal and returns it. Only Resolve, called on behalf of the person, carries
//     it out, once, with the arguments that were shown. A model cannot approve its own proposal: it has no way to call
//     Resolve, and a proposal belongs to the one principal that made it.
//   - Every call is audited before its result is returned, and a mutation's intent is audited before it is carried out.
//     If the audit cannot be written, the call is refused.
//
// Nothing here starts, stops or configures a run, touches Git or a repository, or changes a setting: those are the
// person's. Team's tickets are reached only as the tasks they were imported as; this package knows no Team code.
package appops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// Kind says whether an operation only looks or changes something.
type Kind string

const (
	KindRead     Kind = "read"
	KindMutation Kind = "mutation"
)

// Permission is what a principal needs to call an operation.
type Permission string

// The permissions. There is deliberately none for starting or stopping a run, for settings, Git, repositories,
// runners, or for deleting anything.
const (
	PermProjectsRead    Permission = "projects:read"
	PermTicketsRead     Permission = "tickets:read"
	PermTicketsCreate   Permission = "tickets:create"
	PermTicketsUpdate   Permission = "tickets:update"
	PermScheduleRead    Permission = "schedule:read"
	PermRunsRead        Permission = "runs:read"
	PermQuestionsRead   Permission = "questions:read"
	PermQuestionsAnswer Permission = "questions:answer"
)

// AllPermissions is every permission there is.
var AllPermissions = []Permission{
	PermProjectsRead, PermTicketsRead, PermTicketsCreate, PermTicketsUpdate, PermScheduleRead,
	PermRunsRead, PermQuestionsRead, PermQuestionsAnswer,
}

// Principal is who is calling: one assistant session, or later one MCP client.
type Principal struct {
	// ID names the principal and is what a proposal is bound to, such as "assistant:ast_k3j9x2m4q7p1a8z5".
	ID string
	// Actor is who the audit says did it, such as "assistant:claude-code".
	Actor string
	// Via is where the call came in: "assistant" or "mcp".
	Via string
	// SessionID ties its proposals to one conversation.
	SessionID string
	Grants    []Permission
	// Projects limits what the principal can see. nil means every project; an empty, non-nil list means none.
	Projects []string
}

// Can reports whether the principal holds the permission.
func (p Principal) Can(perm Permission) bool {
	for _, g := range p.Grants {
		if g == perm {
			return true
		}
	}
	return false
}

// CanSee reports whether the project is one of the principal's.
func (p Principal) CanSee(projectID string) bool {
	if p.Projects == nil {
		return true
	}
	for _, id := range p.Projects {
		if id == projectID {
			return true
		}
	}
	return false
}

// Spec describes an operation to a caller: what a model is shown and what an MCP server will list.
type Spec struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Kind        Kind   `json:"kind"`
	// Permission is what the caller needs.
	Permission Permission `json:"permission"`
	// RequiresConfirmation is true for every mutation: the call proposes, and the person decides.
	RequiresConfirmation bool    `json:"requiresConfirmation"`
	Input                *Schema `json:"inputSchema"`
}

// plan is what a mutation would do, worked out against the current state without changing anything.
type plan struct {
	// Args are the normalised arguments, the ones that will be carried out (a ticket's version filled in, for one).
	Args map[string]any
	// Summary is what the person is shown to decide on, in plain words, produced here rather than by the model.
	Summary string
}

type operation struct {
	Spec
	// plan is set for mutations; run for every operation. run is only reached for a mutation through Resolve.
	plan func(context.Context, *env, map[string]any) (*plan, error)
	run  func(context.Context, *env, map[string]any) (any, error)
	// scoped is true when the operation lists across projects, so its answer is limited to the principal's own.
}

// env is what an operation runs with.
type env struct {
	b   Backend
	p   Principal
	now time.Time
}

// Error codes. They are stable: callers (a model, an MCP client, the UI) branch on them.
const (
	CodeUnknownOperation  = "unknown_operation"
	CodePermissionDenied  = "permission_denied"
	CodeInvalidArguments  = "invalid_arguments"
	CodeNotFound          = "not_found"
	CodeConflict          = "conflict"
	CodeRefused           = "refused"
	CodeTooLarge          = "too_large"
	CodeTooManyPending    = "too_many_pending"
	CodeNotPending        = "not_pending"
	CodeExpired           = "expired"
	CodeAuditUnavailable  = "audit_unavailable"
	CodeUnavailable       = "unavailable"
	CodeFailed            = "failed"
	CodeApprovalIsPersons = "approval_is_the_persons"
)

// Error is why an operation did not happen, in words the caller can act on.
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

func newError(code string, err error) *Error {
	return &Error{Code: code, Message: err.Error(), Err: err}
}

func errorf(code, format string, a ...any) *Error {
	err := fmt.Errorf(format, a...)
	return &Error{Code: code, Message: err.Error(), Err: err}
}

// CodeOf is the code of err, or CodeFailed for an error that is not an *Error.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeFailed
}

// classify turns an error from a domain service into one a caller can act on. It says what the person-facing rules
// said, which is written for people; anything unexpected is reported plainly without its internals.
func classify(log *slog.Logger, op string, err error) *Error {
	var e *Error
	switch {
	case errors.As(err, &e):
		return e
	case errors.Is(err, domain.ErrNotFound):
		return newError(CodeNotFound, err)
	case errors.Is(err, domain.ErrForbidden):
		return newError(CodePermissionDenied, err)
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrDuplicate):
		return newError(CodeConflict, err)
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrTransition):
		return newError(CodeInvalidArguments, err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return newError(CodeUnavailable, errors.New("the request was interrupted before it finished"))
	}
	log.Error("application operation failed", "operation", op, "err", err)
	return &Error{Code: CodeFailed, Message: "the operation failed for a reason Werkbord could not explain", Err: err}
}

// Config is what a Service needs.
type Config struct {
	Backend Backend
	Store   store.Store
	Log     *slog.Logger
	Now     func() time.Time
	// ConfirmTTL is how long a proposal waits for the person. Default 15 minutes.
	ConfirmTTL time.Duration
}

// Service runs the operations.
type Service struct {
	b   Backend
	st  store.Store
	log *slog.Logger
	now func() time.Time
	ttl time.Duration
	ops map[string]*operation
}

const (
	defaultConfirmTTL = 15 * time.Minute
	// maxArgsBytes bounds what one call may carry in.
	maxArgsBytes = 64 << 10
	// maxResultBytes bounds what one call may hand back. A model's context is not a place to dump a database.
	maxResultBytes = 48 << 10
	// maxPendingPerSession bounds the proposals one conversation may leave waiting.
	maxPendingPerSession = 10
)

// New builds a Service with every operation.
func New(cfg Config) *Service {
	s := &Service{b: cfg.Backend, st: cfg.Store, log: cfg.Log, now: cfg.Now, ttl: cfg.ConfirmTTL, ops: map[string]*operation{}}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.ttl <= 0 {
		s.ttl = defaultConfirmTTL
	}
	for _, op := range operations() {
		if _, dup := s.ops[op.Name]; dup {
			panic("appops: two operations are called " + op.Name)
		}
		s.ops[op.Name] = op
	}
	return s
}

func (s *Service) time() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

// Catalog lists the operations the principal may call, by name. An operation it holds no permission for is not
// listed, so a model is never offered what would be refused.
func (s *Service) Catalog(p Principal) []Spec {
	var out []Spec
	for _, op := range s.ops {
		if p.Can(op.Permission) {
			out = append(out, op.Spec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Result is what a call returns.
type Result struct {
	Operation string `json:"operation"`
	// Data is the answer to a read, or the outcome of a mutation after the person confirmed it.
	Data any `json:"data,omitempty"`
	// Pending is set when the call was a mutation: it has been checked and written down, and nothing has changed.
	Pending *Proposal `json:"pending,omitempty"`
}

// Proposal is a change waiting for the person.
type Proposal struct {
	ActionID  string          `json:"actionId"`
	Operation string          `json:"operation"`
	Summary   string          `json:"summary"`
	Args      json.RawMessage `json:"args"`
	ExpiresAt time.Time       `json:"expiresAt"`
}

// Call performs an operation for a principal. A read returns its answer. A mutation returns a Proposal and changes
// nothing: only Resolve carries it out.
func (s *Service) Call(ctx context.Context, p Principal, name string, raw json.RawMessage) (*Result, error) {
	op := s.ops[name]
	if op == nil {
		return nil, s.refuse(ctx, p, name, domain.AuditRead, "", domain.AuditOutcomeInvalid, errorf(CodeUnknownOperation, "there is no operation called %q", clip(name, 60)))
	}
	kind := auditKind(op)
	if !p.Can(op.Permission) {
		return nil, s.refuse(ctx, p, name, kind, "", domain.AuditOutcomeDenied,
			errorf(CodePermissionDenied, "you are not permitted to use %s (it needs %s)", name, op.Permission))
	}
	args, canonical, err := decodeArgs(raw)
	if err != nil {
		return nil, s.refuse(ctx, p, name, kind, "", domain.AuditOutcomeInvalid, newError(CodeInvalidArguments, err))
	}
	hash := domain.ArgsDigest(canonical)
	if err := op.Input.Validate(args); err != nil {
		return nil, s.refuse(ctx, p, name, kind, hash, domain.AuditOutcomeInvalid, newError(CodeInvalidArguments, err))
	}
	if pid, ok := args["projectId"].(string); ok && !p.CanSee(pid) {
		// The same answer as a project that does not exist.
		return nil, s.refuseWith(ctx, p, name, kind, hash, domain.AuditOutcomeDenied, "a project outside this principal's projects",
			errorf(CodeNotFound, "project %s was not found", pid))
	}
	e := &env{b: s.b, p: p, now: s.time()}

	if op.Kind == KindRead {
		data, err := op.run(ctx, e, args)
		if err != nil {
			ce := classify(s.log, name, err)
			return nil, s.refuseWith(ctx, p, name, kind, hash, outcomeFor(ce), ce.Message, ce)
		}
		if size, err := sizeOf(data); err != nil {
			return nil, s.refuse(ctx, p, name, kind, hash, domain.AuditOutcomeFailed, classify(s.log, name, err))
		} else if size > maxResultBytes {
			return nil, s.refuse(ctx, p, name, kind, hash, domain.AuditOutcomeInvalid,
				errorf(CodeTooLarge, "the answer is too large to return (%d KB): ask for less, with a filter or a smaller limit", size>>10))
		}
		if err := s.record(ctx, nil, domain.AssistantAuditEntry{SessionID: p.SessionID, Actor: p.Actor, Operation: name, Kind: kind,
			Outcome: domain.AuditOutcomeOK, ArgsHash: hash, Detail: describeArgs(args)}); err != nil {
			return nil, err
		}
		return &Result{Operation: name, Data: data}, nil
	}

	pl, err := op.plan(ctx, e, args)
	if err != nil {
		ce := classify(s.log, name, err)
		return nil, s.refuseWith(ctx, p, name, kind, hash, outcomeFor(ce), ce.Message, ce)
	}
	normalised, err := json.Marshal(pl.Args)
	if err != nil {
		return nil, s.refuse(ctx, p, name, kind, hash, domain.AuditOutcomeFailed, classify(s.log, name, err))
	}
	return s.propose(ctx, p, op, normalised, clip(pl.Summary, 1000))
}

// propose writes a checked change down as a pending action, and the audit line for it, together. A change that is
// already waiting in this conversation is returned as it is rather than proposed twice: a model that repeats itself,
// or a turn that is replayed after a crash, does not pile up requests.
func (s *Service) propose(ctx context.Context, p Principal, op *operation, args []byte, summary string) (*Result, error) {
	hash := domain.ArgsDigest(args)
	now := s.time()
	var act *domain.AssistantAction
	var tooMany bool
	err := s.st.Update(ctx, func(tx store.Tx) error {
		existing, err := tx.Assistant().ListActions(ctx, p.SessionID)
		if err != nil {
			return err
		}
		open := 0
		for i := range existing {
			a := &existing[i]
			if a.State != domain.ActionPending || !now.Before(a.ExpiresAt) {
				continue
			}
			if a.Principal == p.ID && a.Operation == op.Name && a.ArgsHash == hash {
				act = a
				return nil
			}
			open++
		}
		if open >= maxPendingPerSession {
			tooMany = true
			return nil
		}
		act = &domain.AssistantAction{
			ID: domain.NewID(domain.PrefixAssistantAction), SessionID: p.SessionID, Principal: p.ID, Operation: op.Name,
			Args: args, ArgsHash: hash, Summary: summary, State: domain.ActionPending, CreatedAt: now, ExpiresAt: now.Add(s.ttl),
		}
		if err := tx.Assistant().CreateAction(ctx, act); err != nil {
			return err
		}
		return tx.Assistant().AppendAudit(ctx, &domain.AssistantAuditEntry{At: now, SessionID: p.SessionID, ActionID: act.ID, Actor: p.Actor,
			Operation: op.Name, Kind: domain.AuditMutation, Outcome: domain.AuditOutcomeProposed, ArgsHash: hash, Detail: summary})
	})
	switch {
	case tooMany:
		return nil, s.refuse(ctx, p, op.Name, domain.AuditMutation, hash, domain.AuditOutcomeDenied,
			errorf(CodeTooManyPending, "%d changes are already waiting for the person to confirm: wait for them to be answered first", maxPendingPerSession))
	case err != nil:
		s.log.Error("could not record a proposal", "operation", op.Name, "err", err)
		return nil, errorf(CodeAuditUnavailable, "the change could not be recorded, so it was not proposed")
	}
	return &Result{Operation: op.Name, Pending: &Proposal{ActionID: act.ID, Operation: op.Name, Summary: act.Summary, Args: act.Args, ExpiresAt: act.ExpiresAt}}, nil
}

// Decision is the person's answer to a proposal.
type Decision struct {
	SessionID string
	ActionID  string
	Approve   bool
	// By is the person, as the audit says: "owner" for the one person a controller has.
	By string
	// Executor is the principal the change is carried out as. It must be the one that proposed it: a different
	// principal, or the same one from another session, cannot use someone else's confirmation.
	Executor Principal
}

// Resolve carries out, or declines, a proposal on behalf of the person. It is not reachable from a model: only the
// controller's own API calls it, with the controller's credential.
func (s *Service) Resolve(ctx context.Context, d Decision) (*Result, error) {
	var act *domain.AssistantAction
	if err := s.st.View(ctx, func(tx store.Tx) (err error) { act, err = tx.Assistant().GetAction(ctx, d.ActionID); return }); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, errorf(CodeNotFound, "there is no such change waiting")
		}
		return nil, classify(s.log, "resolve", err)
	}
	// A proposal is found only by the session and principal that own it; anyone else sees what they would for one
	// that does not exist.
	if act.SessionID != d.SessionID || act.Principal != d.Executor.ID || d.Executor.SessionID != act.SessionID {
		return nil, errorf(CodeNotFound, "there is no such change waiting")
	}
	op := s.ops[act.Operation]
	now := s.time()
	by := d.By
	if by == "" {
		by = "owner"
	}

	if act.State != domain.ActionPending {
		return nil, errorf(CodeNotPending, "this change was already %s", act.State)
	}
	if !now.Before(act.ExpiresAt) {
		if err := s.finish(ctx, act, domain.ActionPending, domain.ActionExpired, domain.AuditOutcomeExpired, "nobody answered in time", d.Executor); err != nil {
			return nil, err
		}
		return nil, errorf(CodeExpired, "this change was not confirmed in time, so it was dropped: ask for it again")
	}
	if !d.Approve {
		if err := s.finish(ctx, act, domain.ActionPending, domain.ActionRejected, domain.AuditOutcomeRejected, "declined by "+by, d.Executor); err != nil {
			return nil, err
		}
		return &Result{Operation: act.Operation, Data: map[string]any{"state": domain.ActionRejected}}, nil
	}

	// Claim it: of any number of concurrent confirmations, exactly one gets past this line. The intent is audited in
	// the same step, so that a change is never carried out without a line saying who agreed to it.
	err := s.st.Update(ctx, func(tx store.Tx) error {
		if err := tx.Assistant().TransitionAction(ctx, act.ID, domain.ActionPending, domain.ActionExecuting, "", now); err != nil {
			return err
		}
		return tx.Assistant().AppendAudit(ctx, &domain.AssistantAuditEntry{At: now, SessionID: act.SessionID, ActionID: act.ID, Actor: d.Executor.Actor,
			Operation: act.Operation, Kind: domain.AuditMutation, Outcome: domain.AuditOutcomeConfirmed, ArgsHash: act.ArgsHash, Detail: "confirmed by " + by})
	})
	if errors.Is(err, domain.ErrConflict) {
		return nil, errorf(CodeNotPending, "this change was already answered")
	}
	if err != nil {
		s.log.Error("could not record a confirmation", "action", act.ID, "err", err)
		return nil, errorf(CodeAuditUnavailable, "the confirmation could not be recorded, so nothing was changed")
	}

	fail := func(outcome string, cause *Error) (*Result, error) {
		if err := s.finish(ctx, act, domain.ActionExecuting, domain.ActionFailed, domain.AuditOutcomeFailed, clip(outcome, 500), d.Executor); err != nil {
			s.log.Error("could not record a failed change", "action", act.ID, "err", err)
		}
		return nil, cause
	}
	// Nothing about the proposal is taken on trust from the row: the arguments are checked against the digest taken
	// when they were shown, and the principal is checked again, because grants and projects can change in between.
	if domain.ArgsDigest(act.Args) != act.ArgsHash {
		return fail("the stored change does not match what was shown", errorf(CodeFailed, "the stored change does not match what was shown, so it was not carried out"))
	}
	if op == nil || !d.Executor.Can(op.Permission) {
		return fail("no longer permitted", errorf(CodePermissionDenied, "the assistant is no longer permitted to do this"))
	}
	var args map[string]any
	if err := json.Unmarshal(act.Args, &args); err != nil {
		return fail("unreadable arguments", errorf(CodeFailed, "the stored change could not be read, so it was not carried out"))
	}
	if pid, ok := args["projectId"].(string); ok && !d.Executor.CanSee(pid) {
		return fail("outside the assistant's projects", errorf(CodeNotFound, "project %s was not found", pid))
	}
	data, err := op.run(ctx, &env{b: s.b, p: d.Executor, now: now}, args)
	if err != nil {
		ce := classify(s.log, act.Operation, err)
		return fail(ce.Message, ce)
	}
	outcome := ""
	if o, ok := data.(interface{ Outcome() string }); ok {
		outcome = o.Outcome()
	}
	if err := s.finish(ctx, act, domain.ActionExecuting, domain.ActionExecuted, domain.AuditOutcomeOK, clip(outcome, 500), d.Executor); err != nil {
		// The change is made; only the record of it is late. Say so rather than pretend it failed.
		s.log.Error("a change was carried out but could not be recorded as such", "action", act.ID, "err", err)
	}
	return &Result{Operation: act.Operation, Data: data}, nil
}

// finish moves an action to its end and writes the audit line for it, in one step.
func (s *Service) finish(ctx context.Context, act *domain.AssistantAction, from, to domain.AssistantActionState, outcome, detail string, who Principal) error {
	now := s.time()
	err := s.st.Update(ctx, func(tx store.Tx) error {
		if err := tx.Assistant().TransitionAction(ctx, act.ID, from, to, detail, now); err != nil {
			return err
		}
		return tx.Assistant().AppendAudit(ctx, &domain.AssistantAuditEntry{At: now, SessionID: act.SessionID, ActionID: act.ID, Actor: who.Actor,
			Operation: act.Operation, Kind: domain.AuditMutation, Outcome: outcome, ArgsHash: act.ArgsHash, Detail: detail})
	})
	if errors.Is(err, domain.ErrConflict) {
		return errorf(CodeNotPending, "this change was already answered")
	}
	return err
}

// Pending returns a session's changes that are still waiting, dropping any whose time has run out.
func (s *Service) Pending(ctx context.Context, sessionID string, who Principal) ([]domain.AssistantAction, error) {
	var all []domain.AssistantAction
	if err := s.st.View(ctx, func(tx store.Tx) (err error) { all, err = tx.Assistant().ListActions(ctx, sessionID); return }); err != nil {
		return nil, err
	}
	now := s.time()
	var out []domain.AssistantAction
	for i := range all {
		a := &all[i]
		if a.State != domain.ActionPending {
			continue
		}
		if !now.Before(a.ExpiresAt) {
			if err := s.finish(ctx, a, domain.ActionPending, domain.ActionExpired, domain.AuditOutcomeExpired, "nobody answered in time", who); err != nil && CodeOf(err) != CodeNotPending {
				return nil, err
			}
			continue
		}
		out = append(out, *a)
	}
	return out, nil
}

// Withdraw drops every change a session still has waiting. It is what ending or restarting a conversation does, so
// a proposal from a conversation the person has left can never be confirmed later.
func (s *Service) Withdraw(ctx context.Context, sessionID, why string, who Principal) (int, error) {
	var open []domain.AssistantAction
	if err := s.st.View(ctx, func(tx store.Tx) (err error) { open, err = tx.Assistant().ListActions(ctx, sessionID); return }); err != nil {
		return 0, err
	}
	n := 0
	for i := range open {
		if open[i].State != domain.ActionPending {
			continue
		}
		if err := s.finish(ctx, &open[i], domain.ActionPending, domain.ActionWithdrawn, "withdrawn", why, who); err != nil {
			if CodeOf(err) == CodeNotPending {
				continue
			}
			return n, err
		}
		n++
	}
	return n, nil
}

// Recover settles what a restart left in doubt. A proposal that is still waiting stays waiting, and survives the
// restart; one whose time ran out is expired; one that was being carried out when the controller stopped is marked
// failed, because whether it was applied is unknown, and the person is told to look rather than trust a guess.
func (s *Service) Recover(ctx context.Context) (expired, interrupted int, err error) {
	var open []domain.AssistantAction
	if err := s.st.View(ctx, func(tx store.Tx) (err error) { open, err = tx.Assistant().ListOpenActions(ctx); return }); err != nil {
		return 0, 0, err
	}
	system := Principal{Actor: "assistant:recovery"}
	now := s.time()
	for i := range open {
		a := &open[i]
		switch {
		case a.State == domain.ActionExecuting:
			err = s.finish(ctx, a, domain.ActionExecuting, domain.ActionFailed, domain.AuditOutcomeFailed,
				"Werkbord stopped while this change was being carried out. It may or may not have been applied: check the board before asking again.", system)
			interrupted++
		case !now.Before(a.ExpiresAt):
			err = s.finish(ctx, a, domain.ActionPending, domain.ActionExpired, domain.AuditOutcomeExpired, "nobody answered in time", system)
			expired++
		}
		if err != nil && CodeOf(err) != CodeNotPending {
			return expired, interrupted, err
		}
		err = nil
	}
	return expired, interrupted, nil
}

// Note writes a line to the audit about the assistant itself rather than an operation: a session starting, ending, or
// being recovered. If it cannot be written the caller is told, as for any other line.
func (s *Service) Note(ctx context.Context, p Principal, what, detail string) error {
	return s.record(ctx, nil, domain.AssistantAuditEntry{SessionID: p.SessionID, Actor: p.Actor, Operation: what, Kind: domain.AuditSystem,
		Outcome: domain.AuditOutcomeOK, Detail: clip(detail, 400)})
}

// Audit returns the trail, newest first.
func (s *Service) Audit(ctx context.Context, sessionID string, before int64, limit int) ([]domain.AssistantAuditEntry, error) {
	var out []domain.AssistantAuditEntry
	err := s.st.View(ctx, func(tx store.Tx) (err error) {
		out, err = tx.Assistant().ListAudit(ctx, sessionID, before, limit)
		return
	})
	return out, err
}

// AuditReport is the result of checking the chain.
type AuditReport struct {
	Entries int `json:"entries"`
	// BrokenAt is the Seq of the first entry whose hash or link is wrong; 0 if the chain is intact.
	BrokenAt int64  `json:"brokenAt"`
	Problem  string `json:"problem,omitempty"`
}

// VerifyAudit walks the whole trail and checks that every entry's hash is what its contents say and that each links to
// the one before it. A line taken out, edited or reordered in a copy of the database shows here.
func (s *Service) VerifyAudit(ctx context.Context) (AuditReport, error) {
	var all []domain.AssistantAuditEntry
	if err := s.st.View(ctx, func(tx store.Tx) (err error) { all, err = tx.Assistant().AllAudit(ctx); return }); err != nil {
		return AuditReport{}, err
	}
	rep := AuditReport{Entries: len(all)}
	prev := ""
	for _, e := range all {
		switch {
		case e.PrevHash != prev:
			rep.BrokenAt, rep.Problem = e.Seq, "this entry does not follow the one before it: something was removed or reordered"
		case domain.AuditHash(e) != e.Hash:
			rep.BrokenAt, rep.Problem = e.Seq, "this entry was changed after it was written"
		}
		if rep.BrokenAt != 0 {
			return rep, nil
		}
		prev = e.Hash
	}
	return rep, nil
}

// ---- helpers ----

func auditKind(op *operation) domain.AuditKind {
	if op.Kind == KindMutation {
		return domain.AuditMutation
	}
	return domain.AuditRead
}

func outcomeFor(e *Error) string {
	switch e.Code {
	case CodePermissionDenied, CodeNotFound, CodeRefused, CodeApprovalIsPersons:
		return domain.AuditOutcomeDenied
	case CodeInvalidArguments, CodeUnknownOperation, CodeTooLarge:
		return domain.AuditOutcomeInvalid
	}
	return domain.AuditOutcomeFailed
}

// refuse audits a call that did not go ahead and returns why. If the audit cannot be written the caller is told that
// instead: an unrecorded call is not a call.
func (s *Service) refuse(ctx context.Context, p Principal, name string, kind domain.AuditKind, hash, outcome string, e *Error) error {
	return s.refuseWith(ctx, p, name, kind, hash, outcome, e.Message, e)
}

func (s *Service) refuseWith(ctx context.Context, p Principal, name string, kind domain.AuditKind, hash, outcome, detail string, e *Error) error {
	if err := s.record(ctx, nil, domain.AssistantAuditEntry{SessionID: p.SessionID, Actor: p.Actor, Operation: name, Kind: kind,
		Outcome: outcome, ArgsHash: hash, Detail: clip(detail, 400)}); err != nil {
		return err
	}
	return e
}

// record appends a line to the audit.
func (s *Service) record(ctx context.Context, tx store.Tx, e domain.AssistantAuditEntry) error {
	e.At = s.time()
	e.Operation = clip(e.Operation, 80)
	var err error
	if tx != nil {
		err = tx.Assistant().AppendAudit(ctx, &e)
	} else {
		err = s.st.Update(ctx, func(tx store.Tx) error { return tx.Assistant().AppendAudit(ctx, &e) })
	}
	if err != nil {
		s.log.Error("the assistant audit could not be written", "operation", e.Operation, "err", err)
		return errorf(CodeAuditUnavailable, "the request could not be recorded, so it was not carried out")
	}
	return nil
}

// decodeArgs reads a call's arguments as an object, and returns them with a canonical encoding (sorted keys, no
// spacing) whose digest names them.
func decodeArgs(raw json.RawMessage) (map[string]any, []byte, error) {
	if len(raw) > maxArgsBytes {
		return nil, nil, fmt.Errorf("the arguments are larger than %d KB", maxArgsBytes>>10)
	}
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	var args map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&args); err != nil || args == nil {
		return nil, nil, errors.New("the arguments must be a JSON object")
	}
	if dec.More() {
		return nil, nil, errors.New("the arguments must be a single JSON object")
	}
	canonical, err := json.Marshal(args)
	if err != nil {
		return nil, nil, err
	}
	return args, canonical, nil
}

// describeArgs is the short, free-of-prose account of a read for the audit: identifiers and filters.
func describeArgs(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, clip(fmt.Sprint(args[k]), 60)))
	}
	return clip(strings.Join(parts, " "), 300)
}

func sizeOf(v any) (int, error) {
	b, err := json.Marshal(v)
	return len(b), err
}

// clip shortens s to at most max characters, cutting at a character boundary.
func clip(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

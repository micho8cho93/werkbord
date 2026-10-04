// Package agent is the boundary between the controller and coding agents.
//
// Claude Code and Codex speak different protocols (stream-JSON over stdio,
// JSON-RPC over stdio). Each gets an Adapter that translates its protocol into
// the small vocabulary below. The controller only ever talks to Adapters and
// Sessions; it never parses agent output itself.
//
// A Session is interactive: the agent works on a turn, reports TurnEnd, and
// waits for the next message from Send. It ends when its process exits, which
// the controller causes with Close (graceful) or Stop (terminate).
//
// Contract for implementers:
//
//   - The agent process belongs to the controller, not to the caller of Start.
//     The context passed to Start bounds start-up only; cancelling it after
//     Start has returned must not affect the process.
//   - Events never blocks the agent and never loses a control event. Output may
//     be dropped, with a notice, if the consumer falls far behind.
//   - Events is closed only after the process has exited and its output has been
//     delivered. Wait then returns without blocking for long.
//   - Methods are safe for concurrent use. After the process has ended, Send and
//     Respond fail with ErrEnded.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"devboard/internal/domain"
)

// Adapter drives one kind of agent.
type Adapter interface {
	// ID is the stable key stored on runs, e.g. "claude-code".
	ID() string
	// Detect reports whether the agent can be used on this machine (installed,
	// signed in) and its version, without starting a session. It is cheap enough
	// to call on every request: adapters cache what is expensive to find out.
	Detect(ctx context.Context) domain.Agent
	// Start launches the agent in req.WorkDir and delivers req.Prompt as its
	// first message. It returns once the process is running; failures that
	// happen later arrive as events and in the Result.
	Start(ctx context.Context, req StartRequest) (Session, error)
}

// StartRequest describes a session to launch.
type StartRequest struct {
	RunID     string
	WorkDir   string // the run's worktree: the agent's working directory
	Prompt    string // the first message
	ResumeRef string // a SessionRef from an earlier session, to continue that conversation
	// Policy is the run's execution policy, normalized. An adapter passes
	// Instructions(Policy) to its agent through whatever channel the agent has for
	// standing instructions (a system prompt, developer instructions), and does
	// nothing else with it: enforcement is the controller's, not the adapter's.
	// It applies on a resumed session too.
	Policy domain.ExecutionPolicy
}

// Session is a live agent process.
type Session interface {
	// Events delivers normalised events until the process has ended, then closes.
	Events() <-chan Event
	// Send delivers a message from the user. While the agent is working it is
	// queued or steers the turn, as the agent supports; while it waits it begins
	// the next turn.
	Send(ctx context.Context, text string) error
	// Respond answers a Question delivered earlier, by the Ref it carried. The
	// controller has already checked the answer against the question: for an
	// approval it is one of the question's options. It returns ErrUnknownQuestion
	// if the question is no longer open, and ErrEnded if the process is gone; in
	// both cases the agent did not receive it.
	Respond(ctx context.Context, ref, answer string) error
	// Close ends the session gracefully: no more messages, the agent finishes and
	// exits. The caller still has to Wait; Stop if it does not exit.
	Close(ctx context.Context) error
	// Stop terminates the process and everything it started. It returns when the
	// process is gone.
	Stop(ctx context.Context) error
	// Wait blocks until the session has ended and returns how.
	Wait() Result
	// Process identifies the agent's process, so that one a crashed controller
	// left behind can be found later.
	Process() ProcessInfo
}

// ProcessInfo identifies a process across controller restarts.
type ProcessInfo struct {
	PID int
	// ID distinguishes this process from a later one with the same PID. It is
	// empty if it could not be determined, in which case the process is never
	// signalled after a restart.
	ID string
}

// Errors returned by Sessions. They wrap the domain sentinels so that callers
// can map them without knowing about adapters.
var (
	// ErrEnded is returned by Send, Respond and Close once the process has ended.
	ErrEnded = fmt.Errorf("%w: the agent session has ended", domain.ErrConflict)
	// ErrUnknownQuestion is returned by Respond for a ref that is not open.
	ErrUnknownQuestion = fmt.Errorf("%w: that question is not open", domain.ErrConflict)
)

// EventKind classifies an Event.
type EventKind string

const (
	// KindOutput is text for the activity feed: something the agent said, a tool
	// it used, a notice.
	KindOutput EventKind = "output"
	// KindQuestion means the agent is blocked until Respond is called with the
	// question's Ref.
	KindQuestion EventKind = "question"
	// KindQuestionClosed means a question was withdrawn without an answer (the
	// agent gave up on it, or something else resolved it).
	KindQuestionClosed EventKind = "question_closed"
	// KindTurnEnd means the agent finished its turn and waits for a message.
	KindTurnEnd EventKind = "turn_end"
	// KindSessionRef reports the agent's resumable session handle.
	KindSessionRef EventKind = "session_ref"
)

// Event is one normalised piece of agent behaviour.
type Event struct {
	Kind       EventKind
	Stream     domain.OutputStream // KindOutput
	Text       string              // KindOutput; for KindTurnEnd, a short summary if the agent gave one
	Question   *Question           // KindQuestion
	Ref        string              // KindQuestionClosed: the question's Ref
	SessionRef string              // KindSessionRef
}

// Question is something the agent needs answered before it can continue, in
// the one form every adapter reports it. Adapters translate their protocol's
// permission requests and questions into it; the controller never sees the
// protocol. Everything but Ref is shown to the user as it is.
type Question struct {
	// Ref is adapter-defined and unique among the session's open questions. It is
	// how Respond and KindQuestionClosed name the question.
	Ref  string
	Kind domain.QuestionKind
	// Prompt is the question in a sentence or two; Context is what the user needs
	// to judge it (the command, the plan, the file), as plain text.
	Prompt  string
	Context string
	// Options are the suggested answers. AllowFreeText says whether a typed
	// answer is also accepted; set it only if the agent can use one. A question
	// without options takes free text whatever this says.
	Options       []string
	AllowFreeText bool
}

// Result is how a session ended. State is domain.RunCompleted when the process
// exited successfully and domain.RunFailed otherwise. The controller replaces
// it with domain.RunStopped when it was the one that asked the process to end.
type Result struct {
	State    domain.RunState
	Reason   string // why it failed; empty on success
	ExitCode int    // -1 if the process was killed by a signal
}

// Registry holds the adapters available to the controller.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

// Register adds an adapter. IDs must be unique.
func (r *Registry) Register(a Adapter) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.adapters[a.ID()]; ok {
		return fmt.Errorf("agent %q: %w", a.ID(), domain.ErrDuplicate)
	}
	r.adapters[a.ID()] = a
	return nil
}

// Get returns the adapter with the given ID.
func (r *Registry) Get(id string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[id]
	if !ok {
		return nil, fmt.Errorf("agent %q: %w", id, domain.ErrNotFound)
	}
	return a, nil
}

// Detect runs Detect on every adapter, sorted by ID.
func (r *Registry) Detect(ctx context.Context) []domain.Agent {
	r.mu.RLock()
	list := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		list = append(list, a)
	}
	r.mu.RUnlock()
	sort.Slice(list, func(i, j int) bool { return list[i].ID() < list[j].ID() })
	out := make([]domain.Agent, 0, len(list))
	for _, a := range list {
		out = append(out, a.Detect(ctx))
	}
	return out
}

// Available returns the named adapter if it can be used now, and otherwise an
// error saying why (domain.ErrNotFound for an unknown ID, domain.ErrConflict
// for one that is installed wrongly or not signed in).
func (r *Registry) Available(ctx context.Context, id string) (Adapter, error) {
	a, err := r.Get(id)
	if err != nil {
		return nil, err
	}
	if info := a.Detect(ctx); !info.Available {
		reason := info.Detail
		if reason == "" {
			reason = "unavailable"
		}
		return nil, fmt.Errorf("agent %q cannot be used: %s: %w", id, reason, domain.ErrConflict)
	}
	return a, nil
}

var errNotUnix = errors.New("agent processes are only supported on Unix systems")

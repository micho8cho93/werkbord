// Package agent is the boundary between the controller and coding agents.
//
// Claude Code, Codex and future agents speak different protocols (stream-JSON
// over stdio, JSON-RPC, plain text). Each gets an Adapter that translates its
// protocol into the small vocabulary below. The controller only ever talks to
// Adapters and Sessions; it never parses agent output itself.
//
// No adapters are implemented yet. The interface is intentionally limited to
// what both Claude Code and Codex are known to support today: start a session
// in a directory with a prompt, stream output, ask the user a question, resume
// a previous session, and stop.
package agent

import (
	"context"
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
	// signed in) without starting a session.
	Detect(ctx context.Context) domain.Agent
	// Start launches a session. The returned Session is owned by the caller,
	// which must eventually call Wait.
	Start(ctx context.Context, req StartRequest) (Session, error)
}

// StartRequest describes a session to launch.
type StartRequest struct {
	RunID     string
	WorkDir   string // the run's worktree; the agent must not leave it
	Prompt    string
	ResumeRef string // a SessionRef from an earlier run, to continue that conversation
}

// Session is a live agent process.
type Session interface {
	// Updates delivers normalised output until the session ends, then closes.
	Updates() <-chan Update
	// Answer replies to a question previously delivered as an Update.
	Answer(ctx context.Context, questionRef, answer string) error
	// Stop asks the agent to end. Wait still reports the outcome.
	Stop(ctx context.Context) error
	// Wait blocks until the session has ended.
	Wait() Result
}

// UpdateKind classifies an Update.
type UpdateKind string

const (
	UpdateOutput     UpdateKind = "output"      // progress text or a tool action, for the activity feed
	UpdateQuestion   UpdateKind = "question"    // the agent is blocked until the user answers
	UpdateSessionRef UpdateKind = "session_ref" // the agent's resumable session handle became known
)

// Update is one normalised piece of agent output.
type Update struct {
	Kind        UpdateKind
	Text        string   // output text, or the question prompt
	Options     []string // suggested answers, for questions
	QuestionRef string   // adapter-defined handle passed back to Answer
	SessionRef  string
}

// Result is how a session ended. State is RunCompleted, RunFailed or
// RunStopped.
type Result struct {
	State  domain.RunState
	Reason string
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

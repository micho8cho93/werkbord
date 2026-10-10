// Package provider is the boundary between the assistant and the agent runtimes whose conversation it borrows: Claude
// Code and Codex, signed in the way the person already signed them in.
//
// A provider answers one turn: it is given standing instructions, a message and, to continue a conversation, the
// handle it returned last time; it streams text back and says how the turn ended. That is all. A provider is not an
// agent here: it is run with every tool it has turned off, in an empty directory, and a turn in which it tries to
// use one anyway is ended as a violation (ErrPolicy). What the assistant can do to the board it does through
// application operations (internal/appops), in a protocol of its own text that the engine parses
// (internal/assistant), so the same operations serve every provider and, later, MCP.
//
// Nothing here asks for, stores or passes an API key. The provider's own command line does its own sign-in, and
// Detect reports whether it is signed in so the person can be told how to fix it.
package provider

import (
	"context"
	"errors"
	"fmt"

	"devboard/internal/domain"
)

// Turn is one exchange with the provider.
type Turn struct {
	// Ref continues the conversation the provider returned in an earlier turn. Empty starts a new one.
	Ref string
	// System is the standing instructions, sent on every turn so that a changed tool catalog takes effect.
	System string
	Prompt string
	// Model and Reasoning are the person's choices; empty means the provider's own default.
	Model     string
	Reasoning string
	// WorkDir is an empty directory private to the assistant. Providers keep their per-directory state under it.
	WorkDir string
}

// EventKind classifies an Event.
type EventKind string

const (
	// EventText is the next piece of the provider's reply.
	EventText EventKind = "text"
	// EventRef reports the conversation handle as soon as it is known, so that a turn that dies can be continued.
	EventRef EventKind = "ref"
	// EventAlive says the provider is working without having said anything yet (thinking). It exists so that a long
	// silent stretch is not mistaken for a stall.
	EventAlive EventKind = "alive"
)

// Event is one thing a provider reports during a turn.
type Event struct {
	Kind EventKind
	Text string // EventText
	Ref  string // EventRef
}

// Usage is what a turn used, as the provider reports it.
type Usage struct {
	InputTokens  int64 `json:"inputTokens,omitempty"`
	OutputTokens int64 `json:"outputTokens,omitempty"`
}

// Result is how a turn ended well.
type Result struct {
	// Text is the whole reply.
	Text string
	// Ref is the handle to continue with.
	Ref string
	// Model is the model that answered, when the provider says.
	Model string
	Usage Usage
}

// Provider runs turns.
type Provider interface {
	// ID is the stable key, such as "claude-code". It is the same key the coding-agent adapters use.
	ID() string
	// Detect says whether the provider can be used now, without starting a turn. It is cheap enough to call on every
	// request: implementations remember the answer briefly.
	Detect(ctx context.Context) Info
	// Models lists what can be chosen. It never fails: where the provider cannot say, it returns a fallback.
	Models(ctx context.Context) Models
	// Run performs one turn, calling emit for each event in order, and returns when the turn is over. Cancelling ctx
	// cancels the turn: the provider's process is stopped, and every process it started with it. emit must not block.
	Run(ctx context.Context, t Turn, emit func(Event)) (Result, error)
}

// Info is what is known about a provider on this computer.
type Info struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	// SignedIn is true, false, or unknown (nil) when the provider cannot say.
	SignedIn  *bool  `json:"signedIn,omitempty"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	// UsesAPIKey is true when the provider will bill an API key (found in the person's environment) instead of their plan.
	// Werkbord never asks for, stores or passes a key; this only lets the person know their own environment is doing it.
	UsesAPIKey bool `json:"usesApiKey,omitempty"`
	// Auth says how the person is signed in, as the provider reports it, such as "claude.ai" or "ChatGPT". It is never a secret.
	Auth string `json:"auth,omitempty"`
	// Detail says what is wrong when Available is false; Guidance says how to fix it.
	Detail       string       `json:"detail,omitempty"`
	Guidance     string       `json:"guidance,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
}

// Granularity is how finely a provider streams a reply.
type Granularity string

const (
	// StreamTokens: the reply arrives in small pieces as it is generated.
	StreamTokens Granularity = "tokens"
	// StreamMessages: the reply arrives whole, when the provider has finished it.
	StreamMessages Granularity = "messages"
)

// Capabilities is what was found out about a provider by trying it, not by reading its marketing. Each claim here
// is documented, with how it was checked and what is not supported, in docs/ASSISTANT.md.
type Capabilities struct {
	Streaming Granularity `json:"streaming"`
	// Resume: a later turn can continue the same conversation.
	Resume bool `json:"resume"`
	// Cancel: a turn can be stopped in progress.
	Cancel bool `json:"cancel"`
	// ModelChoice and ReasoningChoice: the person can pick them for the assistant.
	ModelChoice     bool `json:"modelChoice"`
	ReasoningChoice bool `json:"reasoningChoice"`
	// NativeTools says how the provider could call tools itself: "mcp" for both today. The assistant does not use it
	// (see docs/ASSISTANT.md); it uses the text protocol, which behaves the same everywhere.
	NativeTools string `json:"nativeTools"`
	// Voice: "none", or "experimental" for a voice path that exists but is not used.
	Voice string `json:"voice"`
	// Notes are the caveats worth showing to a person.
	Notes []string `json:"notes,omitempty"`
}

// Model is something that can be chosen.
type Model struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Default     bool     `json:"default,omitempty"`
	Reasoning   []string `json:"reasoning,omitempty"`
}

// Models is what a person may choose for a provider.
type Models struct {
	ProviderID string  `json:"providerId"`
	Models     []Model `json:"models"`
	// Reasoning are the levels that apply to every model, for a provider that does not tie them to one.
	Reasoning []string `json:"reasoning,omitempty"`
	// Source is "provider" (asked of it), "builtin" (a list that does not go stale, such as aliases) or "configured".
	Source string `json:"source"`
	// Custom says a model that is not listed can still be typed in.
	Custom bool   `json:"custom"`
	Note   string `json:"note,omitempty"`
}

// Kind says why a turn failed, so that the engine and the person can tell what to do about it.
type Kind string

const (
	// KindNotInstalled: the provider's command is not on this computer.
	KindNotInstalled Kind = "not_installed"
	// KindNotSignedIn: the provider is installed but the person has not signed in (or the sign-in expired).
	KindNotSignedIn Kind = "not_signed_in"
	// KindRateLimited: the person's plan has run out of allowance for now.
	KindRateLimited Kind = "rate_limited"
	// KindModelUnavailable: the chosen model is not available to this account.
	KindModelUnavailable Kind = "model_unavailable"
	// KindSessionLost: the provider no longer has the conversation to continue.
	KindSessionLost Kind = "session_lost"
	// KindTransient: the provider or its service had a passing problem; the same turn may well work again.
	KindTransient Kind = "transient"
	// KindPolicy: the provider tried to use a tool of its own. The turn was ended.
	KindPolicy Kind = "policy"
	// KindProtocol: the provider spoke in a way this version of Werkbord does not understand.
	KindProtocol Kind = "protocol"
	// KindFailed: anything else.
	KindFailed Kind = "failed"
)

// Error is why a turn failed.
type Error struct {
	Kind    Kind
	Message string
	// Retry is when a retry could help; the engine retries only these.
	Retry bool
	Err   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

// Errorf builds an Error.
func Errorf(kind Kind, retry bool, format string, a ...any) *Error {
	err := fmt.Errorf(format, a...)
	return &Error{Kind: kind, Retry: retry, Message: err.Error(), Err: err}
}

// ErrPolicy is wrapped by the error for a turn in which the provider tried to use a tool.
var ErrPolicy = fmt.Errorf("%w: the provider tried to use a tool of its own", domain.ErrForbidden)

// KindOf is the kind of err, or KindFailed.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindFailed
}

// Retryable reports whether err is one a retry may fix.
func Retryable(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Retry
}

// Registry holds the providers available to the assistant.
type Registry struct {
	byID  map[string]Provider
	order []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{byID: map[string]Provider{}} }

// Register adds a provider. IDs must be unique.
func (r *Registry) Register(p Provider) error {
	if _, dup := r.byID[p.ID()]; dup {
		return fmt.Errorf("assistant: provider %q is registered twice", p.ID())
	}
	r.byID[p.ID()] = p
	r.order = append(r.order, p.ID())
	return nil
}

// Get returns the provider with the ID.
func (r *Registry) Get(id string) (Provider, bool) { p, ok := r.byID[id]; return p, ok }

// All returns the providers in the order they were registered.
func (r *Registry) All() []Provider {
	out := make([]Provider, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id])
	}
	return out
}

package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// An agent session is one real execution of a coding agent that a person can open, read and talk to. Werkbord keeps
// three kinds of record that touch such executions, and they are different concepts on purpose:
//
//   - a Run: a structured, task-bound execution owned by the runner (domain.Run), with a feed, questions and a worktree;
//   - a Terminal: the agent's own interactive command line in a repository, owned by the terminal service, which may or
//     may not belong to a task;
//   - an assistant conversation (AssistantSession): Salie talking to the person. It is not an agent execution; when it
//     launches one it only points at it (Run.Origin, AgentSession.LaunchedBy).
//
// This file adds no table and no second copy of any of them. SessionRef is the one name an execution has wherever it is
// shown (Salie, Agents, Work, Calendar), and AgentSession is the read model that lists them side by side. The record
// that owns the execution stays the single source of truth; opening a session in another place resolves its SessionRef
// to that record and attaches to the same process.

// SessionKind says which record owns an execution.
type SessionKind string

const (
	// SessionRun is a task run, owned by the runner.
	SessionRun SessionKind = "run"
	// SessionTerminal is a native agent terminal, owned by the terminal service.
	SessionTerminal SessionKind = "terminal"
)

// Valid reports whether k is a known kind.
func (k SessionKind) Valid() bool { return k == SessionRun || k == SessionTerminal }

// SessionRef names one execution: the owning record's kind and its identifier. Its text form, "run:run_k3j9x2m4q7p1a8z5" or
// "terminal:<id>", is what routes, links and conversations carry. It is derived from the owner's own ID, never
// minted separately, so two places that show the same execution cannot disagree about which one it is.
type SessionRef struct {
	Kind SessionKind `json:"kind"`
	ID   string      `json:"id"`
}

var sessionRefID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)

// NewSessionRef returns the reference to a record of the given kind.
func NewSessionRef(kind SessionKind, id string) (SessionRef, error) {
	if !kind.Valid() {
		return SessionRef{}, fmt.Errorf("%w: unknown session kind %q", ErrInvalid, kind)
	}
	if !sessionRefID.MatchString(id) {
		return SessionRef{}, fmt.Errorf("%w: invalid session id", ErrInvalid)
	}
	return SessionRef{Kind: kind, ID: id}, nil
}

// RunRef and TerminalRef are the references to a run and to a native terminal.
func RunRef(id string) SessionRef      { return SessionRef{Kind: SessionRun, ID: id} }
func TerminalRef(id string) SessionRef { return SessionRef{Kind: SessionTerminal, ID: id} }

// String is the text form, "<kind>:<id>".
func (r SessionRef) String() string { return string(r.Kind) + ":" + r.ID }

// ParseSessionRef is the inverse of String. Anything that is not exactly a kind, a colon and an identifier is refused,
// so a reference taken from a URL or a conversation can be passed on without further checking.
func ParseSessionRef(s string) (SessionRef, error) {
	kind, id, ok := strings.Cut(s, ":")
	if !ok {
		return SessionRef{}, fmt.Errorf("%w: a session reference is <kind>:<id>", ErrInvalid)
	}
	return NewSessionRef(SessionKind(kind), id)
}

// SessionOriginSalie marks an execution that Salie launched; the text after the colon is the conversation's ID.
const SessionOriginSalie = "salie"

// SalieOrigin is the origin recorded on a run that the assistant conversation launched.
func SalieOrigin(conversationID string) string { return SessionOriginSalie + ":" + conversationID }

// ConversationOf is the Salie conversation an origin names, or "" for any other origin.
func ConversationOf(origin string) string {
	kind, id, ok := strings.Cut(origin, ":")
	if !ok || kind != SessionOriginSalie {
		return ""
	}
	return id
}

// Capabilities say what a session can do, so that every screen offers only what its provider-specific session
// supports instead of pretending all sessions are alike.
type SessionCapabilities struct {
	// Feed: a structured, persisted activity feed that survives restarts.
	Feed bool `json:"feed"`
	// Message: structured follow-up instructions (a run). Terminal input is separate.
	Message bool `json:"message"`
	// Terminal: the agent's native interactive UI, reached through the terminal renderer.
	Terminal bool `json:"terminal"`
	// Questions: the session raises questions and approvals the person answers.
	Questions bool `json:"questions"`
	// Stop: the session can be ended from here.
	Stop bool `json:"stop"`
}

// AgentSession is the read model of one execution, as every screen lists it. It is assembled from the owning record on
// each read and never stored.
type AgentSession struct {
	// Key is SessionRef.String(): the one identity of this execution.
	Key       string      `json:"key"`
	Kind      SessionKind `json:"kind"`
	ID        string      `json:"id"`
	ProjectID string      `json:"projectId"`
	// TaskID and PlanIDs are the work this session is associated with; both are empty for a standalone session. A plan
	// is reached through its task: plans reference tasks, never sessions.
	TaskID  string   `json:"taskId,omitempty"`
	PlanIDs []string `json:"planIds,omitempty"`
	AgentID string   `json:"agentId"`
	Title   string   `json:"title"`
	// State is the owner's own state word (a RunState, or a terminal's running/exited/stopped/interrupted).
	State string `json:"state"`
	// Live is true while an execution is still running or waiting for the person.
	Live bool `json:"live"`
	// Waiting is true when the session needs the person: a question or approval is open, or a run was blocked at a decision.
	Waiting bool `json:"waiting,omitempty"`
	// Device is where it runs: a runner ID for a run, the execution device for a terminal.
	Device string `json:"device,omitempty"`
	// Repository names the repository a terminal was opened in.
	Repository string `json:"repository,omitempty"`
	// LaunchedBy is the Salie conversation that started it, when one did.
	LaunchedBy   string              `json:"launchedBy,omitempty"`
	Capabilities SessionCapabilities `json:"capabilities"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}

// SessionOfRun is the read model of a run.
func SessionOfRun(r Run, title string, planIDs []string) AgentSession {
	live := r.State.Active()
	return AgentSession{
		Key: RunRef(r.ID).String(), Kind: SessionRun, ID: r.ID, ProjectID: r.ProjectID, TaskID: r.TaskID, PlanIDs: planIDs,
		AgentID: r.AgentID, Title: title, State: string(r.State), Live: live,
		// Needs the person: a question or approval is open, or the policy stopped the run at a decision. An agent that has only finished
		// its turn and awaits the next instruction is idle, not asking.
		Waiting:    (r.State == RunWaitingForUser && r.Waiting == WaitQuestion) || r.State == RunBlocked,
		Device:     r.RunnerID,
		LaunchedBy: ConversationOf(r.Origin),
		Capabilities: SessionCapabilities{
			Feed: true, Message: live, Questions: true, Stop: live,
		},
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

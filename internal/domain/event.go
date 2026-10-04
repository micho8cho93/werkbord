package domain

import (
	"encoding/json"
	"time"
)

// EventType names something that happened. Types are dotted, noun first.
type EventType string

const (
	EventProjectRegistered EventType = "project.registered"
	EventProjectInspected  EventType = "project.inspected"
	EventTaskCreated       EventType = "task.created"
	EventTaskUpdated       EventType = "task.updated"
	EventRunStateChanged   EventType = "run.state_changed"
	EventWorktreeCreated   EventType = "worktree.created"
	EventWorktreeRemoving  EventType = "worktree.removing"
	EventWorktreeRemoved   EventType = "worktree.removed"

	// Question events. A question is announced by agent.question (payload:
	// {question}) and closed by exactly one of these, whose payload is also
	// {question}, carrying the question's final state. They are self-contained
	// and durable, so a notifier can be built later from the log alone.
	EventQuestionAnswered  EventType = "question.answered"  // the user's answer was recorded
	EventQuestionCancelled EventType = "question.cancelled" // it can no longer be answered; the question says why

	// Agent events describe what happens inside a run. They are the activity
	// timeline; every state change among them is accompanied by a
	// run.state_changed event carrying the run, which is what clients use to
	// keep their copy of the run current.
	EventAgentStarted   EventType = "agent.started"   // the agent process is up and has its first message
	EventAgentOutput    EventType = "agent.output"    // payload: AgentOutput
	EventAgentQuestion  EventType = "agent.question"  // the agent is blocked on an answer; payload: {question}
	EventAgentWaiting   EventType = "agent.waiting"   // the agent finished its turn and awaits a message
	EventAgentBlocked   EventType = "agent.blocked"   // the run stopped rather than guess; payload: {blocker}
	EventAgentResumed   EventType = "agent.resumed"   // the user's message or answer reached the agent
	EventAgentCompleted EventType = "agent.completed" // the session ended normally
	EventAgentFailed    EventType = "agent.failed"    // setup, the process or the agent failed
	EventAgentStopped   EventType = "agent.stopped"   // the user (or shutdown) stopped the session
)

// OutputStream says where a piece of agent output came from.
type OutputStream string

const (
	StreamAssistant OutputStream = "assistant" // what the agent said
	StreamTool      OutputStream = "tool"      // a tool call or its result, summarised
	StreamUser      OutputStream = "user"      // what the user sent
	StreamSystem    OutputStream = "system"    // notices from the adapter or controller
	StreamStderr    OutputStream = "stderr"    // the process's standard error
)

// Valid reports whether s is a known output stream.
func (s OutputStream) Valid() bool {
	switch s {
	case StreamAssistant, StreamTool, StreamUser, StreamSystem, StreamStderr:
		return true
	}
	return false
}

// AgentOutput is the payload of an EventAgentOutput.
type AgentOutput struct {
	Stream OutputStream `json:"stream"`
	Text   string       `json:"text"`
}

// Event is a durable record of a change. Seq is assigned by the store and is
// strictly increasing, which lets clients resume a stream after a disconnect
// by asking for everything after the last Seq they saw.
type Event struct {
	Seq       int64           `json:"seq"`
	Type      EventType       `json:"type"`
	ProjectID string          `json:"projectId,omitempty"`
	TaskID    string          `json:"taskId,omitempty"`
	RunID     string          `json:"runId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

// NewEvent builds an unsaved event with payload marshalled to JSON.
func NewEvent(t EventType, payload any) (Event, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Event{}, err
		}
		raw = b
	}
	return Event{Type: t, Payload: raw}, nil
}

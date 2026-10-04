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
	EventQuestionCreated   EventType = "question.created"
	EventQuestionAnswered  EventType = "question.answered"
)

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

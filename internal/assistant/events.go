package assistant

import (
	"context"
	"sync"
	"time"

	"devboard/internal/appops"
	"devboard/internal/domain"
)

// EventType says what happened in a conversation.
type EventType string

const (
	EventTurnStarted EventType = "turn_started"
	// EventDelta is the next piece of the assistant's reply, as it is generated.
	EventDelta EventType = "delta"
	// EventToolCall: the assistant asked for an operation. EventToolResult: how it went.
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	// EventConfirmationRequired: a change was proposed and waits for the person.
	EventConfirmationRequired EventType = "confirmation_required"
	// EventConfirmationResolved: the person confirmed or declined it (or it expired).
	EventConfirmationResolved EventType = "confirmation_resolved"
	// EventNotice is something worth telling the person about how the turn is going: a retry, a recovery.
	EventNotice EventType = "notice"
	// EventRetry says an attempt failed and another begins: whatever Delta events of this turn were shown from the
	// earlier attempt should be discarded. Attempt is the number of the new one.
	EventRetry     EventType = "retry"
	EventCompleted EventType = "turn_completed"
	EventFailed    EventType = "turn_failed"
	EventCancelled EventType = "turn_cancelled"
	// EventGap says the events the client asked to resume from have been dropped from memory; it should reload the
	// session (its pending changes) rather than assume it has seen everything.
	EventGap EventType = "gap"
)

// Event is one thing that happened. Seq is per session and only ever grows, so a client that was disconnected resumes
// from the last one it saw.
type Event struct {
	Seq       int64     `json:"seq"`
	SessionID string    `json:"sessionId"`
	TurnID    string    `json:"turnId,omitempty"`
	Type      EventType `json:"type"`
	At        time.Time `json:"at"`
	// Attempt numbers the provider attempts within a turn, from 1, on Delta events.
	Attempt int    `json:"attempt,omitempty"`
	Text    string `json:"text,omitempty"`
	// Tool is set on tool events.
	Tool *ToolEvent `json:"tool,omitempty"`
	// Proposal is set on EventConfirmationRequired; Action on EventConfirmationResolved.
	Proposal *appops.Proposal `json:"proposal,omitempty"`
	ActionID string           `json:"actionId,omitempty"`
	Outcome  string           `json:"outcome,omitempty"`
	// Error is set on EventFailed.
	Error *ErrorInfo `json:"error,omitempty"`
	// Usage is set on EventCompleted when the provider said.
	Usage *Usage `json:"usage,omitempty"`
	// State is the session's state after this event, on events that end a turn.
	State domain.AssistantSessionState `json:"state,omitempty"`
}

// ToolEvent describes a call and, once run, how it went.
type ToolEvent struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
	// Status is ok, error or pending_confirmation (on a result).
	Status string `json:"status,omitempty"`
	// Code is the error code, on an error.
	Code string `json:"code,omitempty"`
	// Detail is a short human account: the arguments, or the error.
	Detail string `json:"detail,omitempty"`
}

// ErrorInfo is why a turn failed.
type ErrorInfo struct {
	// Code is a provider failure kind (not_signed_in, rate_limited, model_unavailable, session_lost, policy, protocol,
	// transient, failed) or one of the engine's own: timeout, too_many_steps, audit_unavailable, internal.
	Code    string `json:"code"`
	Message string `json:"message"`
	// Retryable says that sending the same message again may well work.
	Retryable bool `json:"retryable"`
}

// Usage is what a turn used.
type Usage struct {
	InputTokens  int64 `json:"inputTokens,omitempty"`
	OutputTokens int64 `json:"outputTokens,omitempty"`
}

const (
	// hubKeep is how many events a conversation remembers for a client that reconnects.
	hubKeep = 1024
	// subBuffer is how far behind a subscriber may fall before it is dropped and has to reconnect.
	subBuffer = 256
)

// hub keeps a conversation's recent events and delivers them to whoever is listening. Publishing never blocks: a
// listener that cannot keep up is dropped, and catches up by reconnecting from the last event it saw, so a slow
// browser cannot slow a turn.
type hub struct {
	mu   sync.Mutex
	seq  int64
	ring []Event
	subs map[*sub]struct{}
}

type sub struct {
	ch     chan Event
	closed bool
}

func newHub() *hub { return &hub{subs: map[*sub]struct{}{}} }

func (h *hub) publish(e Event) Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	e.Seq = h.seq
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	h.ring = append(h.ring, e)
	if len(h.ring) > hubKeep {
		h.ring = append([]Event(nil), h.ring[len(h.ring)-hubKeep:]...)
	}
	for s := range h.subs {
		select {
		case s.ch <- e:
		default:
			h.drop(s) // too far behind: it reconnects from the last event it saw
		}
	}
	return e
}

func (h *hub) drop(s *sub) {
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
	delete(h.subs, s)
}

// subscribe returns the events after seq, then live ones, until ctx ends or the listener falls behind.
func (h *hub) subscribe(ctx context.Context, sessionID string, after int64) <-chan Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	var backlog []Event
	for _, e := range h.ring {
		if e.Seq > after {
			backlog = append(backlog, e)
		}
	}
	// If the client was last at event N and the oldest we still have is above N+1, it missed some.
	gap := after > 0 && (len(h.ring) == 0 && h.seq > after || len(h.ring) > 0 && h.ring[0].Seq > after+1)
	s := &sub{ch: make(chan Event, subBuffer+len(backlog)+1)}
	if gap {
		s.ch <- Event{Seq: after, SessionID: sessionID, Type: EventGap, At: time.Now().UTC(), Text: "some events were no longer available; reload the conversation"}
	}
	for _, e := range backlog {
		s.ch <- e
	}
	h.subs[s] = struct{}{}
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		h.drop(s)
		h.mu.Unlock()
	}()
	return s.ch
}

// last is the Seq of the newest event.
func (h *hub) last() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

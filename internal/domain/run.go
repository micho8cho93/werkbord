package domain

import (
	"fmt"
	"time"
)

// RunState is the runtime state of one execution attempt. See TaskState for
// why the two are kept apart.
type RunState string

const (
	RunStarting       RunState = "starting"
	RunRunning        RunState = "running"
	RunWaitingForUser RunState = "waiting_for_user"
	RunCompleted      RunState = "completed"
	RunFailed         RunState = "failed"
	RunStopped        RunState = "stopped"
)

// runTransitions is the complete set of allowed state changes. Terminal states
// have no outgoing edges: a new attempt is a new Run.
var runTransitions = map[RunState][]RunState{
	RunStarting:       {RunRunning, RunFailed, RunStopped},
	RunRunning:        {RunWaitingForUser, RunCompleted, RunFailed, RunStopped},
	RunWaitingForUser: {RunRunning, RunFailed, RunStopped},
	RunCompleted:      nil,
	RunFailed:         nil,
	RunStopped:        nil,
}

// Valid reports whether s is a known run state.
func (s RunState) Valid() bool {
	_, ok := runTransitions[s]
	return ok
}

// Terminal reports whether no further transitions are possible.
func (s RunState) Terminal() bool {
	return s == RunCompleted || s == RunFailed || s == RunStopped
}

// CanTransitionTo reports whether moving from s to next is allowed.
func (s RunState) CanTransitionTo(next RunState) bool {
	for _, allowed := range runTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// ParseRunState validates a state received from outside the process.
func ParseRunState(s string) (RunState, error) {
	st := RunState(s)
	if !st.Valid() {
		return "", fmt.Errorf("%w: unknown run state %q", ErrInvalid, s)
	}
	return st, nil
}

// Run is one execution attempt by an agent against a task.
type Run struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"taskId"`
	ProjectID  string     `json:"projectId"`
	AgentID    string     `json:"agentId"`
	State      RunState   `json:"state"`
	WorktreeID string     `json:"worktreeId,omitempty"`
	SessionRef string     `json:"sessionRef,omitempty"` // agent-specific handle used to resume a conversation
	Reason     string     `json:"reason,omitempty"`     // why the run ended, for failed/stopped runs
	Version    int64      `json:"version"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`
}

// Transition moves the run to next, enforcing the state machine. It updates
// timestamps but does not persist anything.
func (r *Run) Transition(next RunState, reason string, now time.Time) error {
	if !r.State.CanTransitionTo(next) {
		return fmt.Errorf("%w: run %s cannot go from %s to %s", ErrTransition, r.ID, r.State, next)
	}
	r.State = next
	r.Reason = reason
	r.UpdatedAt = now
	if next.Terminal() {
		t := now
		r.EndedAt = &t
	}
	return nil
}

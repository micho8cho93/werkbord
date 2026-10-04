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
	// RunBlocked: the run's policy forbids guessing and the agent reached a
	// decision it could not safely infer, so it stopped. The session is still
	// there (a message that settles the matter resumes it) but nobody is being
	// asked a question: Run.Blocker says what is in the way. It is execution
	// state, never a Kanban column: the task stays wherever the user put it.
	RunBlocked   RunState = "blocked"
	RunCompleted RunState = "completed"
	RunFailed    RunState = "failed"
	RunStopped   RunState = "stopped"
)

// runTransitions is the complete set of allowed state changes. Terminal states
// have no outgoing edges: a new attempt is a new Run.
//
// A waiting run can complete: a session is interactive, so the normal way for
// one to end is that the agent is idle, the user finishes it, and the process
// exits cleanly.
//
// A blocked run is like an idle waiting one: it has a session and ends the same
// ways, and a message sent to it resumes it.
var runTransitions = map[RunState][]RunState{
	RunStarting:       {RunRunning, RunFailed, RunStopped},
	RunRunning:        {RunWaitingForUser, RunBlocked, RunCompleted, RunFailed, RunStopped},
	RunWaitingForUser: {RunRunning, RunCompleted, RunFailed, RunStopped},
	RunBlocked:        {RunRunning, RunCompleted, RunFailed, RunStopped},
	RunCompleted:      nil,
	RunFailed:         nil,
	RunStopped:        nil,
}

// Valid reports whether s is a known run state.
func (s RunState) Valid() bool {
	_, ok := runTransitions[s]
	return ok
}

// Active reports whether the run still has a session (or can be resumed): it
// has not reached a final state. Active runs hold their worktree.
func (s RunState) Active() bool { return s.Valid() && !s.Terminal() }

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

// WaitingKind says what a run in RunWaitingForUser is waiting for.
type WaitingKind string

const (
	// WaitNone is the value for every state except RunWaitingForUser.
	WaitNone WaitingKind = ""
	// WaitQuestion: the agent asked something (or needs an approval) and is
	// blocked until a Question is answered.
	WaitQuestion WaitingKind = "question"
	// WaitIdle: the agent finished its turn, or its session was interrupted,
	// and is ready for the next message.
	WaitIdle WaitingKind = "idle"
)

// Valid reports whether k is a known waiting kind.
func (k WaitingKind) Valid() bool { return k == WaitNone || k == WaitQuestion || k == WaitIdle }

// Run is one execution attempt by an agent against a task.
//
// A run is an interactive session, not a job: after each turn the agent waits
// for the next message (RunWaitingForUser, WaitIdle), and the session only
// ends when the process exits, the user finishes it, or the user stops it.
type Run struct {
	RunnerID    string   `json:"runnerId,omitempty"`
	Remote      bool     `json:"remote"`
	Branch      string   `json:"branch,omitempty"`
	BaseCommit  string   `json:"baseCommit,omitempty"`
	Uncommitted *bool    `json:"uncommitted,omitempty"`
	HeadCommit  string   `json:"headCommit,omitempty"`
	Usage       Usage    `json:"usage"`
	ID          string   `json:"id"`
	Attempt     int      `json:"attempt"`
	ParentRunID string   `json:"parentRunId,omitempty"`
	Purpose     string   `json:"purpose,omitempty"`
	ScheduleKey string   `json:"scheduleKey,omitempty"`
	Handoff     *Handoff `json:"handoff,omitempty"`
	TaskID      string   `json:"taskId"`
	ProjectID   string   `json:"projectId"`
	AgentID     string   `json:"agentId"`
	// Model and Reasoning are what the agent was started with. Empty means the
	// agent's own default: nothing was passed.
	Model      string      `json:"model,omitempty"`
	Reasoning  string      `json:"reasoning,omitempty"`
	State      RunState    `json:"state"`
	WorktreeID string      `json:"worktreeId,omitempty"`
	SessionRef string      `json:"sessionRef,omitempty"` // agent-specific handle used to resume a conversation
	Reason     string      `json:"reason,omitempty"`     // why the run ended, for failed/stopped runs
	Prompt     string      `json:"prompt,omitempty"`     // the first message sent to the agent
	Waiting    WaitingKind `json:"waiting,omitempty"`    // set only while State is RunWaitingForUser
	// Policy is the execution policy the run was started with. It is a copy:
	// editing the task afterwards does not change a run that is working.
	Policy ExecutionPolicy `json:"policy"`
	// Blocker is set while State is RunBlocked, and stays on the run if it ends
	// that way. It is cleared when the run is resumed.
	Blocker    *Blocker   `json:"blocker,omitempty"`
	Activity   string     `json:"activity,omitempty"` // latest one-line activity, for cards
	ActivityAt *time.Time `json:"activityAt,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	Version    int64      `json:"version"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`

	// PID and ProcessID identify the agent process so that one a crashed
	// controller left behind can be found and stopped. ProcessID is an opaque
	// token that distinguishes the process from a later one that reuses the
	// PID. Neither is shown to clients.
	PID       int    `json:"-"`
	ProcessID string `json:"-"`
}

// Transition moves the run to next, enforcing the state machine. It updates
// timestamps but does not persist anything. Use WaitFor to enter
// RunWaitingForUser, which also records what the run is waiting for.
func (r *Run) Transition(next RunState, reason string, now time.Time) error {
	if next == RunWaitingForUser {
		return fmt.Errorf("%w: use WaitFor to wait for the user", ErrInvalid)
	}
	return r.move(next, WaitNone, reason, now)
}

// Block moves the run to RunBlocked and records why.
func (r *Run) Block(b Blocker, now time.Time) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := r.move(RunBlocked, WaitNone, "", now); err != nil {
		return err
	}
	b.RaisedAt = now
	r.Blocker = &b
	return nil
}

// WaitFor moves the run to RunWaitingForUser and records what it waits for.
func (r *Run) WaitFor(kind WaitingKind, now time.Time) error {
	if kind == WaitNone || !kind.Valid() {
		return fmt.Errorf("%w: run %s cannot wait for %q", ErrInvalid, r.ID, kind)
	}
	return r.move(RunWaitingForUser, kind, "", now)
}

func (r *Run) move(next RunState, kind WaitingKind, reason string, now time.Time) error {
	if !r.State.CanTransitionTo(next) {
		return fmt.Errorf("%w: run %s cannot go from %s to %s", ErrTransition, r.ID, r.State, next)
	}
	r.State = next
	r.Waiting = kind
	r.Reason = reason
	r.UpdatedAt = now
	if next == RunRunning {
		r.Blocker = nil // the user settled it, or the agent went on regardless
	}
	if next.Terminal() {
		t := now
		r.EndedAt = &t
		r.PID, r.ProcessID = 0, ""
	}
	return nil
}

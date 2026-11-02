package domain

import "time"

// WorkPlan groups ordered references to existing tasks. Execution belongs to tasks and runs.
type WorkPlan struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"projectId"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	TaskIDs     []string `json:"taskIds"`
	Decisions   string   `json:"decisions"`
	Questions   string   `json:"questions"`
	// Run is the plan's own guided-run state. It is written only by the plan-run service, never by a plan edit.
	Run       *PlanRun  `json:"run,omitempty"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Plan run states and modes. A plan that was never run has no PlanRun.
const (
	PlanRunRunning = "running" // the plan's steps are armed; dispatch belongs to the scheduler
	PlanRunHeld    = "held"    // undispatched steps were disarmed; a running process was left alone

	PlanModeInOrder = "in_order" // each step also waits for the one before it
	PlanModeAsReady = "as_ready" // each step starts once its own dependencies allow
)

// PlanRun records that a person asked for a plan to run, and which tasks that armed. It owns no run: dispatch stays
// with the scheduler, task by task.
type PlanRun struct {
	State     string    `json:"state"`
	Mode      string    `json:"mode"`
	TaskIDs   []string  `json:"taskIds"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

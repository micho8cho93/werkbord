package domain

import (
	"fmt"
	"strings"
	"time"
)

// TaskState is a task's position in the workflow. It is deliberately separate
// from RunState: a task can sit in Doing while its latest run has failed, and
// moving a task never implies anything about a process.
type TaskState string

// The board has exactly these four columns.
const (
	TaskBacklog TaskState = "backlog"
	TaskDoing   TaskState = "doing"
	TaskReview  TaskState = "review"
	TaskDone    TaskState = "done"
)

// TaskStates lists the workflow states in board order.
var TaskStates = []TaskState{TaskBacklog, TaskDoing, TaskReview, TaskDone}

// Valid reports whether s is one of the four workflow states.
func (s TaskState) Valid() bool {
	switch s {
	case TaskBacklog, TaskDoing, TaskReview, TaskDone:
		return true
	}
	return false
}

// ParseTaskState validates a state received from outside the process.
func ParseTaskState(s string) (TaskState, error) {
	st := TaskState(s)
	if !st.Valid() {
		return "", fmt.Errorf("%w: unknown task state %q", ErrInvalid, s)
	}
	return st, nil
}

// Task is a unit of work on a project's board.
//
// Version is incremented on every write and is used for compare-and-swap
// updates, so two devices (or a device and an agent) cannot silently
// overwrite each other.
type Task struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       TaskState `json:"state"`
	Position    float64   `json:"position"` // ordering within a column; lower is higher on the board
	// Execution is what this task overrides about how its runs are carried out:
	// agent, model, reasoning, interaction and priority. A field that is not set
	// is inherited from the project, then the global defaults (ResolveExecution).
	Execution     ExecutionConfig `json:"execution"`
	Orchestration Orchestration   `json:"orchestration"`
	Version       int64           `json:"version"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

const (
	maxTitleLen       = 200
	maxDescriptionLen = 20000
)

// ValidateTaskTitle trims and checks a title.
func ValidateTaskTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("%w: task title is required", ErrInvalid)
	}
	if len(title) > maxTitleLen {
		return "", fmt.Errorf("%w: task title is longer than %d characters", ErrInvalid, maxTitleLen)
	}
	return title, nil
}

// ValidateTaskDescription checks a description's length.
func ValidateTaskDescription(desc string) error {
	if len(desc) > maxDescriptionLen {
		return fmt.Errorf("%w: task description is longer than %d characters", ErrInvalid, maxDescriptionLen)
	}
	return nil
}

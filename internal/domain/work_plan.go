package domain

import "time"

// WorkPlan groups ordered references to existing tasks. Execution belongs to tasks and runs.
type WorkPlan struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	TaskIDs     []string  `json:"taskIds"`
	Decisions   string    `json:"decisions"`
	Questions   string    `json:"questions"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

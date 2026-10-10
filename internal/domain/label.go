package domain

import (
	"time"

	"devboard/internal/planning"
)

// PrefixLabel marks a label's ID.
const PrefixLabel = "lbl"

// Label is a name and a colour the person chose, kept once and put on any number of tasks in any
// project: "Design", "Q4 launch", "Waiting on legal". Nothing about labels is built in. They are
// not the task's state, not its priority and not who does the work (that is Task.WorkMode), so
// renaming or deleting one changes how work is described and never how it runs.
//
// Version is compare-and-swap, as for tasks.
type Label struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Description string    `json:"description,omitempty"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// LabelUse is a label with how many open tasks carry it, for the places that manage labels.
type LabelUse struct {
	Label
	Tasks int `json:"tasks"`
}

// Plan is a task's planned dates: for the timeline, and nothing else. It is deliberately not the
// task's Orchestration, which says when an agent starts; changing a plan never schedules, delays or
// moves anything.
type Plan = planning.Range

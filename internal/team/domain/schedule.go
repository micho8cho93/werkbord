package domain

import "time"

// Schedule is a shared request for work. It carries no execution permission.
// Rescheduling creates a new execution identity, never reuses an authorization.
type Schedule struct {
	Stale        bool      `json:"stale"`
	ID           string    `json:"id"`
	ExecutionID  string    `json:"executionId"`
	ProjectID    string    `json:"projectId"`
	TicketID     string    `json:"ticketId"`
	MemberID     string    `json:"memberId"`
	Assignment   int64     `json:"assignment"`
	Fence        string    `json:"fence"`
	Version      int64     `json:"version"`
	At           time.Time `json:"at"`
	Timezone     string    `json:"timezone"`
	MissedPolicy string    `json:"missedPolicy"`
	GraceSeconds int       `json:"graceSeconds"`
	Order        int       `json:"order"`
	Priority     int       `json:"priority"`
	Dependencies []string  `json:"dependencies"`
	State        string    `json:"state"`
	Reason       string    `json:"reason,omitempty"`
	DeviceID     string    `json:"deviceId,omitempty"`
	RunID        string    `json:"runId,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func (s Schedule) Terminal() bool { return s.State == "completed" || s.State == "canceled" }

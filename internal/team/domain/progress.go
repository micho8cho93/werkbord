package domain

import (
	"devboard/internal/integration"
	"time"
)

// Progress is an ordered observation from the holding member's enrolled device.
// ClaimAt binds it to the current assignment, not a past ownership interval.
type Progress struct {
	Assignment     int64                 `json:"assignment"`
	Schema         string                `json:"schema"`
	TaskID         string                `json:"taskId"`
	ProjectID      string                `json:"localProjectId"`
	ClaimAt        time.Time             `json:"claimAt"`
	Sequence       int64                 `json:"sequence"`
	Execution      integration.Execution `json:"execution"`
	GitUnavailable bool                  `json:"gitUnavailable"`
	Git            *integration.Git      `json:"git,omitempty"`
}
type ProgressRecord struct {
	Progress
	DeviceID   string    `json:"deviceId"`
	MemberID   string    `json:"memberId"`
	ReportedAt time.Time `json:"reportedAt"`
	Stale      bool      `json:"stale"`
	Digest     string    `json:"-"`
}
type ProgressAck struct {
	Sequence int64 `json:"sequence"`
	Applied  bool  `json:"applied"`
}

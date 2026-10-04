package domain

import "time"

// WorktreeState records whether a worktree is still on disk.
type WorktreeState string

const (
	WorktreeActive  WorktreeState = "active"
	WorktreeRemoved WorktreeState = "removed"
)

// Worktree is a Git worktree the controller created for a run, so that
// concurrent runs never share a working directory.
type Worktree struct {
	ID        string        `json:"id"`
	ProjectID string        `json:"projectId"`
	Path      string        `json:"path"`
	Branch    string        `json:"branch"`
	BaseRef   string        `json:"baseRef"`
	State     WorktreeState `json:"state"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

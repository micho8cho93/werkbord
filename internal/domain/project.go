package domain

import (
	"fmt"
	"strings"
	"time"
)

// Project is a registered local Git repository. The repository itself is
// never copied; RepoPath points at the user's existing checkout.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	RepoPath  string    `json:"repoPath"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GitRepository is the inspected metadata of a project's repository. It is a
// snapshot: it is refreshed on registration and on demand, not kept live.
type GitRepository struct {
	ProjectID     string      `json:"projectId"`
	RootPath      string      `json:"rootPath"`
	CurrentBranch string      `json:"currentBranch"` // empty when HEAD is detached
	HeadCommit    string      `json:"headCommit"`    // empty in a repository with no commits
	DefaultBranch string      `json:"defaultBranch"` // from origin/HEAD when known
	Remotes       []GitRemote `json:"remotes"`
	InspectedAt   time.Time   `json:"inspectedAt"`
}

// GitRemote is a named remote and its fetch URL.
type GitRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

const maxNameLen = 120

// ValidateProjectName trims and checks a display name.
func ValidateProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: project name is required", ErrInvalid)
	}
	if len(name) > maxNameLen {
		return "", fmt.Errorf("%w: project name is longer than %d characters", ErrInvalid, maxNameLen)
	}
	return name, nil
}

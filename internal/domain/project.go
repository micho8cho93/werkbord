package domain

import (
	"fmt"
	"strings"
	"time"
)

// ProjectKind says what a project is attached to.
type ProjectKind string

const (
	// ProjectRepository is a registered local Git repository: what every project was before work
	// projects existed, and what an empty kind means.
	ProjectRepository ProjectKind = "repository"
	// ProjectWork is a board and a timeline with no repository behind it: marketing, operations,
	// research, anything that is not code. It has no Git state, no worktrees and no agent runs.
	ProjectWork ProjectKind = "work"
)

// Valid reports whether k is a project kind.
func (k ProjectKind) Valid() bool { return k == ProjectRepository || k == ProjectWork }

// Project is a registered local Git repository, or a work project with no repository at all. The
// repository itself is never copied; RepoPath points at the user's existing checkout.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is ProjectRepository or ProjectWork. A work project has an empty RepoPath.
	Kind     ProjectKind `json:"kind"`
	RepoPath string      `json:"repoPath"`
	// Execution is the project's defaults for how its tasks are carried out; each
	// task can override them, and what a project does not set comes from the
	// global defaults.
	Execution ExecutionConfig `json:"execution"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// HasRepository reports whether the project is backed by a Git repository. Everything that reads or
// changes a repository (inspection, worktrees, health, Git actions, agent runs) asks this first.
func (p Project) HasRepository() bool { return p.Kind != ProjectWork }

// GitRepository is the inspected metadata of a project's repository. It is a
// snapshot: it is refreshed on registration and on demand, not kept live.
type GitRepository struct {
	ProjectID     string      `json:"projectId"`
	RootPath      string      `json:"rootPath"`
	CommonDir     string      `json:"commonDir"`     // the shared .git directory: a repository's identity, since linked worktrees have their own RootPath
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

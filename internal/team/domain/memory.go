package domain

import (
	"time"
	"werkbord/internal/integration"
)

// MemoryEvidence is a signed member's bounded snapshot from their own Git
// context. Authentication proves the reporter, never deployment or correctness.
// Sequence is monotonic per reporter/device/project/branch/PR, including outages.
type MemoryEvidence struct {
	Scope                 integration.SharedMemoryScope `json:"scope"`
	Sequence              int64                         `json:"sequence"`
	Repository            string                        `json:"repository"`
	Branch                string                        `json:"branch"`
	AuthoritativeRevision string                        `json:"authoritativeRevision,omitempty"`
	Revision              string                        `json:"revision,omitempty"`
	PullRequest           int                           `json:"pullRequest,omitempty"`
	PRState               string                        `json:"prState,omitempty"`
	TargetBranch          string                        `json:"targetBranch,omitempty"`
	Available             bool                          `json:"available"`
	ObservedAt            time.Time                     `json:"observedAt"`
}

func (e MemoryEvidence) Validate(now time.Time) error {
	if e.Scope.Validate() != nil || e.Scope.Kind != "project" || e.Sequence < 1 || e.ObservedAt.IsZero() || e.ObservedAt.After(now.Add(5*time.Minute)) || len(e.Branch) == 0 {
		return ErrInvalid
	}
	if _, err := CleanBranch(e.Branch); err != nil {
		return ErrInvalid
	}
	repo, err := integration.MemoryRepository(e.Repository)
	if err != nil || repo != e.Repository {
		return ErrInvalid
	}
	c := integration.SharedMemoryContent{Kind: "observation", Summary: "Evidence", Classification: "team", Verification: "unverified", Source: integration.MemorySource{Kind: "human", ID: "evidence", Revision: e.Revision}, Context: &integration.MemoryContext{Claim: "evidence", Branch: e.Branch, PullRequest: e.PullRequest}}
	authoritative := c
	authoritative.Source.Revision = e.AuthoritativeRevision
	if c.Validate() != nil || authoritative.Validate() != nil || e.Available && e.Revision == "" {
		return ErrInvalid
	}
	if e.TargetBranch != "" {
		if _, err := CleanBranch(e.TargetBranch); err != nil {
			return ErrInvalid
		}
	}
	switch e.PRState {
	case "", "open", "closed", "merged", "superseded":
	default:
		return ErrInvalid
	}
	if e.PullRequest == 0 && (e.PRState != "" || e.TargetBranch != "") {
		return ErrInvalid
	}
	return nil
}

// MemoryResolution is an explicit curator decision, never a semantic merge.
// Every competing version is pinned to prevent resolving unseen concurrent work.
type MemoryResolution struct {
	Version  int64                     `json:"version"`
	Replaced []MemoryResolutionVersion `json:"replaced"`
}
type MemoryResolutionVersion struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

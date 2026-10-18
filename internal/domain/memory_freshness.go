package domain

import "time"

// MemoryRepositoryRevision contains committed local evidence only. PR signatures
// include authenticated check results; absence from a bounded list is unknown.
type MemoryRepositoryRevision struct {
	Head            string            `json:"head"`
	Branch          string            `json:"branch"`
	Refs            map[string]string `json:"refs"`
	PullRequests    map[string]string `json:"pullRequests,omitempty"`
	GitHubAvailable bool              `json:"gitHubAvailable"`
}

// MemoryFreshness is private reconciliation state, committed with invalidations.
// Revision/EventSeq advance only after every page of a stable observation has
// been processed. Pending/After make large partitions resumable across restarts.
type MemoryFreshness struct {
	Scope       MemoryScope              `json:"scope"`
	Revision    MemoryRepositoryRevision `json:"revision"`
	EventSeq    int64                    `json:"eventSeq"`
	Pending     MemoryRepositoryRevision `json:"pending"`
	PendingSeq  int64                    `json:"pendingSeq"`
	Uncertain   bool                     `json:"uncertain,omitempty"`
	After       *MemoryCursor            `json:"after,omitempty"`
	CheckedAt   time.Time                `json:"checkedAt"`
	SucceededAt *time.Time               `json:"succeededAt,omitempty"`
	Problem     string                   `json:"problem,omitempty"`
	Version     int64                    `json:"version"`
}

package domain

import (
	"time"
	"werkbord/internal/integration"
)

// ProductMapRecord is a reviewed documentation projection. It grants no runner
// authority and contains no source files, local paths or provider credentials.
type ProductMapRecord struct {
	Digest      string                         `json:"digest"`
	ID          string                         `json:"id"`
	ProjectID   string                         `json:"projectId"`
	AuthorID    string                         `json:"authorId"`
	ReviewerID  string                         `json:"reviewerId,omitempty"`
	State       string                         `json:"state"`
	BaseVersion int64                          `json:"baseVersion"`
	Version     int64                          `json:"version"`
	CreatedAt   time.Time                      `json:"createdAt"`
	ReviewedAt  *time.Time                     `json:"reviewedAt,omitempty"`
	Snapshot    integration.ProductMapSnapshot `json:"snapshot"`
}

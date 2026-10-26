package domain

import (
	"fmt"
	"werkbord/internal/integration"
)

var ErrPublicationQueueFull = fmt.Errorf("%w: publication queue is full; waiting for workspace acknowledgements", ErrConflict)

// PublicationGrant is a local owner's affirmative choice for one destination.
type PublicationGrant struct {
	WorkspaceID    string `json:"workspaceId"`
	ProjectID      string `json:"projectId"`
	LocalProjectID string `json:"localProjectId"`
	Repository     string `json:"repository"`
	Enabled        bool   `json:"enabled"`
	Semantic       bool   `json:"semantic"`
}
type MemoryPublication struct {
	Grant            PublicationGrant           `json:"grant"`
	MemoryID         string                     `json:"memoryId"`
	SourceRepository string                     `json:"sourceRepository"`
	Revision         int64                      `json:"revision"`
	State            string                     `json:"state"`
	Summary          string                     `json:"summary"`
	Event            *integration.MemoryEvent   `json:"event,omitempty"`
	Receipt          *integration.MemoryReceipt `json:"receipt,omitempty"`
}

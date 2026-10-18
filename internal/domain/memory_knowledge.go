package domain

import "time"

// MemoryEvidence is immutable historical evidence, separate from the editable
// recommendation. Keys are hashes of stable structured source identities.
type MemoryEvidence struct {
	OwnerID    string           `json:"-"`
	Key        string           `json:"key"`
	MemoryID   string           `json:"memoryId"`
	Source     MemoryProvenance `json:"source"`
	Excerpt    string           `json:"excerpt"`
	ObservedAt time.Time        `json:"observedAt"`
}

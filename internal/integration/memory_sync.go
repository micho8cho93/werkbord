package integration

import "errors"

// MemoryEvent is an ordered immutable projection, never an Individual record.
// Each device has an independent stream in each workspace.
type MemoryEvent struct {
	Schema   string         `json:"schema"`
	ID       string         `json:"id"`
	Sequence int64          `json:"sequence"`
	SourceID string         `json:"sourceId"`
	Revision int64          `json:"revision"`
	Action   string         `json:"action"`
	Proposal MemoryProposal `json:"proposal"`
	Previous string         `json:"previous,omitempty"` // previous proposal's client ID
}

func (e MemoryEvent) Validate() error {
	if e.Schema != MemorySchema || !Identifier(e.ID) || !Identifier(e.SourceID) || e.Sequence < 1 || e.Revision < 1 || e.Proposal.ClientID != e.ID || e.Proposal.Content.Source.ID != e.SourceID || e.Previous != "" && !Identifier(e.Previous) {
		return errors.New("invalid memory event")
	}
	if e.Action != "propose" && e.Action != "revoke" && e.Action != "cancel" {
		return errors.New("invalid memory event action")
	}
	if e.Proposal.Supersedes != "" {
		return errors.New("supersession is resolved from the previous delivery identity")
	}
	if e.Proposal.Scope.Kind != "repository" || e.Proposal.Content.Classification != "team" {
		return errors.New("publication requires exact repository scope")
	}
	return e.Proposal.Validate()
}

type MemoryEventPage struct {
	Schema   string        `json:"schema"`
	Events   []MemoryEvent `json:"events"`
	Tracking []MemoryEvent `json:"tracking,omitempty"`
}
type MemoryReceipt struct {
	EventID  string             `json:"eventId"`
	Sequence int64              `json:"sequence"`
	Record   SharedMemoryRecord `json:"record"`
}

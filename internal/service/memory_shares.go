package service

import (
	"context"
	"fmt"
	"time"
	"werkbord/internal/domain"
	"werkbord/internal/integration"
	"werkbord/internal/store"
)

// ApproveMemoryShare approves an immutable manually sanitized projection, never
// an export of a private memory, handoff, evidence set or execution record.
func (s *Memory) ApproveMemoryShare(ctx context.Context, in integration.MemoryProposal, reviewed bool, expires time.Time) (integration.MemoryShare, error) {
	var out integration.MemoryShare
	if err := in.Validate(); err != nil {
		return out, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if !reviewed {
		return out, fmt.Errorf("%w: review the sanitized statement before sharing", domain.ErrInvalid)
	}
	now := s.deps.now().UTC().Truncate(time.Millisecond)
	out = integration.MemoryShare{Schema: integration.MemorySchema, ID: domain.NewID("share"), Proposal: in, ApprovedAt: now, ExpiresAt: expires.UTC().Truncate(time.Millisecond)}
	if !expires.After(now) {
		return out, domain.ErrInvalid
	}
	err := s.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Memory().ApproveShare(ctx, s.owner, out) })
	return out, err
}
func (s *Memory) MemoryShares(ctx context.Context, w, after string) (integration.MemoryShares, error) {
	var out integration.MemoryShares
	err := s.deps.Store.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Memory().Shares(ctx, s.owner, w, after, s.deps.now())
		return err
	})
	return out, err
}
func (s *Memory) RevokeMemoryShare(ctx context.Context, id string) error {
	return s.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Memory().RevokeShare(ctx, s.owner, id) })
}

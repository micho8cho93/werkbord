package service

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

func memoryPrincipal(a Actor) store.MemoryPrincipal {
	p := store.MemoryPrincipal{WorkspaceID: a.Workspace.ID, MemberID: a.Member.ID}
	if a.Device != nil {
		p.DeviceID = a.Device.ID
	}
	return p
}

// Each entry point pins the supplied authenticated identity, even for in-process
// callers, and rechecks credentials in the storage transaction. No MCP grant may
// propose or approve policy through these human endpoints.
func (s *Service) ProposeMemory(ctx context.Context, a Actor, in integration.MemoryProposal) (integration.SharedMemoryRecord, error) {
	var out integration.SharedMemoryRecord
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	id := domain.NewID("mem")
	err := s.db.Update(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		var err error
		out, err = tx.ProposeMemory(ctx, memoryPrincipal(a), in, id, s.stamp())
		return err
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}
func (s *Service) ChangeMemory(ctx context.Context, a Actor, id string, in integration.MemoryCommand) (integration.SharedMemoryRecord, error) {
	var out integration.SharedMemoryRecord
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	err := s.db.Update(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		var err error
		out, err = tx.ChangeMemory(ctx, memoryPrincipal(a), id, in, s.stamp())
		return err
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}
func (s *Service) ReadMemories(ctx context.Context, a Actor, scope integration.SharedMemoryScope, after string, published bool) (integration.MemoryPage, error) {
	var out integration.MemoryPage
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	err := s.db.View(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		var err error
		out, err = tx.ReadMemories(ctx, memoryPrincipal(a), scope, after, published)
		return err
	})
	return out, err
}

func (s *Service) DeliverMemory(ctx context.Context, a Actor, in integration.MemoryEvent) (integration.MemoryReceipt, error) {
	var out integration.MemoryReceipt
	if a.MCPGrantID != "" || a.Device == nil {
		return out, domain.ErrForbidden
	}
	err := s.db.Update(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		var e error
		out, e = tx.DeliverMemory(ctx, memoryPrincipal(a), in, domain.NewID("mem"), s.stamp())
		return e
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

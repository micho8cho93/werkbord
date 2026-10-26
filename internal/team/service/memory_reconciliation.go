package service

import (
	"context"
	"werkbord/internal/integration"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
)

func (s *Service) ReportMemoryEvidence(ctx context.Context, a Actor, e domain.MemoryEvidence) error {
	if a.MCPGrantID != "" {
		return domain.ErrForbidden
	}
	err := s.db.Update(AuthenticatedContext(ctx, a), func(tx store.Tx) error { return tx.ReportMemoryEvidence(ctx, memoryPrincipal(a), e, s.stamp()) })
	s.changed(a.Workspace.ID, err)
	return err
}
func (s *Service) SearchMemories(ctx context.Context, a Actor, project, query, after, id string) (integration.MemoryPage, error) {
	var out integration.MemoryPage
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	err := s.db.View(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		var err error
		out, err = tx.SearchMemories(ctx, memoryPrincipal(a), project, query, after, id, s.stamp())
		return err
	})
	return out, err
}
func (s *Service) MemoryHistory(ctx context.Context, a Actor, project, id string) (store.MemoryHistory, error) {
	var out store.MemoryHistory
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	err := s.db.View(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		var err error
		out, err = tx.MemoryHistory(ctx, memoryPrincipal(a), project, id, s.stamp())
		return err
	})
	return out, err
}

// ReconcileMemory runs only deterministic database maintenance, without model,
// Git, network or runner credentials. Replicated storage requires quorum.
func (s *Service) ReconcileMemory(ctx context.Context) error {
	return s.db.Update(ctx, func(tx store.Tx) error { return tx.ReconcileMemoryBatch(ctx, s.stamp()) })
}

func (s *Service) ResolveMemory(ctx context.Context, a Actor, project, id string, in domain.MemoryResolution) (integration.SharedMemoryRecord, error) {
	var out integration.SharedMemoryRecord
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	err := s.db.Update(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		var err error
		out, err = tx.ResolveMemory(ctx, memoryPrincipal(a), project, id, in, s.stamp())
		return err
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

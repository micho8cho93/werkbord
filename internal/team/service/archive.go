package service

import (
	"context"
	"fmt"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// ArchiveTicket closes work in the coordination board, preserving its reports.
// It never sends a stop command to a member's runner.
func (s *Service) ArchiveTicket(ctx context.Context, a Actor, projectID, ticketID string, version int64, archived bool) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx *store.Tx, x access) (err error) {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		if k, err = tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID); err != nil {
			return err
		}
		if k.Version != version {
			return fmt.Errorf("%w: this ticket changed; reload it and try again", domain.ErrConflict)
		}
		if k.Status.Held() {
			if k.AssigneeID != a.Member.ID && !x.can(domain.PPTicketsAssign) {
				return forbidden("close work someone else holds")
			}
		} else if k.Status == domain.TicketDone {
			if err := x.require(domain.PPTicketsReopen, "archive or restore finished tickets"); err != nil {
				return err
			}
		} else if k.CreatorID != a.Member.ID && !x.can(domain.PPTicketsEdit) {
			return forbidden("close or restore someone else's ticket")
		}
		now := s.stamp()
		k.ArchivedAt = nil
		kind := domain.ActTicketRestored
		if archived {
			k.ArchivedAt = &now
			kind = domain.ActTicketArchived
		}
		k.UpdatedAt = now
		if k, err = s.save(ctx, tx, a, k); err != nil {
			return err
		}
		return s.record(ctx, tx, a, k, kind, "")
	})
	return k, err
}

func (s *Service) ArchiveDone(ctx context.Context, a Actor, projectID string) (int, error) {
	n := 0
	err := s.mutate(ctx, a, projectID, func(tx *store.Tx, x access) error {
		if err := x.require(domain.PPTicketsReopen, "archive finished tickets"); err != nil {
			return err
		}
		tickets, err := tx.Tickets(ctx, a.Workspace.ID, projectID)
		if err != nil {
			return err
		}
		now := s.stamp()
		for _, k := range tickets {
			if k.Status != domain.TicketDone || k.ArchivedAt != nil {
				continue
			}
			k.ArchivedAt, k.UpdatedAt = &now, now
			if k, err = s.save(ctx, tx, a, k); err != nil {
				return err
			}
			if err := s.record(ctx, tx, a, k, domain.ActTicketArchived, "Cleared from Done"); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

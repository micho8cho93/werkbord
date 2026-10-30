package service

import (
	"context"
	"fmt"
	"math"

	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
)

// maxPosition keeps a position a plain, comparable number: the queue only ever needs room between two neighbours.
const maxPosition = 1e12

// ReorderTicket sets where a ticket sits in its project's queue, an order every member sees. It is coordination only: it
// changes no status, owner or report, and leaves the ticket's version alone so an open edit does not go stale.
//
// Who may move what: a ticket someone holds is ordered by its owner (or by anyone who may edit tickets); a ticket nobody holds is
// the project's shared priority, which is for people who may edit tickets. Finished and archived tickets are not in the queue.
func (s *Service) ReorderTicket(ctx context.Context, a Actor, projectID, ticketID string, position float64) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		if math.IsNaN(position) || math.IsInf(position, 0) || math.Abs(position) > maxPosition {
			return fmt.Errorf("%w: that is not a place in the queue", domain.ErrInvalid)
		}
		if k, err = tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID); err != nil {
			return err
		}
		if k.ArchivedAt != nil || k.Status == domain.TicketDone {
			return fmt.Errorf("%w: %s is finished, so it is not in the queue", domain.ErrConflict, k.Key)
		}
		switch {
		case k.Claimed() && k.AssigneeID == a.Member.ID:
		case x.can(domain.PPTicketsEdit):
		case k.Claimed():
			return forbidden("reorder work someone else holds")
		default:
			return forbidden("change the project's shared order")
		}
		if err := tx.SetTicketPosition(ctx, a.Workspace.ID, projectID, ticketID, position); err != nil {
			return err
		}
		k.Position = position
		return nil
	})
	return k, err
}

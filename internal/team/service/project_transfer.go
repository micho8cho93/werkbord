package service

import (
	"context"
	"errors"
	"fmt"

	"werkbord/internal/integration"
	"werkbord/internal/planning"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
)

func teamTransfer(ctx context.Context, tx store.Tx, a Actor, x access) (integration.ProjectTransfer, error) {
	out := integration.ProjectTransfer{Name: x.Project.Name, Repository: x.Project.Repository, Tickets: []integration.TransferTicket{}, Revision: fmt.Sprint(x.Project.Revision)}
	if err := x.require(domain.PPTicketsView, "read tickets for a project move"); err != nil {
		return out, err
	}
	if x.Project.Archived {
		return out, domain.ErrConflict
	}
	tickets, err := tx.Tickets(ctx, a.Workspace.ID, x.Project.ID)
	if err != nil {
		return out, err
	}
	for _, t := range tickets {
		if t.ArchivedAt == nil {
			desc := t.Description
			if t.Requirements != "" {
				desc += "\n\nRequirements:\n" + t.Requirements
			}
			out.Tickets = append(out.Tickets, integration.TransferTicket{Title: t.Title, Description: desc})
		}
	}
	return out, out.Validate()
}
func (s *Service) ExportTransfer(ctx context.Context, a Actor, id string) (integration.ProjectTransfer, error) {
	var out integration.ProjectTransfer
	err := s.view(ctx, a, id, func(tx store.Tx, x access) (err error) {
		if err := a.require(domain.PermProjectsManage, "move a project"); err != nil {
			return err
		}
		out, err = teamTransfer(ctx, tx, a, x)
		return
	})
	return out, err
}
func (s *Service) ArchiveTransfer(ctx context.Context, a Actor, id string, expected integration.ProjectTransfer) error {
	return s.mutate(ctx, a, id, func(tx store.Tx, x access) error {
		if err := a.require(domain.PermProjectsManage, "archive moved project"); err != nil {
			return err
		}
		actual, err := teamTransfer(ctx, tx, a, x)
		if err != nil {
			return err
		}
		if !actual.Equal(expected) {
			return fmt.Errorf("%w: project changed; destination is saved, source stays active", domain.ErrConflict)
		}
		if err := tx.AddActivity(ctx, a.Workspace.ID, domain.Activity{ProjectID: id, ActorID: a.Member.ID, Kind: domain.ActProjectArchived, Detail: x.Project.Name, CreatedAt: s.stamp()}); err != nil {
			return err
		}
		p := x.Project
		p.Archived = true
		p.UpdatedAt = s.stamp()
		return tx.UpdateProject(ctx, p)
	})
}
func (s *Service) ImportTransfer(ctx context.Context, a Actor, in integration.ProjectTransfer) (string, error) {
	if err := a.require(domain.PermProjectsCreate, "move a project here"); err != nil {
		return "", err
	}
	if err := in.Validate(); err != nil {
		return "", fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	if in.Source == "" {
		return "", domain.ErrInvalid
	}
	id := in.TransferID(domain.PrefixProject)
	err := s.db.Update(ctx, func(tx store.Tx) error {
		if _, err := s.visibleProject(ctx, tx, a, id); err == nil {
			x, e := s.access(ctx, tx, a, id)
			if e != nil {
				return e
			}
			existing, e := teamTransfer(ctx, tx, a, x)
			if e != nil {
				return e
			}
			if !existing.SameText(in) {
				return fmt.Errorf("%w: the earlier destination has changed", domain.ErrConflict)
			}
			return nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		now := s.stamp()
		p := domain.Project{ID: id, WorkspaceID: a.Workspace.ID, Name: in.Name, Repository: in.Repository, CreatedAt: now, UpdatedAt: now}
		if err := tx.InsertProject(ctx, p); err != nil {
			return err
		}
		if err := tx.AddProjectMember(ctx, a.Workspace.ID, domain.ProjectMember{ProjectID: id, MemberID: a.Member.ID, Role: domain.ProjectOwner, AddedBy: a.Member.ID, AddedAt: now}); err != nil {
			return err
		}
		for _, t := range in.Tickets {
			n, err := tx.NextTicketNumber(ctx, a.Workspace.ID)
			if err != nil {
				return err
			}
			k := domain.Ticket{ID: domain.NewID(domain.PrefixTicket), ProjectID: id, Number: n, Key: domain.TicketKey(n), Title: t.Title, Description: t.Description, Status: domain.TicketAvailable, CreatorID: a.Member.ID, WorkMode: planning.ModeHuman, Version: 1, CreatedAt: now, UpdatedAt: now}
			k.Position = float64(k.Number)
			if err := tx.InsertTicket(ctx, a.Workspace.ID, k); err != nil {
				return err
			}
			if err := s.record(ctx, tx, a, k, domain.ActTicketCreated, "Moved project"); err != nil {
				return err
			}
		}
		return nil
	})
	s.changed(a.Workspace.ID, err)
	return id, err
}

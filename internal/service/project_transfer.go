package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/planning"
	"devboard/internal/store"
)

func personalTransfer(ctx context.Context, tx store.Tx, id string) (integration.ProjectTransfer, error) {
	out := integration.ProjectTransfer{Tickets: []integration.TransferTicket{}}
	p, err := tx.Projects().Get(ctx, id)
	if err != nil {
		return out, err
	}
	if p.Inactive() {
		return out, domain.ErrConflict
	}
	out.Name = p.Name
	if !p.HasRepository() {
		var previous integration.ProjectTransfer
		if e := tx.Settings().Get(ctx, "project-transfer:"+id, &previous); e == nil {
			out.Repository = previous.Repository
		} else if !errors.Is(e, domain.ErrNotFound) {
			return out, e
		}
	}
	if p.HasRepository() {
		repo, e := tx.Repositories().Get(ctx, id)
		if e != nil {
			return out, e
		}
		for _, r := range repo.Remotes {
			if r.Name == "origin" {
				if _, e := integration.RepositoryIdentity(r.URL); e != nil {
					return out, fmt.Errorf("%w: origin must be a credential-free remote address before moving", domain.ErrInvalid)
				}
				out.Repository = r.URL
			}
		}
	}
	runs, err := tx.Runs().ListActive(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range runs {
		if r.ProjectID == id {
			return out, fmt.Errorf("%w: stop active runs before moving", domain.ErrConflict)
		}
	}
	tasks, err := tx.Tasks().ListByProject(ctx, id)
	if err != nil {
		return out, err
	}
	revision, _ := json.Marshal(struct {
		Project *domain.Project
		Tasks   []domain.Task
	}{p, tasks})
	out.Revision = fmt.Sprintf("%x", sha256.Sum256(revision))
	for _, t := range tasks {
		if t.Orchestration.Enabled {
			return out, fmt.Errorf("%w: disable automatic schedules before moving", domain.ErrConflict)
		}
		if t.ArchivedAt == nil {
			out.Tickets = append(out.Tickets, integration.TransferTicket{Title: t.Title, Description: t.Description})
		}
	}
	return out, out.Validate()
}
func (s *Projects) ExportTransfer(ctx context.Context, id string) (integration.ProjectTransfer, error) {
	var out integration.ProjectTransfer
	err := s.Store.View(ctx, func(tx store.Tx) (err error) { out, err = personalTransfer(ctx, tx, id); return })
	return out, err
}
func (s *Projects) ArchiveTransfer(ctx context.Context, id string, expected integration.ProjectTransfer) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		actual, err := personalTransfer(ctx, tx, id)
		if err != nil {
			return err
		}
		if !actual.Equal(expected) {
			return fmt.Errorf("%w: project changed; destination is saved, source stays active", domain.ErrConflict)
		}
		if err := tx.Projects().SetLifecycle(ctx, id, "archived", s.now()); err != nil {
			return err
		}
		ev := newEvent(domain.EventProjectUpdated, map[string]any{"lifecycle": "archived"})
		ev.ProjectID = id
		return em.emit(ev)
	})
}
func (s *Projects) ImportTransfer(ctx context.Context, in integration.ProjectTransfer) (string, error) {
	if err := in.Validate(); err != nil {
		return "", fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	if in.Source == "" {
		return "", domain.ErrInvalid
	}
	id := in.TransferID(domain.PrefixProject)
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if _, err := tx.Projects().Get(ctx, id); err == nil {
			existing, e := personalTransfer(ctx, tx, id)
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
		now := s.now()
		p := &domain.Project{ID: id, Name: in.Name, Kind: domain.ProjectWork, CreatedAt: now, UpdatedAt: now}
		if err := tx.Projects().Create(ctx, p); err != nil {
			return err
		}
		// Keep a remote as inert metadata, never as a local path or execution configuration.
		if err := tx.Settings().Set(ctx, "project-transfer:"+id, in, now); err != nil {
			return err
		}
		for i, t := range in.Tickets {
			task := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: id, Title: t.Title, Description: t.Description, State: domain.TaskBacklog, Position: float64(i+1) * 1024, WorkMode: planning.ModeHuman, Version: 1, CreatedAt: now, UpdatedAt: now}
			if err := tx.Tasks().Create(ctx, task); err != nil {
				return err
			}
		}
		ev := newEvent(domain.EventProjectRegistered, ProjectDetail{Project: *p})
		ev.ProjectID = id
		return em.emit(ev)
	})
	return id, err
}

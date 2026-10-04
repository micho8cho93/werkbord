package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
)

// Projects registers and describes local repositories.
type Projects struct {
	Deps
	Git gitrepo.Inspector
}

// ProjectDetail is a project with its latest repository snapshot.
type ProjectDetail struct {
	domain.Project
	Repository *domain.GitRepository `json:"repository,omitempty"`
}

// Register validates path as a Git repository and records it as a project.
// The repository's top level is stored, so registering a subdirectory
// registers the whole repository. Registering the same repository twice
// returns domain.ErrDuplicate. Nothing is copied.
func (s *Projects) Register(ctx context.Context, path, name string) (*ProjectDetail, error) {
	repo, err := s.Git.Inspect(ctx, path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(repo.RootPath)
	}
	if name, err = domain.ValidateProjectName(name); err != nil {
		return nil, err
	}

	now := s.now()
	p := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: name, RepoPath: repo.RootPath, CreatedAt: now, UpdatedAt: now}
	repo.ProjectID = p.ID

	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		if existing, err := tx.Projects().GetByPath(ctx, p.RepoPath); err == nil {
			return fmt.Errorf("%s is already registered as %q: %w", p.RepoPath, existing.Name, domain.ErrDuplicate)
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err := tx.Projects().Create(ctx, p); err != nil {
			return err
		}
		if err := tx.Repositories().Upsert(ctx, repo); err != nil {
			return err
		}
		ev := newEvent(domain.EventProjectRegistered, ProjectDetail{Project: *p, Repository: repo})
		ev.ProjectID = p.ID
		return em.emit(ev)
	})
	if err != nil {
		return nil, err
	}
	s.log().Info("project registered", "project", p.ID, "path", p.RepoPath)
	return &ProjectDetail{Project: *p, Repository: repo}, nil
}

// Refresh re-inspects a project's repository and stores the new snapshot.
func (s *Projects) Refresh(ctx context.Context, id string) (*ProjectDetail, error) {
	var p *domain.Project
	if err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		p, err = tx.Projects().Get(ctx, id)
		return err
	}); err != nil {
		return nil, err
	}
	repo, err := s.Git.Inspect(ctx, p.RepoPath)
	if err != nil {
		return nil, err
	}
	repo.ProjectID = p.ID
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := tx.Repositories().Upsert(ctx, repo); err != nil {
			return err
		}
		ev := newEvent(domain.EventProjectInspected, repo)
		ev.ProjectID = p.ID
		return em.emit(ev)
	})
	if err != nil {
		return nil, err
	}
	return &ProjectDetail{Project: *p, Repository: repo}, nil
}

// Get returns a project and its repository snapshot.
func (s *Projects) Get(ctx context.Context, id string) (*ProjectDetail, error) {
	var out *ProjectDetail
	err := s.Store.View(ctx, func(tx store.Tx) error {
		p, err := tx.Projects().Get(ctx, id)
		if err != nil {
			return err
		}
		out = &ProjectDetail{Project: *p}
		repo, err := tx.Repositories().Get(ctx, id)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		out.Repository = repo
		return nil
	})
	return out, err
}

// List returns all projects with their repository snapshots.
func (s *Projects) List(ctx context.Context) ([]ProjectDetail, error) {
	var out []ProjectDetail
	err := s.Store.View(ctx, func(tx store.Tx) error {
		ps, err := tx.Projects().List(ctx)
		if err != nil {
			return err
		}
		out = make([]ProjectDetail, 0, len(ps))
		for _, p := range ps {
			d := ProjectDetail{Project: p}
			repo, err := tx.Repositories().Get(ctx, p.ID)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			d.Repository = repo
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

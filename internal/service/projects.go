package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
)

// Projects registers and describes local repositories.
type Projects struct {
	Deps
	Git     gitrepo.Inspector
	Catalog AgentCatalog // checks the agent in an execution config; nil checks only its shape

	mu    sync.Mutex
	locks map[string]chan struct{} // per project: at most one inspection at a time
}

// lock serialises inspections of one project. An inspection reads Git, then
// writes what it read; two running at once can finish in the opposite order to
// the one they read in, leaving the older snapshot stored as the latest. Other
// projects are unaffected, and a caller that gives up while queued returns
// without having started an inspection.
func (s *Projects) lock(ctx context.Context, projectID string) (unlock func(), err error) {
	s.mu.Lock()
	if s.locks == nil {
		s.locks = map[string]chan struct{}{}
	}
	l, ok := s.locks[projectID]
	if !ok {
		l = make(chan struct{}, 1)
		s.locks[projectID] = l
	}
	s.mu.Unlock()
	select {
	case l <- struct{}{}:
		return func() { <-l }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
		// The same repository can have several top-level paths (its linked
		// worktrees), so identity comes from the shared Git directory.
		if repo.CommonDir != "" {
			if other, err := tx.Repositories().GetByCommonDir(ctx, repo.CommonDir); err == nil {
				existing, err := tx.Projects().Get(ctx, other.ProjectID)
				if err != nil {
					return err
				}
				return fmt.Errorf("%s is the same repository as project %q (%s): %w", p.RepoPath, existing.Name, existing.RepoPath, domain.ErrDuplicate)
			} else if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
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
	unlock, err := s.lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()

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
	// A project is the repository registered at RepoPath. If that directory was
	// deleted and recreated inside another repository, Git now resolves it to
	// the outer one; storing that snapshot would make the project silently
	// describe, and later act on, a different repository.
	if repo.RootPath != p.RepoPath {
		return nil, fmt.Errorf("project %q is registered at %s, which now belongs to the repository at %s; "+
			"restore the original repository or register the new location as a new project: %w",
			p.Name, p.RepoPath, repo.RootPath, domain.ErrConflict)
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

// SetExecution replaces a project's default execution configuration: what its
// tasks use unless they override it. What it leaves unset comes from the global
// defaults.
func (s *Projects) SetExecution(ctx context.Context, id string, cfg domain.ExecutionConfig) (*ProjectDetail, error) {
	cfg = cfg.Normalized()
	if err := validateExecution(ctx, s.Catalog, cfg); err != nil {
		return nil, err
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := tx.Projects().SetExecution(ctx, id, cfg, s.now()); err != nil {
			return err
		}
		p, err := tx.Projects().Get(ctx, id)
		if err != nil {
			return err
		}
		ev := newEvent(domain.EventProjectUpdated, p)
		ev.ProjectID = id
		return em.emit(ev)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

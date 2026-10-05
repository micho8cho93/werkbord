package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// access is what an actor is, and may do, on one project.
type access struct {
	Project domain.Project
	// Role is the actor's effective role on the project: their own project role,
	// or owner for a workspace member whose role carries projects.manage.
	Role domain.ProjectRole
	// Member reports whether the actor is on the project. A workspace owner can
	// see and manage every project without being on it, but only a member can
	// hold tickets or be assigned them.
	Member bool
	actor  Actor
}

func (x access) can(p domain.ProjectPermission) bool { return x.Role.Can(p) }

func (x access) require(p domain.ProjectPermission, what string) error {
	if !x.can(p) {
		return forbidden(what)
	}
	return nil
}

// access loads a project the actor may see, with their role on it. A project the
// actor may not see is reported as not found.
func (s *Service) access(ctx context.Context, tx *store.Tx, a Actor, projectID string) (access, error) {
	p, err := tx.Project(ctx, a.Workspace.ID, projectID)
	if err != nil {
		return access{}, err
	}
	role, on, err := tx.ProjectRole(ctx, a.Workspace.ID, projectID, a.Member.ID)
	if err != nil {
		return access{}, err
	}
	x := access{Project: p, Role: role, Member: on, actor: a}
	if a.Member.Can(domain.PermProjectsManage) {
		x.Role = domain.ProjectOwner
		return x, nil
	}
	if !on {
		if a.Member.Can(domain.PermProjectsViewAll) {
			x.Role = ""
			return x, nil
		}
		return access{}, fmt.Errorf("%w: project", domain.ErrNotFound)
	}
	return x, nil
}

// mutate runs fn as one write transaction on a project: it resolves the actor's
// access, lets fn change things, moves the project's revision, and, once the
// transaction has committed, wakes everyone waiting for the project to change.
func (s *Service) mutate(ctx context.Context, a Actor, projectID string, fn func(tx *store.Tx, x access) error) error {
	err := s.db.Update(ctx, func(tx *store.Tx) error {
		x, err := s.access(ctx, tx, a, projectID)
		if err != nil {
			return err
		}
		if err := fn(tx, x); err != nil {
			return err
		}
		_, err = tx.BumpRevision(ctx, a.Workspace.ID, projectID)
		return err
	})
	if err == nil {
		s.hub.notify(projectID)
	}
	return err
}

// view runs fn as a read-only transaction with the actor's access to a project.
func (s *Service) view(ctx context.Context, a Actor, projectID string, fn func(tx *store.Tx, x access) error) error {
	return s.db.View(ctx, func(tx *store.Tx) error {
		x, err := s.access(ctx, tx, a, projectID)
		if err != nil {
			return err
		}
		return fn(tx, x)
	})
}

// ---- change notification ----

// hub wakes the requests waiting for a project to change. It holds no data: a
// waiter that wakes re-reads the project's revision from the database, so a
// notification that is missed or duplicated is harmless.
type hub struct {
	mu      sync.Mutex
	waiters map[string]map[chan struct{}]struct{}
}

func (h *hub) subscribe(projectID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.waiters == nil {
		h.waiters = map[string]map[chan struct{}]struct{}{}
	}
	if h.waiters[projectID] == nil {
		h.waiters[projectID] = map[chan struct{}]struct{}{}
	}
	h.waiters[projectID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.waiters[projectID], ch)
		if len(h.waiters[projectID]) == 0 {
			delete(h.waiters, projectID)
		}
		h.mu.Unlock()
	}
}

func (h *hub) notify(projectID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.waiters[projectID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// MaxSyncWait bounds how long WaitForChange holds a request open. It stays below
// the HTTP server's write timeout.
const MaxSyncWait = 20 * time.Second

// Sync is the answer to "has the project changed since revision N?".
type Sync struct {
	Revision int64 `json:"revision"`
	Changed  bool  `json:"changed"`
}

// WaitForChange returns as soon as a project's revision is above since, or after
// wait (at most MaxSyncWait) with Changed false. Members' boards use it to stay
// in step: when it reports a change they reload the board.
func (s *Service) WaitForChange(ctx context.Context, a Actor, projectID string, since int64, wait time.Duration) (Sync, error) {
	if wait > MaxSyncWait {
		wait = MaxSyncWait
	}
	// Subscribe before reading, so a change between the read and the wait is not lost.
	ch, cancel := s.hub.subscribe(projectID)
	defer cancel()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		var rev int64
		err := s.view(ctx, a, projectID, func(tx *store.Tx, x access) error { rev = x.Project.Revision; return nil })
		if err != nil {
			return Sync{}, err
		}
		if rev > since {
			return Sync{Revision: rev, Changed: true}, nil
		}
		if wait <= 0 {
			return Sync{Revision: rev}, nil
		}
		select {
		case <-ch:
		case <-timer.C:
			return Sync{Revision: rev}, nil
		case <-ctx.Done():
			return Sync{Revision: rev}, ctx.Err()
		}
	}
}

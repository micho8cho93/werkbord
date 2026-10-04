// Package store defines the persistence boundary. Services depend only on
// these interfaces; internal/store/sqlite is the one implementation.
//
// All access goes through View (read-only, consistent snapshot) or Update
// (serialised read-write transaction). Writes and the events that describe
// them are committed in the same Update, so the event log can never disagree
// with the tables.
package store

import (
	"context"

	"devboard/internal/domain"
)

// Store is the root persistence handle owned by the controller.
type Store interface {
	// View runs fn in a read-only transaction.
	View(ctx context.Context, fn func(Tx) error) error
	// Update runs fn in a read-write transaction. Returning an error rolls
	// back. Update calls are serialised.
	Update(ctx context.Context, fn func(Tx) error) error
	// Ping checks that the database is reachable.
	Ping(ctx context.Context) error
	// Close releases the database. It must be called once, after all users
	// have stopped.
	Close() error
}

// Tx exposes the repositories bound to one transaction.
type Tx interface {
	Projects() ProjectRepo
	Repositories() GitRepositoryRepo
	Tasks() TaskRepo
	Runs() RunRepo
	Questions() QuestionRepo
	Worktrees() WorktreeRepo
	Events() EventRepo
}

// ProjectRepo persists projects. GetByPath returns domain.ErrNotFound when no
// project has that path; Create returns domain.ErrDuplicate for a path that
// is already registered.
type ProjectRepo interface {
	Create(ctx context.Context, p *domain.Project) error
	Get(ctx context.Context, id string) (*domain.Project, error)
	GetByPath(ctx context.Context, repoPath string) (*domain.Project, error)
	List(ctx context.Context) ([]domain.Project, error)
}

// GitRepositoryRepo stores the latest inspection snapshot per project.
//
// Upsert returns domain.ErrDuplicate when another project's snapshot already
// has the same non-empty CommonDir: two projects may not share a repository.
// GetByCommonDir returns domain.ErrNotFound when no snapshot has it.
type GitRepositoryRepo interface {
	Upsert(ctx context.Context, r *domain.GitRepository) error
	Get(ctx context.Context, projectID string) (*domain.GitRepository, error)
	GetByCommonDir(ctx context.Context, commonDir string) (*domain.GitRepository, error)
}

// TaskRepo persists tasks. Update is compare-and-swap: it succeeds only if the
// stored version equals t.Version, then increments t.Version. Otherwise it
// returns domain.ErrConflict.
type TaskRepo interface {
	Create(ctx context.Context, t *domain.Task) error
	Get(ctx context.Context, id string) (*domain.Task, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.Task, error)
	Update(ctx context.Context, t *domain.Task) error
	// MaxPosition returns the largest position in a column, or 0 when empty.
	MaxPosition(ctx context.Context, projectID string, state domain.TaskState) (float64, error)
}

// RunRepo persists runs. Update is compare-and-swap like TaskRepo.Update.
type RunRepo interface {
	Create(ctx context.Context, r *domain.Run) error
	Get(ctx context.Context, id string) (*domain.Run, error)
	Update(ctx context.Context, r *domain.Run) error
	ListByTask(ctx context.Context, taskID string) ([]domain.Run, error)
	// ListActive returns all runs not in a terminal state, oldest first.
	ListActive(ctx context.Context) ([]domain.Run, error)
}

// QuestionRepo persists agent questions.
type QuestionRepo interface {
	Create(ctx context.Context, q *domain.Question) error
	Get(ctx context.Context, id string) (*domain.Question, error)
	Update(ctx context.Context, q *domain.Question) error
	ListPending(ctx context.Context) ([]domain.Question, error)
	ListByRun(ctx context.Context, runID string) ([]domain.Question, error)
}

// WorktreeRepo persists worktrees created for runs.
//
// The database enforces what a row may say, whoever writes it: Create returns
// domain.ErrInvalid for a malformed path, branch or base ref and
// domain.ErrDuplicate for a path in use or a branch that already has an active
// worktree in the project. Update is compare-and-swap like TaskRepo.Update and
// changes only the removal state: RemovingSince can be set (never cleared)
// while no active run uses the worktree, and State can become removed only
// after that, in a later update. Violations return domain.ErrConflict, as does
// an active run created on a worktree that is being or has been removed.
// Records of active worktrees cannot be deleted, directly or by deleting their
// project.
type WorktreeRepo interface {
	Create(ctx context.Context, w *domain.Worktree) error
	Get(ctx context.Context, id string) (*domain.Worktree, error)
	Update(ctx context.Context, w *domain.Worktree) error
	ListByProject(ctx context.Context, projectID string) ([]domain.Worktree, error)
	// ListActive returns every worktree still on disk, across projects.
	ListActive(ctx context.Context) ([]domain.Worktree, error)
}

// EventRepo is the append-only event log.
type EventRepo interface {
	// Append assigns e.Seq and e.CreatedAt (if zero) and stores e.
	Append(ctx context.Context, e *domain.Event) error
	// ListAfter returns up to limit events with Seq > after, in order.
	ListAfter(ctx context.Context, after int64, limit int) ([]domain.Event, error)
	// LatestSeq returns the highest Seq, or 0 for an empty log.
	LatestSeq(ctx context.Context) (int64, error)
}

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
	"time"

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
	// Savepoint rolls back only fn on failure, retaining other work in this transaction.
	Savepoint(context.Context, func() error) error
	Projects() ProjectRepo
	Repositories() GitRepositoryRepo
	Tasks() TaskRepo
	Runs() RunRepo
	Questions() QuestionRepo
	Worktrees() WorktreeRepo
	Health() HealthRepo
	Settings() SettingsRepo
	Runners() RunnerRepo
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
	// SetExecution replaces the project's default execution configuration.
	SetExecution(ctx context.Context, id string, cfg domain.ExecutionConfig, at time.Time) error
}

// SettingsRepo stores the app's own small settings as JSON documents by key
// (see domain.SettingExecution). Get returns domain.ErrNotFound for a key that
// was never set, and leaves dst alone.
type SettingsRepo interface {
	Get(ctx context.Context, key string, dst any) error
	Set(ctx context.Context, key string, value any, at time.Time) error
}

// RunnerRepo persists the computers that can run agents.
type RunnerRepo interface {
	Get(ctx context.Context, id string) (*domain.Runner, error)
	Save(ctx context.Context, r *domain.Runner) error
	// UpsertLocal registers this computer, or refreshes its record: name, system,
	// version and last-seen time change; its ID and creation time never do. It
	// returns the stored runner.
	UpsertLocal(ctx context.Context, r *domain.Runner) (*domain.Runner, error)
	List(ctx context.Context) ([]domain.Runner, error)
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
	// ListActive returns all runs not in a terminal state (including blocked
	// ones), oldest first.
	ListActive(ctx context.Context) ([]domain.Run, error)
	// ListByProject returns up to limit of the project's runs, newest first: its
	// run history.
	ListByProject(ctx context.Context, projectID string, limit int) ([]domain.Run, error)
	// TouchActivity records a run's latest activity without changing its
	// version: activity changes many times a second and is not a state change,
	// so it must not make a concurrent state change fail its compare-and-swap.
	// It does nothing for a run that has ended.
	TouchActivity(ctx context.Context, id, activity string, at time.Time) error
	// ListLatestByProject returns each of the project's tasks' most recent run,
	// oldest first. Tasks that have never run are absent.
	ListLatestByProject(ctx context.Context, projectID string) ([]domain.Run, error)
}

// QuestionRepo persists agent questions.
type QuestionRepo interface {
	Create(ctx context.Context, q *domain.Question) error
	Get(ctx context.Context, id string) (*domain.Question, error)
	Update(ctx context.Context, q *domain.Question) error
	// ListPending returns every pending question, across projects, oldest first.
	ListPending(ctx context.Context) ([]domain.Question, error)
	// ListPendingByProject returns one project's pending questions, oldest first.
	ListPendingByProject(ctx context.Context, projectID string) ([]domain.Question, error)
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

// HealthRepo persists repository health findings: the memory of what was found,
// not the source of truth about the repository (see domain.HealthFinding).
//
// A finding is identified by (project, ID). Upsert writes the whole finding,
// state included, and the database refuses a finding whose state and timestamps
// disagree (an open finding cannot carry a resolution time, a dismissed one must
// say how severe it was when dismissed).
type HealthRepo interface {
	// Upsert inserts or replaces a finding.
	Upsert(ctx context.Context, f *domain.HealthFinding) error
	// Get returns one finding, or domain.ErrNotFound.
	Get(ctx context.Context, projectID, id string) (*domain.HealthFinding, error)
	// ListByProject returns a project's findings, worst first. With no states
	// given, every state is returned.
	ListByProject(ctx context.Context, projectID string, states ...domain.HealthState) ([]domain.HealthFinding, error)
	// ListOpen returns the open findings of every project that are at least as
	// severe as min, worst first, then oldest first.
	ListOpen(ctx context.Context, min domain.HealthSeverity) ([]domain.HealthFinding, error)
	// DeleteResolvedBefore forgets findings that resolved before t, and returns how many.
	DeleteResolvedBefore(ctx context.Context, t time.Time) (int, error)
	// GetCheck returns when a project's health was last worked out, or
	// domain.ErrNotFound if it never was.
	GetCheck(ctx context.Context, projectID string) (*domain.HealthCheck, error)
	SetCheck(ctx context.Context, c domain.HealthCheck) error
}

// EventRepo is the sequenced event log; terminal transcript retention is bounded.
type EventRepo interface {
	ListAfterProject(ctx context.Context, projectID string, after int64, limit int) ([]domain.Event, error)
	// ReplayFloor is the latest event removed by retention. Older clients resnapshot.
	ReplayFloor(ctx context.Context) (int64, error)
	// Prune removes at most 10,000 old terminal-run events; active evidence is retained.
	Prune(ctx context.Context, now time.Time) (int, error)
	// Append assigns e.Seq and e.CreatedAt (if zero) and stores e.
	Append(ctx context.Context, e *domain.Event) error
	// ListAfter returns up to limit events with Seq > after, in order.
	ListAfter(ctx context.Context, after int64, limit int) ([]domain.Event, error)
	// LatestSeq returns the highest Seq, or 0 for an empty log.
	LatestSeq(ctx context.Context) (int64, error)
	// ListByRun returns up to limit of a run's events that precede seq
	// `before` (0 means from the end of the log): the newest such events, in
	// ascending order. To page backwards, pass the first Seq of the previous page.
	ListByRun(ctx context.Context, runID string, before int64, limit int) ([]domain.Event, error)
}

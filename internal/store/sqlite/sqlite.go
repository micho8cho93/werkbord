// Package sqlite implements store.Store on SQLite using the pure-Go
// modernc.org/sqlite driver, so the controller builds without cgo. Opening,
// migrating and backing up the database is the shared internal/sqlitekit; this
// package is the schema and the repositories.
//
// Concurrency model: one writer connection (all Update calls queue on it and
// take the write lock up front with BEGIN IMMEDIATE) and a small pool of
// read-only connections. In WAL mode readers never block the writer and see a
// consistent snapshot for the duration of a View.
package sqlite

import (
	"context"
	"database/sql"
	"log/slog"

	"devboard/internal/sqlitekit"
	"devboard/internal/store"
)

// DB is a store.Store backed by a SQLite file.
type DB struct {
	pool   *sqlitekit.Pool
	writer *sql.DB
	reader *sql.DB
	log    *slog.Logger
}

var _ store.Store = (*DB)(nil)

// Open opens (creating if necessary) the database at path and applies any
// pending migrations before returning.
func Open(ctx context.Context, path string, log *slog.Logger) (*DB, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ms, err := Migrations()
	if err != nil {
		return nil, err
	}
	pool, err := sqlitekit.Open(ctx, path, sqlitekit.Options{
		Migrations: ms, Product: productName, BackupPrefix: backupPrefix, Log: log,
	})
	if err != nil {
		return nil, err
	}
	return &DB{pool: pool, writer: pool.Writer, reader: pool.Reader, log: log}, nil
}

// SchemaVersion reports the applied migration version.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	return d.pool.SchemaVersion(ctx)
}

// View implements store.Store.
func (d *DB) View(ctx context.Context, fn func(store.Tx) error) error {
	return d.pool.View(ctx, func(tx *sql.Tx) error { return fn(&txn{q: tx}) })
}

// Update implements store.Store.
func (d *DB) Update(ctx context.Context, fn func(store.Tx) error) error {
	return d.pool.Update(ctx, func(tx *sql.Tx) error { return fn(&txn{q: tx}) })
}

// Ping implements store.Store.
func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

// Close implements store.Store.
func (d *DB) Close() error { return d.pool.Close() }

// queryer is satisfied by *sql.Tx.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type txn struct{ q queryer }

func (t *txn) Projects() store.ProjectRepo           { return projectRepo{t.q} }
func (t *txn) Repositories() store.GitRepositoryRepo { return gitRepoRepo{t.q} }
func (t *txn) Tasks() store.TaskRepo                 { return taskRepo{t.q} }
func (t *txn) Runs() store.RunRepo                   { return runRepo{t.q} }
func (t *txn) Settings() store.SettingsRepo          { return settingsRepo{t.q} }
func (t *txn) Runners() store.RunnerRepo             { return runnerRepo{t.q} }
func (t *txn) Questions() store.QuestionRepo         { return questionRepo{t.q} }
func (t *txn) Worktrees() store.WorktreeRepo         { return worktreeRepo{t.q} }
func (t *txn) Health() store.HealthRepo              { return healthRepo{t.q} }
func (t *txn) Events() store.EventRepo               { return eventRepo{t.q} }

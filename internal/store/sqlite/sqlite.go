// Package sqlite implements store.Store on SQLite using the pure-Go
// modernc.org/sqlite driver, so the controller builds without cgo.
//
// Concurrency model: one writer connection (all Update calls queue on it and
// take the write lock up front with BEGIN IMMEDIATE) and a small pool of
// read-only connections. In WAL mode readers never block the writer and see a
// consistent snapshot for the duration of a View.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"devboard/internal/store"
)

// DB is a store.Store backed by a SQLite file.
type DB struct {
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
	if strings.ContainsRune(path, '?') {
		return nil, fmt.Errorf("database path must not contain '?': %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	common := url.Values{}
	common.Add("_pragma", "foreign_keys(1)")
	common.Add("_pragma", "busy_timeout(5000)")

	wq := cloneValues(common)
	wq.Add("_pragma", "journal_mode(WAL)")
	// FULL, not NORMAL: with NORMAL a committed transaction can be lost in a
	// power cut, and records of worktrees (written before a directory is
	// created, and after one is removed) would then disagree with the disk.
	wq.Add("_pragma", "synchronous(FULL)")
	wq.Set("_txlock", "immediate")
	writer, err := sql.Open("sqlite", path+"?"+wq.Encode())
	if err != nil {
		return nil, err
	}
	writer.SetMaxOpenConns(1)
	writer.SetConnMaxLifetime(0)

	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	// The file may hold secrets-adjacent data (repo paths, agent output).
	_ = os.Chmod(path, 0o600)

	ms, err := Migrations()
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	applied, err := migrate(ctx, writer, ms)
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	if applied > 0 {
		log.Info("database migrated", "path", path, "applied", applied, "version", len(ms))
	}

	rq := cloneValues(common)
	rq.Set("_query_only", "1")
	reader, err := sql.Open("sqlite", path+"?"+rq.Encode())
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader.SetMaxOpenConns(4)

	return &DB{writer: writer, reader: reader, log: log}, nil
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// SchemaVersion reports the applied migration version.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	return schemaVersion(ctx, d.reader)
}

// View implements store.Store.
func (d *DB) View(ctx context.Context, fn func(store.Tx) error) error {
	tx, err := d.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	return fn(&txn{q: tx})
}

// Update implements store.Store.
func (d *DB) Update(ctx context.Context, fn func(store.Tx) error) error {
	tx, err := d.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(&txn{q: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// Ping implements store.Store.
func (d *DB) Ping(ctx context.Context) error {
	var one int
	return d.reader.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

// Close implements store.Store.
func (d *DB) Close() error {
	return errors.Join(d.reader.Close(), d.writer.Close())
}

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
func (t *txn) Questions() store.QuestionRepo         { return questionRepo{t.q} }
func (t *txn) Worktrees() store.WorktreeRepo         { return worktreeRepo{t.q} }
func (t *txn) Health() store.HealthRepo              { return healthRepo{t.q} }
func (t *txn) Events() store.EventRepo               { return eventRepo{t.q} }

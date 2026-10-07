// Package sqlitekit is the SQLite plumbing shared by every Werkbord product:
// opening a database with the pure-Go modernc.org/sqlite driver (so nothing
// needs cgo), versioned migrations, and a copy of the database before an
// upgrade. It knows nothing about any product's schema or store interfaces;
// each product brings its own migrations and repositories.
//
// Concurrency model: one writer connection (all writes queue on it and take the
// write lock up front with BEGIN IMMEDIATE) and a small pool of read-only
// connections. In WAL mode readers never block the writer and see a consistent
// snapshot for the duration of a transaction.
package sqlitekit

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
)

// Options describe the database to open.
type Options struct {
	// Migrations are applied before Open returns.
	Migrations []Migration
	// Product names the program, for the message when the database is newer than it.
	Product string
	// BackupPrefix names the pre-upgrade copies: <prefix>-v<version>-<time>.db.
	BackupPrefix string
	Log          *slog.Logger
	// Replica opens the database as a local copy of data that lives elsewhere (Werkbord Team's
	// replicated store keeps one of its cluster's database): its schema arrives with the data, so
	// nothing is migrated or backed up before an upgrade, and a committed write need not survive a
	// power cut, because the copy is made again from the cluster.
	Replica bool
}

// Pool is the writer and the readers of one database.
type Pool struct {
	Writer *sql.DB
	Reader *sql.DB
}

// Open opens (creating if necessary) the database at path and applies any
// pending migrations before returning.
func Open(ctx context.Context, path string, opt Options) (*Pool, error) {
	log := opt.Log
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
	if opt.Replica {
		wq.Add("_pragma", "synchronous(NORMAL)")
	} else {
		wq.Add("_pragma", "synchronous(FULL)")
	}
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

	if !opt.Replica {
		BackupBeforeUpgrade(ctx, writer, path, opt.BackupPrefix, len(opt.Migrations), log)
		applied, err := Migrate(ctx, writer, opt.Migrations, opt.Product)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		if applied > 0 {
			log.Info("database migrated", "path", path, "applied", applied, "version", len(opt.Migrations))
		}
	}

	rq := cloneValues(common)
	rq.Set("_query_only", "1")
	reader, err := sql.Open("sqlite", path+"?"+rq.Encode())
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader.SetMaxOpenConns(4)

	return &Pool{Writer: writer, Reader: reader}, nil
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// SchemaVersion reports the applied migration version.
func (p *Pool) SchemaVersion(ctx context.Context) (int, error) {
	return SchemaVersion(ctx, p.Reader)
}

// Ping checks that the database answers.
func (p *Pool) Ping(ctx context.Context) error {
	var one int
	return p.Reader.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

// Close closes every connection.
func (p *Pool) Close() error {
	return errors.Join(p.Reader.Close(), p.Writer.Close())
}

// View runs fn in a read-only transaction (a consistent snapshot).
func (p *Pool) View(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := p.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	return fn(tx)
}

// Update runs fn in the serialised read-write transaction and commits it if fn
// returns nil.
func (p *Pool) Update(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := p.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migration is one versioned schema change.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations returns the embedded migrations in version order. Files are named
// NNNN_description.sql; versions must be unique and contiguous from 1.
func Migrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []Migration
	for _, e := range entries {
		name := e.Name()
		prefix, rest, ok := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
		if !ok {
			return nil, fmt.Errorf("migration %q: name must be NNNN_description.sql", name)
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %q: bad version: %w", name, err)
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		ms = append(ms, Migration{Version: v, Name: rest, SQL: string(body)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Version < ms[j].Version })
	for i, m := range ms {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrations are not contiguous: expected version %d, found %d", i+1, m.Version)
		}
	}
	return ms, nil
}

// migrate applies every migration newer than the database's current version,
// each in its own transaction. It refuses to run against a database created
// by a newer build, rather than risk corrupting it.
func migrate(ctx context.Context, db *sql.DB, ms []Migration) (applied int, err error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}
	current, err := schemaVersion(ctx, db)
	if err != nil {
		return 0, err
	}
	if latest := len(ms); current > latest {
		return 0, fmt.Errorf("database schema version %d is newer than this build supports (%d); upgrade devboard", current, latest)
	}
	for _, m := range ms {
		if m.Version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func applyMigration(ctx context.Context, db *sql.DB, m Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("migration %04d_%s: %w", m.Version, m.Name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.Version, m.Name, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("record migration %04d: %w", m.Version, err)
	}
	return tx.Commit()
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return int(v.Int64), nil
}

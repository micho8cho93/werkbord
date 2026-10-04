package sqlitekit

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Migration is one versioned schema change.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// LoadMigrations reads the migrations in dir of fsys, in version order. Files are
// named NNNN_description.sql; versions must be unique and contiguous from 1.
func LoadMigrations(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
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
		body, err := fs.ReadFile(fsys, dir+"/"+name)
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

// Migrate applies every migration newer than the database's current version,
// each in its own transaction. It refuses to run against a database created
// by a newer build, rather than risk corrupting it; product names the program
// to upgrade in that message.
func Migrate(ctx context.Context, db *sql.DB, ms []Migration, product string) (applied int, err error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}
	current, err := SchemaVersion(ctx, db)
	if err != nil {
		return 0, err
	}
	if latest := len(ms); current > latest {
		return 0, fmt.Errorf("database schema version %d is newer than this build supports (%d); upgrade %s", current, latest, product)
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

// ForeignKeysOff is the first line of a migration that has to rebuild a table
// other tables refer to. SQLite cannot change a CHECK constraint or a column in
// place, and dropping a parent table with foreign keys on would cascade into its
// children, so such a migration runs with them off.
const ForeignKeysOff = "-- migrate:foreign-keys-off"

func applyMigration(ctx context.Context, db *sql.DB, m Migration) error {
	// foreign_keys cannot be changed inside a transaction, so it is set on a
	// connection held for the whole migration, and restored afterwards. The writer
	// pool has one connection, so nothing else can be using the database.
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	off := strings.HasPrefix(strings.TrimSpace(m.SQL), ForeignKeysOff)
	if off {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return fmt.Errorf("migration %04d_%s: %w", m.Version, m.Name, err)
		}
		defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`) }()
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("migration %04d_%s: %w", m.Version, m.Name, err)
	}
	if off {
		// What the rebuild must not break: every reference still resolves.
		rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err != nil {
			return fmt.Errorf("migration %04d_%s: check foreign keys: %w", m.Version, m.Name, err)
		}
		var table string
		broken := rows.Next()
		if broken {
			var rowid sql.NullInt64
			var parent string
			var fk int
			_ = rows.Scan(&table, &rowid, &parent, &fk)
		}
		_ = rows.Close()
		if broken {
			return fmt.Errorf("migration %04d_%s: it would leave a row in %s pointing at a row that does not exist", m.Version, m.Name, table)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.Version, m.Name, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("record migration %04d: %w", m.Version, err)
	}
	return tx.Commit()
}

// SchemaVersion reports the highest applied migration version (0 for none).
func SchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return int(v.Int64), nil
}

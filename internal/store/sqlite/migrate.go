package sqlite

import (
	"context"
	"database/sql"
	"embed"

	"devboard/internal/sqlitekit"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migration is one versioned schema change.
type Migration = sqlitekit.Migration

// Migrations returns the embedded migrations in version order. Files are named
// NNNN_description.sql; versions must be unique and contiguous from 1.
func Migrations() ([]Migration, error) { return sqlitekit.LoadMigrations(migrationFS, "migrations") }

// migrate applies every migration newer than the database's current version
// (see sqlitekit.Migrate).
func migrate(ctx context.Context, db *sql.DB, ms []Migration) (applied int, err error) {
	return sqlitekit.Migrate(ctx, db, ms, productName)
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	return sqlitekit.SchemaVersion(ctx, db)
}

// productName is what a database from a newer build tells the user to upgrade.
const productName = "devboard"

package sqlite

import (
	"context"

	"werkbord/internal/sqlitekit"
)

// backupPrefix names the pre-upgrade copies: werkbord-v<version>-<time>.db.
const backupPrefix = "werkbord"

// keepBackups is how many pre-upgrade copies of the database are kept.
const keepBackups = sqlitekit.KeepBackups

// BackupDir is where pre-upgrade copies of the database at dbPath are kept.
func BackupDir(dbPath string) string { return sqlitekit.BackupDir(dbPath) }

// Info describes a database file without opening it for writing or migrating it.
type Info = sqlitekit.Info

// Inspect reads a database file's version and runs SQLite's quick integrity
// check, read-only: it never creates, migrates or changes anything, so it is
// safe to run beside a controller that has the database open.
func Inspect(ctx context.Context, path string) (Info, error) {
	ms, err := Migrations()
	if err != nil {
		return Info{}, err
	}
	return sqlitekit.Inspect(ctx, path, len(ms))
}

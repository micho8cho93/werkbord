package sqlitekit

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// KeepBackups is how many pre-upgrade copies of the database are kept.
const KeepBackups = 5

// BackupDir is where pre-upgrade copies of the database at dbPath are kept.
func BackupDir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "backups") }

// BackupBeforeUpgrade copies the database before a migration changes it, if
// there is anything to migrate. A migration is the one thing that rewrites a
// user's data in place, and it runs the first time a new build starts, with no
// one watching: a copy means a bad migration, or a bad new version, costs nothing.
// A database that is new (nothing to lose) or already current is not copied.
//
// It uses VACUUM INTO, which writes a consistent, compact copy without stopping
// anyone. A failure to back up is logged and does not stop the upgrade: refusing
// to start because a disk is full would be worse for the user than the risk.
//
// Copies are named <prefix>-v<schema version>-<UTC time>.db.
func BackupBeforeUpgrade(ctx context.Context, db *sql.DB, path, prefix string, latest int, log *slog.Logger) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&n); err != nil || n == 0 {
		return
	}
	current, err := SchemaVersion(ctx, db)
	if err != nil || current == 0 || current >= latest {
		return
	}
	dir := BackupDir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Warn("cannot back up the database before upgrading it", "err", err)
		return
	}
	dst := filepath.Join(dir, fmt.Sprintf("%s-v%d-%s.db", prefix, current, time.Now().UTC().Format("20060102T150405Z")))
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		log.Warn("cannot back up the database before upgrading it", "err", err)
		return
	}
	_ = os.Chmod(dst, 0o600)
	log.Info("database backed up before upgrading", "from_version", current, "to_version", latest, "backup", dst)
	PruneBackups(dir, prefix)
}

// PruneBackups keeps the newest few of the backups named with prefix.
func PruneBackups(dir, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix+"-v") && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // the timestamp sorts oldest first within a version, and versions rise
	sort.SliceStable(names, func(i, j int) bool { return backupTime(names[i]) < backupTime(names[j]) })
	for len(names) > KeepBackups {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// backupTime is the timestamp in a backup's name.
func backupTime(name string) string {
	i := strings.LastIndex(name, "-")
	if i < 0 {
		return name
	}
	return strings.TrimSuffix(name[i+1:], ".db")
}

// Info describes a database file without opening it for writing or migrating it.
type Info struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	// Version is the applied schema version; Latest is the newest this build knows.
	Version int `json:"version"`
	Latest  int `json:"latest"`
	// NewerThanBuild: the file was written by a newer build, which this one refuses to open.
	NewerThanBuild bool  `json:"newerThanBuild"`
	Size           int64 `json:"size"`
	// Integrity is the result of SQLite's own quick check: "ok" or what it found.
	Integrity string `json:"integrity"`
}

// Inspect reads a database file's version and runs SQLite's quick integrity
// check, read-only: it never creates, migrates or changes anything, so it is
// safe to run beside a program that has the database open. latest is the newest
// schema version this build knows.
func Inspect(ctx context.Context, path string, latest int) (Info, error) {
	info := Info{Path: path, Latest: latest}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return info, nil
		}
		return info, err
	}
	info.Exists, info.Size = true, fi.Size()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return info, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&n); err != nil {
		return info, fmt.Errorf("read %s: %w", path, err)
	}
	if n > 0 {
		if info.Version, err = SchemaVersion(ctx, db); err != nil {
			return info, err
		}
	}
	info.NewerThanBuild = info.Version > info.Latest
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&info.Integrity); err != nil {
		return info, fmt.Errorf("check %s: %w", path, err)
	}
	return info, nil
}

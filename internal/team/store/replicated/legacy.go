package replicated

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devboard/internal/sqlitekit"
	"devboard/internal/team/store"
)

// A workspace that Team kept in one SQLite file (team.db, before replication) moves into a cluster like this:
//
//  1. PrepareImport makes a consistent copy of the file (VACUUM INTO, so the original is only read), has SQLite check
//     it, checks that it is no newer than this build, and adds the tables the cluster's protocol keeps. What it
//     leaves is a SQLite file that is the legacy database exactly, plus those tables: the data is not translated,
//     read back or re-inserted row by row, so nothing can be lost in the translation.
//  2. The caller starts the cluster's first node and loads the file into it with rqlite's own restore (Client.Load).
//  3. VerifyImport reads the cluster back and compares it with the original: the schema, the number of rows of every table,
//     and a hash of every row.
//
// Only then does the caller switch the workspace to the cluster. The original file is never changed.

// ImportInfo describes a prepared import.
type ImportInfo struct {
	// File is the prepared SQLite file, in the work directory.
	File string
	// Legacy is the digest of the original's contents (before the cluster's tables were added).
	Legacy Digest
	// SchemaVersion is the schema version the original has.
	SchemaVersion int
	// Tables are the original's tables, and Owners the table each index and trigger belongs to.
	Tables map[string]bool
	Owners map[string]string
}

// ErrNotLegacy: the file is not a Team database.
var ErrNotLegacy = errors.New("replicated: that file is not a Werkbord Team database")

// PrepareImport prepares the legacy database at path for loading into a cluster, in workDir (which it makes).
func PrepareImport(ctx context.Context, path, workDir string) (ImportInfo, error) {
	if _, err := os.Stat(path); err != nil {
		return ImportInfo{}, err
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return ImportInfo{}, err
	}
	copyPath := filepath.Join(workDir, "legacy-copy.sqlite")
	outPath := filepath.Join(workDir, "import.sqlite")
	_ = os.Remove(copyPath)
	_ = os.Remove(outPath)

	src, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return ImportInfo{}, err
	}
	defer src.Close()
	if _, err := src.ExecContext(ctx, `VACUUM INTO ?`, copyPath); err != nil {
		return ImportInfo{}, fmt.Errorf("replicated: copying the database: %w", err)
	}
	_ = os.Chmod(copyPath, 0o600)

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(copyPath)+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return ImportInfo{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		return ImportInfo{}, fmt.Errorf("replicated: the database failed SQLite's integrity check (%v %s): it is not moved", err, res)
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return ImportInfo{}, err
	}
	broken := rows.Next()
	_ = rows.Close()
	if broken {
		return ImportInfo{}, errors.New("replicated: the database has rows that point at rows that do not exist: it is not moved")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('schema_migrations', 'workspaces', 'members')`).Scan(&n); err != nil || n != 3 {
		return ImportInfo{}, ErrNotLegacy
	}
	info := ImportInfo{File: outPath, Tables: map[string]bool{}, Owners: map[string]string{}}
	if info.SchemaVersion, err = sqlitekit.SchemaVersion(ctx, db); err != nil {
		return ImportInfo{}, err
	}
	ms, err := store.Migrations()
	if err != nil {
		return ImportInfo{}, err
	}
	if info.SchemaVersion > len(ms) {
		return ImportInfo{}, ErrSchemaNewer{Have: info.SchemaVersion, Max: len(ms)}
	}
	if info.Legacy, err = digestOf(ctx, sqlQuerier{db}); err != nil {
		return ImportInfo{}, err
	}
	for t := range info.Legacy.Tables {
		info.Tables[t] = true
	}
	orows, err := db.QueryContext(ctx, `SELECT name, tbl_name FROM sqlite_master WHERE sql IS NOT NULL`)
	if err != nil {
		return ImportInfo{}, err
	}
	for orows.Next() {
		var name, tbl string
		if err := orows.Scan(&name, &tbl); err != nil {
			_ = orows.Close()
			return ImportInfo{}, err
		}
		info.Owners[name] = tbl
	}
	_ = orows.Close()

	// What is loaded is another file: a copy of the copy, with the cluster's own tables added as a new cluster starts with
	// them. The copy itself stays exactly the original, because it is what the caller keeps to go back to.
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, outPath); err != nil {
		return ImportInfo{}, err
	}
	out, err := sql.Open("sqlite", "file:"+filepath.ToSlash(outPath)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(DELETE)")
	if err != nil {
		return ImportInfo{}, err
	}
	out.SetMaxOpenConns(1)
	for _, g := range genesisStatements() {
		if _, err := out.ExecContext(ctx, g.SQL, g.Args...); err != nil {
			out.Close()
			return ImportInfo{}, fmt.Errorf("replicated: preparing the cluster's tables: %w", err)
		}
	}
	if err := out.Close(); err != nil {
		return ImportInfo{}, err
	}
	_ = os.Chmod(outPath, 0o600)
	if _, err := checkFile(ctx, outPath); err != nil {
		return ImportInfo{}, err
	}
	return info, nil
}

// LoadImport loads a prepared import into the cluster, which must be empty (it has no workspace tables yet), with
// rqlite's own restore.
func (s *Store) LoadImport(ctx context.Context, info ImportInfo) error {
	raw, err := os.ReadFile(info.File)
	if err != nil {
		return err
	}
	return s.client.Load(ctx, raw)
}

// VerifyImport compares the cluster's data with the legacy database it came from: the schema of every table,
// index and trigger, the rows of every table (counted and hashed), the schema version, and SQLite's integrity check of
// what the cluster holds. A difference is an error naming it.
func VerifyImport(ctx context.Context, c *Client, info ImportInfo) error {
	remote, err := digestOf(ctx, clusterQuerier{c, LevelLinearizable})
	if err != nil {
		return err
	}
	want := info.Legacy
	got := remote.Restrict(info.Tables, info.Owners)
	if diff := want.Diff(got); diff != "" {
		return fmt.Errorf("replicated: the cluster's data is not the original's: %s", diff)
	}
	for t, n := range want.Tables {
		if got.Tables[t] != n {
			return fmt.Errorf("replicated: table %s has %d rows in the cluster and %d in the original", t, got.Tables[t], n)
		}
	}
	res, err := c.Query(ctx, LevelLinearizable, Stmt{SQL: `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`}, Stmt{SQL: `PRAGMA integrity_check`})
	if err != nil {
		return err
	}
	if v := fmt.Sprint(res[0].Values[0][0]); v != fmt.Sprint(info.SchemaVersion) {
		return fmt.Errorf("replicated: the cluster's schema version is %s, the original's is %d", v, info.SchemaVersion)
	}
	if v := fmt.Sprint(res[1].Values[0][0]); !strings.EqualFold(v, "ok") {
		return fmt.Errorf("replicated: the cluster's database failed SQLite's integrity check: %s", v)
	}
	return nil
}

// ExportLegacy turns a backup of the cluster's database into a Team database of the single-file kind, which is how a
// workspace goes back to one file: a copy of the backup without the tables the cluster keeps for itself, whose every
// other table is checked, row by row, to be the backup's. The result is written to out, only if it is whole.
func ExportLegacy(ctx context.Context, backupFile, out string) error {
	if _, err := checkFile(ctx, backupFile); err != nil {
		return err
	}
	tmp := out + ".partial"
	_ = os.Remove(tmp)
	raw, err := os.ReadFile(backupFile)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
			_ = os.Remove(tmp + "-wal")
			_ = os.Remove(tmp + "-shm")
		}
	}()
	src, err := sql.Open("sqlite", "file:"+filepath.ToSlash(backupFile)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer src.Close()
	before, err := digestOf(ctx, sqlQuerier{src})
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(tmp)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(DELETE)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	for _, t := range []string{"_wal", "_fence", "_meta"} {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+t); err != nil {
			db.Close()
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
		db.Close()
		return err
	}
	after, err := digestOf(ctx, sqlQuerier{db})
	if err != nil {
		db.Close()
		return err
	}
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		db.Close()
		return fmt.Errorf("replicated: the exported database failed SQLite's integrity check (%v %s)", err, res)
	}
	if err := db.Close(); err != nil {
		return err
	}
	tables := map[string]bool{}
	owners := map[string]string{}
	for t := range after.Tables {
		tables[t] = true
	}
	orows, err := src.QueryContext(ctx, `SELECT name, tbl_name FROM sqlite_master WHERE sql IS NOT NULL`)
	if err != nil {
		return err
	}
	for orows.Next() {
		var name, tbl string
		if err := orows.Scan(&name, &tbl); err != nil {
			_ = orows.Close()
			return err
		}
		owners[name] = tbl
	}
	_ = orows.Close()
	if diff := after.Diff(before.Restrict(tables, owners)); diff != "" {
		return fmt.Errorf("replicated: the exported database is not the cluster's data: %s", diff)
	}
	ms, err := store.Migrations()
	if err != nil {
		return err
	}
	if after.Tables["schema_migrations"] == 0 && len(ms) > 0 {
		return errors.New("replicated: the exported database has no schema version")
	}
	if err := os.Rename(tmp, out); err != nil {
		return err
	}
	ok = true
	return nil
}

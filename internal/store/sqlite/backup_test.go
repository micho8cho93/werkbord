package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/store"
)

// oldDatabase makes a database at an earlier schema version with one project in it.
func oldDatabase(t *testing.T, version int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devboard.db")
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	defer raw.Close()
	ms, _ := Migrations()
	if _, err := migrate(ctx, raw, ms[:version]); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO projects (id, name, repo_path, created_at, updated_at) VALUES ('prj_1', 'keep me', '/repos/keep', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpgradeBacksUpTheOldDatabaseFirst(t *testing.T) {
	path := oldDatabase(t, 5)
	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	entries, err := os.ReadDir(BackupDir(path))
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "devboard-v5-") {
		t.Fatalf("backups = %v, %v", entries, err)
	}
	// The backup is the database as it was: still version 5, with the data, and not migrated.
	info, err := Inspect(ctx, filepath.Join(BackupDir(path), entries[0].Name()))
	if err != nil || info.Version != 5 || info.Integrity != "ok" {
		t.Fatalf("backup = %+v, %v", info, err)
	}
	if fi, _ := os.Stat(filepath.Join(BackupDir(path), entries[0].Name())); fi.Mode().Perm() != 0o600 {
		t.Errorf("the backup is readable by others: %v", fi.Mode())
	}
	// The live one was upgraded and kept its data.
	var name string
	if err := db.View(ctx, func(tx store.Tx) error {
		p, err := tx.Projects().Get(ctx, "prj_1")
		if err == nil {
			name = p.Name
		}
		return err
	}); err != nil || name != "keep me" {
		t.Fatalf("project after upgrade = %q, %v", name, err)
	}
	if v, _ := db.SchemaVersion(ctx); v != len(mustMigrations(t)) {
		t.Fatalf("schema version = %d", v)
	}
}

func mustMigrations(t *testing.T) []Migration {
	t.Helper()
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func TestNoBackupForANewOrCurrentDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devboard.db")
	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := os.Stat(BackupDir(path)); err == nil {
		t.Fatal("a brand new database was backed up: there was nothing to lose")
	}
	db, err = Open(ctx, path, nil) // already current
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := os.Stat(BackupDir(path)); err == nil {
		t.Fatal("a current database was backed up on every start")
	}
}

func TestOldBackupsArePruned(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 8; i++ {
		name := filepath.Join(dir, "devboard-v"+string(rune('0'+i))+"-2026010"+string(rune('0'+i))+"T000000Z.db")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o600) // not ours: never touched
	pruneBackups(dir)
	entries, _ := os.ReadDir(dir)
	var dbs []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".db") {
			dbs = append(dbs, e.Name())
		}
	}
	if len(dbs) != keepBackups || dbs[0] != "devboard-v4-20260104T000000Z.db" {
		t.Fatalf("kept %v: the newest %d should stay", dbs, keepBackups)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("a file that is not a backup was removed")
	}
}

func TestInspectDoesNotTouchTheDatabase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	info, err := Inspect(ctx, missing)
	if err != nil || info.Exists {
		t.Fatalf("missing = %+v, %v", info, err)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("Inspect created the database")
	}
	path := oldDatabase(t, 3)
	before, _ := os.ReadFile(path)
	info, err = Inspect(ctx, path)
	if err != nil || !info.Exists || info.Version != 3 || info.Latest != len(mustMigrations(t)) || info.NewerThanBuild || info.Integrity != "ok" || info.Size == 0 {
		t.Fatalf("old = %+v, %v", info, err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("Inspect changed the database file")
	}

	// A database from a newer build is recognised, and Open refuses it.
	newer := filepath.Join(t.TempDir(), "newer.db")
	raw, _ := sql.Open("sqlite", newer)
	ms := mustMigrations(t)
	if _, err := migrate(ctx, raw, ms); err != nil {
		t.Fatal(err)
	}
	_, _ = raw.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, 'future', 1)`, len(ms)+1)
	_ = raw.Close()
	if info, err := Inspect(ctx, newer); err != nil || !info.NewerThanBuild {
		t.Fatalf("newer = %+v, %v", info, err)
	}
	if _, err := Open(ctx, newer, nil); err == nil || !strings.Contains(err.Error(), "newer than this build") {
		t.Fatalf("Open of a newer database: %v", err)
	}
}

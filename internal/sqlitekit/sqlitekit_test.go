package sqlitekit

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

var testFS = fstest.MapFS{
	"m/0001_things.sql": {Data: []byte(`CREATE TABLE things (id INTEGER PRIMARY KEY, name TEXT NOT NULL) STRICT;`)},
	"m/0002_more.sql":   {Data: []byte(`ALTER TABLE things ADD COLUMN note TEXT;`)},
}

func testMigrations(t *testing.T) []Migration {
	t.Helper()
	ms, err := LoadMigrations(testFS, "m")
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func TestLoadMigrationsRejectsGapsAndBadNames(t *testing.T) {
	gap := fstest.MapFS{"m/0001_a.sql": {Data: []byte("")}, "m/0003_c.sql": {Data: []byte("")}}
	if _, err := LoadMigrations(gap, "m"); err == nil || !strings.Contains(err.Error(), "contiguous") {
		t.Fatalf("a gap in versions should be refused, got %v", err)
	}
	bad := fstest.MapFS{"m/notaversion.sql": {Data: []byte("")}}
	if _, err := LoadMigrations(bad, "m"); err == nil {
		t.Fatal("a file that is not NNNN_name.sql should be refused")
	}
}

func TestOpenMigratesAndReopensWithoutReapplying(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.db")
	opt := Options{Migrations: testMigrations(t), Product: "thing", BackupPrefix: "thing"}
	p, err := Open(ctx, path, opt)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := p.SchemaVersion(ctx); v != 2 {
		t.Fatalf("schema version = %d, want 2", v)
	}
	if err := p.Update(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO things (name) VALUES ('a')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A failed Update rolls back.
	_ = p.Update(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `INSERT INTO things (name) VALUES ('b')`)
		return os.ErrInvalid
	})
	var n int
	if err := p.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM things`).Scan(&n)
	}); err != nil || n != 1 {
		t.Fatalf("rows = %d, err = %v; want 1", n, err)
	}
	// The read pool cannot write.
	if _, err := p.Reader.ExecContext(ctx, `INSERT INTO things (name) VALUES ('c')`); err == nil {
		t.Fatal("the reader connection accepted a write")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p, err = Open(ctx, path, opt)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if v, _ := p.SchemaVersion(ctx); v != 2 {
		t.Fatalf("reopened schema version = %d", v)
	}
}

func TestOpenRefusesADatabaseFromANewerBuildAndNamesTheProduct(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.db")
	ms := testMigrations(t)
	p, err := Open(ctx, path, Options{Migrations: ms, Product: "thing", BackupPrefix: "thing"})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	_, err = Open(ctx, path, Options{Migrations: ms[:1], Product: "thing", BackupPrefix: "thing"})
	if err == nil || !strings.Contains(err.Error(), "upgrade thing") {
		t.Fatalf("an older build opened a newer database, or did not say what to upgrade: %v", err)
	}
}

func TestUpgradeBacksUpAndPrunesByPrefix(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.db")
	ms := testMigrations(t)
	p, err := Open(ctx, path, Options{Migrations: ms[:1], Product: "thing", BackupPrefix: "thing"})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	p, err = Open(ctx, path, Options{Migrations: ms, Product: "thing", BackupPrefix: "thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	files, _ := filepath.Glob(filepath.Join(BackupDir(path), "thing-v1-*.db"))
	if len(files) != 1 {
		t.Fatalf("expected one pre-upgrade backup, found %v", files)
	}
	// Another product's backups in the same directory are not touched by pruning.
	dir := BackupDir(path)
	other := filepath.Join(dir, "other-v1-20200101T000000Z.db")
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < KeepBackups+2; i++ {
		name := filepath.Join(dir, "thing-v1-2021010"+string(rune('1'+i))+"T000000Z.db")
		_ = os.WriteFile(name, nil, 0o600)
	}
	PruneBackups(dir, "thing")
	left, _ := filepath.Glob(filepath.Join(dir, "thing-v*.db"))
	if len(left) != KeepBackups {
		t.Fatalf("kept %d backups, want %d", len(left), KeepBackups)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("pruning removed another product's backup")
	}
}

func TestInspectDoesNotCreateOrMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.db")
	info, err := Inspect(ctx, path, 2)
	if err != nil || info.Exists {
		t.Fatalf("missing file: %+v, %v", info, err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("Inspect created the database")
	}
	p, err := Open(ctx, path, Options{Migrations: testMigrations(t)[:1], Product: "thing", BackupPrefix: "thing"})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	info, err = Inspect(ctx, path, 2)
	if err != nil || !info.Exists || info.Version != 1 || info.NewerThanBuild || info.Integrity != "ok" {
		t.Fatalf("%+v, %v", info, err)
	}
	info, _ = Inspect(ctx, path, 0)
	if !info.NewerThanBuild {
		t.Fatal("a database newer than the build should say so")
	}
}

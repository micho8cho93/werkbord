package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/sqlitekit"
)

func TestMigrationsAreContiguousAndOpenIsRepeatable(t *testing.T) {
	ms, err := Migrations()
	if err != nil || len(ms) == 0 {
		t.Fatalf("%v %v", ms, err)
	}
	path := filepath.Join(t.TempDir(), "team.db")
	for i := 0; i < 2; i++ {
		db, err := Open(context.Background(), path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := db.SchemaVersion(context.Background()); v != len(ms) {
			t.Fatalf("schema version %d, want %d", v, len(ms))
		}
		if err := db.Ping(context.Background()); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
	}
}

// A database written by a newer Werkbord Team is refused, by name.
func TestADatabaseFromANewerTeamIsRefused(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "team.db")
	ms, _ := Migrations()
	future := append(append([]sqlitekit.Migration(nil), ms...), sqlitekit.Migration{Version: len(ms) + 1, Name: "future", SQL: `CREATE TABLE future (x INTEGER) STRICT;`})
	p, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: future, Product: productName, BackupPrefix: backupPrefix})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	if _, err := Open(ctx, path, nil); err == nil || !strings.Contains(err.Error(), "upgrade werkbord-team") {
		t.Fatalf("an older Team opened a newer database, or did not say what to upgrade: %v", err)
	}
}

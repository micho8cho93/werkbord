package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/sqlitekit"
	"devboard/internal/team/domain"
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

// A database made by Team 0.1.0 (schema 1) upgrades in place and keeps its data;
// existing project memberships become plain members, and projects start at revision 0.
func TestUpgradingAVersion1Database(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "team.db")
	ms, _ := Migrations()
	p, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: ms[:1], Product: productName, BackupPrefix: backupPrefix})
	if err != nil {
		t.Fatal(err)
	}
	err = p.Update(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO workspaces VALUES ('w1', 'Acme', 1)`,
			`INSERT INTO members VALUES ('m1', 'w1', 'Ada', '', 'owner', 'h1', 1)`,
			`INSERT INTO projects VALUES ('p1', 'w1', 'Shop', '', '', 0, 1, 1)`,
			`INSERT INTO project_members VALUES ('p1', 'm1', 'w1', 'm1', 1)`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()

	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v, _ := db.SchemaVersion(ctx); v != len(ms) {
		t.Fatalf("schema %d", v)
	}
	err = db.View(ctx, func(tx *Tx) error {
		pr, err := tx.Project(ctx, "w1", "p1")
		if err != nil || pr.Name != "Shop" || pr.Revision != 0 {
			t.Fatalf("%+v %v", pr, err)
		}
		role, on, err := tx.ProjectRole(ctx, "w1", "p1", "m1")
		if err != nil || !on || role != domain.ProjectContributor {
			t.Fatalf("%q %v %v", role, on, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The database itself refuses a ticket that is in an impossible state, whatever the service does.
func TestTheSchemaRefusesImpossibleTickets(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, a ...any) error {
		return db.pool.Update(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, q, a...); return err })
	}
	for _, q := range []string{
		`INSERT INTO workspaces VALUES ('w1', 'Acme', 1)`,
		`INSERT INTO workspaces VALUES ('w2', 'Rival', 1)`,
		`INSERT INTO projects (id, workspace_id, name, created_at, updated_at) VALUES ('p1', 'w1', 'Shop', 1, 1)`,
	} {
		if err := exec(q); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(id string, n int, ws, project, status string, assignee any) error {
		return exec(`INSERT INTO tickets (id, workspace_id, project_id, number, title, status, assignee_id, creator_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, 't', ?, ?, 'm1', 1, 1)`, id, ws, project, n, status, assignee)
	}
	if err := insert("t1", 1, "w1", "p1", "available", nil); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"in progress with nobody":        insert("t2", 2, "w1", "p1", "in_progress", nil),
		"available with somebody":        insert("t3", 3, "w1", "p1", "available", "m1"),
		"a made-up status":               insert("t4", 4, "w1", "p1", "limbo", nil),
		"a duplicate number":             insert("t5", 1, "w1", "p1", "backlog", nil),
		"a project of another workspace": insert("t6", 6, "w2", "p1", "backlog", nil),
	} {
		if err == nil {
			t.Errorf("accepted: %s", name)
		}
	}
	if err := insert("t7", 2, "w1", "p1", "in_progress", "m1"); err != nil {
		t.Fatal(err)
	}
	// A single-use invite cannot be used twice, even by a writer that skips the service.
	if err := exec(`INSERT INTO project_invites (id, workspace_id, project_id, code_hash, role, created_by, created_at, expires_at, max_uses) VALUES ('i1', 'w1', 'p1', 'h', 'member', 'm1', 1, 99, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := exec(`UPDATE project_invites SET uses = 2 WHERE id = 'i1'`); err == nil {
		t.Error("an invite was used more often than allowed")
	}
}

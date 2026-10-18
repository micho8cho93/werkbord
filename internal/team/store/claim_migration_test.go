package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"devboard/internal/sqlitekit"
)

// The ownership CHECK rebuild runs with foreign keys ON on both SQLite and
// rqlite. Its parent drop must carry every child row across the cascade.
func TestClaimMigrationPreservesTicketsAndEveryDependentTable(t *testing.T) {
	ctx := context.Background()
	migrations, _ := Migrations()
	target := migrationNamed(t, migrations, "claim_before_work")
	path := filepath.Join(t.TempDir(), "team.db")
	old, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: migrations[:target], Product: productName})
	if err != nil {
		t.Fatal(err)
	}
	seed := []string{
		`INSERT INTO workspaces(id,name,created_at) VALUES('w','Team',1)`,
		`INSERT INTO members(id,workspace_id,name,role,token_hash,created_at) VALUES('m','w','Ada','owner','hash',1)`,
		`INSERT INTO projects(id,workspace_id,name,created_at,updated_at) VALUES('p','w','Repo',1,1)`,
		`INSERT INTO devices(id,workspace_id,member_id,name,public_key,created_at,updated_at) VALUES('d','w','m','Laptop','public',1,1)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,assignee_id,creator_id,branch,claimed_at,created_at,updated_at,source_ref) VALUES('t','w','p',1,'Working','in_progress','m','m','wb-1-working',2,1,2,'source')`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,creator_id,created_at,updated_at) VALUES('other','w','p',2,'Next','available','m',1,1)`,
		`INSERT INTO ticket_commits VALUES('t','abcdef0','Work','Ada',2)`,
		`INSERT INTO ticket_progress VALUES('w','p','t','d','m',7,2,'digest','{"state":"running"}',2)`,
		`INSERT INTO ticket_schedules VALUES('w','p','t',3,'{"executionId":"exe_test"}')`,
		`INSERT INTO labels(id,workspace_id,name,name_key,color,created_at,updated_at) VALUES('label','w','Bug','bug','#2447ff',1,1)`,
		`INSERT INTO ticket_labels VALUES('t','label','w')`,
		`INSERT INTO ticket_dependencies VALUES('t','other','w')`,
	}
	for _, query := range seed {
		if _, err := old.Writer.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"tickets", "ticket_commits", "ticket_progress", "ticket_schedules", "ticket_labels", "ticket_dependencies"}
	snapshot := func(db *sql.DB, table string) [][]any {
		t.Helper()
		rows, err := db.QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		columns, _ := rows.Columns()
		result := [][]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(values))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			result = append(result, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := map[string][][]any{}
	for _, table := range tables {
		before[table] = snapshot(old.Writer, table)
	}
	if _, err := sqlitekit.Migrate(ctx, old.Writer, migrations, productName); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if after := snapshot(old.Writer, table); !reflect.DeepEqual(before[table], after) {
			t.Errorf("%s changed: before %v, after %v", table, before[table], after)
		}
	}
	if rows := snapshot(old.Writer, "pragma_foreign_key_check"); len(rows) != 0 {
		t.Fatalf("broken foreign keys: %v", rows)
	}
	if _, err := old.Writer.ExecContext(ctx, `UPDATE tickets SET assignee_id='m',claimed_at=3 WHERE id='other'`); err != nil {
		t.Fatal("claimed Available is not supported:", err)
	}
	var claim int64
	_ = old.Writer.QueryRowContext(ctx, `SELECT assignment FROM tickets WHERE id='other'`).Scan(&claim)
	if _, err := old.Writer.ExecContext(ctx, `UPDATE tickets SET status='in_progress' WHERE id='other'`); err != nil {
		t.Fatal(err)
	}
	var started int64
	_ = old.Writer.QueryRowContext(ctx, `SELECT assignment FROM tickets WHERE id='other'`).Scan(&started)
	if claim == 0 || claim != started {
		t.Fatalf("starting changed claim interval: %d -> %d", claim, started)
	}
	_ = old.Close()
	reopened, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = reopened.Close()
}

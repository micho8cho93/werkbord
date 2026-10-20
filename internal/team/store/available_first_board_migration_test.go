package store

import (
	"context"
	"path/filepath"
	"testing"

	"devboard/internal/sqlitekit"
)

// The board used to start at Backlog, and a claimed ticket waited in Available. Now it starts at Available and a claimed ticket
// is In Progress. The migration moves the rows and nothing else: ownership, history and the assignment counter that fences an
// approved execution stay as they were.
func TestAvailableFirstBoardMigrationMovesRowsAndKeepsClaims(t *testing.T) {
	ctx := context.Background()
	migrations, _ := Migrations()
	target := migrationNamed(t, migrations, "available_first_board")
	path := filepath.Join(t.TempDir(), "team.db")
	old, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: migrations[:target], Product: productName})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,created_at) VALUES('w','Team',1)`,
		`INSERT INTO members(id,workspace_id,name,role,token_hash,created_at) VALUES('m','w','Ada','owner','hash',1)`,
		`INSERT INTO projects(id,workspace_id,name,created_at,updated_at) VALUES('p','w','Repo',1,1)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,creator_id,created_at,updated_at) VALUES('idea','w','p',1,'An idea','backlog','m',1,1)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,creator_id,created_at,updated_at) VALUES('open','w','p',2,'Open','available','m',1,1)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,assignee_id,creator_id,claimed_at,created_at,updated_at) VALUES('claimed','w','p',3,'Claimed','available','m','m',2,1,2)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,assignee_id,creator_id,claimed_at,created_at,updated_at) VALUES('busy','w','p',4,'Busy','in_progress','m','m',2,1,2)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,assignee_id,creator_id,claimed_at,created_at,updated_at) VALUES('waiting','w','p',5,'Waiting','review','m','m',2,1,2)`,
	} {
		if _, err := old.Writer.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	type row struct {
		status     string
		assignee   *string
		version    int64
		assignment int64
	}
	read := func() map[string]row {
		t.Helper()
		rows, err := old.Writer.QueryContext(ctx, `SELECT id, status, assignee_id, version, assignment FROM tickets`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]row{}
		for rows.Next() {
			var id string
			var r row
			if err := rows.Scan(&id, &r.status, &r.assignee, &r.version, &r.assignment); err != nil {
				t.Fatal(err)
			}
			out[id] = r
		}
		return out
	}
	before := read()
	if _, err := sqlitekit.Migrate(ctx, old.Writer, migrations, productName); err != nil {
		t.Fatal(err)
	}
	after := read()
	want := map[string]string{"idea": "available", "open": "available", "claimed": "in_progress", "busy": "in_progress", "waiting": "review"}
	for id, status := range want {
		if after[id].status != status {
			t.Errorf("%s is %s, want %s", id, after[id].status, status)
		}
		if (after[id].assignee == nil) != (before[id].assignee == nil) {
			t.Errorf("%s changed hands", id)
		}
	}
	// A claim keeps its assignment counter: an execution approved for it stays approved.
	if after["claimed"].assignment != before["claimed"].assignment {
		t.Errorf("starting a claim changed its interval: %d -> %d", before["claimed"].assignment, after["claimed"].assignment)
	}
	// What changed has a new version, so an editor holding the old one reloads instead of writing the old status back.
	for _, id := range []string{"idea", "claimed"} {
		if after[id].version != before[id].version+1 {
			t.Errorf("%s version %d -> %d", id, before[id].version, after[id].version)
		}
	}
	for _, id := range []string{"open", "busy", "waiting"} {
		if after[id].version != before[id].version {
			t.Errorf("%s was not changed but its version moved: %d -> %d", id, before[id].version, after[id].version)
		}
	}
}

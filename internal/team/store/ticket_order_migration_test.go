package store

import (
	"context"
	"path/filepath"
	"testing"

	"werkbord/internal/sqlitekit"
)

// Tickets that exist before the shared queue order keep the order they always had: creation order.
func TestTicketOrderMigrationKeepsTheCreationOrder(t *testing.T) {
	ctx := context.Background()
	migrations, _ := Migrations()
	target := migrationNamed(t, migrations, "ticket_order")
	db, err := sqlitekit.Open(ctx, filepath.Join(t.TempDir(), "team.db"), sqlitekit.Options{Migrations: migrations[:target], Product: productName})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,created_at) VALUES('w','Team',1)`,
		`INSERT INTO members(id,workspace_id,name,role,token_hash,created_at) VALUES('m','w','Ada','owner','hash',1)`,
		`INSERT INTO projects(id,workspace_id,name,created_at,updated_at) VALUES('p','w','Repo',1,1)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,creator_id,created_at,updated_at) VALUES('c','w','p',3,'Third','available','m',1,1)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,creator_id,created_at,updated_at) VALUES('a','w','p',1,'First','available','m',1,1)`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,creator_id,created_at,updated_at) VALUES('b','w','p',2,'Second','available','m',1,1)`,
	} {
		if _, err := db.Writer.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlitekit.Migrate(ctx, db.Writer, migrations, productName); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Writer.QueryContext(ctx, `SELECT id, position FROM tickets WHERE project_id = 'p' ORDER BY position, number`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var order []string
	for rows.Next() {
		var id string
		var position float64
		if err := rows.Scan(&id, &position); err != nil {
			t.Fatal(err)
		}
		order = append(order, id)
	}
	if got := len(order); got != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("order after migrating = %v", order)
	}
}

package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/planning"
	"devboard/internal/sqlitekit"
	"devboard/internal/team/domain"
)

// A workspace from before labels, modes and dependencies is upgraded without any of its tickets changing.
func TestPlanningUpgradeKeepsExistingTicketsAsTheyWere(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "team.db")
	ms, _ := Migrations()
	before := migrationNamed(t, ms, "planning") // everything before the planning migration
	p, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: ms[:before], Product: productName, BackupPrefix: backupPrefix})
	if err != nil {
		t.Fatal(err)
	}
	err = p.Update(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO workspaces (id, name, created_at) VALUES ('w1', 'Acme', 1)`,
			`INSERT INTO projects (id, workspace_id, name, created_at, updated_at) VALUES ('p1', 'w1', 'Shop', 1, 1)`,
			`INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at) VALUES ('t1', 'w1', 'p1', 1, 'old', 'available', 'm1', 1, 1)`,
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
	if err := db.View(ctx, func(tx Tx) error {
		k, err := tx.Ticket(ctx, "w1", "p1", "t1")
		if err != nil {
			return err
		}
		if k.WorkMode != planning.ModeAgent || k.Mode() != planning.ModeAgent || len(k.LabelIDs) != 0 || len(k.Dependencies) != 0 || !k.Plan.IsZero() || k.Status != domain.TicketAvailable {
			t.Errorf("an old ticket changed: %+v", k)
		}
		if k.LabelIDs == nil || k.Dependencies == nil {
			t.Error("empty lists, not null")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func seedTickets(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, created_at) VALUES ('w1', 'Acme', 1)`,
		`INSERT INTO workspaces (id, name, created_at) VALUES ('w2', 'Rival', 1)`,
		`INSERT INTO projects (id, workspace_id, name, created_at, updated_at) VALUES ('p1', 'w1', 'Shop', 1, 1)`,
		`INSERT INTO projects (id, workspace_id, name, created_at, updated_at) VALUES ('p2', 'w2', 'Theirs', 1, 1)`,
		`INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at) VALUES ('t1', 'w1', 'p1', 1, 'one', 'backlog', 'm1', 1, 1)`,
		`INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at) VALUES ('t2', 'w1', 'p1', 2, 'two', 'backlog', 'm1', 1, 1)`,
		`INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at) VALUES ('x1', 'w2', 'p2', 3, 'rival', 'backlog', 'm9', 1, 1)`,
		`INSERT INTO labels (id, workspace_id, name, name_key, color, created_at, updated_at) VALUES ('l1', 'w1', 'Design', 'design', '#112233', 1, 1)`,
		`INSERT INTO labels (id, workspace_id, name, name_key, color, created_at, updated_at) VALUES ('lx', 'w2', 'Theirs', 'theirs', '#112233', 1, 1)`,
	} {
		if err := db.pool.Update(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, q); return err }); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// The database refuses to link across workspaces, to depend on oneself, or to hold a colour or mode it cannot read,
// whatever writes the row.
func TestTheSchemaKeepsPlanningInsideOneWorkspace(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedTickets(t, db)
	exec := func(q string, a ...any) error {
		return db.pool.Update(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, q, a...); return err })
	}
	for name, q := range map[string]string{
		"another workspace's label on a ticket":      `INSERT INTO ticket_labels (ticket_id, label_id, workspace_id) VALUES ('t1', 'lx', 'w1')`,
		"a label on another workspace's ticket":      `INSERT INTO ticket_labels (ticket_id, label_id, workspace_id) VALUES ('x1', 'l1', 'w1')`,
		"a dependency on another workspace":          `INSERT INTO ticket_dependencies (ticket_id, depends_on_id, workspace_id) VALUES ('t1', 'x1', 'w1')`,
		"a ticket that depends on itself":            `INSERT INTO ticket_dependencies (ticket_id, depends_on_id, workspace_id) VALUES ('t1', 't1', 'w1')`,
		"a colour that is not #rrggbb":               `INSERT INTO labels (id, workspace_id, name, name_key, color, created_at, updated_at) VALUES ('l2', 'w1', 'x', 'x', 'red', 1, 1)`,
		"the same name ignoring case in a workspace": `INSERT INTO labels (id, workspace_id, name, name_key, color, created_at, updated_at) VALUES ('l3', 'w1', 'DESIGN', 'design', '#000000', 1, 1)`,
		"an unknown work mode":                       `UPDATE tickets SET work_mode = 'robot' WHERE id = 't1'`,
	} {
		if err := exec(q); err == nil {
			t.Errorf("accepted: %s", name)
		}
	}
	// The same name in another workspace is fine.
	if err := exec(`INSERT INTO labels (id, workspace_id, name, name_key, color, created_at, updated_at) VALUES ('l4', 'w2', 'Design', 'design', '#000000', 1, 1)`); err != nil {
		t.Errorf("another workspace may use the name: %v", err)
	}
}

func TestAnotherWorkspacesLabelCannotBeLinked(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedTickets(t, db)
	// The write fails, and so the transaction that made it does: nothing is half done.
	err = db.Update(ctx, func(tx Tx) error {
		if err := tx.SetTicketLabels(ctx, "w1", "t1", []string{"l1"}); err != nil {
			return err
		}
		return tx.SetTicketLabels(ctx, "w1", "t1", []string{"l1", "lx"})
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
	_ = db.View(ctx, func(tx Tx) error {
		k, _ := tx.Ticket(ctx, "w1", "p1", "t1")
		if len(k.LabelIDs) != 0 {
			t.Errorf("a failed write left %v behind", k.LabelIDs)
		}
		return nil
	})
}

func TestLabelsAndLinksRoundTripAndCascade(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedTickets(t, db)

	err = db.Update(ctx, func(tx Tx) error {
		if err := tx.InsertLabel(ctx, "w1", domain.Label{ID: "l9", Name: "Ops", Color: "#abcdef"}); err != nil {
			return err
		}
		if err := tx.InsertLabel(ctx, "w1", domain.Label{ID: "l10", Name: "ops", Color: "#abcdef"}); err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Errorf("duplicate name: %v", err)
		}
		if err := tx.SetTicketLabels(ctx, "w1", "t1", []string{"l9", "l1"}); err != nil {
			return err
		}
		if err := tx.SetTicketDependencies(ctx, "w1", "t2", []string{"t1"}); err != nil {
			return err
		}
		// Setting the same set again leaves the rows alone.
		if err := tx.SetTicketLabels(ctx, "w1", "t1", []string{"l9", "l1"}); err != nil {
			return err
		}
		ts, err := tx.Tickets(ctx, "w1", "p1")
		if err != nil {
			return err
		}
		if len(ts) != 2 || strings.Join(ts[0].LabelIDs, ",") != "l9,l1" || strings.Join(ts[1].Dependencies, ",") != "t1" {
			t.Errorf("tickets = %+v", ts)
		}
		use, _ := tx.LabelUsage(ctx, "w1", "")
		if use["l9"] != 1 || use["l1"] != 1 {
			t.Errorf("usage = %v", use)
		}
		projects, err := tx.DeleteLabel(ctx, "w1", "l9", time.UnixMilli(5).UTC())
		if err != nil || len(projects) != 1 || projects[0] != "p1" {
			t.Errorf("delete: %v, %v", projects, err)
		}
		k, _ := tx.Ticket(ctx, "w1", "p1", "t1")
		if strings.Join(k.LabelIDs, ",") != "l1" || k.Version != 2 {
			t.Errorf("after delete: %+v", k)
		}
		if _, err := tx.DeleteLabel(ctx, "w1", "l9", time.UnixMilli(5).UTC()); err == nil {
			t.Error("deleting twice")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

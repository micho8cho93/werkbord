package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devboard/internal/planning"
	"devboard/internal/sqlitekit"
)

// migrationNamed is the position of the named migration in Team's list.
func migrationNamed(t *testing.T, ms []sqlitekit.Migration, name string) int {
	t.Helper()
	for i, m := range ms {
		if strings.Contains(m.Name, name) {
			return i
		}
	}
	t.Fatalf("no migration named %q", name)
	return -1
}

func schedule(workspace, project, ticket, document string) string {
	return `INSERT INTO ticket_schedules (workspace_id, project_id, ticket_id, version, document) VALUES ('` + workspace + `', '` + project + `', '` + ticket + `', 1, '` + document + `')`
}

// A workspace from before tickets had one list of dependencies is upgraded: what its shared requests waited for is
// now what the tickets wait for, and nothing else is added. A row that cannot be carried over is skipped.
func TestOneDependencyListUpgradeCopiesOnlyWhatRequestsHad(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "team.db")
	ms, _ := Migrations()
	last := migrationNamed(t, ms, "one_dependency_list")
	if last != len(ms)-1 {
		t.Fatalf("this test expects the migration to be the newest, it is %d of %d", last+1, len(ms))
	}
	p, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: ms[:last], Product: productName, BackupPrefix: backupPrefix})
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`INSERT INTO workspaces (id, name, created_at) VALUES ('w1', 'Acme', 1), ('w2', 'Rival', 1)`,
		`INSERT INTO projects (id, workspace_id, name, created_at, updated_at) VALUES ('p1', 'w1', 'Shop', 1, 1), ('p2', 'w1', 'Other', 1, 1), ('px', 'w2', 'Theirs', 1, 1)`,
	}
	for i, id := range []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8"} {
		project := "p1"
		if id == "t8" {
			project = "p2"
		}
		stmts = append(stmts, `INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at) VALUES ('`+id+`', 'w1', '`+project+`', `+string(rune('1'+i))+`, 'ticket', 'available', 'm1', 1, 1)`)
	}
	stmts = append(stmts,
		`INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at) VALUES ('x1', 'w2', 'px', 9, 'rival', 'available', 'm9', 1, 1)`,
		// t5 already waits for t2 (a planning dependency) and its request lists t1 and t2.
		`INSERT INTO ticket_dependencies (ticket_id, depends_on_id, workspace_id) VALUES ('t5', 't2', 'w1')`,
		// t1 waits for two tickets, in this order.
		schedule("w1", "p1", "t1", `{"ticketId":"t1","dependencies":["t3","t2"]}`),
		// t2: itself (refused by the database), a ticket that is gone, another project, another workspace, and a good one.
		schedule("w1", "p1", "t2", `{"dependencies":["t2","gone","t8","x1","t4"]}`),
		// t3: the same ticket twice.
		schedule("w1", "p1", "t3", `{"dependencies":["t4","t4"]}`),
		// t4: not a list of ids.
		schedule("w1", "p1", "t4", `{"dependencies":"t1"}`),
		// t5: partly there already.
		schedule("w1", "p1", "t5", `{"dependencies":["t1","t2"]}`),
		// t6: null, and ids that are not text.
		schedule("w1", "p1", "t6", `{"dependencies":null}`),
		schedule("w1", "p1", "t7", `{"dependencies":[1,true,["t1"],{"a":"t1"}]}`),
		// A document that is not JSON at all, and a request filed under another project than its ticket's.
		schedule("w1", "p1", "t8", `not json`),
		schedule("w2", "px", "x1", `{"dependencies":["t1"]}`),
	)
	err = p.Update(ctx, func(tx *sql.Tx) error {
		for _, q := range stmts {
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
		t.Fatalf("a bad row failed the upgrade: %v", err)
	}
	defer db.Close()
	want := map[string][]string{
		"t1": {"t3", "t2"},
		"t2": {"t4"},
		"t3": {"t4"},
		"t4": {},
		"t5": {"t2", "t1"},
		"t6": {},
		"t7": {},
		"t8": {},
	}
	read := func() map[string][]string {
		got := map[string][]string{}
		if err := db.View(ctx, func(tx Tx) error {
			for id := range want {
				if id == "x1" {
					continue
				}
				pid := "p1"
				if id == "t8" {
					pid = "p2"
				}
				k, err := tx.Ticket(ctx, "w1", pid, id)
				if err != nil {
					return err
				}
				got[id] = k.Dependencies
			}
			x, err := tx.Ticket(ctx, "w2", "px", "x1")
			if err != nil {
				return err
			}
			got["x1"] = x.Dependencies
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	want["x1"] = []string{}
	got := read()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies after the upgrade\n got %v\nwant %v", got, want)
	}

	// Running the migration again adds nothing, and does not fail.
	for i := 0; i < 2; i++ {
		if err := db.pool.Update(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, ms[last].SQL); return err }); err != nil {
			t.Fatalf("running the migration again: %v", err)
		}
	}
	if again := read(); !reflect.DeepEqual(again, want) {
		t.Fatalf("running it again changed the dependencies\n got %v\nwant %v", again, want)
	}
	// The requests themselves are untouched.
	if err := db.View(ctx, func(tx Tx) error {
		v, err := tx.Schedule(ctx, "w1", "p1", "t1")
		if err != nil || !reflect.DeepEqual(v.Dependencies, []string{"t3", "t2"}) {
			t.Errorf("the request changed: %+v, %v", v, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The copy cannot see circles. When it makes one, the analysis reports it (and does not loop), and the tickets keep
// the dependencies they were given.
func TestOneDependencyListUpgradeMayMakeACircleWhichIsReported(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "team.db")
	ms, _ := Migrations()
	last := len(ms) - 1
	p, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: ms[:last], Product: productName, BackupPrefix: backupPrefix})
	if err != nil {
		t.Fatal(err)
	}
	err = p.Update(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO workspaces (id, name, created_at) VALUES ('w1', 'Acme', 1)`,
			`INSERT INTO projects (id, workspace_id, name, created_at, updated_at) VALUES ('p1', 'w1', 'Shop', 1, 1)`,
			`INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at) VALUES ('a', 'w1', 'p1', 1, 'A', 'available', 'm1', 1, 1)`,
			`INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at) VALUES ('b', 'w1', 'p1', 2, 'B', 'available', 'm1', 1, 1)`,
			// b already waits for a on the ticket; a's request waits for b.
			`INSERT INTO ticket_dependencies (ticket_id, depends_on_id, workspace_id) VALUES ('b', 'a', 'w1')`,
			schedule("w1", "p1", "a", `{"dependencies":["b"]}`),
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
	var items []planning.Item
	if err := db.View(ctx, func(tx Tx) error {
		ts, err := tx.Tickets(ctx, "w1", "p1")
		for _, k := range ts {
			items = append(items, planning.Item{ID: k.ID, Title: k.Title, Dependencies: k.Dependencies})
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range planning.Analyze(items) {
		found = found || w.Code == planning.CodeDependencyCycle
	}
	if !found {
		t.Errorf("the circle was not reported: %+v", planning.Analyze(items))
	}
}

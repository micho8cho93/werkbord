package replicated

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/sqlitekit"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/store"
)

// A future version of Team brings a migration. Hosts that start together apply it once; a host that is still an older Team
// refuses to read or write what it does not understand, rather than writing with the wrong idea of the schema.

func futureMigration(t *testing.T) []sqlitekit.Migration {
	t.Helper()
	ms, err := store.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]sqlitekit.Migration(nil), ms...), sqlitekit.Migration{Version: len(ms) + 1, Name: "member_pronouns", SQL: `
-- A later release adds a column and an index, and fills the column in for the members that exist.
ALTER TABLE members ADD COLUMN pronouns TEXT NOT NULL DEFAULT '';
CREATE INDEX members_by_pronouns ON members (workspace_id, pronouns);
UPDATE members SET pronouns = 'they/them' WHERE role = 'owner';
`})
}

func TestASchemaUpgradeRunsOnceAcrossHostsAndAnOlderHostRefusesWhatItDoesNotKnow(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	old := hostsOf(t, c)
	k := seed(t, old[0])
	settle(t, old...)
	before := position(t, old[0]).Seq
	future := futureMigration(t)

	// Two hosts of the new version start together.
	var wg sync.WaitGroup
	news := make([]*Store, 2)
	for i := range news {
		wg.Add(1)
		go func() {
			defer wg.Done()
			news[i] = open(t, c, i, func(o *Options) { o.Migrations = future })
		}()
	}
	wg.Wait()
	// One write, whichever host made it: the migration ran once, for the whole cluster.
	for i, s := range news {
		settle(t, s)
		if got := position(t, s).Seq; got != before+1 {
			t.Errorf("new host %d is at position %d, want %d: the migration ran more than once", i, got, before+1)
		}
		if v, _ := s.SchemaVersion(bg); v != len(future) {
			t.Errorf("new host %d: schema %d", i, v)
		}
	}
	// The data was carried across, and a statement that only the migrated schema can answer gets its answer, on the other new host too.
	var pronouns string
	eventually(t, 10*time.Second, "the column to be filled in", func() bool {
		_ = news[1].update(bg, func(q store.Queryer) error {
			return q.QueryRowContext(bg, `SELECT pronouns FROM members WHERE role = 'owner'`).Scan(&pronouns)
		})
		return pronouns == "they/them"
	})

	// A host of the old version, whose own copy has taken in the migration's statements, refuses to read or write.
	eventually(t, 15*time.Second, "the old host to notice the schema is newer", func() bool {
		err := old[2].View(bg, func(tx store.Tx) error { _, err := tx.Members(bg, k.ws); return err })
		var newer ErrSchemaNewer
		return errors.As(err, &newer)
	})
	err := old[2].Update(bg, func(tx store.Tx) error { _, err := tx.BumpRevision(bg, k.ws, k.project); return err })
	var newer ErrSchemaNewer
	if !errors.As(err, &newer) || newer.Have != len(future) || newer.Max != len(future)-1 || !strings.Contains(err.Error(), "upgrade werkbord-team") {
		t.Fatalf("an older host wrote with the wrong idea of the schema, or did not say what to upgrade: %v", err)
	}
	// The new hosts carry on, and the cluster is writable.
	if _, err := note(news[0], k); err != nil {
		t.Fatal(err)
	}
}

// The migration that gives a ticket one list of dependencies runs on the replicated store with a workspace's data in it: what a
// shared request waited for becomes what its ticket waits for, a row that cannot be carried over is skipped, and it is
// applied once for the whole cluster.
func TestTheOneDependencyListMigrationCarriesDataAcrossOnTheCluster(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	all, err := store.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	last := len(all) - 1
	if !strings.Contains(all[last].Name, "one_dependency_list") {
		t.Fatalf("this test expects the migration to be the newest, it is %q", all[last].Name)
	}
	before := all[:last]
	old := open(t, c, 0, func(o *Options) { o.Migrations = before })
	k := seed(t, old)
	second, gone := domain.NewID(domain.PrefixTicket), domain.NewID(domain.PrefixTicket)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := old.Update(bg, func(tx store.Tx) error {
		n, err := tx.NextTicketNumber(bg, k.ws)
		if err != nil {
			return err
		}
		return tx.InsertTicket(bg, k.ws, domain.Ticket{ID: second, ProjectID: k.project, Number: n, Title: "Second", Status: domain.TicketAvailable, CreatorID: k.owner, CreatedAt: now, UpdatedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	// The first ticket's request waited for the second, for a ticket that is gone, and for itself. A request that is not JSON is skipped.
	if err := old.update(bg, func(q store.Queryer) error {
		for _, s := range []struct{ ticket, doc string }{
			{k.ticket, `{"dependencies":["` + second + `","` + gone + `","` + k.ticket + `"]}`},
			{second, `not json`},
		} {
			if _, err := q.ExecContext(bg, `INSERT INTO ticket_schedules (workspace_id, project_id, ticket_id, version, document) VALUES (?, ?, ?, 1, ?)`, k.ws, k.project, s.ticket, s.doc); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	settle(t, old)
	seq := position(t, old).Seq
	_ = old.Close()

	upgraded := open(t, c, 1)
	settle(t, upgraded)
	if v, _ := upgraded.SchemaVersion(bg); v != len(all) {
		t.Fatalf("schema %d, want %d", v, len(all))
	}
	var got, other []string
	eventually(t, 10*time.Second, "the migrated dependencies", func() bool {
		return upgraded.View(bg, func(tx store.Tx) error {
			a, err := tx.Ticket(bg, k.ws, k.project, k.ticket)
			if err != nil {
				return err
			}
			b, err := tx.Ticket(bg, k.ws, k.project, second)
			got, other = a.Dependencies, b.Dependencies
			return err
		}) == nil && len(got) == 1
	})
	if got[0] != second || len(other) != 0 {
		t.Fatalf("dependencies after the upgrade: %v and %v", got, other)
	}
	// Applied once, as one write.
	if got := position(t, upgraded).Seq; got != seq+1 {
		t.Errorf("the cluster is at position %d, want %d: the migration was not one write", got, seq+1)
	}
}

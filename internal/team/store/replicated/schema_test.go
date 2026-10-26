package replicated

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"werkbord/internal/sqlitekit"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/infra/rqlite/rqlitetest"
	"werkbord/internal/team/store"
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
	target := -1
	for i, migration := range all {
		if migration.Name == "one_dependency_list" {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("one_dependency_list migration is missing")
	}
	before := all[:target]
	old := open(t, c, 0, func(o *Options) { o.Migrations = before })
	k := seedBeforeSourceRef(t, old)
	second, gone := domain.NewID(domain.PrefixTicket), domain.NewID(domain.PrefixTicket)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := old.update(bg, func(q store.Queryer) error {
		n, err := store.NewTx(q).NextTicketNumber(bg, k.ws)
		if err != nil {
			return err
		}
		return insertTicketBeforeSourceRef(bg, q, k.ws, domain.Ticket{ID: second, ProjectID: k.project, Number: n, Title: "Second", Status: domain.TicketAvailable, CreatorID: k.owner, CreatedAt: now, UpdatedAt: now})
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

	upgraded := open(t, c, 1, func(o *Options) { o.Migrations = all[:target+1] })
	settle(t, upgraded)
	if v, _ := upgraded.SchemaVersion(bg); v != target+1 {
		t.Fatalf("schema %d, want %d", v, target+1)
	}
	var migrated string
	eventually(t, 10*time.Second, "the migrated dependency", func() bool {
		return upgraded.update(bg, func(q store.Queryer) error {
			return q.QueryRowContext(bg, `SELECT depends_on_id FROM ticket_dependencies WHERE workspace_id = ? AND ticket_id = ?`, k.ws, k.ticket).Scan(&migrated)
		}) == nil && migrated == second
	})
	// Applied once, as one write.
	if got := position(t, upgraded).Seq; got != seq+1 {
		t.Errorf("the cluster is at position %d, want %d: the migration was not one write", got, seq+1)
	}
	_ = upgraded.Close()

	// Later schema migrations are independent writes. Open the current schema only after checking the target migration's position.
	current := open(t, c, 2)
	settle(t, current)
	if v, _ := current.SchemaVersion(bg); v != len(all) {
		t.Fatalf("current schema %d, want %d", v, len(all))
	}
	var got, other []string
	eventually(t, 10*time.Second, "the migrated dependencies in the current schema", func() bool {
		return current.View(bg, func(tx store.Tx) error {
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
}

// This test starts before migration 0015. Use only columns present at that schema version;
// the current InsertTicket query includes source_ref from a later migration.
func seedBeforeSourceRef(t testing.TB, s *Store) seeded {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	k := seeded{ws: domain.NewID(domain.PrefixWorkspace), owner: domain.NewID(domain.PrefixMember), project: domain.NewID(domain.PrefixProject), ticket: domain.NewID(domain.PrefixTicket)}
	err := s.update(bg, func(q store.Queryer) error {
		tx := store.NewTx(q)
		if err := tx.InsertWorkspace(bg, domain.Workspace{ID: k.ws, Name: "Acme", CreatedAt: now}); err != nil {
			return err
		}
		if err := tx.InsertMember(bg, domain.Member{ID: k.owner, WorkspaceID: k.ws, Name: "Ada", Role: domain.RoleOwner, CreatedAt: now}, "hash-"+k.owner); err != nil {
			return err
		}
		if err := tx.InsertProject(bg, domain.Project{ID: k.project, WorkspaceID: k.ws, Name: "App", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if err := tx.AddProjectMember(bg, k.ws, domain.ProjectMember{ProjectID: k.project, MemberID: k.owner, AddedBy: k.owner, AddedAt: now, Role: domain.ProjectOwner}); err != nil {
			return err
		}
		n, err := tx.NextTicketNumber(bg, k.ws)
		if err != nil {
			return err
		}
		return insertTicketBeforeSourceRef(bg, q, k.ws, domain.Ticket{ID: k.ticket, ProjectID: k.project, Number: n, Title: "First", Status: domain.TicketAvailable, CreatorID: k.owner, CreatedAt: now, UpdatedAt: now})
	})
	if err != nil {
		t.Fatalf("seeding pre-source_ref schema: %v", err)
	}
	return k
}

func insertTicketBeforeSourceRef(ctx context.Context, q store.Queryer, workspaceID string, ticket domain.Ticket) error {
	_, err := q.ExecContext(ctx, `INSERT INTO tickets (id, workspace_id, project_id, number, title, status, creator_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, ticket.ID, workspaceID, ticket.ProjectID, ticket.Number, ticket.Title, string(ticket.Status), ticket.CreatorID,
		ticket.CreatedAt.UTC().UnixMilli(), ticket.UpdatedAt.UTC().UnixMilli())
	return err
}

func TestClaimUpgradeKeepsWorkAndReportsOnTheReplicatedStore(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	all, err := store.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	target := -1
	for i, migration := range all {
		if migration.Name == "claim_before_work" {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("claim_before_work migration is missing")
	}
	old := open(t, c, 0, func(o *Options) { o.Migrations = all[:target] })
	k := seed(t, old)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := old.update(bg, func(q store.Queryer) error {
		if _, err := q.ExecContext(bg, `UPDATE tickets SET status='in_progress',assignee_id=?,branch='wb-1-work',claimed_at=? WHERE id=?`, k.owner, now.UnixMilli(), k.ticket); err != nil {
			return err
		}
		return store.NewTx(q).ReplaceCommits(bg, k.ticket, []domain.Commit{{SHA: "abcdef0", Subject: "Preserve", CommittedAt: now}})
	}); err != nil {
		t.Fatal(err)
	}
	if err := old.update(bg, func(q store.Queryer) error {
		_, err := q.ExecContext(bg, `INSERT INTO ticket_schedules VALUES(?,?,?,3,'{"executionId":"exe_upgrade"}')`, k.ws, k.project, k.ticket)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := ticketOf(t, old, k)
	_ = old.Close()
	upgraded := open(t, c, 1)
	settle(t, upgraded)
	got := ticketOf(t, upgraded, k)
	if got.Status != domain.TicketInProgress || got.AssigneeID != before.AssigneeID || got.Assignment != before.Assignment || len(got.Commits) != 1 || got.Commits[0].Subject != "Preserve" {
		t.Fatalf("upgrade lost active work: %+v", got)
	}
	if err := upgraded.update(bg, func(q store.Queryer) error {
		var doc string
		if err := q.QueryRowContext(bg, `SELECT document FROM ticket_schedules WHERE ticket_id=?`, k.ticket).Scan(&doc); err != nil {
			return err
		}
		if doc != `{"executionId":"exe_upgrade"}` {
			t.Fatalf("lost schedule: %s", doc)
		}
		_, err := q.ExecContext(bg, `UPDATE tickets SET status='available' WHERE id=?`, k.ticket)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	settle(t, upgraded)
	reserved := ticketOf(t, upgraded, k)
	if reserved.Status != domain.TicketAvailable || reserved.AssigneeID != k.owner || reserved.Assignment != before.Assignment {
		t.Fatalf("reservation changed ownership: %+v", reserved)
	}
}

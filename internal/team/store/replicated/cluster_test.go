package replicated

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/store"
)

// These tests run the replicated store on real rqlite clusters (the pinned program, as Team ships it),
// as several hosts at once. They skip when the program has not been fetched (scripts/fetch-rqlite.sh);
// CI requires it.

var bg = context.Background()

func testLog(t testing.TB) *slog.Logger {
	if os.Getenv("WERKBORD_TEST_VERBOSE") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.DiscardHandler)
}

// open opens a store as the host that runs node i of the cluster: it reads that node first.
func open(t testing.TB, c *rqlitetest.Cluster, i int, mutate ...func(*Options)) *Store {
	t.Helper()
	o := Options{Dir: t.TempDir(), Nodes: c.ClientAddrs(i), FixedNodes: true, Auth: Auth{User: rqlite.UserApp, Pass: c.Creds.App}, HostID: c.Node(i).ID, Log: testLog(t),
		Poll: 40 * time.Millisecond, Create: true, WaitForCluster: 30 * time.Second}
	for _, m := range mutate {
		m(&o)
	}
	ctx, cancel := context.WithTimeout(bg, 90*time.Second)
	defer cancel()
	s, err := Open(ctx, o)
	if err != nil {
		t.Fatalf("opening the store on %s: %v", c.Node(i).ID, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func ms(t testing.TB) int {
	t.Helper()
	m, err := store.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	return len(m)
}

func eventually(t testing.TB, d time.Duration, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// workspace creates a workspace with an owner and a project and a ticket, and returns their IDs.
type seeded struct{ ws, owner, project, ticket string }

func seed(t testing.TB, s store.Store) seeded {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	k := seeded{ws: domain.NewID(domain.PrefixWorkspace), owner: domain.NewID(domain.PrefixMember), project: domain.NewID(domain.PrefixProject), ticket: domain.NewID(domain.PrefixTicket)}
	err := s.Update(bg, func(tx store.Tx) error {
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
		return tx.InsertTicket(bg, k.ws, domain.Ticket{ID: k.ticket, ProjectID: k.project, Number: n, Title: "First", Status: domain.TicketAvailable, CreatorID: k.owner, CreatedAt: now, UpdatedAt: now})
	})
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	return k
}

func ticketOf(t testing.TB, s store.Store, k seeded) domain.Ticket {
	t.Helper()
	var out domain.Ticket
	if err := s.View(bg, func(tx store.Tx) (err error) { out, err = tx.Ticket(bg, k.ws, k.project, k.ticket); return }); err != nil {
		t.Fatal(err)
	}
	return out
}

func position(t testing.TB, s *Store) fence {
	t.Helper()
	f, err := s.repl.fence(bg)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTheStoreCreatesAndMigratesTheClusterOnce(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	a := open(t, c, 0)
	if v, err := a.SchemaVersion(bg); err != nil || v != ms(t) {
		t.Fatalf("schema %d %v, want %d", v, err, ms(t))
	}
	// One write per migration, and the position says so.
	if f := position(t, a); f.Seq != int64(ms(t)) {
		t.Fatalf("position %d, want %d", f.Seq, ms(t))
	}
	// Opening again, on the same cluster, changes nothing.
	b := open(t, c, 0)
	if f := position(t, b); f.Seq != int64(ms(t)) {
		t.Fatalf("a second host migrated again: position %d", f.Seq)
	}
	if err := a.Ping(bg); err != nil {
		t.Fatal(err)
	}
}

func TestTwoHostsOpeningTogetherMigrateOnce(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	stores := make([]*Store, 3)
	var wg sync.WaitGroup
	for i := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stores[i] = open(t, c, i)
		}()
	}
	wg.Wait()
	for i, s := range stores {
		if f := position(t, s); f.Seq != int64(ms(t)) {
			t.Errorf("host %d is at position %d, want %d: a migration ran more than once", i, f.Seq, ms(t))
		}
		if v, _ := s.SchemaVersion(bg); v != ms(t) {
			t.Errorf("host %d: schema %d", i, v)
		}
	}
	var conflicts int64
	for _, s := range stores {
		conflicts += s.Counters().Conflicts
	}
	t.Logf("%d guarded writes were refused and run again while the three migrated", conflicts)
}

func TestAWriteOnOneHostIsReadOnAnother(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	a, b := open(t, c, 0), open(t, c, 1)
	k := seed(t, a)
	eventually(t, 10*time.Second, "host B to see host A's write", func() bool {
		var n int
		_ = b.View(bg, func(tx store.Tx) error {
			ws, err := tx.Members(bg, k.ws)
			n = len(ws)
			return err
		})
		return n == 1
	})
	if got := ticketOf(t, b, k); got.Title != "First" || got.Status != domain.TicketAvailable {
		t.Fatalf("%+v", got)
	}
	// Both are at the same position once settled.
	eventually(t, 10*time.Second, "equal positions", func() bool { return position(t, a) == position(t, b) })
}

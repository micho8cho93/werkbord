package replicated

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/store"
)

// The guarantees Team's use cases rely on (the contract in store.Store), checked on a real cluster with
// several hosts writing at once.

func addTickets(t testing.TB, s store.Store, k seeded, n int) []string {
	t.Helper()
	var ids []string
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.Update(bg, func(tx store.Tx) error {
		ids = ids[:0]
		for i := 0; i < n; i++ {
			num, err := tx.NextTicketNumber(bg, k.ws)
			if err != nil {
				return err
			}
			id := domain.NewID(domain.PrefixTicket)
			if err := tx.InsertTicket(bg, k.ws, domain.Ticket{ID: id, ProjectID: k.project, Number: num, Title: fmt.Sprintf("T%d", i), Status: domain.TicketAvailable, CreatorID: k.owner, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return ids
}

func addMember(t testing.TB, s store.Store, k seeded, name string) string {
	t.Helper()
	id := domain.NewID(domain.PrefixMember)
	now := time.Now().UTC()
	if err := s.Update(bg, func(tx store.Tx) error {
		if err := tx.InsertMember(bg, domain.Member{ID: id, WorkspaceID: k.ws, Name: name, Role: domain.RoleMember, CreatedAt: now}, "h-"+id); err != nil {
			return err
		}
		return tx.AddProjectMember(bg, k.ws, domain.ProjectMember{ProjectID: k.project, MemberID: id, AddedBy: k.owner, AddedAt: now, Role: domain.ProjectContributor})
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// A ticket is claimed by one person, whichever host they are on and however many claim it at once.
func TestExactlyOneHostClaimsATicket(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := []*Store{open(t, c, 0), open(t, c, 1), open(t, c, 2)}
	k := seed(t, hosts[0])
	const tickets = 12
	ids := addTickets(t, hosts[0], k, tickets)
	people := make([]string, len(hosts))
	for i := range people {
		people[i] = addMember(t, hosts[0], k, fmt.Sprintf("person-%d", i))
	}
	wins := make([]atomic.Int64, tickets)
	var winner sync.Map
	var wg sync.WaitGroup
	for h := range hosts {
		for i, id := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var ok bool
				err := hosts[h].Update(bg, func(tx store.Tx) error {
					cur, err := tx.Ticket(bg, k.ws, k.project, id)
					if err != nil {
						return err
					}
					ok, err = tx.ClaimTicket(bg, k.ws, k.project, id, people[h], "branch-"+cur.Key, time.Now())
					if err != nil || !ok {
						return err
					}
					return tx.AddActivity(bg, k.ws, domain.Activity{ProjectID: k.project, TicketID: id, ActorID: people[h], Kind: domain.ActTicketClaimed, CreatedAt: time.Now()})
				})
				if err != nil {
					t.Errorf("host %d, ticket %d: %v", h, i, err)
					return
				}
				if ok {
					wins[i].Add(1)
					winner.Store(id, people[h])
				}
			}()
		}
	}
	wg.Wait()
	for i := range wins {
		if n := wins[i].Load(); n != 1 {
			t.Errorf("ticket %d was claimed by %d hosts", i, n)
		}
	}
	// The record agrees with who was told they won, and every claim left exactly one entry in the activity.
	for _, id := range ids {
		got, err := func() (domain.Ticket, error) {
			var k2 domain.Ticket
			err := hosts[1].View(bg, func(tx store.Tx) (err error) { k2, err = tx.Ticket(bg, k.ws, k.project, id); return })
			return k2, err
		}()
		if err != nil {
			t.Fatal(err)
		}
		w, _ := winner.Load(id)
		if got.AssigneeID != w || got.Status != domain.TicketInProgress {
			t.Errorf("ticket %s is %s held by %s, but %v was told they won", id, got.Status, got.AssigneeID, w)
		}
	}
	var acts int
	eventually(t, 10*time.Second, "host 2 to have every entry", func() bool {
		_ = hosts[2].View(bg, func(tx store.Tx) error {
			a, err := tx.Activity(bg, k.ws, k.project, 0, 1000)
			acts = len(a)
			return err
		})
		return acts >= tickets
	})
	if acts != tickets {
		t.Errorf("%d claims left %d entries in the activity", tickets, acts)
	}
}

// Every write from every host is kept, in one order, and no host's change is lost to another's.
func TestConcurrentWritesFromManyHostsAreAllKept(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := []*Store{open(t, c, 0), open(t, c, 1), open(t, c, 2)}
	k := seed(t, hosts[0])
	const each = 15
	var wg sync.WaitGroup
	for h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := hosts[h].Update(bg, func(tx store.Tx) error {
					if _, err := tx.BumpRevision(bg, k.ws, k.project); err != nil {
						return err
					}
					return tx.AddActivity(bg, k.ws, domain.Activity{ProjectID: k.project, ActorID: k.owner, Kind: domain.ActTicketMoved, Detail: fmt.Sprintf("host %d #%d", h, i), CreatedAt: time.Now()})
				}); err != nil {
					t.Errorf("host %d: %v", h, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	var rev int64
	if err := hosts[0].Update(bg, func(tx store.Tx) (err error) { rev, err = tx.Revision(bg, k.ws, k.project); return }); err != nil {
		t.Fatal(err)
	}
	if rev != 3*each {
		t.Fatalf("the revision is %d after %d bumps: updates were lost or repeated", rev, 3*each)
	}
	// Another host's copy takes in the last writes within moments; it is never wrong, only a little behind.
	var n int
	eventually(t, 10*time.Second, "host 1 to have every entry", func() bool {
		_ = hosts[1].View(bg, func(tx store.Tx) error {
			a, err := tx.Activity(bg, k.ws, k.project, 0, 1000)
			n = len(a)
			return err
		})
		return n == 3*each
	})
	var conflicts int64
	for _, h := range hosts {
		conflicts += h.Counters().Conflicts
	}
	t.Logf("%d writes, %d guarded writes refused and run again", 3*each, conflicts)
	// Every host ends up with the same data as the cluster.
	for i, h := range hosts {
		if _, _, err := h.Verify(bg); err != nil {
			t.Errorf("host %d: %v", i, err)
		}
	}
}

// A function that fails writes nothing; one that reads after it writes sees its own writes; one that is
// refused by the guard is run again, and its effects outside the transaction are the caller's to avoid.
func TestUpdateIsAtomicAndSeesItsOwnWrites(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	s := open(t, c, 0)
	k := seed(t, s)
	sentinel := errors.New("no")
	err := s.Update(bg, func(tx store.Tx) error {
		if _, err := tx.BumpRevision(bg, k.ws, k.project); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("%v", err)
	}
	var rev int64
	_ = s.View(bg, func(tx store.Tx) (err error) { rev, err = tx.Revision(bg, k.ws, k.project); return })
	if rev != 0 {
		t.Fatalf("a failed update changed the revision to %d", rev)
	}
	// Read-your-writes, through a write that returns a value (UPDATE ... RETURNING), a guarded write and a read.
	before := position(t, s)
	err = s.Update(bg, func(tx store.Tx) error {
		r1, err := tx.BumpRevision(bg, k.ws, k.project)
		if err != nil {
			return err
		}
		r2, err := tx.BumpRevision(bg, k.ws, k.project)
		if err != nil {
			return err
		}
		if r1 != 1 || r2 != 2 {
			t.Errorf("revisions %d, %d", r1, r2)
		}
		ok, err := tx.ClaimTicket(bg, k.ws, k.project, k.ticket, k.owner, "b", time.Now())
		if err != nil || !ok {
			t.Errorf("claim %v %v", ok, err)
		}
		got, err := tx.Ticket(bg, k.ws, k.project, k.ticket)
		if err != nil || got.Status != domain.TicketInProgress || got.AssigneeID != k.owner {
			t.Errorf("a read after the write: %+v %v", got, err)
		}
		again, err := tx.ClaimTicket(bg, k.ws, k.project, k.ticket, k.owner, "b", time.Now())
		if err != nil || again {
			t.Errorf("a second claim in the same update: %v %v", again, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// All of it was one write.
	if after := position(t, s); after.Seq != before.Seq+1 {
		t.Fatalf("one update made %d writes", after.Seq-before.Seq)
	}
	// An update that only reads writes nothing.
	before = position(t, s)
	if err := s.Update(bg, func(tx store.Tx) error { _, err := tx.Revision(bg, k.ws, k.project); return err }); err != nil {
		t.Fatal(err)
	}
	if after := position(t, s); after != before {
		t.Fatal("a read-only update moved the cluster's position")
	}
	// A statement the schema refuses is the domain's error, as it is on a single file.
	err = s.Update(bg, func(tx store.Tx) error {
		return tx.InsertMember(bg, domain.Member{ID: domain.NewID(domain.PrefixMember), WorkspaceID: k.ws, Name: "ada", Role: domain.RoleMember, CreatedAt: time.Now()}, "h")
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a duplicate name: %v", err)
	}
}

// When another write gets in first the function is run again from the new position, and what it decides is
// decided on that.
func TestAGuardedWriteThatLostTheRaceIsRunAgain(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 2})
	a, b := open(t, c, 0), open(t, c, 1)
	k := seed(t, a)
	// Wait until b has the seed, then make b's write lose a race by moving the position while it is deciding.
	eventually(t, 10*time.Second, "b to have the seed", func() bool { return position(t, a).Seq == position(t, b).Seq })
	runs := 0
	var seen []int64
	err := b.Update(bg, func(tx store.Tx) error {
		runs++
		rev, err := tx.Revision(bg, k.ws, k.project)
		if err != nil {
			return err
		}
		seen = append(seen, rev)
		if runs == 1 {
			// While b is deciding, a writes.
			if err := a.Update(bg, func(tx store.Tx) error { _, err := tx.BumpRevision(bg, k.ws, k.project); return err }); err != nil {
				return err
			}
		}
		_, err = tx.BumpRevision(bg, k.ws, k.project)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if runs != 2 || len(seen) != 2 || seen[0] != 0 || seen[1] != 1 {
		t.Fatalf("the function ran %d times, reading revisions %v: it must run again from the new position", runs, seen)
	}
	var rev int64
	_ = b.View(bg, func(tx store.Tx) (err error) { rev, err = tx.Revision(bg, k.ws, k.project); return })
	if rev != 2 {
		t.Fatalf("revision %d, want 2: neither write may be lost", rev)
	}
}

func TestVerifyFindsACopyThatDiffersAndReplacesIt(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	s := open(t, c, 0)
	k := seed(t, s)
	if _, _, err := s.Verify(bg); err != nil {
		t.Fatalf("a good copy: %v", err)
	}
	// Damage the copy behind the store's back.
	s.writeMu.Lock()
	pool, err := s.repl.writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Writer.ExecContext(bg, `UPDATE members SET name = 'Mallory' WHERE id = ?`, k.owner); err != nil {
		t.Fatal(err)
	}
	s.writeMu.Unlock()
	if _, _, err := s.Verify(bg); err == nil {
		t.Fatal("a damaged copy was not noticed")
	}
	// It was replaced, and is good again.
	var name string
	_ = s.View(bg, func(tx store.Tx) error {
		m, err := tx.Member(bg, k.ws, k.owner)
		name = m.Name
		return err
	})
	if name != "Ada" {
		t.Fatalf("the copy says %q after it was replaced", name)
	}
	if _, _, err := s.Verify(bg); err != nil {
		t.Fatalf("after replacing it: %v", err)
	}
}

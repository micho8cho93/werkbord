package replicated

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/store"
)

// What happens to the workspace when hosts fail, are cut off, come back, join and leave. Every test runs real
// rqlite nodes, as the supervisor starts them, with a Team storage layer on each, as the host that runs that node.

var counter atomic.Int64

// note writes one entry to the activity, through s, and returns what it wrote.
func note(s store.Store, k seeded) (string, error) {
	text := fmt.Sprintf("entry-%d", counter.Add(1))
	err := s.Update(bg, func(tx store.Tx) error {
		return tx.AddActivity(bg, k.ws, domain.Activity{ProjectID: k.project, ActorID: k.owner, Kind: domain.ActTicketMoved, Detail: text, CreatedAt: time.Now()})
	})
	return text, err
}

func notes(s store.Store, k seeded) map[string]bool {
	out := map[string]bool{}
	_ = s.View(bg, func(tx store.Tx) error {
		a, err := tx.Activity(bg, k.ws, k.project, 0, 10000)
		for _, e := range a {
			out[e.Detail] = true
		}
		return err
	})
	return out
}

// noteSoon writes through s, trying again while the cluster is electing a leader.
func noteSoon(t testing.TB, s store.Store, k seeded, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	var last error
	for time.Now().Before(deadline) {
		text, err := note(s, k)
		if err == nil {
			return text
		}
		last = err
		if !errors.Is(err, domain.ErrReadOnly) && !errors.Is(err, domain.ErrBusy) {
			t.Fatalf("a write failed for a reason that is not the cluster's availability: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no write was accepted within %s: %v", within, last)
	return ""
}

func hostsOf(t testing.TB, c *rqlitetest.Cluster) []*Store {
	var out []*Store
	for i := range c.Nodes() {
		out = append(out, open(t, c, i))
	}
	return out
}

func leaderIndex(t testing.TB, c *rqlitetest.Cluster) int {
	t.Helper()
	return c.WaitLeader(20 * time.Second).Index
}

// settle waits until every store has the cluster's whole history.
func settle(t testing.TB, hosts ...*Store) {
	t.Helper()
	eventually(t, 30*time.Second, "every host to have the same history", func() bool {
		var first fence
		for i, h := range hosts {
			f, err := h.repl.fence(bg)
			if err != nil {
				return false
			}
			if i == 0 {
				first = f
			} else if f != first {
				return false
			}
		}
		return true
	})
}

func TestTheLeaderDiesAndAnotherIsElected(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	before := noteSoon(t, hosts[0], k, 20*time.Second)
	lead := leaderIndex(t, c)
	c.Node(lead).Kill()

	// The hosts that are left write, to the new leader, with no one doing anything about it.
	var alive []*Store
	for i, h := range hosts {
		if i != lead {
			alive = append(alive, h)
		}
	}
	during := noteSoon(t, alive[0], k, 30*time.Second)
	newLead := c.WaitLeader(20*time.Second, aliveNodes(c, lead)...)
	if newLead.Index == lead {
		t.Fatal("the dead node is still the leader")
	}
	if err := func() error { _, err := note(alive[1], k); return err }(); err != nil {
		t.Fatalf("the other survivor cannot write: %v", err)
	}
	// The host whose own node died still reads, and a write from it goes to the others.
	if !notes(hosts[lead], k)[before] {
		t.Error("the host of the dead node cannot read what it had")
	}
	if _, err := note(hosts[lead], k); err != nil {
		t.Fatalf("the host whose node died could not write through the cluster's other nodes: %v", err)
	}
	settle(t, alive...)
	if !notes(alive[1], k)[during] {
		t.Error("a write made while the new leader was elected is missing from the other host")
	}

	// The old leader comes back, and takes in everything it missed.
	c.Node(lead).Start()
	c.WaitMembers(30*time.Second, 3)
	settle(t, hosts...)
	if !notes(hosts[lead], k)[during] {
		t.Error("the old leader did not catch up")
	}
	for i, h := range hosts {
		if _, _, err := h.Verify(bg); err != nil {
			t.Errorf("host %d: %v", i, err)
		}
	}
}

func aliveNodes(c *rqlitetest.Cluster, except int) []*rqlitetest.Node {
	var out []*rqlitetest.Node
	for _, n := range c.Nodes() {
		if n.Index != except {
			out = append(out, n)
		}
	}
	return out
}

func TestAFollowerDiesAndComesBack(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	lead := leaderIndex(t, c)
	follower := (lead + 1) % 3
	c.Node(follower).Kill()
	// No pause at all: the leader and the other follower are a quorum.
	var texts []string
	for i := 0; i < 5; i++ {
		text, err := note(hosts[lead], k)
		if err != nil {
			t.Fatalf("write %d with one follower down: %v", i, err)
		}
		texts = append(texts, text)
	}
	c.Node(follower).Start()
	c.WaitMembers(30*time.Second, 3)
	settle(t, hosts...)
	got := notes(hosts[follower], k)
	for _, text := range texts {
		if !got[text] {
			t.Errorf("the follower that was down is missing %s", text)
		}
	}
}

func TestAHostCutOffFromTheOthersRefusesToWrite(t *testing.T) {
	needLsof(t)
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3, Owner: lsofOwner})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	before := noteSoon(t, hosts[0], k, 20*time.Second)
	settle(t, hosts...)
	lead := leaderIndex(t, c)
	lone := (lead + 1) % 3 // a follower, alone
	c.Isolate(lone)
	others := []*Store{}
	for i, h := range hosts {
		if i != lone {
			others = append(others, h)
		}
	}
	// Two of three keep a quorum and keep working.
	kept := noteSoon(t, others[0], k, 30*time.Second)
	if _, err := note(others[1], k); err != nil {
		t.Fatal(err)
	}

	// The one on its own is refused, whatever it asks. It says why, and that it can still read.
	_, err := note(hosts[lone], k)
	if !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("a host with no quorum accepted a write, or failed for another reason: %v", err)
	}
	eventually(t, 20*time.Second, "the cut-off host to say it is read-only", func() bool {
		st := hosts[lone].Status(bg)
		return st.ReadOnly && !st.Topology.Writable == (st.Topology.ReachableVoters < st.Topology.Quorum)
	})
	if !notes(hosts[lone], k)[before] {
		t.Error("the cut-off host cannot read what it had")
	}
	// What it was asked to write is nowhere: it refused, and says so each time.
	var refused []string
	for i := 0; i < 3; i++ {
		text, err := note(hosts[lone], k)
		if !errors.Is(err, domain.ErrReadOnly) {
			t.Fatalf("%v", err)
		}
		refused = append(refused, text)
	}

	// The network is mended: the host catches up, and what it refused was never written.
	c.Heal()
	settle(t, hosts...)
	got := notes(hosts[lone], k)
	if !got[kept] {
		t.Error("the host that was cut off did not catch up")
	}
	for _, text := range refused {
		for i, h := range hosts {
			if notes(h, k)[text] {
				t.Errorf("host %d has %s, which the cut-off host refused to write", i, text)
			}
		}
	}
	if len(got) != len(notes(hosts[lead], k)) {
		t.Errorf("the hosts disagree after the network was mended: %d and %d entries", len(got), len(notes(hosts[lead], k)))
	}
	if _, err := note(hosts[lone], k); err != nil {
		t.Fatalf("the host cannot write after the network was mended: %v", err)
	}
}

func TestACutOffLeaderStopsAcceptingWritesAndTheOthersCarryOn(t *testing.T) {
	needLsof(t)
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3, Owner: lsofOwner})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	settle(t, hosts...)
	lead := leaderIndex(t, c)
	c.Isolate(lead)
	var rest []*Store
	for i, h := range hosts {
		if i != lead {
			rest = append(rest, h)
		}
	}
	// The other two elect a leader and write.
	text := noteSoon(t, rest[0], k, 40*time.Second)
	// The old leader, which may believe for a moment that it still leads, must not take a write: a write that
	// only it holds could not be kept. Whatever it answers, it must not have written.
	_, err := note(hosts[lead], k)
	if !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("the cut-off leader's host: %v", err)
	}
	c.Heal()
	settle(t, hosts...)
	got := notes(hosts[lead], k)
	if !got[text] {
		t.Error("the old leader's host did not take in what the others wrote")
	}
	// Exactly the writes that were acknowledged are there.
	if n, m := len(got), len(notes(hosts[(lead+1)%3], k)); n != m {
		t.Errorf("%d and %d entries", n, m)
	}
}

func TestLosingTwoOfThreeLeavesTheWorkspaceReadOnlyAndItRecovers(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	before := noteSoon(t, hosts[0], k, 20*time.Second)
	settle(t, hosts...)
	c.Node(1).Kill()
	c.Node(2).Kill()
	survivor := hosts[0]

	// Writes stop, at once and with a reason that says so. Reading continues.
	eventually(t, 30*time.Second, "the survivor to refuse writes", func() bool {
		_, err := note(survivor, k)
		return errors.Is(err, domain.ErrReadOnly)
	})
	started := time.Now()
	_, err := note(survivor, k)
	if !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("%v", err)
	}
	if d := time.Since(started); d > 20*time.Second {
		t.Errorf("refusing a write took %s", d)
	}
	if !notes(survivor, k)[before] {
		t.Error("a read-only host cannot read what it had")
	}
	eventually(t, 20*time.Second, "the status to say read-only", func() bool {
		st := survivor.Status(bg)
		return st.ReadOnly && st.State == domain.StorageReplicated && !st.Topology.Writable && st.Topology.ReachableVoters == 1 && st.Topology.Quorum == 2
	})
	st := survivor.Status(bg)
	if st.Reason == "" || st.Topology.Level != domain.TopologyRecommended {
		t.Fatalf("%+v", st)
	}
	// The service reports it the way the API does: as the domain's error.
	if err := survivor.Update(bg, func(tx store.Tx) error { _, err := tx.BumpRevision(bg, k.ws, k.project); return err }); !errors.Is(err, domain.ErrReadOnly) {
		t.Fatalf("%v", err)
	}

	// A quorum comes back: writes resume, nothing was lost, and nothing was invented.
	c.Node(1).Start()
	c.WaitMembers(40*time.Second, 3)
	after := noteSoon(t, survivor, k, 40*time.Second)
	c.Node(2).Start()
	c.WaitMembers(40*time.Second, 3)
	settle(t, hosts...)
	for i, h := range hosts {
		got := notes(h, k)
		if !got[before] || !got[after] {
			t.Errorf("host %d lost an entry", i)
		}
		if _, _, err := h.Verify(bg); err != nil {
			t.Errorf("host %d: %v", i, err)
		}
	}
}

func TestACompleteOutageIsSurvivedByEveryHost(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	before := noteSoon(t, hosts[0], k, 20*time.Second)
	settle(t, hosts...)
	for _, n := range c.Nodes() {
		n.Stop()
	}
	// Every host still reads; none writes.
	for i, h := range hosts {
		if !notes(h, k)[before] {
			t.Errorf("host %d cannot read during an outage of the whole cluster", i)
		}
		if _, err := note(h, k); !errors.Is(err, domain.ErrReadOnly) {
			t.Errorf("host %d: %v", i, err)
		}
	}
	c.StartAll(c.Nodes()...)
	c.WaitMembers(60*time.Second, 3)
	after := noteSoon(t, hosts[2], k, 60*time.Second)
	settle(t, hosts...)
	for i, h := range hosts {
		if got := notes(h, k); !got[before] || !got[after] {
			t.Errorf("host %d", i)
		}
	}
}

func TestAHostThatRestartsKeepsItsCopyAndOnlyTakesWhatItMissed(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 2})
	dir := t.TempDir()
	a := open(t, c, 0)
	b := open(t, c, 1, func(o *Options) { o.Dir = dir })
	k := seed(t, a)
	settle(t, a, b)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	missed := []string{}
	for i := 0; i < 5; i++ {
		text, err := note(a, k)
		if err != nil {
			t.Fatal(err)
		}
		missed = append(missed, text)
	}
	b2 := open(t, c, 1, func(o *Options) { o.Dir = dir })
	settle(t, a, b2)
	got := notes(b2, k)
	for _, m := range missed {
		if !got[m] {
			t.Errorf("%s is missing", m)
		}
	}
	if n := b2.Counters().Snapshots; n != 0 {
		t.Errorf("a host that had a copy took %d fresh ones instead of the writes it missed", n)
	}
}

func TestAHostFarBehindTakesAFreshCopy(t *testing.T) {
	oldKeep, oldAge := walKeep, walMinAge
	walKeep, walMinAge = 5, 0
	t.Cleanup(func() { walKeep, walMinAge = oldKeep, oldAge })
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 2})
	a := open(t, c, 0)
	dir := t.TempDir()
	b := open(t, c, 1, func(o *Options) { o.Dir = dir })
	k := seed(t, a)
	settle(t, a, b)
	_ = b.Close()
	var texts []string
	for i := 0; i < 30; i++ {
		text, err := note(a, k)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, text)
	}
	removed, err := a.Prune(bg)
	if err != nil || removed == 0 {
		t.Fatalf("pruned %d: %v", removed, err)
	}
	b2 := open(t, c, 1, func(o *Options) { o.Dir = dir })
	settle(t, a, b2)
	if b2.Counters().Snapshots == 0 {
		t.Error("a host further behind than the log reaches did not take a fresh copy")
	}
	got := notes(b2, k)
	for _, text := range texts {
		if !got[text] {
			t.Errorf("%s is missing", text)
		}
	}
	if _, _, err := b2.Verify(bg); err != nil {
		t.Fatal(err)
	}
}

func TestAHostIsAddedAsAReplicaCatchesUpAndIsMadeAVoter(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	for i := 0; i < 10; i++ {
		noteSoon(t, hosts[0], k, 20*time.Second)
	}
	n := c.Add(true)
	// Its storage layer takes the whole workspace, and it votes for nothing yet.
	fresh := open(t, c, n.Index, func(o *Options) { o.Create = false })
	settle(t, append(hosts, fresh)...)
	st := fresh.Status(bg)
	if st.Topology.Voters != 3 || st.Topology.NonVoters != 1 || len(st.Hosts) != 4 {
		t.Fatalf("%+v", st.Topology)
	}
	if _, _, err := fresh.Verify(bg); err != nil {
		t.Fatal(err)
	}
	// It becomes a voter once it has caught up, and the cluster then has four.
	ctx := bg
	if err := n.Sup.BecomeVoter(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, 20*time.Second, "four voters", func() bool {
		st := hosts[0].Status(bg)
		return st.Topology.Voters == 4 && st.Topology.Level == domain.TopologyEven
	})
	noteSoon(t, fresh, k, 30*time.Second)
	settle(t, append(hosts, fresh)...)
}

func TestAHostIsRemovedByAMembershipChangeAndTheClusterKeepsWorking(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	settle(t, hosts...)
	// The cluster's own view of itself decides whether the removal is safe.
	st := hosts[0].Status(bg)
	infra := make([]domain.StorageHost, 0, len(st.Hosts))
	infra = append(infra, st.Hosts...)
	victim := c.Node(2)
	if err := domain.CheckRemoval(infra, victim.ID); err != nil {
		t.Fatal(err)
	}
	// A leader is moved off before it is removed, as the host that makes the change does it.
	for _, h := range infra {
		if h.NodeID == victim.ID && h.Leader {
			if err := c.Node(0).Admin().StepDown(bg, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := c.Node(0).Admin().Remove(bg, victim.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, 20*time.Second, "two voters", func() bool {
		s := hosts[0].Status(bg)
		return s.Topology.Voters == 2 && s.Topology.Writable
	})
	noteSoon(t, hosts[0], k, 30*time.Second)
	// With two left, losing one stops writes (two hosts are not highly available), and the status says so honestly.
	if got := hosts[0].Status(bg).Topology; got.HighAvailability || got.Level != domain.TopologyTwo {
		t.Fatalf("%+v", got)
	}
	_ = rqlite.Version
}

// ---- the answer to a write is lost ----

func TestAWriteWhoseAnswerIsLostIsAppliedExactlyOnce(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	lossy := newLossyProxy(t, c.Node(0).HTTP.String())
	s := open(t, c, 0, func(o *Options) { o.Nodes = append([]string{lossy.addr}, o.Nodes...) })
	k := seed(t, s)
	count := func() int { return len(notes(s, k)) }
	base := count()

	// The cluster takes the write and the answer never arrives: it was applied, once, and the caller is told so.
	lossy.mode.Store(int32(loseResponse))
	text, err := note(s, k)
	if err != nil {
		t.Fatal(err)
	}
	if lossy.dropped.Load() == 0 {
		t.Fatal("the proxy dropped no answer: the test did nothing")
	}
	if !notes(s, k)[text] || count() != base+1 {
		t.Fatalf("the write whose answer was lost was applied %d times", count()-base)
	}
	// The request never arrives: it was not applied, and the caller is not told it was; trying again once is the caller's.
	// (A node that took a request and did not answer is tried last from then on; the test wants the proxy again.)
	s.client.Prefer(lossy.addr)
	lossy.mode.Store(int32(loseRequest))
	lossy.dropped.Store(0)
	text2, err := note(s, k)
	if err != nil {
		t.Fatal(err)
	}
	if lossy.dropped.Load() == 0 {
		t.Fatal("the proxy dropped no request")
	}
	if !notes(s, k)[text2] || count() != base+2 {
		t.Fatalf("%d entries, want %d", count(), base+2)
	}
	if _, _, err := s.Verify(bg); err != nil {
		t.Fatal(err)
	}
	if s.Counters().Resolved == 0 && s.Counters().Retries == 0 {
		t.Error("neither path of the resolution was taken")
	}
}

func TestAClusterOfOneTakesAReplicaAndItsHostReadsAndVerifies(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	first := open(t, c, 0)
	k := seed(t, first)
	noteSoon(t, first, k, 20*time.Second)
	n := c.Add(true)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("replica:\n%s", strings.Join(n.Sup.Status().Tail, "\n"))
			t.Logf("leader:\n%s", strings.Join(c.Node(0).Sup.Status().Tail, "\n"))
		}
	})
	fresh := open(t, c, n.Index, func(o *Options) { o.Create = false })
	settle(t, first, fresh)
	if _, _, err := fresh.Verify(bg); err != nil {
		t.Fatal(err)
	}
	if err := n.Sup.BecomeVoter(bg); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "two voters", func() bool { return first.Status(bg).Topology.Voters == 2 })
	noteSoon(t, fresh, k, 30*time.Second)
}

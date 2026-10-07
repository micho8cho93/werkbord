//go:build !windows

package rqlite_test

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/infra/rqlite/rqlitetest"
)

// These tests run the program Werkbord ships, as it is shipped (pinned, verified, unmodified), and
// check what the supervisor and the admin client do with it. They skip when the program has not been
// fetched (scripts/fetch-rqlite.sh); CI requires it.

func ctxT(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

func TestASingleNodeStartsReportsItsVersionAndStops(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	n := c.Node(0)
	st := n.Sup.Status()
	if st.State != rqlite.StateRunning || st.ReportedVersion != rqlite.Tag || st.BinarySHA256 == "" || st.PID == 0 {
		t.Fatalf("status %+v", st)
	}
	h, err := n.Admin().Health(ctxT(t, 10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != rqlite.Tag || h.Commit != rqlite.SourceCommit {
		t.Fatalf("the running program reports version %q commit %q, want %q and %q", h.Version, h.Commit, rqlite.Tag, rqlite.SourceCommit)
	}
	if h.State != "Leader" || !h.Voter || len(h.Members) != 1 {
		t.Fatalf("health %+v", h)
	}
	// Its files are private, and the credentials are in a file only its owner can read.
	fi, err := os.Stat(n.Config().DataDir + "/auth.json")
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the credentials file: %v %v", fi, err)
	}
	n.Stop()
	if st := n.Sup.Status(); st.State != rqlite.StateStopped {
		t.Fatalf("after Stop: %+v", st)
	}
	// And it starts again, from its data, a member of the cluster it was.
	n.Start()
	if h, err := n.Admin().Health(ctxT(t, 10*time.Second)); err != nil || h.State != "Leader" {
		t.Fatalf("after a restart: %+v %v", h, err)
	}
}

func TestTheNodeWantsCredentials(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	n := c.Node(0)
	for _, tc := range []struct {
		name, user, pass string
		want             int
	}{{"no credentials", "", "", http.StatusUnauthorized}, {"a wrong password", rqlite.UserApp, "nope-nope-nope-nope-nope", http.StatusUnauthorized}} {
		req, _ := http.NewRequest("GET", "http://"+n.HTTP.String()+"/status", nil)
		if tc.user != "" {
			req.SetBasicAuth(tc.user, tc.pass)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Errorf("%s: %d", tc.name, res.StatusCode)
		}
	}
	// The application user can read and write data and cannot change the cluster.
	req, _ := http.NewRequest("DELETE", "http://"+n.HTTP.String()+"/remove", strings.NewReader(`{"id":"host-1"}`))
	req.SetBasicAuth(rqlite.UserApp, c.Creds.App)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the application user removed a node: %d", res.StatusCode)
	}
}

func TestAThreeNodeClusterFormsAndSurvivesTheLossOfOne(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	ctx := ctxT(t, 90*time.Second)
	nodes, err := c.Node(1).Admin().Nodes(ctx)
	if err != nil || len(nodes) != 3 {
		t.Fatalf("%v %v", nodes, err)
	}
	for _, n := range nodes {
		if !n.Voter || !n.Reachable {
			t.Errorf("%+v", n)
		}
	}
	leader := c.WaitLeader(10 * time.Second)
	var follower *rqlitetest.Node
	for _, n := range c.Nodes() {
		if n != leader {
			follower = n
			break
		}
	}
	follower.Kill()
	// The others still have a leader, and the cluster says which member is gone.
	leader = c.WaitLeader(15*time.Second, leaderAndOthers(c, follower)...)
	nodes, err = leader.Admin().Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gone := 0
	for _, n := range nodes {
		if !n.Reachable {
			gone++
			if n.ID != follower.ID {
				t.Errorf("%s is reported unreachable, but %s was killed", n.ID, follower.ID)
			}
		}
	}
	if gone != 1 {
		t.Fatalf("%d members reported unreachable: %+v", gone, nodes)
	}
	// It comes back and rejoins what it was, with no join step.
	follower.Start()
	c.WaitMembers(30*time.Second, 3)
}

func leaderAndOthers(c *rqlitetest.Cluster, except *rqlitetest.Node) []*rqlitetest.Node {
	var out []*rqlitetest.Node
	for _, n := range c.Nodes() {
		if n != except {
			out = append(out, n)
		}
	}
	return out
}

func TestAReplicaJoinsReadOnlyAndBecomesAVoterOnlyWhenCaughtUp(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	ctx := ctxT(t, 3*time.Minute)
	n := c.Add(true)
	nodes, err := c.Node(0).Admin().Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range nodes {
		if m.ID == n.ID {
			found = true
			if m.Voter {
				t.Fatalf("a joining replica is a voter already: %+v", m)
			}
		}
	}
	if !found {
		t.Fatalf("the replica is not in the cluster: %+v", nodes)
	}
	if err := n.Sup.BecomeVoter(ctx); err != nil {
		t.Fatal(err)
	}
	nodes, err = c.Node(0).Admin().Nodes(ctx)
	if err != nil || len(nodes) != 4 {
		t.Fatalf("%+v %v", nodes, err)
	}
	for _, m := range nodes {
		if !m.Voter || !m.Reachable {
			t.Errorf("after the change: %+v", m)
		}
	}
	// A voter has nothing to become.
	if err := n.Sup.BecomeVoter(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAMemberIsRemovedByARealMembershipChange(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	ctx := ctxT(t, 90*time.Second)
	victim := c.Node(2)
	if err := c.Node(0).Admin().Remove(ctx, victim.ID); err != nil {
		t.Fatal(err)
	}
	nodes, err := c.Node(0).Admin().Nodes(ctx)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("%+v %v", nodes, err)
	}
	for _, n := range nodes {
		if n.ID == victim.ID {
			t.Fatalf("%s is still a member", victim.ID)
		}
	}
	if err := c.Node(0).Admin().Remove(ctx, "not a node id"); err == nil {
		t.Error("an invalid ID was sent to the cluster")
	}
}

func TestAMajorityLostLeavesNoLeader(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	ctx := ctxT(t, 90*time.Second)
	c.Node(1).Kill()
	c.Node(2).Kill()
	survivor := c.Node(0)
	deadline := time.Now().Add(20 * time.Second)
	for {
		err := survivor.Admin().Ready(ctx, false)
		if err != nil {
			if err != rqlite.ErrNoLeader {
				t.Fatalf("the error is %v, want ErrNoLeader", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a node with a minority of the cluster still says it is ready")
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Its own process is up, and says so.
	if err := survivor.Admin().Alive(ctx); err != nil {
		t.Fatal(err)
	}
}

var _ = netip.Addr{}

func TestAloneNodeIsMovedToOtherAddressesWithItsData(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	n := c.Node(0)
	ctx := ctxT(t, 2*time.Minute)
	post := func(addr netip.AddrPort, body string, level string) string {
		req, _ := http.NewRequest("POST", "http://"+addr.String()+"/db/execute", strings.NewReader(body))
		if level != "" {
			req, _ = http.NewRequest("POST", "http://"+addr.String()+"/db/query?level="+level, strings.NewReader(body))
		}
		req.SetBasicAuth(rqlite.UserApp, c.Creds.App)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	if out := post(n.HTTP, `["CREATE TABLE t (x INTEGER)","INSERT INTO t VALUES (7)"]`, ""); strings.Contains(out, "error") {
		t.Fatal(out)
	}
	cfg := n.Config()
	cfg.HTTPAddr = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(rqlitetest.FreePort(t)))
	cfg.RaftAddr = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(rqlitetest.FreePort(t)))
	cfg.RaftAdvAddr = netip.AddrPort{}
	if err := n.Sup.Readdress(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if out := post(cfg.HTTPAddr, `["SELECT x FROM t"]`, "strong"); !strings.Contains(out, "[[7]]") {
		t.Fatalf("the data did not survive the move: %s", out)
	}
	h, err := rqlite.NewAdmin(cfg.HTTPAddr, c.Creds).Health(ctx)
	if err != nil || h.State != "Leader" || len(h.Members) != 1 || !strings.HasSuffix(h.Members[0].Addr, ":"+strconv.Itoa(int(cfg.RaftAddr.Port()))) {
		t.Fatalf("%+v %v", h, err)
	}
}

func TestANodeInAClusterIsNotMovedToOtherAddresses(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	n := c.Node(0)
	cfg := n.Config()
	cfg.HTTPAddr = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(rqlitetest.FreePort(t)))
	cfg.RaftAddr = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(rqlitetest.FreePort(t)))
	cfg.Join = nil
	err := n.Sup.Readdress(ctxT(t, time.Minute), cfg)
	if err == nil || !strings.Contains(err.Error(), "only a node that is alone") {
		t.Fatalf("%v", err)
	}
	// And nothing was stopped.
	if st := n.Sup.Status(); st.State != rqlite.StateRunning {
		t.Fatalf("%+v", st)
	}
}

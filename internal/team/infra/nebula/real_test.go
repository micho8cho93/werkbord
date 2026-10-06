//go:build !windows

package nebula

import (
	"os"
	"os/user"
	"strings"
	"syscall"
	"testing"
	"time"

	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
)

// These tests run the program Werkbord ships, as it is shipped (pinned, verified,
// unmodified), as several nodes on loopback. They need no privileges: the nodes run
// without a network interface, which is enough for them to find each other, handshake
// and refuse each other, and that is what is tested here. What passes over a tunnel
// once it is up is tested against Nebula's own firewall in internal/team/infra/overlay.

func discovery(s *overlay.NodeSpec) { s.Discovery, s.Relay = true, true }
func lighthouses(ps ...overlay.Peer) func(*overlay.NodeSpec) {
	return func(s *overlay.NodeSpec) { s.Lighthouses = ps }
}

func TestEveryRoleGetsAConfigurationTheRealProgramAccepts(t *testing.T) {
	dir := realBinaryDir(t)
	n := newNetwork(t)
	lh := n.issue("lh", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	cases := map[string]NebulaConfig{
		"a Workspace and Connectivity Host": n.config(lh, discovery),
		"a Workspace Host":                  n.config(n.issue("h", pki.GroupWorkspaceHost), lighthouses(n.peer(lh))),
		"a member's device":                 n.config(n.issue("m", pki.GroupMember), lighthouses(n.peer(lh))),
		"a runner":                          n.config(n.issue("r", pki.GroupMember, pki.GroupRunner), lighthouses(n.peer(lh))),
		"a relay-only host":                 n.config(n.issue("c", pki.GroupConnectivityHost), func(s *overlay.NodeSpec) { discovery(s); s.Advertise = []string{"203.0.113.7:4242"} }),
		"a node with a blocklist and relays": n.config(n.issue("b", pki.GroupMember), func(s *overlay.NodeSpec) {
			s.Lighthouses = []overlay.Peer{n.peer(lh)}
			s.Blocklist = []string{strings.Repeat("a", 64)}
			s.RelayedBy = []netipAddr{lh.addr}
		}),
	}
	for name, cfg := range cases {
		out, err := nebulaTest(t, dir, cfg)
		if err != nil {
			t.Errorf("%s: the program rejects what was rendered: %v\n%s", name, err, out)
		}
	}
	// The control: the same check does fail on a configuration that is wrong.
	bad := n.config(n.issue("x", pki.GroupMember), nil)
	p, _ := prepare(bad, time.Now(), true)
	if err := os.WriteFile(p.configPath, []byte("firewall: [this is not a mapping\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := execTest(dir, p.configPath); err == nil {
		t.Errorf("the program accepted a broken configuration: %s", out)
	}
}

func TestANodeFindsTheWorkspacesLighthouseAndTheyHandshake(t *testing.T) {
	dir := realBinaryDir(t)
	n := newNetwork(t)
	lh := n.issue("lh", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	a := n.issue("member-a", pki.GroupMember)
	sl := startReal(t, dir, n.config(lh, discovery))
	sa := startReal(t, dir, n.config(a, lighthouses(n.peer(lh))))
	waitFor(t, "the lighthouse to complete a handshake with the member's node", 60*time.Second, func() bool {
		return tailHas(sl, `"msg":"Handshake message received"`) && tailHas(sl, `"certName":"member-a"`)
	})
	waitFor(t, "the member's node to hear back", 60*time.Second, func() bool { return tailHas(sa, `"certName":"lh"`) })
	if st := sa.Status(); st.State != StateRunning {
		t.Errorf("state = %s", st.State)
	}
}

func TestSeveralLighthousesAreAllUsedAndOneMayBeOffline(t *testing.T) {
	dir := realBinaryDir(t)
	n := newNetwork(t)
	lh1 := n.issue("lh1", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	lh2 := n.issue("lh2", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	a := n.issue("a", pki.GroupMember)
	b := n.issue("b", pki.GroupMember)
	s1 := startReal(t, dir, n.config(lh1, discovery))
	s2 := startReal(t, dir, n.config(lh2, discovery))
	both := lighthouses(n.peer(lh1), n.peer(lh2))
	sa := startReal(t, dir, n.config(a, both))

	// A device that knows two lighthouses reports to both.
	waitFor(t, "the first lighthouse to hear from the node", 60*time.Second, func() bool { return tailHas(s1, `"certName":"a"`) })
	waitFor(t, "the second lighthouse to hear from the node", 60*time.Second, func() bool { return tailHas(s2, `"certName":"a"`) })

	// The first host goes offline. The node keeps running, and a node that arrives now
	// still finds the network through the second, with the first in its list and down.
	if err := s1.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	sb := startReal(t, dir, n.config(b, both))
	waitFor(t, "a new node to reach the network through the lighthouse that is still up", 90*time.Second, func() bool {
		return tailHas(s2, `"certName":"b"`) && tailHas(sb, `"certName":"lh2"`)
	})
	if sa.Status().State != StateRunning || sb.Status().State != StateRunning {
		t.Errorf("a lighthouse being offline stopped a node: %s, %s", sa.Status().State, sb.Status().State)
	}
	if !strings.Contains(strings.Join(sb.Status().Tail, "\n"), "10.77.0.2") {
		t.Error("the node was not told about the first lighthouse")
	}
}

func TestTheNetworkRefusesARevokedCertificateAndAnotherAuthoritys(t *testing.T) {
	dir := realBinaryDir(t)
	n := newNetwork(t)
	lh := n.issue("lh", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	revoked := n.issue("revoked", pki.GroupMember)
	fine := n.issue("fine", pki.GroupMember)
	other := newNetwork(t)
	other.next = 60
	stranger := other.issue("stranger", pki.GroupMember)

	sl := startReal(t, dir, n.config(lh, func(s *overlay.NodeSpec) { discovery(s); s.Blocklist = []string{revoked.fingerprint} }))
	startReal(t, dir, n.config(fine, lighthouses(n.peer(lh))))
	sr := startReal(t, dir, n.config(revoked, lighthouses(n.peer(lh))))
	ss := startReal(t, dir, other.config(stranger, lighthouses(n.peer(lh))))

	waitFor(t, "the revoked certificate to be refused", 90*time.Second, func() bool { return tailHas(sl, "certificate is in the block list") })
	waitFor(t, "the other authority's certificate to be refused", 90*time.Second, func() bool { return tailHas(sl, "could not find ca for the certificate") })
	waitFor(t, "the legitimate node to complete", 90*time.Second, func() bool { return tailHas(sl, `"certName":"fine"`) })
	// The lighthouse logs "Handshake message sent" only for a handshake it accepted.
	for _, l := range sl.Status().Tail {
		if strings.Contains(l, `"msg":"Handshake message sent"`) && (strings.Contains(l, `"certName":"revoked"`) || strings.Contains(l, `"certName":"stranger"`)) {
			t.Errorf("a handshake with a device that must be refused completed: %s", l)
		}
	}
	for _, s := range []*Supervisor{sr, ss} {
		if tailHas(s, `"certName":"lh"`) {
			t.Error("a node that must be refused heard back from the lighthouse")
		}
	}
}

func TestANewBlocklistIsAppliedWithoutRestartingTheNode(t *testing.T) {
	dir := realBinaryDir(t)
	n := newNetwork(t)
	lh := n.issue("lh", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	a := n.issue("a", pki.GroupMember)
	cfg := n.config(lh, discovery)
	sl := startReal(t, dir, cfg)
	pid := sl.Status().PID

	cfg2 := cfg
	cfg2.Node.Blocklist = []string{a.fingerprint}
	if err := sl.Reload(t.Context(), cfg2); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the node to re-read its configuration", 15*time.Second, func() bool { return tailHas(sl, `"msg":"Blocklisted certificates"`) })
	if sl.Status().PID != pid {
		t.Error("a change to the blocklist restarted the node: it is applied by a reload")
	}
	startReal(t, dir, n.config(a, lighthouses(n.peer(lh))))
	waitFor(t, "the now-revoked device to be refused", 90*time.Second, func() bool { return tailHas(sl, "certificate is in the block list") })

	// Something that is only read at start restarts it.
	cfg3 := cfg2
	cfg3.Node.Port = freeUDPPort(t)
	if err := sl.Reload(t.Context(), cfg3); err != nil {
		t.Fatal(err)
	}
	if sl.Status().PID == pid {
		t.Error("a new listening port did not restart the node")
	}
}

func TestADeadNodeIsRestartedByTheSupervisor(t *testing.T) {
	dir := realBinaryDir(t)
	n := newNetwork(t)
	lh := n.issue("lh", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	s, err := New(Options{BinaryDirs: []string{dir}, backoff: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(t.Context()) })
	if err := s.StartNebula(t.Context(), n.config(lh, discovery)); err != nil {
		t.Fatal(err)
	}
	first := s.Status().PID
	if err := syscall.Kill(first, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the node to be started again", 60*time.Second, func() bool {
		st := s.Status()
		return st.Restarts >= 1 && st.PID != 0 && st.PID != first && st.State == StateRunning
	})
}

func TestWithoutPrivilegesTheNodeSaysSoAndHowToFixIt(t *testing.T) {
	dir := realBinaryDir(t)
	if u, err := user.Current(); err == nil && u.Uid == "0" {
		t.Skip("running as root: creating the interface would succeed")
	}
	n := newNetwork(t)
	nd := n.issue("a", pki.GroupMember)
	s, _ := New(Options{BinaryDirs: []string{dir}})
	err := s.StartNebula(t.Context(), n.config(nd, func(s *overlay.NodeSpec) { s.TunDisabled = false }))
	if err == nil {
		_ = s.Stop(t.Context())
		t.Skip("this user may create network interfaces")
	}
	if !strings.Contains(err.Error(), "docs/TEAM_NETWORK.md") {
		t.Errorf("the failure does not say how to fix it: %v", err)
	}
	if s.Status().State != StateFailed || s.Status().Restarts != 0 {
		t.Errorf("status = %+v", s.Status())
	}
}

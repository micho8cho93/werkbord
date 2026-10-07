package rqlite

import (
	"net/netip"
	"strings"
	"testing"
)

func goodConfig(t *testing.T) NodeConfig {
	t.Helper()
	creds, err := NewCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return NodeConfig{DataDir: t.TempDir(), NodeID: "host-1", HTTPAddr: netip.MustParseAddrPort("127.0.0.1:4001"), RaftAddr: netip.MustParseAddrPort("127.0.0.1:4002"), Credentials: creds}
}

// The database listens on loopback or on the workspace's private network and nowhere else.
func TestANodeIsNeverBoundWhereItCouldBeReachedFromOutside(t *testing.T) {
	net10 := netip.MustParsePrefix("10.128.0.0/16")
	cases := []struct {
		name     string
		http, rf string
		network  netip.Prefix
		ok       bool
	}{
		{"loopback", "127.0.0.1:4001", "127.0.0.1:4002", netip.Prefix{}, true},
		{"IPv6 loopback", "[::1]:4001", "[::1]:4002", netip.Prefix{}, true},
		{"the workspace's network", "10.128.0.5:4001", "10.128.0.5:4002", net10, true},
		{"a private address with no network named", "192.168.1.5:4001", "192.168.1.5:4002", netip.Prefix{}, true},
		{"a private address outside the network named", "192.168.1.5:4001", "192.168.1.5:4002", net10, false},
		{"every interface", "0.0.0.0:4001", "0.0.0.0:4002", netip.Prefix{}, false},
		{"every interface, IPv6", "[::]:4001", "[::]:4002", netip.Prefix{}, false},
		{"a public address", "203.0.113.7:4001", "203.0.113.7:4002", netip.Prefix{}, false},
		{"a public Raft address", "127.0.0.1:4001", "203.0.113.7:4002", netip.Prefix{}, false},
		{"two interfaces", "127.0.0.1:4001", "10.128.0.5:4002", net10, false},
		{"no port", "127.0.0.1:0", "127.0.0.1:4002", netip.Prefix{}, false},
		{"the same address twice", "127.0.0.1:4001", "127.0.0.1:4001", netip.Prefix{}, false},
	}
	for _, c := range cases {
		cfg := goodConfig(t)
		cfg.HTTPAddr, cfg.RaftAddr, cfg.Network = netip.MustParseAddrPort(c.http), netip.MustParseAddrPort(c.rf), c.network
		if err := cfg.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: %v (want ok=%v)", c.name, err, c.ok)
		}
	}
	// An advertised address is held to the same rule.
	cfg := goodConfig(t)
	cfg.RaftAdvAddr = netip.MustParseAddrPort("198.51.100.9:4002")
	if err := cfg.Validate(); err == nil {
		t.Error("a public advertised address was accepted")
	}
}

func TestAConfigurationWithAnythingOddInItIsRefused(t *testing.T) {
	for name, mutate := range map[string]func(*NodeConfig){
		"a relative data directory":    func(c *NodeConfig) { c.DataDir = "data" },
		"an unclean data directory":    func(c *NodeConfig) { c.DataDir += "/../x" },
		"a newline in the directory":   func(c *NodeConfig) { c.DataDir += "/a\nb" },
		"a node ID with a space":       func(c *NodeConfig) { c.NodeID = "host 1" },
		"a node ID that starts with -": func(c *NodeConfig) { c.NodeID = "-fk" },
		"an empty node ID":             func(c *NodeConfig) { c.NodeID = "" },
		"a node ID that is a flag":     func(c *NodeConfig) { c.NodeID = "x -auth /dev/null" },
		"no credentials":               func(c *NodeConfig) { c.Credentials = Credentials{} },
		"a short password":             func(c *NodeConfig) { c.Credentials.App = "short" },
		"a quote in a password":        func(c *NodeConfig) { c.Credentials.Admin = strings.Repeat("a", 20) + `"` },
		"a read-only replica alone":    func(c *NodeConfig) { c.NonVoter = true },
		"joining itself":               func(c *NodeConfig) { c.Join = []netip.AddrPort{c.RaftAddr} },
		"a join address with no port":  func(c *NodeConfig) { c.Join = []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")} },
		"a negative timeout":           func(c *NodeConfig) { c.ElectionTimeout = -1 },
	} {
		cfg := goodConfig(t)
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := goodConfig(t).Validate(); err != nil {
		t.Fatal(err)
	}
}

// The command line is built from constants, numbers and checked addresses; it carries the options that keep
// Team's schema working, and a join only when asked.
func TestTheCommandLine(t *testing.T) {
	cfg := goodConfig(t)
	args := strings.Join(cfg.args(), " ")
	for _, want := range []string{"-fk", "-node-id host-1", "-http-addr 127.0.0.1:4001", "-raft-addr 127.0.0.1:4002", "-auth " + cfg.DataDir + "/auth.json", "-raft-shutdown-stepdown=true"} {
		if !strings.Contains(args, want) {
			t.Errorf("%q is not in %q", want, args)
		}
	}
	for _, not := range []string{"-join", "-raft-non-voter", "-raft-cluster-remove-shutdown", "-raft-remove-shutdown", "-bootstrap-expect"} {
		if strings.Contains(args, not) {
			t.Errorf("%q is in a first node's command line: %q", not, args)
		}
	}
	if !strings.HasSuffix(args, cfg.DataDir+"/node") {
		t.Errorf("the data directory is not last: %q", args)
	}
	cfg.Join = []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:5002"), netip.MustParseAddrPort("127.0.0.1:6002")}
	cfg.NonVoter = true
	args = strings.Join(cfg.args(), " ")
	for _, want := range []string{"-join 127.0.0.1:5002,127.0.0.1:6002", "-join-as " + UserNode, "-raft-non-voter"} {
		if !strings.Contains(args, want) {
			t.Errorf("%q is not in %q", want, args)
		}
	}
}

func TestCredentialsAreRandomAndTheAuthFileGivesEachUserLeast(t *testing.T) {
	a, _ := NewCredentials()
	b, _ := NewCredentials()
	if a == b || a.App == a.Admin || a.Admin == a.Node {
		t.Fatal("credentials repeat")
	}
	raw, err := a.authFile()
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	// The node that joins can do nothing to the data; the app cannot change the cluster.
	if strings.Count(text, `"all"`) != 1 {
		t.Errorf("exactly one user (the administrator) has every permission:\n%s", text)
	}
	if strings.Contains(text, `"remove"`) || strings.Contains(text, `"leader-ops"`) {
		t.Errorf("the application user can change the cluster:\n%s", text)
	}
}

// The admin client speaks to private addresses only.
func TestTheDatabaseClientsTalkOnlyToPrivateAddresses(t *testing.T) {
	creds, _ := NewCredentials()
	for _, bad := range []string{"203.0.113.7:4001", "8.8.8.8:80", "[2001:db8::1]:4001"} {
		a := NewAdmin(netip.MustParseAddrPort(bad), creds)
		if err := a.Alive(t.Context()); err == nil || !strings.Contains(err.Error(), "not on loopback or a private network") {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if err := NewAdmin(netip.AddrPort{}, creds).Alive(t.Context()); err == nil {
		t.Error("no address was accepted")
	}
}

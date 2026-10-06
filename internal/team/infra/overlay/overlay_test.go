package overlay_test

import (
	"net/netip"
	"strings"
	"testing"

	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
)

func spec() overlay.NodeSpec {
	return overlay.NodeSpec{Name: "dev_aaaaaaaaaaaaaaaa", Addr: netip.MustParseAddr("10.77.0.5"), Bits: 16, Port: 4242, UseRelays: true,
		CA: "/d/ca.crt", Cert: "/d/node.crt", Key: "/d/node.key", Policy: overlay.PolicyFor([]string{pki.GroupMember}, overlay.DefaultPorts()),
		Lighthouses: []overlay.Peer{
			{Addr: netip.MustParseAddr("10.77.0.2"), Endpoints: []string{"203.0.113.9:4242"}},
			{Addr: netip.MustParseAddr("10.77.0.1"), Endpoints: []string{"198.51.100.4:4242", "team.example.org:4242"}},
		}}
}

func TestRenderingIsDeterministicAndNamesEveryLighthouse(t *testing.T) {
	a, err := overlay.Render(spec())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := overlay.Render(spec())
	if string(a) != string(b) {
		t.Error("the same spec rendered two different files")
	}
	s := spec()
	s.Lighthouses[0], s.Lighthouses[1] = s.Lighthouses[1], s.Lighthouses[0]
	c, _ := overlay.Render(s)
	if string(a) != string(c) {
		t.Error("the order the lighthouses were listed in changed the file")
	}
	text := string(a)
	for _, want := range []string{`"10.77.0.1"`, `"10.77.0.2"`, `"203.0.113.9:4242"`, `"198.51.100.4:4242"`, `"team.example.org:4242"`, "am_lighthouse: false", "inbound_action: \"drop\""} {
		if !strings.Contains(text, want) {
			t.Errorf("the configuration lacks %s:\n%s", want, text)
		}
	}
	// A device that is not a lighthouse lists them; a lighthouse lists none (the network program says so).
	d := spec()
	d.Discovery, d.Relay = true, true
	out, _ := overlay.Render(d)
	if !strings.Contains(string(out), "am_lighthouse: true") || strings.Contains(string(out), "hosts:") {
		t.Errorf("a discovery host's configuration lists lighthouses or is not one:\n%s", out)
	}
}

func TestEveryRuleIsExplainedInTheFile(t *testing.T) {
	out, _ := overlay.Render(spec())
	var ports int
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "port:") && strings.Contains(line, `"7`) {
			ports++
			if !strings.Contains(line, "#") {
				t.Errorf("a rule has no reason beside it: %s", line)
			}
		}
	}
	if ports == 0 {
		t.Error("no rule with a port was rendered")
	}
}

func TestTheSpecIsValidatedBeforeAnythingIsWritten(t *testing.T) {
	for name, edit := range map[string]func(*overlay.NodeSpec){
		"no address":                                  func(s *overlay.NodeSpec) { s.Addr = netip.Addr{} },
		"an IPv6 address":                             func(s *overlay.NodeSpec) { s.Addr = netip.MustParseAddr("fd00::1") },
		"a /8":                                        func(s *overlay.NodeSpec) { s.Bits = 8 },
		"a discovery host with no port":               func(s *overlay.NodeSpec) { s.Discovery, s.Port = true, 0 },
		"a relay with no port":                        func(s *overlay.NodeSpec) { s.Relay, s.Port = true, 0 },
		"no certificate path":                         func(s *overlay.NodeSpec) { s.Cert = "" },
		"a newline in a path":                         func(s *overlay.NodeSpec) { s.Key = "/d/node.key\nsshd: {}" },
		"a newline in the name":                       func(s *overlay.NodeSpec) { s.Name = "x\nsshd:" },
		"a bad endpoint":                              func(s *overlay.NodeSpec) { s.Advertise = []string{"nonsense"} },
		"a lighthouse that is the node itself":        func(s *overlay.NodeSpec) { s.Lighthouses[0].Addr = s.Addr },
		"a repeated lighthouse":                       func(s *overlay.NodeSpec) { s.Lighthouses[1].Addr = s.Lighthouses[0].Addr },
		"a blocklist entry that is not a fingerprint": func(s *overlay.NodeSpec) { s.Blocklist = []string{"not-a-fingerprint"} },
		"metrics on a public address":                 func(s *overlay.NodeSpec) { s.Stats = "0.0.0.0:9100" },
		"a log level that does not exist":             func(s *overlay.NodeSpec) { s.LogLevel = "everything" },
		"a rule for a group that does not exist": func(s *overlay.NodeSpec) {
			s.Policy.Inbound = append(s.Policy.Inbound, overlay.Rule{Port: "22", Proto: "tcp", Group: "root"})
		},
		"a rule that opens any port": func(s *overlay.NodeSpec) {
			s.Policy.Inbound = append(s.Policy.Inbound, overlay.Rule{Port: "any", Proto: "tcp", Group: pki.GroupMember})
		},
		"a rule with a port that is not a port": func(s *overlay.NodeSpec) {
			s.Policy.Inbound = append(s.Policy.Inbound, overlay.Rule{Port: "22; drop", Proto: "tcp", Group: pki.GroupMember})
		},
		"a rule whose reason has a newline": func(s *overlay.NodeSpec) {
			s.Policy.Inbound = append(s.Policy.Inbound, overlay.Rule{Port: "22", Proto: "tcp", Group: pki.GroupMember, Why: "x\nsshd:"})
		},
	} {
		s := spec()
		s.Lighthouses = append([]overlay.Peer(nil), s.Lighthouses...)
		edit(&s)
		if _, err := overlay.Render(s); err == nil {
			t.Errorf("%s was rendered", name)
		}
	}
	// A sorted blocklist renders; so does a good one.
	s := spec()
	s.Blocklist = []string{strings.Repeat("b", 64), strings.Repeat("a", 64)}
	out, err := overlay.Render(s)
	if err != nil || strings.Index(string(out), strings.Repeat("a", 64)) > strings.Index(string(out), strings.Repeat("b", 64)) {
		t.Errorf("blocklist: %v\n%s", err, out)
	}
}

// ---- the policy ----

func TestThePolicyIsDefaultDeny(t *testing.T) {
	ports := overlay.DefaultPorts()
	every := [][]string{{}, {pki.GroupMember}, {pki.GroupMember, pki.GroupRunner}, {pki.GroupWorkspaceHost}, {pki.GroupConnectivityHost},
		{pki.GroupWorkspaceHost, pki.GroupConnectivityHost}, {pki.GroupWorkspaceHost, pki.GroupRunner, pki.GroupMember}}
	for _, groups := range every {
		p := overlay.PolicyFor(groups, ports)
		if err := p.Validate(); err != nil {
			t.Errorf("%v: %v", groups, err)
		}
		for _, r := range append(append([]overlay.Rule(nil), p.Inbound...), p.Outbound...) {
			if r.Group == "" {
				t.Errorf("%v: a rule with no group (it would match anyone): %+v", groups, r)
			}
			if r.Proto != "icmp" && (r.Port == "" || r.Port == "any" || r.Port == "0") {
				t.Errorf("%v: a rule that opens any port: %+v", groups, r)
			}
			if r.Why == "" {
				t.Errorf("%v: a rule with no reason: %+v", groups, r)
			}
		}
		out, err := overlay.Render(func() overlay.NodeSpec { s := spec(); s.Policy = p; return s }())
		if err != nil {
			t.Fatal(err)
		}
		text := string(out)
		if !strings.Contains(text, `outbound_action: "drop"`) || !strings.Contains(text, `inbound_action: "drop"`) {
			t.Errorf("%v: the default is not drop", groups)
		}
		for _, banned := range []string{"host: any", `host: "any"`, "cidr:", "local_cidr", "ca_sha", "any\n    proto: \"any\"", `proto: "any"`} {
			if strings.Contains(text, banned) {
				t.Errorf("%v: the configuration contains %s", groups, banned)
			}
		}
	}
	if p := overlay.PolicyFor(nil, ports); len(p.Inbound)+len(p.Outbound) != 0 {
		t.Errorf("a node with no group has rules: %+v", p)
	}
}

func TestWhatEachRoleMayReach(t *testing.T) {
	ports := overlay.DefaultPorts()
	member := overlay.PolicyFor([]string{pki.GroupMember, pki.GroupRunner}, ports)
	host := overlay.PolicyFor([]string{pki.GroupWorkspaceHost}, ports)
	conn := overlay.PolicyFor([]string{pki.GroupConnectivityHost}, ports)
	M, H, C := []string{pki.GroupMember}, []string{pki.GroupWorkspaceHost}, []string{pki.GroupConnectivityHost}

	cases := []struct {
		name    string
		p       overlay.Policy
		inbound bool
		proto   string
		port    int
		peer    []string
		want    bool
	}{
		{"a host accepts the API from a member", host, true, "tcp", ports.API, M, true},
		{"a host accepts the API from a host", host, true, "tcp", ports.API, H, true},
		{"a host accepts replication from a host (4001)", host, true, "tcp", 4001, H, true},
		{"a host accepts replication from a host (4002)", host, true, "tcp", 4002, H, true},
		{"a host refuses replication from a member (4001)", host, true, "tcp", 4001, M, false},
		{"a host refuses replication from a member (4002)", host, true, "tcp", 4002, M, false},
		{"a host refuses replication from a Connectivity Host", host, true, "tcp", 4001, C, false},
		{"a host refuses SSH from a member", host, true, "tcp", 22, M, false},
		{"a host refuses SSH from a host", host, true, "tcp", 22, H, false},
		{"a host refuses the device service from a member", host, true, "tcp", ports.DeviceService, M, false},
		{"a host refuses UDP on the API port", host, true, "udp", ports.API, M, false},
		{"a member's device accepts messages from a host", member, true, "tcp", ports.DeviceService, H, true},
		{"a member's device refuses messages from a member", member, true, "tcp", ports.DeviceService, M, false},
		{"a member's device refuses the API port from a member", member, true, "tcp", ports.API, M, false},
		{"a member's device refuses anything from a Connectivity Host", member, true, "tcp", ports.DeviceService, C, false},
		{"a member's device refuses SSH from a host", member, true, "tcp", 22, H, false},
		{"a member's device may start the API on a host", member, false, "tcp", ports.API, H, true},
		{"a member's device may not start anything on a member", member, false, "tcp", ports.DeviceService, M, false},
		{"a member's device may not start replication on a host", member, false, "tcp", 4001, H, false},
		{"a member's device may not start SSH on a host", member, false, "tcp", 22, H, false},
		{"a host may start replication on a host", host, false, "tcp", 4001, H, true},
		{"a host may not start replication on a member", host, false, "tcp", 4001, M, false},
		{"a host may reach a runner's device service", host, false, "tcp", ports.DeviceService, []string{pki.GroupRunner}, true},
		{"a Connectivity Host accepts no port from a member", conn, true, "tcp", ports.API, M, false},
		{"a Connectivity Host accepts no port from a host", conn, true, "tcp", 4001, H, false},
		{"a Connectivity Host accepts ping from a host", conn, true, "icmp", 0, H, true},
		{"a Connectivity Host does not answer a member's ping", conn, true, "icmp", 0, M, false},
		{"a member's device answers a host's ping", member, true, "icmp", 0, H, true},
		{"a member's device does not answer a member's ping", member, true, "icmp", 0, M, false},
	}
	for _, c := range cases {
		if got := c.p.Allows(c.inbound, c.proto, c.port, c.peer); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPortsAreWrittenOnce(t *testing.T) {
	ports := overlay.Ports{API: 9000, DeviceService: 9001, Replication: "9100-9101"}
	p := overlay.PolicyFor([]string{pki.GroupWorkspaceHost}, ports)
	if !p.Allows(true, "tcp", 9000, []string{pki.GroupMember}) || !p.Allows(true, "tcp", 9101, []string{pki.GroupWorkspaceHost}) || p.Allows(true, "tcp", overlay.APIPort, []string{pki.GroupMember}) {
		t.Error("the configured ports are not the ones opened")
	}
}

package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/domain"
)

func TestMemberDirectoryReportsConnectionsWithoutDeviceAccess(t *testing.T) {
	n := withNetwork(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	n.svc.SetClock(func() time.Time { return now })
	viewer, _ := n.member(n.owner, "Viewer")
	report := func(name string, ownerID string, connections *domain.DeviceConnections) Actor {
		t.Helper()
		invite := n.invite(n.owner, EnrollInviteInput{ForMemberID: ownerID, Capabilities: []domain.Capability{domain.CapabilityRunner}})
		joined := n.mustJoin(invite, laptop(t, name), "Ada")
		actor := n.deviceActor(joined.DeviceToken)
		p := desktopProfile()
		p.Connections = connections
		if err := n.svc.Heartbeat(bg, actor, p); err != nil {
			t.Fatal(err)
		}
		return actor
	}
	first := report("home-runner", n.owner.Member.ID, &domain.DeviceConnections{RunnerConnected: true, PhonesPaired: 2, PhonesConnected: 1})
	report("office-runner", n.owner.Member.ID, &domain.DeviceConnections{RunnerConnected: true})
	legacy := report("older-runner", n.owner.Member.ID, nil)
	gone := report("revoked-runner", n.owner.Member.ID, &domain.DeviceConnections{RunnerConnected: true, PhonesPaired: 1, PhonesConnected: 1})
	if _, err := n.svc.RevokeDevice(bg, n.owner, gone.Device.ID); err != nil {
		t.Fatal(err)
	}
	get := func() *domain.MemberConnections {
		t.Helper()
		members, err := n.svc.ListMembers(bg, viewer)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range members {
			if m.ID == n.owner.Member.ID {
				return m.Connections
			}
		}
		t.Fatal("owner missing from directory")
		return nil
	}
	c := get()
	if c.RunnersConnected != 2 || c.RunnersUnknown != 1 || c.PhonesConnected != 1 || c.PhonesPaired != 2 || !c.PhonesUnknown {
		t.Fatalf("connections: %+v", c)
	}
	// Ordinary members see aggregate availability, never device IDs, names, keys or phone grants.
	members, _ := n.svc.ListMembers(bg, viewer)
	raw, _ := json.Marshal(members)
	for _, secret := range []string{first.Device.ID, first.Device.PublicKey, "home-runner", "office-runner"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("directory leaked device metadata %q", secret)
		}
	}
	visible, err := n.svc.ListDevices(bg, viewer)
	if err != nil || len(visible) != 0 {
		t.Fatalf("directory widened device access: %+v %v", visible, err)
	}
	// Changes are persisted immediately, even inside the normal six-hour profile refresh window.
	p := desktopProfile()
	p.Connections = &domain.DeviceConnections{RunnerConnected: false, PhonesPaired: 2}
	if err := n.svc.Heartbeat(bg, first, p); err != nil {
		t.Fatal(err)
	}
	c = get()
	if c.RunnersConnected != 1 || c.RunnersDisconnected != 1 || c.PhonesConnected != 0 {
		t.Fatalf("changed report: %+v", c)
	}
	// Legacy reports stay unknown; stale devices can no longer claim a live runner or phone.
	now = now.Add(domain.DeviceOnlineWindow + time.Second)
	c = get()
	if c.RunnersConnected != 0 || c.RunnersDisconnected != 3 || c.PhonesConnected != 0 || !c.PhonesUnknown {
		t.Fatalf("stale report: %+v", c)
	}
	// Updating a legacy device replaces unknown with an explicit disconnected report.
	if err := n.svc.Heartbeat(bg, legacy, p); err != nil {
		t.Fatal(err)
	}
}

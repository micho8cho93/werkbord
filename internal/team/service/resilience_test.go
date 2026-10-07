package service

import (
	"strings"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

func desktopProfile() domain.DeviceProfile {
	return domain.DeviceProfile{Platform: "darwin", Form: domain.FormDesktop, Version: "2.7.0"}
}

// A device says it is here with its own credential, and the workspace's clock decides when.
func TestOnlyADeviceReportsForItself(t *testing.T) {
	n := withNetwork(t)
	r := n.invite(n.owner, EnrollInviteInput{Capabilities: []domain.Capability{domain.CapabilityRunner}})
	d := laptop(t, "bo-macbook")
	resp := n.mustJoin(r, d, "Bo")
	devActor := n.deviceActor(resp.DeviceToken)

	// A person's own token reports nothing about a device.
	wantErr(t, n.svc.Heartbeat(bg, n.owner, desktopProfile()), domain.ErrForbidden)

	before := time.Now().Add(-time.Second)
	if err := n.svc.Heartbeat(bg, devActor, desktopProfile()); err != nil {
		t.Fatal(err)
	}
	got, err := n.svc.GetDevice(bg, n.owner, d.DeviceID())
	if err != nil || got.LastSeenAt == nil || got.LastSeenAt.Before(before) || !got.OnlineAt(time.Now()) {
		t.Fatalf("the device was not seen: %+v %v", got, err)
	}
	// A profile that is not one is refused, and says why.
	err = n.svc.Heartbeat(bg, devActor, domain.DeviceProfile{Platform: "beos", Form: domain.FormDesktop})
	wantErr(t, err, domain.ErrInvalid)
	// The device cannot name another device: the profile is always the caller's.
	bad := desktopProfile()
	bad.DeviceID = n.host.DeviceID()
	if err := n.svc.Heartbeat(bg, devActor, bad); err != nil {
		t.Fatal(err)
	}
	rs, err := n.svc.Resilience(bg, n.owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, dh := range rs.Devices {
		if dh.DeviceID == n.host.DeviceID() && dh.Profile != nil {
			t.Fatal("a device wrote another device's profile")
		}
	}
}

func TestResilienceIsForThosePeopleWhoManageDevices(t *testing.T) {
	n := withNetwork(t)
	bo, _ := n.member(n.owner, "Bo")
	_, err := n.svc.Resilience(bg, bo)
	wantErr(t, err, domain.ErrForbidden)
	// An admin sees it; so does the owner.
	for _, a := range []Actor{n.admin("Ann"), n.owner} {
		if _, err := n.svc.Resilience(bg, a); err != nil {
			t.Fatalf("%s cannot see the resilience report: %v", a.Member.Role, err)
		}
	}
}

// A workspace that has only its first host says so, in the words an administrator can act on, and offers the way to fix it.
func TestAWorkspaceWithOneHostIsToldToAddTwoMore(t *testing.T) {
	n := withNetwork(t)
	rs, err := n.svc.Resilience(bg, n.owner)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing has spoken for the first host yet: it is a host of a workspace whose data is in one file in this test.
	var titles []string
	for _, a := range rs.Advice {
		titles = append(titles, a.Title)
	}
	if rs.WorkspaceHosts.Configured != 1 || rs.ConnectivityHosts != 1 {
		t.Fatalf("hosts %+v connectivity %d", rs.WorkspaceHosts, rs.ConnectivityHosts)
	}
	found := false
	for _, a := range rs.Advice {
		if a.Code == domain.AdviceCodeSingleFile && a.Action != nil && a.Action.Kind == "add_hosts" {
			found = true
		}
	}
	if !found {
		t.Fatalf("an administrator is not told to add hosts: %v", titles)
	}
	// The host speaks for itself, and is then online and described.
	if err := n.svc.RecordLocalHeartbeat(bg, n.owner.Workspace.ID, n.host.DeviceID(), desktopProfile()); err != nil {
		t.Fatal(err)
	}
	rs, _ = n.svc.Resilience(bg, n.owner)
	if rs.Hosts.Value != "1/1 online" {
		t.Fatalf("hosts line = %q", rs.Hosts.Value)
	}
	for _, d := range rs.Devices {
		if d.DeviceID == n.host.DeviceID() && (d.Profile == nil || !d.HostFit.Ideal) {
			t.Fatalf("the host's profile did not arrive: %+v", d)
		}
	}
}

// A sleeping host is named in the report, and a profile is not rewritten on every beat.
func TestASleepingHostIsNamedAndAProfileIsWrittenOnlyWhenItChanges(t *testing.T) {
	n := withNetwork(t)
	sleepy := desktopProfile()
	sleepy.Form, sleepy.Sleeps, sleepy.SleepEvents = domain.FormLaptop, true, 4
	if err := n.svc.RecordLocalHeartbeat(bg, n.owner.Workspace.ID, n.host.DeviceID(), sleepy); err != nil {
		t.Fatal(err)
	}
	rs, _ := n.svc.Resilience(bg, n.owner)
	found := false
	for _, a := range rs.Advice {
		if a.Code == domain.AdviceCodeHostSleeps && strings.Contains(a.Title, "ada-server") && strings.Contains(a.Title, "sleeps automatically") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the sleeping host is not named: %+v", rs.Advice)
	}
	// Beating again with the same profile does not move when it was reported.
	var first time.Time
	_ = n.db.View(bg, func(tx store.Tx) error {
		ps, err := tx.DeviceProfiles(bg, n.owner.Workspace.ID)
		first = ps[n.host.DeviceID()].ReportedAt
		return err
	})
	time.Sleep(5 * time.Millisecond)
	if err := n.svc.RecordLocalHeartbeat(bg, n.owner.Workspace.ID, n.host.DeviceID(), sleepy); err != nil {
		t.Fatal(err)
	}
	var second time.Time
	_ = n.db.View(bg, func(tx store.Tx) error {
		ps, err := tx.DeviceProfiles(bg, n.owner.Workspace.ID)
		second = ps[n.host.DeviceID()].ReportedAt
		return err
	})
	if !first.Equal(second) {
		t.Fatalf("an unchanged profile was written again: %v then %v", first, second)
	}
}

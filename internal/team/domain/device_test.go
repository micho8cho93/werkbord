package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid"
)

func goodDevice() Device {
	return Device{ID: "dev_aaaaaaaaaaaaaaaa", WorkspaceID: "tws_1", MemberID: "tmb_1", Name: "laptop",
		PublicKey: deviceid.EncodePublicKey(make([]byte, 32)), Capabilities: []Capability{}, HostStatus: HostNone, ConnectivityStatus: HostNone}
}

func TestCapabilitiesAreParsedAndClassified(t *testing.T) {
	for _, c := range Capabilities() {
		if got, err := ParseCapability(string(c)); err != nil || got != c {
			t.Errorf("%s: %v", c, err)
		}
	}
	for _, bad := range []string{"", "host", "admin", "owner", "Runner", "root"} {
		if _, err := ParseCapability(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	infra := map[Capability]bool{CapabilityWorkspaceHost: true, CapabilityConnectivityHost: true, CapabilityRunner: false}
	for c, want := range infra {
		if c.Infrastructure() != want {
			t.Errorf("%s.Infrastructure() = %v", c, c.Infrastructure())
		}
	}
	if len(Capabilities()) != len(infra) {
		t.Error("a capability is not classified")
	}
}

// Capabilities and roles are separate vocabularies: no capability is a role, no
// permission is granted for holding one, and no role is named for one.
func TestCapabilitiesAndRolesDoNotOverlap(t *testing.T) {
	for _, c := range Capabilities() {
		if Role(c).Valid() {
			t.Errorf("%s is both a capability and a role", c)
		}
		for _, p := range AllPermissions() {
			if strings.EqualFold(string(p), string(c)) {
				t.Errorf("%s is both a capability and a permission", c)
			}
		}
	}
	for _, r := range Roles() {
		if Capability(r).Valid() {
			t.Errorf("%s is both a role and a capability", r)
		}
	}
	// A device record carries no role, and a member record no capability.
	for _, f := range fieldNames(reflect.TypeOf(Device{})) {
		if f == "Role" {
			t.Error("Device has a Role")
		}
	}
	for _, f := range fieldNames(reflect.TypeOf(Member{})) {
		if strings.Contains(f, "Capabilit") || strings.Contains(f, "Host") || strings.Contains(f, "Runner") {
			t.Errorf("Member has a %s: capabilities belong to devices", f)
		}
	}
}

func fieldNames(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		out = append(out, t.Field(i).Name)
	}
	return out
}

func TestCleanCapabilitiesDedupesAndSorts(t *testing.T) {
	got, err := CleanCapabilities([]Capability{CapabilityRunner, CapabilityWorkspaceHost, CapabilityRunner})
	if err != nil || !reflect.DeepEqual(got, []Capability{CapabilityRunner, CapabilityWorkspaceHost}) {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := CleanCapabilities(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil → %#v %v (want an empty, non-nil list, so JSON says [])", got, err)
	}
	if _, err := CleanCapabilities([]Capability{"nope"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestADeviceRevokedHoldsNothing(t *testing.T) {
	d := goodDevice()
	d.Capabilities = []Capability{CapabilityRunner, CapabilityWorkspaceHost}
	d.HostStatus = HostActive
	if !d.Has(CapabilityRunner) || d.Has(CapabilityConnectivityHost) {
		t.Fatal("Has is wrong for a live device")
	}
	at := time.Now()
	d.RevokedAt = &at
	for _, c := range Capabilities() {
		if d.Has(c) {
			t.Errorf("a revoked device holds %s", c)
		}
	}
	d.LastSeenAt = &at
	if d.OnlineAt(at) {
		t.Error("a revoked device is online")
	}
}

func TestOnlineIsDerivedFromLastSeen(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	d := goodDevice()
	if d.OnlineAt(now) {
		t.Fatal("never seen, online")
	}
	for age, want := range map[time.Duration]bool{0: true, time.Minute: true, DeviceOnlineWindow: true, DeviceOnlineWindow + time.Millisecond: false, time.Hour: false} {
		seen := now.Add(-age)
		d.LastSeenAt = &seen
		if got := d.OnlineAt(now); got != want {
			t.Errorf("seen %v ago: online = %v, want %v", age, got, want)
		}
	}
}

func TestDeviceValidation(t *testing.T) {
	if err := goodDevice().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(d *Device){
		"a bad ID":                          func(d *Device) { d.ID = "laptop" },
		"a bad key":                         func(d *Device) { d.PublicKey = "AAAA" },
		"no name":                           func(d *Device) { d.Name = "  " },
		"no workspace":                      func(d *Device) { d.WorkspaceID = "" },
		"no owner":                          func(d *Device) { d.MemberID = "" },
		"an unknown capability":             func(d *Device) { d.Capabilities = []Capability{"root"} },
		"a host status without the role":    func(d *Device) { d.HostStatus = HostActive },
		"a connectivity status without it":  func(d *Device) { d.ConnectivityStatus = HostJoining },
		"the role without a status":         func(d *Device) { d.Capabilities = []Capability{CapabilityWorkspaceHost} },
		"a status that does not exist":      func(d *Device) { d.HostStatus = "great" },
		"a runner with a host status (odd)": func(d *Device) { d.Capabilities = []Capability{CapabilityRunner}; d.HostStatus = HostActive },
	}
	for name, mutate := range cases {
		d := goodDevice()
		mutate(&d)
		if err := d.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := goodDevice()
	ok.Capabilities = []Capability{CapabilityWorkspaceHost, CapabilityConnectivityHost}
	ok.HostStatus, ok.ConnectivityStatus = HostActive, HostUnavailable
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}

// What Team shares about a device is exactly this list. Adding a field is a
// decision about what Team may know, and fails here until it is made on purpose.
func TestADeviceHasExactlyTheseFields(t *testing.T) {
	want := []string{"ID", "WorkspaceID", "MemberID", "Name", "PublicKey", "Capabilities", "HostStatus", "ConnectivityStatus", "LastSeenAt", "RevokedAt", "CreatedAt", "UpdatedAt"}
	if got := fieldNames(reflect.TypeOf(Device{})); !reflect.DeepEqual(got, want) {
		t.Fatalf("Device fields = %v\nwant %v", got, want)
	}
	b, _ := json.Marshal(goodDevice())
	for _, banned := range []string{"private", "secret", "token", "password", "credential", "path", "env"} {
		if strings.Contains(strings.ToLower(string(b)), banned) {
			t.Errorf("a device serialises %q", banned)
		}
	}
}

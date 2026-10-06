package domain

import (
	"fmt"
	"sort"
	"time"

	"devboard/internal/deviceid"
)

// Capability is something a *device* does for a workspace. It belongs to the
// device, not to the person who owns it, and is independent of the person's Role:
//
//   - A Workspace Host holds a replica of the workspace's shared state.
//   - A Connectivity Host is a publicly reachable machine the workspace's devices
//     use to find and reach one another.
//   - A Runner executes its owner's work, with its owner's credentials, on its
//     owner's machine.
//
// Team coordinates these; it never executes anything on any of them.
type Capability string

const (
	CapabilityWorkspaceHost    Capability = "workspace_host"
	CapabilityConnectivityHost Capability = "connectivity_host"
	CapabilityRunner           Capability = "runner"
)

// Capabilities lists every capability.
func Capabilities() []Capability {
	return []Capability{CapabilityWorkspaceHost, CapabilityConnectivityHost, CapabilityRunner}
}

// Valid reports whether c is a capability that exists.
func (c Capability) Valid() bool {
	for _, k := range Capabilities() {
		if c == k {
			return true
		}
	}
	return false
}

// Infrastructure reports whether c is a host capability: the workspace's own
// machinery, which only someone who may manage devices can grant. A runner is
// not: a person's own machine running their own work is theirs to declare.
func (c Capability) Infrastructure() bool {
	return c == CapabilityWorkspaceHost || c == CapabilityConnectivityHost
}

// ParseCapability turns text into a capability that exists.
func ParseCapability(s string) (Capability, error) {
	c := Capability(s)
	if !c.Valid() {
		return "", invalid("capability %q does not exist (capabilities: workspace_host, connectivity_host, runner)", s)
	}
	return c, nil
}

// HostStatus is where a device is in a host role it has been granted: the same
// states for Workspace Host and for Connectivity Host.
type HostStatus string

const (
	// HostNone: the device does not hold the role.
	HostNone HostStatus = "none"
	// HostJoining: granted, and not yet confirmed working.
	HostJoining HostStatus = "joining"
	// HostActive: serving the role.
	HostActive HostStatus = "active"
	// HostUnavailable: holds the role but is not serving it now.
	HostUnavailable HostStatus = "unavailable"
)

// Valid reports whether s is a host status that exists.
func (s HostStatus) Valid() bool {
	switch s {
	case HostNone, HostJoining, HostActive, HostUnavailable:
		return true
	}
	return false
}

// DeviceOnlineWindow is how recently a device must have been seen to count as
// online. The registry records when a device was last seen; "online" is derived
// from it, so a device that stops reporting goes offline without anyone writing.
const DeviceOnlineWindow = 2 * time.Minute

// MaxDevicesPerMember bounds how many unrevoked devices one member may have, so
// registration is not a way to fill the registry.
const MaxDevicesPerMember = 50

// Device is a machine registered in a workspace. It holds only what other members
// and other devices may know: the device's ID, who owns it, its name, its public
// signing key, what it does for the workspace and when it was last seen. It has
// no private key (that never leaves the device), no credential for Git, an agent
// or a model, no path on the machine and no environment, and nothing in the
// registry's schema could hold one.
type Device struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	// MemberID is the member who owns the device.
	MemberID string `json:"memberId"`
	Name     string `json:"name"`
	// PublicKey is the device's public signing key (deviceid.EncodePublicKey).
	PublicKey    string       `json:"publicKey"`
	Capabilities []Capability `json:"capabilities"`
	// HostStatus is the state of the Workspace Host role, ConnectivityStatus of the
	// Connectivity Host role; HostNone when the device does not hold it.
	HostStatus         HostStatus `json:"hostStatus"`
	ConnectivityStatus HostStatus `json:"connectivityStatus"`
	LastSeenAt         *time.Time `json:"lastSeenAt,omitempty"`
	// RevokedAt is set when the device was revoked; revocation is permanent.
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// Revoked reports whether the device has been revoked.
func (d Device) Revoked() bool { return d.RevokedAt != nil }

// Has reports whether the device holds capability c. A revoked device holds none.
func (d Device) Has(c Capability) bool {
	if d.Revoked() {
		return false
	}
	for _, have := range d.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// OnlineAt reports whether the device was seen within DeviceOnlineWindow of now.
// A revoked device is never online.
func (d Device) OnlineAt(now time.Time) bool {
	return !d.Revoked() && d.LastSeenAt != nil && now.Sub(*d.LastSeenAt) <= DeviceOnlineWindow
}

// Key returns the device's parsed public key.
func (d Device) Key() (deviceid.PublicKey, error) { return deviceid.ParsePublicKey(d.PublicKey) }

// CleanCapabilities checks a list of capabilities and returns it without
// duplicates, in a stable order.
func CleanCapabilities(in []Capability) ([]Capability, error) {
	seen := map[Capability]bool{}
	out := []Capability{}
	for _, c := range in {
		if !c.Valid() {
			_, err := ParseCapability(string(c))
			return nil, err
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// ValidateRoleStatuses checks that a device holds a host role only if it has the
// capability for it, and does not hold a status for a role it was not granted.
func (d Device) ValidateRoleStatuses() error {
	for _, r := range []struct {
		c      Capability
		status HostStatus
	}{{CapabilityWorkspaceHost, d.HostStatus}, {CapabilityConnectivityHost, d.ConnectivityStatus}} {
		if !r.status.Valid() {
			return invalid("host status %q does not exist", r.status)
		}
		holds := false
		for _, have := range d.Capabilities {
			holds = holds || have == r.c
		}
		if holds && r.status == HostNone && !d.Revoked() {
			return invalid("a device granted %s has a status for it (joining, active or unavailable)", r.c)
		}
		if !holds && r.status != HostNone {
			return invalid("a device without %s cannot have status %q for it", r.c, r.status)
		}
	}
	return nil
}

// Validate checks a device record is well formed: the ID and key are what they
// should be, the name usable, the capabilities and statuses consistent.
func (d Device) Validate() error {
	if err := (deviceid.Public{ID: deviceid.ID(d.ID), Name: d.Name, PublicKey: d.PublicKey}).Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if d.WorkspaceID == "" || d.MemberID == "" {
		return invalid("a device belongs to a workspace and a member")
	}
	if _, err := CleanCapabilities(d.Capabilities); err != nil {
		return err
	}
	return d.ValidateRoleStatuses()
}

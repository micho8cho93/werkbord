package service

import (
	"context"
	"errors"
	"fmt"

	"devboard/internal/deviceid"
	"devboard/internal/envelope"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// The device registry: which machines take part in the workspace, who owns each,
// what each does for it, and which have been revoked.
//
// Everything here is coordination metadata. A device's private key never reaches
// Team (registration carries the public key and a proof that the device holds the
// private one), and nothing here contacts a device or runs anything on one:
// "Runner" and the host capabilities are facts Team records about a device, which
// the devices themselves act on. A device's capabilities are independent of its
// owner's role, and are decided by who may manage devices, not by who owns them.

// DeviceInput is what registering a device takes.
type DeviceInput struct {
	// Device is the device's public record: its ID, name and public key.
	Device deviceid.Public
	// Proof is the device's signature over deviceid.RegistrationStatement, which
	// shows it holds the key. Registering a key one does not hold is refused.
	Proof []byte
	// Capabilities the device is to have. A member may declare their own device a
	// Runner; the host capabilities need devices.manage.
	Capabilities []domain.Capability
}

func initialStatus(caps []domain.Capability, c domain.Capability) domain.HostStatus {
	for _, have := range caps {
		if have == c {
			return domain.HostJoining
		}
	}
	return domain.HostNone
}

// RegisterDevice records a device the actor owns.
func (s *Service) RegisterDevice(ctx context.Context, a Actor, in DeviceInput) (domain.Device, error) {
	if err := a.require(domain.PermDevicesOwn, "register devices"); err != nil {
		return domain.Device{}, err
	}
	name, err := deviceid.CleanName(in.Device.Name)
	if err != nil {
		return domain.Device{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	in.Device.Name = name
	if err := in.Device.Validate(); err != nil {
		return domain.Device{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if err := deviceid.VerifyRegistration(a.Workspace.ID, a.Member.ID, in.Device, in.Proof); err != nil {
		return domain.Device{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	caps, err := domain.CleanCapabilities(in.Capabilities)
	if err != nil {
		return domain.Device{}, err
	}
	for _, c := range caps {
		if c.Infrastructure() {
			if err := a.require(domain.PermDevicesManage, "give a device the "+string(c)+" capability"); err != nil {
				return domain.Device{}, err
			}
		}
	}
	now := s.stamp()
	d := domain.Device{
		ID: string(in.Device.ID), WorkspaceID: a.Workspace.ID, MemberID: a.Member.ID, Name: name, PublicKey: in.Device.PublicKey,
		Capabilities: caps, HostStatus: initialStatus(caps, domain.CapabilityWorkspaceHost), ConnectivityStatus: initialStatus(caps, domain.CapabilityConnectivityHost),
		CreatedAt: now, UpdatedAt: now,
	}
	err = s.db.Update(ctx, func(tx store.Tx) error {
		own, err := tx.Devices(ctx, a.Workspace.ID, a.Member.ID)
		if err != nil {
			return err
		}
		live := 0
		for _, o := range own {
			if !o.Revoked() {
				live++
			}
		}
		if live >= domain.MaxDevicesPerMember {
			return fmt.Errorf("%w: a member may have at most %d devices; revoke one first", domain.ErrConflict, domain.MaxDevicesPerMember)
		}
		return tx.InsertDevice(ctx, d)
	})
	s.changed(a.Workspace.ID, err)
	if err != nil {
		return domain.Device{}, err
	}
	return d, nil
}

// deviceFor loads a device the actor may see: their own, or any with
// devices.view_all. One they may not see is reported as not found.
func (s *Service) deviceFor(ctx context.Context, tx store.Tx, a Actor, id string) (domain.Device, error) {
	d, err := tx.Device(ctx, a.Workspace.ID, id)
	if err != nil {
		return d, err
	}
	if d.MemberID == a.Member.ID && a.Member.Can(domain.PermDevicesOwn) {
		return d, nil
	}
	if a.Member.Can(domain.PermDevicesViewAll) {
		return d, nil
	}
	return domain.Device{}, fmt.Errorf("%w: device", domain.ErrNotFound)
}

// canChange reports whether the actor may change the device at all (rename it,
// declare it a runner, revoke it): the owner may, and so may anyone who manages devices.
func canChange(a Actor, d domain.Device) bool {
	return (d.MemberID == a.Member.ID && a.Member.Can(domain.PermDevicesOwn)) || a.Member.Can(domain.PermDevicesManage)
}

// ListDevices lists the devices the actor may see: all of them with
// devices.view_all, otherwise their own.
func (s *Service) ListDevices(ctx context.Context, a Actor) ([]domain.Device, error) {
	only := a.Member.ID
	if a.Member.Can(domain.PermDevicesViewAll) {
		only = ""
	} else if err := a.require(domain.PermDevicesOwn, "see devices"); err != nil {
		return nil, err
	}
	var out []domain.Device
	err := s.db.View(ctx, func(tx store.Tx) (err error) { out, err = tx.Devices(ctx, a.Workspace.ID, only); return })
	return out, err
}

// GetDevice returns a device the actor may see.
func (s *Service) GetDevice(ctx context.Context, a Actor, id string) (domain.Device, error) {
	var d domain.Device
	err := s.db.View(ctx, func(tx store.Tx) (err error) { d, err = s.deviceFor(ctx, tx, a, id); return })
	return d, err
}

// SetDeviceCapabilities replaces a device's capabilities. Its owner may add or
// remove Runner on it; the host capabilities can be changed only by someone who
// manages devices. Granting a host capability starts that role as joining;
// removing it ends it.
func (s *Service) SetDeviceCapabilities(ctx context.Context, a Actor, id string, want []domain.Capability) (domain.Device, error) {
	want, err := domain.CleanCapabilities(want)
	if err != nil {
		return domain.Device{}, err
	}
	var out domain.Device
	err = s.db.Update(ctx, func(tx store.Tx) error {
		d, err := s.deviceFor(ctx, tx, a, id)
		if err != nil {
			return err
		}
		if !canChange(a, d) {
			return forbidden("change this device")
		}
		if d.Revoked() {
			return fmt.Errorf("%w: the device has been revoked", domain.ErrConflict)
		}
		have := map[domain.Capability]bool{}
		for _, c := range d.Capabilities {
			have[c] = true
		}
		wanted := map[domain.Capability]bool{}
		for _, c := range want {
			wanted[c] = true
			if c.Infrastructure() && !have[c] {
				if err := a.require(domain.PermDevicesManage, "give a device the "+string(c)+" capability"); err != nil {
					return err
				}
			}
		}
		for c := range have {
			if c.Infrastructure() && !wanted[c] {
				if err := a.require(domain.PermDevicesManage, "take the "+string(c)+" capability from a device"); err != nil {
					return err
				}
			}
		}
		d.Capabilities = want
		d.HostStatus = statusAfter(d.HostStatus, wanted[domain.CapabilityWorkspaceHost])
		d.ConnectivityStatus = statusAfter(d.ConnectivityStatus, wanted[domain.CapabilityConnectivityHost])
		d.UpdatedAt = s.stamp()
		if err := tx.SaveDevice(ctx, d); err != nil {
			return err
		}
		out = d
		return nil
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

// statusAfter is a role's status once the capability for it has been granted or
// removed: a role newly held is joining, one given up is none, one kept stays.
func statusAfter(current domain.HostStatus, holds bool) domain.HostStatus {
	switch {
	case !holds:
		return domain.HostNone
	case current == domain.HostNone:
		return domain.HostJoining
	}
	return current
}

// SetDeviceRoleStatus records where a device is in a host role it holds. Only
// someone who manages devices does, for now; once devices report for themselves it
// is their signed message that does.
func (s *Service) SetDeviceRoleStatus(ctx context.Context, a Actor, id string, role domain.Capability, status domain.HostStatus) (domain.Device, error) {
	if err := a.require(domain.PermDevicesManage, "set a device's host status"); err != nil {
		return domain.Device{}, err
	}
	if !role.Infrastructure() {
		return domain.Device{}, fmt.Errorf("%w: only the host roles have a status", domain.ErrInvalid)
	}
	if !status.Valid() {
		return domain.Device{}, fmt.Errorf("%w: host status %q does not exist", domain.ErrInvalid, status)
	}
	var out domain.Device
	err := s.db.Update(ctx, func(tx store.Tx) error {
		d, err := tx.Device(ctx, a.Workspace.ID, id)
		if err != nil {
			return err
		}
		if role == domain.CapabilityWorkspaceHost {
			d.HostStatus = status
		} else {
			d.ConnectivityStatus = status
		}
		if err := d.ValidateRoleStatuses(); err != nil {
			return err
		}
		d.UpdatedAt = s.stamp()
		if err := tx.SaveDevice(ctx, d); err != nil {
			return err
		}
		out = d
		return nil
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

// RenameDevice changes a device's display name. Its ID and key stay.
func (s *Service) RenameDevice(ctx context.Context, a Actor, id, name string) (domain.Device, error) {
	name, err := deviceid.CleanName(name)
	if err != nil {
		return domain.Device{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	var out domain.Device
	err = s.db.Update(ctx, func(tx store.Tx) error {
		d, err := s.deviceFor(ctx, tx, a, id)
		if err != nil {
			return err
		}
		if !canChange(a, d) {
			return forbidden("rename this device")
		}
		d.Name, d.UpdatedAt = name, s.stamp()
		if err := tx.SaveDevice(ctx, d); err != nil {
			return err
		}
		out = d
		return nil
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

// RevokeDevice permanently revokes a device: it can no longer sign anything a
// verifier accepts, it holds no capability, and it is not a host. Its owner may
// revoke it (a lost laptop), and so may anyone who manages devices. Revoking a
// revoked device is not an error.
func (s *Service) RevokeDevice(ctx context.Context, a Actor, id string) (domain.Device, error) {
	var out domain.Device
	err := s.db.Update(ctx, func(tx store.Tx) error {
		d, err := s.deviceFor(ctx, tx, a, id)
		if err != nil {
			return err
		}
		if !canChange(a, d) {
			return forbidden("revoke this device")
		}
		if err := s.refuseIfClusterMember(d); err != nil {
			return err
		}
		if err := s.revokeDevice(ctx, tx, a.Workspace.ID, id); err != nil {
			return err
		}
		out, err = tx.Device(ctx, a.Workspace.ID, id)
		return err
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

// revokeDevice ends a device in both layers at once, in one transaction.
//
// The application layer is authoritative and immediate: the device is marked revoked
// (so no signed message from it is accepted, and Authenticate refuses its credential)
// and its credential is deleted. The network layer follows as defence in depth: every
// certificate the workspace issued to it is marked revoked, which puts its fingerprint
// on the blocklist every host gives its network program, so the network stops
// accepting it too. If a host has not yet reloaded, the device may still send packets
// to it; they reach an API that no longer knows who it is.
func (s *Service) revokeDevice(ctx context.Context, tx store.Tx, workspaceID, id string) error {
	now := s.stamp()
	if _, err := tx.RevokeDevice(ctx, workspaceID, id, now); err != nil {
		return err
	}
	if err := tx.DeleteDeviceCredential(ctx, workspaceID, id); err != nil {
		return err
	}
	if _, err := tx.RevokeCertificates(ctx, workspaceID, id, now); err != nil {
		return err
	}
	if _, err := tx.Provision(ctx, workspaceID, id); err == nil {
		if err := tx.ClearProvision(ctx, workspaceID, id, now); err != nil {
			return err
		}
	}
	return nil
}

// revokeDevicesOf revokes every device a member has, as revokeDevice does for one.
func (s *Service) revokeDevicesOf(ctx context.Context, tx store.Tx, workspaceID, memberID string) error {
	devs, err := tx.Devices(ctx, workspaceID, memberID)
	if err != nil {
		return err
	}
	for _, d := range devs {
		if err := s.refuseIfClusterMember(d); err != nil {
			return err
		}
		if err := s.revokeDevice(ctx, tx, workspaceID, d.ID); err != nil {
			return err
		}
	}
	return nil
}

// RecordDeviceSeen notes that a device has been seen, now. The clock is the
// server's, never the device's: a device cannot claim to have been seen later.
func (s *Service) RecordDeviceSeen(ctx context.Context, a Actor, id string) error {
	return s.db.Update(ctx, func(tx store.Tx) error {
		d, err := s.deviceFor(ctx, tx, a, id)
		if err != nil {
			return err
		}
		if !canChange(a, d) {
			return forbidden("report for this device")
		}
		return tx.RecordDeviceSeen(ctx, a.Workspace.ID, id, s.stamp())
	})
}

// DevicesWithCapability lists the workspace's unrevoked devices that hold a
// capability, for anyone allowed to see every device.
func (s *Service) DevicesWithCapability(ctx context.Context, a Actor, c domain.Capability) ([]domain.Device, error) {
	if err := a.require(domain.PermDevicesViewAll, "see the workspace's devices"); err != nil {
		return nil, err
	}
	if !c.Valid() {
		_, err := domain.ParseCapability(string(c))
		return nil, err
	}
	var out []domain.Device
	err := s.db.View(ctx, func(tx store.Tx) (err error) { out, err = tx.DevicesWithCapability(ctx, a.Workspace.ID, c); return })
	return out, err
}

// DeviceDirectory is the registry as a signed-message verifier sees it
// (envelope.Directory): it answers with a device's owner and public key, and
// says if it has been revoked. A device that is not registered in the workspace,
// or whose owner has left it, is unknown.
func (s *Service) DeviceDirectory() envelope.Directory { return deviceDirectory{s} }

type deviceDirectory struct{ s *Service }

func (d deviceDirectory) Device(ctx context.Context, workspaceID, deviceID string) (envelope.Device, error) {
	var dev domain.Device
	err := d.s.db.View(ctx, func(tx store.Tx) (err error) { dev, err = tx.Device(ctx, workspaceID, deviceID); return })
	if errors.Is(err, domain.ErrNotFound) {
		return envelope.Device{}, envelope.ErrUnknownKey
	}
	if err != nil {
		return envelope.Device{}, err
	}
	key, err := dev.Key()
	if err != nil {
		return envelope.Device{}, envelope.ErrUnknownKey
	}
	return envelope.Device{ID: dev.ID, WorkspaceID: dev.WorkspaceID, OwnerID: dev.MemberID, PublicKey: key, Revoked: dev.Revoked()}, nil
}

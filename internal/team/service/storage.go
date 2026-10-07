package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// The workspace's data is held by its Workspace Hosts. This file is the workspace's side of that: what an
// administrator is told about it, and the two things an administrator does to it, adding a host to the
// cluster and taking one out. The cluster itself (its nodes, its quorum, its membership changes) is
// rqlite's, and the host's own supervision of it is infrastructure (internal/team/infra/rqlite); the service
// is written against StorageControl and knows neither.
//
// A host's role is independent of a person's: whoever may manage devices (an Admin, or the Owner) may add and
// remove hosts, and a host can be a member's always-on machine without that member being able to do anything
// more than before.

// StoragePlan is what a host that is being added needs to take part in the database cluster. The
// credentials come from the host that makes the plan and are sealed to the new host with the rest of its
// secrets; they never leave in any other form.
type StoragePlan struct {
	ClusterID                                string
	AppPassword, AdminPassword, NodePassword string
	// NodeID, HTTPAddr and RaftAddr are the new host's node's name and where it listens.
	NodeID, HTTPAddr, RaftAddr string
	// JoinRaft are the Raft addresses of the voting hosts it joins through.
	JoinRaft []string
}

// HostToAdd is a device that is to become a Workspace Host.
type HostToAdd struct {
	// NodeID is the device's ID; OverlayAddr its address on the workspace's private network.
	NodeID, OverlayAddr string
	// ExistingHosts are the overlay addresses of the Workspace Hosts that hold the data now.
	ExistingHosts []string
}

// StorageControl is what the service needs of the machinery that holds the workspace's data. It exists only
// on a Workspace Host that holds a copy in a cluster; a host that keeps its data in one file has none, and the
// operations that need it say so.
type StorageControl interface {
	// Status is the storage as it is now.
	Status(ctx context.Context) domain.StorageStatus
	// PlanHost prepares the cluster for a host to join, and returns what the host needs. The cluster must be able
	// to take a membership change (a quorum answers). It may have to move this host's own node onto the
	// workspace's private network, which a workspace that began on one host has not done yet.
	PlanHost(ctx context.Context, h HostToAdd) (StoragePlan, error)
	// RemoveHost takes a node out of the cluster with a membership change, after checking that a quorum survives
	// it. It returns a warning for the administrator when the arrangement it leaves is a weaker one. A node that is
	// not in the cluster (already removed) is not an error.
	RemoveHost(ctx context.Context, nodeID string) (warning string, err error)
	// Backup takes a backup now, to the configured destination.
	Backup(ctx context.Context) (domain.BackupStatus, error)
}

// SetStorage gives the service the control of the workspace's replicated database. A host that keeps the data
// in one file leaves it nil.
func (s *Service) SetStorage(c StorageControl) { s.storage = c }

// StorageReplicated reports whether this host's workspace keeps its data in a cluster.
func (s *Service) StorageReplicated() bool { return s.storage != nil }

// RemoteChanged is called when this host's copy of the data has taken in writes made through another host, so
// that everything waiting for the workspace to change looks again.
func (s *Service) RemoteChanged() { s.hub.notifyAll() }

func (s *Service) needStorage() (StorageControl, error) {
	if s.storage == nil {
		return nil, fmt.Errorf("%w: this workspace keeps its data in one file on this host, not in a cluster of Workspace Hosts (see docs/TEAM_STORAGE.md to move it)", domain.ErrConflict)
	}
	return s.storage, nil
}

// StorageStatus reports the workspace's storage to someone who manages devices: how many hosts hold the data,
// whether a quorum answers, and whether the workspace is read-only.
func (s *Service) StorageStatus(ctx context.Context, a Actor) (domain.StorageStatus, error) {
	if err := a.require(domain.PermDevicesViewAll, "see where the workspace's data is kept"); err != nil {
		return domain.StorageStatus{}, err
	}
	return s.storageStatus(ctx), nil
}

// StorageBrief is what anyone may be told about the storage: whether it is accepting changes.
type StorageBrief struct {
	State    domain.StorageState `json:"state"`
	ReadOnly bool                `json:"readOnly"`
	Reason   string              `json:"reason,omitempty"`
}

// StorageBriefStatus is the storage in a form that holds nothing about the hosts.
func (s *Service) StorageBriefStatus(ctx context.Context) StorageBrief {
	st := s.storageStatus(ctx)
	return StorageBrief{State: st.State, ReadOnly: st.ReadOnly, Reason: st.Reason}
}

func (s *Service) storageStatus(ctx context.Context) domain.StorageStatus {
	if s.storage == nil {
		return domain.StorageStatus{State: domain.StorageSingleFile, Writable: true, Hosts: []domain.StorageHost{},
			Notes: []string{"This workspace keeps its data in one file on one host. It has no copy but its backups, and it stops if this host does. docs/TEAM_STORAGE.md says how to move it into a cluster."}}
	}
	return s.storage.Status(ctx)
}

// StorageBackup takes a backup now.
func (s *Service) StorageBackup(ctx context.Context, a Actor) (domain.BackupStatus, error) {
	if err := a.require(domain.PermDevicesManage, "back up the workspace"); err != nil {
		return domain.BackupStatus{}, err
	}
	c, err := s.needStorage()
	if err != nil {
		return domain.BackupStatus{}, err
	}
	return c.Backup(ctx)
}

// ---- adding a host ----

// existingHostAddrs lists the overlay addresses of the devices that are Workspace Hosts now, but for one.
func existingHostAddrs(ctx context.Context, tx store.Tx, workspaceID, except string) ([]string, error) {
	hosts, err := tx.DevicesWithCapability(ctx, workspaceID, domain.CapabilityWorkspaceHost)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, h := range hosts {
		if h.ID == except || h.HostStatus != domain.HostActive {
			continue
		}
		n, err := tx.DeviceNetwork(ctx, workspaceID, h.ID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, n.OverlayAddr)
	}
	sort.Strings(out)
	return out, nil
}

// storagePlanFor prepares the cluster for the device to join, if the workspace keeps its data in one. It is
// done before the write that records the provision, because it may take a while and must not be repeated with it.
func (s *Service) storagePlanFor(ctx context.Context, a Actor, deviceID string) (*StoragePlan, error) {
	if s.storage == nil {
		return nil, nil
	}
	var existing []string
	var overlay string
	err := s.db.View(ctx, func(tx store.Tx) error {
		dev, err := tx.Device(ctx, a.Workspace.ID, deviceID)
		if err != nil {
			return err
		}
		if dev.Revoked() || !dev.Has(domain.CapabilityWorkspaceHost) {
			return fmt.Errorf("%w: the device must be an unrevoked device that has been given the Workspace Host capability", domain.ErrConflict)
		}
		n, err := tx.DeviceNetwork(ctx, a.Workspace.ID, deviceID)
		if err != nil {
			return err
		}
		overlay = n.OverlayAddr
		existing, err = existingHostAddrs(ctx, tx, a.Workspace.ID, deviceID)
		return err
	})
	if err != nil {
		return nil, err
	}
	plan, err := s.storage.PlanHost(ctx, HostToAdd{NodeID: deviceID, OverlayAddr: overlay, ExistingHosts: existing})
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// ActivateLocalHost marks the Workspace Host that runs this server active: its node has joined the cluster,
// caught up, and been checked against the cluster's data. It is the host speaking for itself, once it has
// verified that; it is not reachable over HTTP. Until then the host is "joining", which is what the registry
// and the status say.
func (s *Service) ActivateLocalHost(ctx context.Context, workspaceID, deviceID string) error {
	err := s.db.Update(ctx, func(tx store.Tx) error {
		dev, err := tx.Device(ctx, workspaceID, deviceID)
		if err != nil {
			return err
		}
		if dev.Revoked() || !dev.Has(domain.CapabilityWorkspaceHost) || dev.HostStatus == domain.HostActive {
			return nil
		}
		dev.HostStatus, dev.UpdatedAt = domain.HostActive, s.stamp()
		return tx.SaveDevice(ctx, dev)
	})
	s.changed(workspaceID, err)
	return err
}

// ---- removing a host ----

// HostRemoval is what removing a host did.
type HostRemoval struct {
	Device domain.Device `json:"device"`
	// Warning is set when the arrangement that is left is a weaker one than before.
	Warning string `json:"warning,omitempty"`
}

// RemoveWorkspaceHost takes a device out of the Workspace Hosts: it leaves the database cluster by a real
// membership change, which is made only if a quorum of the voting hosts answers now and a quorum of those that remain
// will (so the workspace is never left unable to write), and then it loses the capability. It is not revoked, and it
// stays on the network; whoever must be put out of the workspace is revoked as well. Removing a host that is
// not in the cluster any more only records that.
func (s *Service) RemoveWorkspaceHost(ctx context.Context, a Actor, deviceID string) (HostRemoval, error) {
	if err := a.require(domain.PermDevicesManage, "remove a Workspace Host"); err != nil {
		return HostRemoval{}, err
	}
	c, err := s.needStorage()
	if err != nil {
		return HostRemoval{}, err
	}
	var dev domain.Device
	if err := s.db.View(ctx, func(tx store.Tx) (err error) { dev, err = tx.Device(ctx, a.Workspace.ID, deviceID); return }); err != nil {
		return HostRemoval{}, err
	}
	if !dev.Has(domain.CapabilityWorkspaceHost) {
		return HostRemoval{}, fmt.Errorf("%w: %s is not a Workspace Host", domain.ErrConflict, dev.Name)
	}
	warning, err := c.RemoveHost(ctx, deviceID)
	if err != nil {
		return HostRemoval{}, err
	}
	var out domain.Device
	err = s.db.Update(ctx, func(tx store.Tx) error {
		d, err := tx.Device(ctx, a.Workspace.ID, deviceID)
		if err != nil {
			return err
		}
		var caps []domain.Capability
		for _, c := range d.Capabilities {
			if c != domain.CapabilityWorkspaceHost {
				caps = append(caps, c)
			}
		}
		d.Capabilities, d.HostStatus, d.UpdatedAt = caps, domain.HostNone, s.stamp()
		if caps == nil {
			d.Capabilities = []domain.Capability{}
		}
		if err := tx.ClearProvision(ctx, a.Workspace.ID, deviceID, s.stamp()); err != nil {
			return err
		}
		if err := tx.SaveDevice(ctx, d); err != nil {
			return err
		}
		out = d
		return nil
	})
	s.changed(a.Workspace.ID, err)
	if err != nil {
		return HostRemoval{}, fmt.Errorf("%s left the database cluster, but recording that failed: %w (run the removal again to record it)", dev.Name, err)
	}
	return HostRemoval{Device: out, Warning: warning}, nil
}

// refuseIfClusterMember stops a device that is a member of the database cluster from being revoked or its owner
// removed while it is one: its removal from the cluster is a membership change that is checked, and a device
// that vanished from the registry while still voting would leave the cluster counting a host that nothing could ask.
func (s *Service) refuseIfClusterMember(d domain.Device) error {
	if s.storage != nil && d.Has(domain.CapabilityWorkspaceHost) && d.HostStatus != domain.HostNone && !d.Revoked() {
		return fmt.Errorf("%w: %s is one of the workspace's Workspace Hosts and holds a copy of its data; take it out of the hosts first (a host removal is checked so that the workspace can still be written to), then revoke it", domain.ErrConflict, d.Name)
	}
	return nil
}

package server

import (
	"context"
	"fmt"
	"log/slog"

	"devboard/internal/team/config"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/service"
)

// FirstHostParams are what a person gives to start a workspace on their own computer: its name and who they are.
// Everything else (the workspace's keys, the device's own keys, the first administrator, the private network, the
// database, this computer as the first Workspace Host) is made for them.
type FirstHostParams struct {
	Name  string
	Owner string
	Email string
	// Network is how the private network is made: whether this host is a Connectivity Host (auto: when an address it
	// advertises could be reached from outside) and whether an administrator approves every device.
	Network NetworkOptions
}

// FirstHost is a workspace that was just made.
type FirstHost struct {
	Created service.Created
	Network NetworkCreated
}

// CreateFirstHost makes a new workspace on this computer, as its first Workspace Host, in one step that either completes or
// leaves nothing behind:
//
//   - this computer's own device keys (application, network, sealing), kept sealed in its vault;
//   - the workspace, and its owner, who is its first administrator;
//   - the workspace's data, in a cluster of one host that other hosts can join later;
//   - the workspace's own trust identity and private network, with this computer registered as a device of the owner, a
//     Workspace Host and, when it can be reached from outside, a Connectivity Host;
//   - a credential for this computer's device to speak to its own workspace with, sealed in the vault.
//
// cfg.Endpoints are the addresses at which this computer can be reached by the devices that will join. A data directory that
// already holds a workspace is refused.
func CreateFirstHost(ctx context.Context, cfg config.Config, log *slog.Logger, p FirstHostParams) (out FirstHost, err error) {
	if err := cfg.Validate(); err != nil {
		return FirstHost{}, err
	}
	if cfg.Storage != config.StorageReplicated {
		return FirstHost{}, fmt.Errorf("a workspace made this way keeps its data in a cluster of Workspace Hosts, not in %s", cfg.Storage)
	}
	keys, err := pki.NewHostKeys()
	if err != nil {
		return FirstHost{}, err
	}
	w, err := CreateStorage(ctx, cfg, log, keys.DeviceID())
	if err != nil {
		return FirstHost{}, err
	}
	// Whatever goes wrong from here, what was made is taken away, so that trying again starts from nothing.
	defer func() {
		if err != nil {
			_ = w.Close()
			undoCreate(cfg)
		}
	}()
	created, err := w.Service.CreateWorkspace(ctx, p.Name, p.Owner, p.Email)
	if err != nil {
		return FirstHost{}, err
	}
	opt := p.Network
	opt.HostKeys = keys
	nc, err := CreateNetwork(ctx, cfg, log, w.Service, created, opt)
	if err != nil {
		return FirstHost{}, fmt.Errorf("the workspace's private network could not be made: %w", err)
	}
	token, err := w.Service.IssueLocalDeviceCredential(ctx, created.Workspace.ID, nc.HostDeviceID)
	if err != nil {
		return FirstHost{}, err
	}
	sealer, err := SealerFor(cfg)
	if err != nil {
		return FirstHost{}, err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return FirstHost{}, err
	}
	if err := v.SaveDeviceToken(token); err != nil {
		return FirstHost{}, err
	}
	// The daemon starts the host from what is on disk; the storage this made is closed first so that it can.
	if cerr := w.Close(); cerr != nil {
		log.Warn("stopping the new workspace's storage", "err", cerr)
	}
	return FirstHost{Created: created, Network: nc}, nil
}

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/service"
	"devboard/internal/team/store"
)

// Workspace is a host's workspace as it runs: its storage and the service built on it.
type Workspace struct {
	Storage *Storage
	Service *service.Service
}

// readyWait is how long opening a workspace waits for its database to be ready: a node that is joining or electing
// a leader may take a little while, and a command that opens a workspace needs it.
const readyWait = 3 * time.Minute

// OpenWorkspace opens the workspace this host holds, starts its storage and waits for it to be ready.
func OpenWorkspace(ctx context.Context, cfg config.Config, log *slog.Logger) (*Workspace, error) {
	st, err := OpenStorage(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	w := &Workspace{Storage: st, Service: service.New(st.DB())}
	st.Attach(w.Service, nil)
	st.Start(ctx)
	if err := st.WaitReady(ctx, readyWait); err != nil {
		_ = w.Close()
		return nil, err
	}
	return w, nil
}

// Close stops the storage.
func (w *Workspace) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return w.Storage.Close(ctx)
}

// owned is a store whose Close also stops what runs it.
type owned struct {
	store.Store
	w *Workspace
}

func (o owned) Close() error { return o.w.Close() }

// randomID is a short random name.
func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// CreateStorage makes the place where a new workspace's data will be, as the configuration says (a cluster of one host,
// which other hosts can later join, or one file), and opens it. nodeID names this host's node in a cluster (the device's
// ID, when it has one). A data directory that already holds a workspace is refused.
func CreateStorage(ctx context.Context, cfg config.Config, log *slog.Logger, nodeID string) (*Workspace, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if _, ok, err := readMarker(cfg); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("%s already says where this host keeps a workspace's data: this data directory already has one", cfg.StorageMarkerPath())
	}
	if _, err := os.Stat(cfg.DBPath()); err == nil {
		return nil, fmt.Errorf("%s already exists: this data directory already has a workspace", cfg.DBPath())
	}
	switch cfg.Storage {
	case config.StorageSingleFile:
		if err := writeMarker(cfg, storageMarker{Kind: config.StorageSingleFile}); err != nil {
			return nil, err
		}
	case config.StorageReplicated:
		// Before anything is made: the program has to be there.
		if err := checkProgram(ctx, cfg, log); err != nil {
			return nil, err
		}
		if nodeID == "" {
			nodeID = "host-" + randomID()
		}
		if err := prepareCluster(cfg, nodeID); err != nil {
			return nil, err
		}
	}
	w, err := OpenWorkspace(ctx, cfg, log)
	if err != nil {
		undoCreate(cfg)
		return nil, err
	}
	return w, nil
}

// prepareCluster makes what the first host of a cluster starts from: the cluster's credentials, sealed in this host's vault
// (before anything refers to them), and then the marker that says the data is in a cluster, here, on loopback.
func prepareCluster(cfg config.Config, nodeID string) error {
	if _, err := rqlite.ThisPlatform(); err != nil {
		return err
	}
	creds, err := rqlite.NewCredentials()
	if err != nil {
		return err
	}
	sealer, err := SealerFor(cfg)
	if err != nil {
		return err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return err
	}
	lo := netip.MustParseAddr("127.0.0.1")
	httpAddr := netip.AddrPortFrom(lo, uint16(cfg.StoragePort)).String()
	raftAddr := netip.AddrPortFrom(lo, uint16(cfg.StorageRaftPort)).String()
	if err := v.SaveStorage(pki.StorageSecrets{ClusterID: randomID() + randomID(), AppPassword: creds.App, AdminPassword: creds.Admin, NodePassword: creds.Node,
		NodeID: nodeID, HTTPAddr: httpAddr, RaftAddr: raftAddr}); err != nil {
		return err
	}
	return writeMarker(cfg, storageMarker{Kind: config.StorageReplicated, NodeID: nodeID, HTTP: httpAddr, Raft: raftAddr, Role: RoleVoter, Fresh: true})
}

// undoCreate removes what CreateStorage made when it could not open it, so that trying again is possible.
func undoCreate(cfg config.Config) {
	_ = os.Remove(cfg.StorageMarkerPath())
	_ = os.RemoveAll(cfg.StorageDir())
	if v, err := (&Storage{cfg: cfg}).vault(); err == nil && !v.Exists() {
		_ = os.RemoveAll(cfg.PKIDir())
	}
}

// ErrStorageKind is returned for a workspace in storage of a kind an operation does not apply to.
var ErrStorageKind = errors.New("this operation applies to a workspace whose data is in a cluster of Workspace Hosts")

var _ = domain.ErrConflict

// checkProgram finds the database program, verifies it against the pin and has it report its version, in a scratch directory,
// so that a command that is about to need it fails before it has changed anything.
func checkProgram(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	sup, err := rqlite.New(rqlite.Options{BinaryDirs: databaseDirs(cfg), Log: log})
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "werkbord-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if _, err := sup.Check(ctx, dir); err != nil {
		if errors.Is(err, rqlite.ErrBinaryNotFound) {
			return ErrNoDatabaseProgram
		}
		return err
	}
	return nil
}

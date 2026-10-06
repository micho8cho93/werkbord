package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/nebula"
	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
)

// NodeSource is where a host's network node learns what it should be: its place on the
// network and its certificate. A host that holds the workspace's database reads them from it;
// a host that only joined asks a Workspace Host's API over the network it is already on (that
// is implemented by the command, which is the part of Team that makes requests).
type NodeSource interface {
	// NodeConfig is this device's address, roles, the discovery hosts and relays to use,
	// and the certificates to refuse.
	NodeConfig(ctx context.Context) (domain.NodeConfig, error)
	// Certificate returns this device's node certificate (PEM) and when it stops working,
	// renewing it first when under a third of its life remains.
	Certificate(ctx context.Context, cfg domain.NodeConfig) ([]byte, time.Time, error)
}

// nodeRunner keeps a host's network node running and current: it asks its source what the node
// should be, starts it, and applies each change (a renewed certificate, another discovery host, a
// longer blocklist) by reloading it, or restarting it when the change is one only read at start.
type nodeRunner struct {
	cfg    config.Config
	log    *slog.Logger
	keys   *pki.HostKeys
	caCert func() ([]byte, error)
	src    NodeSource

	// noTun runs the node without a network interface. Tests set it, because a test has no right
	// to create one; nothing in the product does.
	noTun bool

	mu      sync.Mutex
	sup     *nebula.Supervisor
	applied [32]byte // what the node was last started or reloaded with
	retryAt time.Time
}

func newNodeRunner(cfg config.Config, log *slog.Logger, keys *pki.HostKeys, caCert func() ([]byte, error), src NodeSource) *nodeRunner {
	return &nodeRunner{cfg: cfg, log: log, keys: keys, caCert: caCert, src: src}
}

const reconcileEvery = 15 * time.Second

func (r *nodeRunner) loop(ctx context.Context) {
	for {
		r.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconcileEvery):
		}
	}
}

func (r *nodeRunner) supervisorStatus() (nebula.Status, bool) {
	r.mu.Lock()
	sup := r.sup
	r.mu.Unlock()
	if sup == nil {
		return nebula.Status{}, false
	}
	return sup.Status(), true
}

// Status describes the node for the API.
func (r *nodeRunner) Status() any {
	if st, ok := r.supervisorStatus(); ok {
		return st
	}
	return map[string]any{"state": "not_running", "reason": nodeOffReason(r.cfg)}
}

func nodeOffReason(cfg config.Config) string {
	if !cfg.RunNode {
		return "this host is configured not to run the network node (WERKBORD_TEAM_NETWORK_NODE=off)"
	}
	return "the network node has not been started yet"
}

func (r *nodeRunner) stop(ctx context.Context) error {
	r.mu.Lock()
	sup := r.sup
	r.mu.Unlock()
	if sup == nil {
		return nil
	}
	return sup.Stop(ctx)
}

func (r *nodeRunner) reconcile(ctx context.Context) {
	cfg, err := r.src.NodeConfig(ctx)
	if err != nil {
		r.log.Warn("this host's place on the network is not known yet", "err", err)
		return
	}
	spec, err := nodeSpec(cfg, overlay.Ports{API: apiPortOr(r.cfg.Addr), DeviceService: overlay.DeviceServicePort, Replication: overlay.ReplicationPorts}, "-", "-", "-")
	if err != nil {
		r.log.Error("this host's network configuration is not valid", "err", err)
		return
	}
	spec.TunDisabled = spec.TunDisabled || r.noTun
	cert, _, err := r.src.Certificate(ctx, cfg)
	if err != nil {
		r.log.Error("this host has no valid certificate for the network", "err", err)
		return
	}
	caPEM, err := r.caCert()
	if err != nil {
		r.log.Error("the network authority's certificate is missing", "err", err)
		return
	}
	priv, _, err := r.keys.NetworkKeyPEMs()
	if err != nil {
		r.log.Error("this host's network key", "err", err)
		return
	}
	nc := nebula.NebulaConfig{DataDir: r.cfg.NodeDir(), Node: spec, CACertPEM: caPEM, NodeCertPEM: cert, NodeKeyPEM: priv}
	rendered, err := overlay.Render(func() overlay.NodeSpec { s := spec; s.CA, s.Cert, s.Key = "ca", "cert", "key"; return s }())
	if err != nil {
		r.log.Error("rendering the network configuration", "err", err)
		return
	}
	digest := sha256.Sum256(append(append(append([]byte{}, rendered...), cert...), caPEM...))

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sup == nil {
		sup, err := nebula.New(nebula.Options{BinaryDirs: r.binaryDirs(), Log: r.log})
		if err != nil {
			r.log.Error("the network node cannot run on this platform", "err", err)
			return
		}
		r.sup = sup
	}
	st := r.sup.Status()
	switch {
	case st.State == nebula.StateStopped || st.State == nebula.StateFailed && r.applied == [32]byte{}:
		if time.Now().Before(r.retryAt) {
			return
		}
		if err := r.sup.StartNebula(ctx, nc); err != nil {
			// Not retried at once: whatever stopped it (no privileges, a port in use) will not
			// have changed in 15 seconds, and each try starts a program.
			r.retryAt = time.Now().Add(time.Minute)
			r.log.Error("the network node did not start; trying again in a minute", "err", err)
			return
		}
		r.applied = digest
		r.log.Info("the network node is running", "address", cfg.OverlayAddr, "discovery", cfg.Discovery, "relay", cfg.Relay, "version", nebula.Version)
	case r.applied != digest:
		if err := r.sup.Reload(ctx, nc); err != nil {
			r.log.Error("applying a changed network configuration", "err", err)
			return
		}
		r.applied = digest
		r.log.Info("the network configuration was updated", "blocklist", len(cfg.Blocklist), "discoveryHosts", len(cfg.DiscoveryHosts))
	}
}

// binaryDirs are the places the pinned program may have been installed: where the operator
// said, beside the executable, and in the directory the installer uses. What is found must still
// match the pin; the PATH is never searched.
func (r *nodeRunner) binaryDirs() []string {
	var dirs []string
	dirs = append(dirs, r.cfg.NebulaDirs...)
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			d := filepath.Dir(exe)
			dirs = append(dirs, filepath.Join(d, "libexec", "werkbord-team"), filepath.Join(filepath.Dir(d), "libexec", "werkbord-team"), d)
		}
	}
	return dirs
}

// ---- the source of a host that holds the workspace's database ----

type localSource struct{ n *network }

func (s localSource) NodeConfig(ctx context.Context) (domain.NodeConfig, error) {
	return s.n.svc.NodeConfigOf(ctx, s.n.mat.Meta.WorkspaceID, s.n.mat.Meta.HostDeviceID)
}

// Certificate is the one this host has if it is valid and has more than a third of its life
// left, otherwise a new one.
func (s localSource) Certificate(ctx context.Context, cfg domain.NodeConfig) ([]byte, time.Time, error) {
	n := s.n
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.nodeCert == nil {
		if b, err := os.ReadFile(filepath.Join(n.cfg.NodeDir(), "node.crt")); err == nil {
			if info, err := n.mat.CA.VerifyNode(b, time.Now()); err == nil && info.Name == cfg.DeviceID && info.Addr.String() == cfg.OverlayAddr {
				n.nodeCert, n.notAfter = b, info.NotAfter
			}
		}
	}
	if n.nodeCert != nil && time.Until(n.notAfter) > pki.DefaultNodeValidity/3 {
		return n.nodeCert, n.notAfter, nil
	}
	b, err := n.svc.RenewLocalCertificate(ctx, n.mat.Meta.WorkspaceID, n.mat.Meta.HostDeviceID)
	if err != nil {
		return nil, time.Time{}, err
	}
	n.nodeCert, n.notAfter = []byte(b.Certificate), b.ExpiresAt
	n.log.Info("this host's certificate for the network was renewed", "expires", b.ExpiresAt.Format(time.RFC3339))
	return n.nodeCert, n.notAfter, nil
}

// ---- a host that only joined ----

// RunDeviceNode runs the network node of a host that enrolled but does not hold the workspace's
// database: from the keys in its vault, and what src (the Workspace Hosts' API, over the network
// it is on) says it should be. It returns when ctx ends.
func RunDeviceNode(ctx context.Context, cfg config.Config, log *slog.Logger, src NodeSource) error {
	sealer, err := SealerFor(cfg)
	if err != nil {
		return err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return err
	}
	if !v.Exists() {
		return errors.New("this machine has not joined a workspace: run `werkbord-team device join <link>`")
	}
	mat, err := v.Load(time.Now())
	if err != nil {
		return err
	}
	r := newNodeRunner(cfg, log.With("component", "network"), mat.Host, v.CACertificate, src)
	r.loop(ctx)
	stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	return r.stop(stop)
}

// DecodeNodeConfig reads the workspace's description of a node, as it arrives in a bundle.
func DecodeNodeConfig(raw json.RawMessage) (domain.NodeConfig, error) {
	var c domain.NodeConfig
	if len(raw) == 0 {
		return c, errors.New("the workspace sent no description of this device's place on the network")
	}
	err := json.Unmarshal(raw, &c)
	return c, err
}

func apiPortOr(addr string) int {
	if p := apiPort(addr); p > 0 {
		return p
	}
	return overlay.APIPort
}

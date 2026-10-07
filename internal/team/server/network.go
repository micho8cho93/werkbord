package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"devboard/internal/enrollment"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/overlaynet"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/service"
	"devboard/internal/transport"
)

// The customer-owned private network, as a running part of the server.
//
// A host that has a key vault (made by `werkbord-team workspace create --network`, or by
// joining as a host) takes part in the workspace's private network. Depending on what it
// holds it:
//
//   - answers new devices that are joining, over TLS, with the workspace's own identity
//     (the enrollment endpoint), if it holds the workspace's keys;
//   - runs the network's node, from the pinned program, supervised, and keeps its
//     certificate, its list of discovery hosts and its blocklist current;
//   - serves the workspace's API on its private-network address, so members reach it
//     there and nowhere on the Internet;
//   - checks, now and then, that the other hosts' endpoints answer, from where it stands.
//
// Everything it reaches is another machine of the same workspace. Nothing is reached,
// registered with or downloaded from anything Werkbord operates.

// network is the runtime of a host's part in the private network.
type network struct {
	cfg  config.Config
	log  *slog.Logger
	svc  *service.Service
	v    *pki.Vault
	mat  *pki.Material
	auth *authority

	mu       sync.Mutex
	leaf     *pki.ServerCertificate
	nodeCert []byte    // this host's own node certificate (PEM)
	notAfter time.Time // and when it stops working
	// node runs this host's own network node.
	node *nodeRunner
	// fallback is where the node learns what it should be while the database is not up (RunOptions).
	fallback NodeSource
}

// overlayAddr is this host's address on the private network, as the workspace records it.
func (n *network) overlayAddr(ctx context.Context) (netip.Addr, error) {
	cfg, err := n.svc.NodeConfigOf(ctx, n.mat.Meta.WorkspaceID, n.mat.Meta.HostDeviceID)
	if err != nil {
		if n.fallback != nil && errors.Is(err, domain.ErrReadOnly) {
			cfg, err = n.fallback.NodeConfig(ctx)
		}
		if err != nil {
			return netip.Addr{}, err
		}
	}
	return netip.ParseAddr(cfg.OverlayAddr)
}

// SealerFor is what seals this host's keys: a passphrase from the file the configuration names, or else a key kept
// beside the data (and apart from the sealed files).
func SealerFor(cfg config.Config) (pki.Sealer, error) {
	if cfg.PassphraseFile != "" {
		b, err := os.ReadFile(cfg.PassphraseFile)
		if err != nil {
			return nil, fmt.Errorf("the passphrase file: %w", err)
		}
		return pki.NewPassphraseSealer(bytes.TrimRight(b, "\r\n"))
	}
	return pki.NewFileSealer(cfg.SealingKeyPath())
}

// openNetwork opens this host's key vault if it has one. A host without one takes no
// part in a private network and nothing here runs. A vault that cannot be opened (the
// wrong passphrase, a damaged file) is an error: a host that quietly ran without the
// network it is configured for would be worse than one that did not start.
func openNetwork(cfg config.Config, log *slog.Logger, svc *service.Service) (*network, error) {
	if _, err := os.Stat(filepath.Join(cfg.PKIDir(), "workspace.json")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	sealer, err := SealerFor(cfg)
	if err != nil {
		return nil, err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return nil, err
	}
	mat, err := v.Load(time.Now())
	if err != nil {
		return nil, fmt.Errorf("opening the workspace's keys in %s: %w", cfg.PKIDir(), err)
	}
	n := &network{cfg: cfg, log: log.With("component", "network"), svc: svc, v: v, mat: mat}
	n.node = newNodeRunner(cfg, n.log, mat.Host, v.CACertificate, localSource{n})
	if mat.Meta.Authority {
		if n.auth, err = newAuthority(mat, apiPort(cfg.Addr)); err != nil {
			return nil, err
		}
		svc.SetNetwork(n.auth)
	}
	return n, nil
}

func apiPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

// Meta says which workspace and which host this is.
func (n *network) Meta() pki.Meta { return n.mat.Meta }

// NodeStatus is the supervised node's state, for the API.
func (n *network) NodeStatus() any { return n.node.Status() }

// run starts everything this host does for the network and returns when ctx ends.
func (n *network) run(ctx context.Context, handler http.Handler) {
	var wg sync.WaitGroup
	start := func(name string, f func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					n.log.Error("a part of the network runtime stopped", "part", name, "panic", r)
				}
			}()
			f(ctx)
		}()
	}
	n.registerEndpoints(ctx)
	if n.auth != nil {
		start("enrollment endpoint", n.serveEnrollment)
		start("reachability checks", n.probeLoop)
	}
	if n.cfg.RunNode {
		start("node", n.node.loop)
		start("API on the private network", func(ctx context.Context) { n.serveOverlayAPI(ctx, handler) })
	}
	<-ctx.Done()
	stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), n.cfg.ShutdownTimeout)
	defer cancel()
	if err := n.node.stop(stop); err != nil {
		n.log.Warn("stopping the network node", "err", err)
	}
	wg.Wait()
}

// ---- where this host says it can be reached ----

func (n *network) endpointHosts() []string { return n.cfg.Endpoints }

func (n *network) registerEndpoints(ctx context.Context) {
	if n.auth == nil || len(n.cfg.Endpoints) == 0 {
		return
	}
	var boot, nets []string
	for _, h := range n.endpointHosts() {
		boot = append(boot, net.JoinHostPort(h, strconv.Itoa(n.cfg.BootstrapPort())))
		nets = append(nets, net.JoinHostPort(h, strconv.Itoa(n.cfg.NetworkPort)))
	}
	changed, err := n.svc.SetLocalEndpoints(ctx, n.mat.Meta.WorkspaceID, n.mat.Meta.HostDeviceID, boot, nets)
	if err != nil {
		n.log.Error("recording where this host can be reached", "err", err)
		return
	}
	if changed {
		n.log.Info("this host advertises", "enrollment", boot, "network", nets)
	}
	if !service.ConnectivityPossible(nets) {
		n.log.Warn("none of this host's addresses can be reached from outside its own network: devices elsewhere cannot be expected to reach it. See docs/TEAM_NETWORK.md, \"Connectivity Hosts\"", "addresses", nets)
	}
}

// ---- answering devices that are joining ----

// serverCert is the enrollment endpoint's certificate, reissued when it has under a
// third of its life left, for the addresses this host is reached at.
func (n *network) serverCert() (enrollment.ServerCert, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	if n.leaf != nil && n.leaf.Fresh(now) {
		return n.leaf.ServerCert, nil
	}
	hosts := append([]string(nil), n.endpointHosts()...)
	if recorded, err := n.svc.LocalBootstrapEndpoints(context.Background(), n.mat.Meta.WorkspaceID, n.mat.Meta.HostDeviceID); err == nil {
		hosts = append(hosts, hostsOf(recorded)...)
	}
	// So that the host's own administrator can enroll a device on the same machine or LAN.
	hosts = append(hosts, "127.0.0.1", "::1", "localhost")
	if h, _, err := net.SplitHostPort(n.cfg.BootstrapAddr); err == nil && h != "" && h != "0.0.0.0" && h != "::" {
		hosts = append(hosts, h)
	}
	leaf, err := n.mat.Trust.IssueServerCertificate(hosts, now)
	if err != nil {
		return enrollment.ServerCert{}, err
	}
	n.leaf = &leaf
	return leaf.ServerCert, nil
}

func (n *network) serveEnrollment(ctx context.Context) {
	auth := n.svc.EnrollAuthority(n.mat.Meta.WorkspaceID)
	es := enrollment.NewServer(enrollment.ServerOptions{Authority: auth, WorkspaceID: n.mat.Meta.WorkspaceID, PollKey: auth.PollKey, Cert: n.serverCert, Log: n.log})
	ln, err := net.Listen("tcp", n.cfg.BootstrapAddr)
	if err != nil {
		n.log.Error("cannot listen for devices that are joining", "addr", n.cfg.BootstrapAddr, "err", err)
		return
	}
	srv := es.HTTPServer()
	n.log.Info("answering devices that join this workspace", "addr", ln.Addr().String(), "fingerprint", n.mat.Trust.Fingerprint())
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(es.Listener(ln)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		n.log.Error("the endpoint for joining devices stopped", "err", err)
	}
}

// ---- checking that the other hosts can be reached ----

const probeEvery = 5 * time.Minute

func (n *network) probeLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(20 * time.Second):
	}
	for {
		n.probeOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(probeEvery):
		}
	}
}

// probeOnce tries each host's advertised enrollment endpoint, from here, and records
// what happened. A host's check of itself is recorded too, and counts for nothing
// external; a check of another host from a machine on a different network is what shows
// that host can be reached from there.
func (n *network) probeOnce(ctx context.Context) {
	ws := n.mat.Meta.WorkspaceID
	targets, err := n.svc.ProbeTargets(ctx, ws)
	if err != nil {
		n.log.Warn("listing the hosts to check", "err", err)
		return
	}
	for _, t := range targets {
		for _, ep := range t.Endpoints {
			probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			_, err := enrollment.Probe(probeCtx, ep, n.mat.Trust.PublicKey(), nil, nil)
			cancel()
			if err := n.svc.RecordLocalReachability(ctx, ws, n.mat.Meta.HostDeviceID, t.DeviceID, service.ReachReport{Endpoint: ep, OK: err == nil}); err != nil {
				n.log.Warn("recording a reachability check", "endpoint", ep, "err", err)
			}
		}
	}
}

// ---- the API on the private network ----

// serveOverlayAPI serves the workspace's API on this host's private-network address, once
// the node has made that address exist. It is how members reach the workspace: over the
// network, to a host, and nowhere else. A host whose API already listens on every address
// has nothing to add.
func (n *network) serveOverlayAPI(ctx context.Context, handler http.Handler) {
	if handler == nil {
		return
	}
	if h, _, err := net.SplitHostPort(n.cfg.Addr); err == nil {
		if ip := net.ParseIP(h); h == "" || (ip != nil && ip.IsUnspecified()) {
			return
		}
	}
	ws, id := n.mat.Meta.WorkspaceID, n.mat.Meta.HostDeviceID
	port := apiPortOr(n.cfg.Addr)
	for ctx.Err() == nil {
		cfg, err := n.svc.NodeConfigOf(ctx, ws, id)
		if err == nil {
			if addr, perr := netip.ParseAddr(cfg.OverlayAddr); perr == nil && hasLocalAddr(addr) {
				ln, err := net.Listen("tcp", net.JoinHostPort(addr.String(), strconv.Itoa(port)))
				if err == nil {
					srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}
					n.log.Info("serving the workspace's API on the private network", "addr", ln.Addr().String())
					go func() {
						<-ctx.Done()
						_ = srv.Close()
					}()
					if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
						n.log.Error("the API on the private network stopped", "err", err)
					}
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func hasLocalAddr(a netip.Addr) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, ia := range addrs {
		if p, err := netip.ParsePrefix(ia.String()); err == nil && p.Addr() == a {
			return true
		}
	}
	return false
}

// ---- the network as a transport ----

// nodeAdapter lets the transport start and describe this host's network node.
type nodeAdapter struct{ n *network }

func (a nodeAdapter) Start(ctx context.Context) error { a.n.node.reconcile(ctx); return nil }

func (a nodeAdapter) Stop(ctx context.Context) error { return a.n.node.stop(ctx) }

func (a nodeAdapter) State() overlaynet.NodeState {
	st, ok := a.n.node.supervisorStatus()
	if !ok {
		return overlaynet.NodeState{State: overlaynet.StateStopped, Detail: nodeOffReason(a.n.cfg)}
	}
	return overlaynet.NodeState{State: overlaynet.State(st.State), Detail: st.LastError}
}

// Transport is this host's place on the workspace's private network as a transport.Transport:
// what the routing of authenticated messages between devices will use, so that it never names
// the network program.
func (n *network) Transport(ctx context.Context) (transport.Transport, error) {
	ws, id := n.mat.Meta.WorkspaceID, n.mat.Meta.HostDeviceID
	cfg, err := n.svc.NodeConfigOf(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	self, err := netip.ParseAddr(cfg.OverlayAddr)
	if err != nil {
		return nil, err
	}
	prefix, err := netip.ParsePrefix(n.mat.Meta.NetworkPrefix)
	if err != nil {
		return nil, err
	}
	return overlaynet.New(overlaynet.Config{
		Node: nodeAdapter{n}, Network: prefix, Stack: overlaynet.OSStack{},
		Self: transport.NodeInfo{ID: transport.NodeID(id), Name: n.mat.Meta.WorkspaceName, Addrs: []netip.Addr{self}},
		Peers: func(ctx context.Context) ([]overlaynet.Peer, error) {
			ps, err := n.svc.NetworkPeers(ctx, ws, id)
			if err != nil {
				return nil, err
			}
			out := make([]overlaynet.Peer, 0, len(ps))
			for _, p := range ps {
				if a, err := netip.ParseAddr(p.OverlayAddr); err == nil {
					out = append(out, overlaynet.Peer{ID: transport.NodeID(p.DeviceID), Name: p.Name, Addr: a, LastSeen: p.LastSeen})
				}
			}
			return out, nil
		},
	})
}

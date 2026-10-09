package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"devboard/internal/envelope"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/runnerlink"
)

// deviceLoop supervises this device, never a member's developer processes.
func (d *Daemon) deviceLoop(ctx context.Context) {
	var infraCancel context.CancelFunc
	var infraDone chan error
	var hostRole bool
	stop := func() {
		if infraCancel != nil {
			infraCancel()
			<-infraDone
			infraCancel = nil
			infraDone = nil
		}
	}
	defer stop()
	for {
		if ctx.Err() != nil {
			return
		}
		// Complete durable local transitions before starting any sidecar after a restart.
		if _, err := os.Stat(filepath.Join(d.o.Config.DataDir, "leaving")); err == nil {
			// After a crash, run only networking while revocation finishes; never restart a removed database host.
			if infraCancel == nil {
				_ = d.loadDevice()
				d.mu.RLock()
				mat, host, v := d.mat, d.host, d.vault
				d.mu.RUnlock()
				if mat != nil {
					cfg := d.workspaceConfig()
					runCtx, cancel := context.WithCancel(ctx)
					infraCancel = cancel
					infraDone = make(chan error, 1)
					done := infraDone
					source := &daemonNodeSource{d: d, host: host, v: v, cfg: cfg}
					go func() { done <- RunDeviceNode(runCtx, cfg, d.o.Log, source) }()
				}
			}
			if err := d.revokeLeavingDevice(ctx); err != nil {
				d.problem(err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Second):
				}
				continue
			}
			stop()
			if err := d.finishLeave(ctx); err != nil {
				d.problem(err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Second):
				}
			}
			continue
		}
		if _, err := os.Stat(filepath.Join(d.workspaceConfig().DataDir, "demoting")); err == nil {
			stop()
			if err := d.finishDemotion(); err != nil {
				d.problem(err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Second):
				}
				continue
			}
		}
		if err := d.resumeJoin(ctx); err != nil {
			d.problem(err)
		}
		d.mu.RLock()
		mat := d.mat
		d.mu.RUnlock()
		if mat == nil {
			if err := d.loadDevice(); err != nil {
				d.problem(err)
			}
		}
		d.mu.RLock()
		mat, host := d.mat, d.host
		d.mu.RUnlock()
		if mat != nil {
			if infraCancel == nil {
				cfg := d.workspaceConfig()
				if b, err := os.ReadFile(filepath.Join(cfg.DataDir, "endpoints.json")); err == nil {
					_ = json.Unmarshal(b, &cfg.Endpoints)
					cfg.BootstrapAddr = net.JoinHostPort("0.0.0.0", fmt.Sprint(cfg.BootstrapPort()))
				}
				runCtx, cancel := context.WithCancel(ctx)
				infraCancel = cancel
				infraDone = make(chan error, 1)
				hostRole = mat.Meta.Authority
				done, isHost := infraDone, hostRole
				source := &daemonNodeSource{d: d, host: host, v: d.vault, cfg: cfg}
				go func() {
					if isHost {
						done <- RunWith(runCtx, cfg, d.o.Log, d.o.Version, RunOptions{NodeFallback: source})
					} else {
						done <- RunDeviceNode(runCtx, cfg, d.o.Log, source)
					}
				}()
			}
			if err := d.reconcileDevice(ctx); err != nil {
				d.problem(err)
				if errors.Is(err, errDemoting) {
					stop()
					continue
				}
			} else {
				d.problem(nil)
			}
			if !mat.Meta.Authority {
				if err := d.collectHost(ctx); errors.Is(err, errHostingBlocked) {
					d.problem(err)
				}
			}
			d.mu.RLock()
			roleChanged := d.mat != nil && d.mat.Meta.Authority != hostRole
			d.mu.RUnlock()
			if roleChanged {
				stop()
				d.notify()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case err := <-infraDone:
			if err != nil {
				d.problem(fmt.Errorf("the workspace service stopped: %w", err))
			}
			infraCancel = nil
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
		case <-time.After(10 * time.Second):
		}
	}
}

func (d *Daemon) reconcileDevice(ctx context.Context) error {
	d.mu.RLock()
	host, mat, v := d.host, d.mat, d.vault
	bridge, h := d.bridge, d.handler
	d.mu.RUnlock()
	member, err := v.Secret("member")
	if err != nil {
		return err
	}
	if bridge == nil {
		if token, err := v.Secret("runner-access"); err == nil {
			base := d.state.Settings().WerkbordBase
			if base == "" {
				base = localwerkbord.DefaultBase
			}
			bridge, err = localwerkbord.New(base, string(token))
			if err == nil {
				d.mu.Lock()
				d.bridge = bridge
				d.mu.Unlock()
			}
		} else {
			// Register a controller already installed by this user. Nothing is installed or executed here.
			_ = d.attachRunner(ctx)
			d.mu.RLock()
			bridge = d.bridge
			d.mu.RUnlock()
		}
	}
	runnerOK := false
	if bridge != nil {
		_, err := bridge.Status(ctx)
		runnerOK = err == nil
	}
	d.mu.Lock()
	d.runnerOK, d.lastRunnerCheck = runnerOK, time.Now()
	d.mu.Unlock()
	me, err := host.Me(ctx)
	if err != nil {
		return errors.New("your workspace is offline; keep a Workspace Host online to reconnect")
	}
	if me.Member.ID != string(member) || me.Workspace.ID != mat.Meta.WorkspaceID {
		return errors.New("the workspace answered for a different identity")
	}
	d.mu.Lock()
	d.lastSync = time.Now()
	d.role = string(me.Member.Role)
	d.mu.Unlock()
	if err := host.Heartbeat(ctx, d.profile()); err != nil {
		return err
	}
	if mat.Meta.Authority {
		ackPath := filepath.Join(d.workspaceConfig().DataDir, "provision-ack")
		if _, err := os.Stat(ackPath); err == nil && host.AckProvision(ctx) == nil {
			_ = os.Remove(ackPath)
		}
	}
	ds, err := host.Devices(ctx)
	if err != nil {
		return err
	}
	for _, dv := range ds {
		if dv.ID == mat.Host.DeviceID() && runnerOK && !dv.Has(domain.CapabilityRunner) {
			caps := append(slices.Clone(dv.Capabilities), domain.CapabilityRunner)
			if err := host.Do(ctx, "PUT", "/devices/"+dv.ID+"/capabilities", map[string]any{"capabilities": caps}, nil); err != nil {
				return err
			}
		}
		if dv.ID == mat.Host.DeviceID() && mat.Meta.Authority && !dv.Has(domain.CapabilityWorkspaceHost) {
			archive := fmt.Sprintf("retired-storage-%d", time.Now().UnixNano())
			if err := writeDaemonFile(filepath.Join(d.workspaceConfig().DataDir, "demoting"), []byte(archive)); err != nil {
				return err
			}
			return errDemoting
		}
		if dv.MemberID == me.Member.ID && dv.ID != mat.Host.DeviceID() && !dv.Revoked() {
			if err := d.state.NoteDevice(dv.ID, dv.Name, dv.PublicKey); err != nil {
				return err
			}
		}
	}
	if err := d.publishEndpoints(ctx, host, mat); err != nil {
		return err
	}
	if bundle, err := host.NetworkConfig(ctx); err == nil {
		prefix, _ := netip.ParsePrefix(mat.Meta.NetworkPrefix)
		var bases []string
		if mat.Meta.Authority {
			bases = append(bases, "http://"+d.workspaceConfig().ClientAddr())
		}
		for _, a := range bundle.APIAddrs {
			bases = append(bases, "http://"+net.JoinHostPort(a, fmt.Sprint(apiPortOr(d.o.Config.Addr))))
		}
		_ = host.SetBases(bases, prefix)
		_ = v.SaveJoinInfo(pki.JoinInfo{APIAddrs: bundle.APIAddrs, APIPort: apiPortOr(d.o.Config.Addr), Node: bundle.Node})
	}
	if h == nil {
		ver, err := envelope.NewVerifier(workspaceDirectory{d: d, client: host, self: mat, memberID: me.Member.ID}, d.state, mat.Host.DeviceID())
		if err != nil {
			return err
		}
		h = &runnerlink.Handler{Self: runnerlink.Self{DeviceID: mat.Host.DeviceID(), MemberID: me.Member.ID}, Verifier: ver, State: d.state, Handoffs: host, Log: d.o.Log}
		d.mu.Lock()
		d.memberID = me.Member.ID
		d.handler = h
		d.mu.Unlock()
	}
	// The loop is the sole inbox consumer. Local approval and send endpoints never call Handle concurrently.
	h.Bridge = bridge
	msgs, err := host.Inbox(ctx, 0)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		result := h.Handle(ctx, m)
		if !result.Retry {
			if err := host.Ack(ctx, m.ID, result.State, result.Body); err != nil {
				return err
			}
		}
	}
	if bridge != nil && runnerOK {
		d.synchronize(ctx, host, bridge, mat, me.Member.ID)
	} else {
		d.setSync(syncView{})
	}
	if bridge != nil {
		if err := d.coordinateSchedules(ctx, host, bridge, mat.Host.DeviceID()); err != nil {
			return err
		}
	}
	return nil
}

var errDemoting = errors.New("this device is leaving its Workspace Host role; its workspace copy is being preserved")

// finishDemotion runs only after the host API and database have stopped. It retains the old database, so re-promotion
// joins with fresh cluster membership rather than reviving a removed database node.
func (d *Daemon) finishDemotion() error {
	d.control.Lock()
	defer d.control.Unlock()
	cfg := d.workspaceConfig()
	b, err := os.ReadFile(filepath.Join(cfg.DataDir, "demoting"))
	if err != nil {
		return err
	}
	name := string(b)
	if !strings.HasPrefix(name, "retired-storage-") || filepath.Base(name) != name {
		return errors.New("invalid saved host transition")
	}
	if _, err := os.Stat(cfg.StorageDir()); err == nil {
		if err := os.Rename(cfg.StorageDir(), filepath.Join(cfg.DataDir, name)); err != nil {
			return err
		}
	}
	if err := os.Remove(cfg.StorageMarkerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	v, err := d.setupVault(cfg)
	if err != nil {
		return err
	}
	if err := v.Demote(time.Now()); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(cfg.DataDir, "demoting")); err != nil {
		return err
	}
	return d.loadDevice()
}

// Every device publishes its own discovered addresses. This also permits an Admin to enable connectivity on a
// member's always-on device without making it a Workspace Host or giving it authority keys.
func (d *Daemon) publishEndpoints(ctx context.Context, host *hostclient.Client, mat *pki.Material) error {
	prefix, _ := netip.ParsePrefix(mat.Meta.NetworkPrefix)
	var boot, nets, addresses []string
	for _, address := range localEndpoints() {
		ip, err := netip.ParseAddr(address)
		if err != nil || prefix.Contains(ip) {
			continue
		}
		addresses = append(addresses, address)
		nets = append(nets, net.JoinHostPort(address, fmt.Sprint(d.o.Config.NetworkPort)))
		if mat.Meta.Authority {
			boot = append(boot, net.JoinHostPort(address, fmt.Sprint(d.o.Config.BootstrapPort())))
		}
	}
	slices.Sort(boot)
	slices.Sort(nets)
	var recorded domain.DeviceNetwork
	path := "/devices/" + mat.Host.DeviceID() + "/network"
	if err := host.Do(ctx, "GET", path, nil, &recorded); err != nil {
		return err
	}
	oldBoot, oldNet := slices.Clone(recorded.BootstrapEndpoints), slices.Clone(recorded.NetworkEndpoints)
	slices.Sort(oldBoot)
	slices.Sort(oldNet)
	if slices.Equal(boot, oldBoot) && slices.Equal(nets, oldNet) {
		return nil
	}
	if err := host.Do(ctx, "PUT", path, map[string]any{"bootstrapEndpoints": boot, "networkEndpoints": nets}, nil); err != nil {
		return err
	}
	if mat.Meta.Authority {
		b, _ := json.Marshal(addresses)
		return writeDaemonFile(filepath.Join(d.workspaceConfig().DataDir, "endpoints.json"), b)
	}
	return nil
}

func (d *Daemon) collectHost(ctx context.Context) error {
	d.mu.RLock()
	v, mat, host := d.vault, d.mat, d.host
	d.mu.RUnlock()
	sealed, err := host.CollectProvision(ctx)
	if err != nil {
		return err
	}
	ca, err := v.CACertificate()
	if err != nil {
		return err
	}
	// Something is waiting for this device: it has been asked to be a Workspace Host. Another workspace on this computer
	// that already is one has the ports; say so rather than start a second that cannot run.
	if err := d.hostingAllowed(); err != nil {
		return err
	}
	secrets, err := mat.Host.OpenSecrets(time.Now(), mat.Meta.WorkspaceID, mat.Meta.Fingerprint, string(ca), sealed)
	if err != nil {
		return err
	}
	if err := v.Promote(secrets, time.Now()); err != nil {
		return err
	}
	if err := writeDaemonFile(filepath.Join(d.workspaceConfig().DataDir, "provision-ack"), []byte("stored\n")); err != nil {
		return err
	}
	if err := d.loadDevice(); err != nil {
		return err
	}
	return nil // Reconcile retries the acknowledgement after the new Host has started.
}

// A joining device starts from a locally cached, signed certificate, then refreshes via its own workspace.
type daemonNodeSource struct {
	d    *Daemon
	host *hostclient.Client
	v    *pki.Vault
	cfg  config.Config
}

func (s *daemonNodeSource) NodeConfig(ctx context.Context) (domain.NodeConfig, error) {
	if b, err := s.host.NetworkConfig(ctx); err == nil {
		if c, err := DecodeNodeConfig(b.Node); err == nil {
			_ = s.v.SaveJoinInfo(pki.JoinInfo{APIAddrs: b.APIAddrs, APIPort: apiPortOr(s.cfg.Addr), Node: b.Node})
			return c, nil
		}
	}
	j, err := s.v.JoinInfo()
	if err != nil {
		return domain.NodeConfig{}, err
	}
	return DecodeNodeConfig(j.Node)
}
func (s *daemonNodeSource) Certificate(ctx context.Context, c domain.NodeConfig) ([]byte, time.Time, error) {
	path := filepath.Join(s.cfg.NodeDir(), "node.crt")
	if b, err := os.ReadFile(path); err == nil {
		if cert, err := pki.ReadNodeCert(b); err == nil && cert.Name == c.DeviceID && cert.Addr.String() == c.OverlayAddr && time.Until(cert.NotAfter) > pki.DefaultNodeValidity/3 && CertGroupsMatch(cert.Groups, c.Capabilities) {
			return b, cert.NotAfter, nil
		}
	}
	b, err := s.host.RenewCertificate(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	cert, err := pki.ReadNodeCert([]byte(b.NodeCertificate))
	if err != nil {
		return nil, time.Time{}, err
	}
	if err := writeDaemonFile(path, []byte(b.NodeCertificate)); err != nil {
		return nil, time.Time{}, err
	}
	return []byte(b.NodeCertificate), cert.NotAfter, nil
}

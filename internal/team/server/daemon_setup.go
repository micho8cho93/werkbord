package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"devboard/internal/enrollment"
	"devboard/internal/httpkit"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/pki"
)

func readBounded(r io.Reader, n int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, n+1))
	if int64(len(b)) > n {
		return nil, errors.New("the request is too large")
	}
	return b, err
}
func writeDaemonFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	return os.Rename(f.Name(), path)
}

func (d *Daemon) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Owner string `json:"owner"`
		Email string `json:"email"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	// The first Workspace Host is a host: it takes this computer's host ports. Say so before asking for a license.
	if err := d.hostingAllowed(); err != nil {
		daemonFail(w, err)
		return
	}
	if _, err := d.licenseClaims(); err != nil {
		daemonFail(w, errors.New("import an active Team license before creating your workspace"))
		return
	}
	err := d.launchOperation("Creating your workspace", func(ctx context.Context) error {
		d.control.Lock()
		defer d.control.Unlock()
		cfg := d.workspaceConfig()
		if _, err := os.Stat(cfg.DataDir); !errors.Is(err, os.ErrNotExist) {
			return errors.New("this device already has a workspace; leave it before creating another")
		}
		if _, err := os.Stat(filepath.Join(d.o.Config.DataDir, "pending-join")); err == nil {
			return errors.New("this device is waiting to join a workspace")
		}
		stage, err := os.MkdirTemp(d.o.Config.DataDir, ".workspace-create-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		cfg.DataDir = stage
		// Interface discovery stays local; no external account or IP lookup service is used.
		cfg.Endpoints = localEndpoints()
		cfg.BootstrapAddr = "0.0.0.0:" + strconv.Itoa(cfg.BootstrapPort())
		out, err := CreateFirstHost(ctx, cfg, d.o.Log, FirstHostParams{Name: in.Name, Owner: in.Owner, Email: in.Email, Network: NetworkOptions{Connectivity: "auto", Approval: domain.ApprovalAdmin}})
		if err != nil {
			return err
		}
		v, err := d.setupVault(cfg)
		if err != nil {
			return err
		}
		if err := v.SaveSecret("member", []byte(out.Created.Owner.ID)); err != nil {
			return err
		}
		// Keep the discovered endpoints in this installation, so enrollment stays reachable after restarting.
		b, _ := json.Marshal(cfg.Endpoints)
		if err := writeDaemonFile(filepath.Join(stage, "endpoints.json"), b); err != nil {
			return err
		}
		return os.Rename(stage, d.workspaceConfig().DataDir)
	})
	if err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 202, map[string]bool{"started": true})
}

func localEndpoints() []string {
	var out []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				out = append(out, ip.String())
			}
		}
	}
	if len(out) == 0 {
		out = []string{"127.0.0.1"}
	}
	if len(out) > enrollment.MaxEndpoints {
		out = out[:enrollment.MaxEndpoints]
	}
	return out
}

func (d *Daemon) inspectInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Link string `json:"link"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	inv, err := enrollment.Parse(in.Link, time.Now())
	if err != nil {
		daemonFail(w, invitationProblem(inv, err, time.Now()))
		return
	}
	httpkit.WriteJSON(w, 200, map[string]any{"name": inv.WorkspaceName, "identity": inv.Fingerprint, "expiresAt": inv.Expiry()})
}

// invitationProblem says, in words for the person joining, what is wrong with an invitation or with using it. The
// workspace answers a spent, withdrawn, expired and mistaken invitation alike (so that nobody can probe for them), so
// this is where the person learns the difference that can be known on their own computer: a clock that is wrong, a link
// that was cut short, or an invitation that really has run out.
func invitationProblem(inv enrollment.Invitation, err error, now time.Time) error {
	const clock = "If it was only just made, check that this computer's date and time are set automatically (System Settings > General > Date & Time)."
	switch {
	case errors.Is(err, enrollment.ErrExpired):
		return fmt.Errorf("this invitation ended on %s; this computer's clock says it is now %s. %s Otherwise ask your administrator for a new one",
			inv.Expiry().Local().Format("2 Jan 15:04"), now.Local().Format("2 Jan 15:04"), clock)
	case errors.Is(err, enrollment.ErrClockBehind):
		return fmt.Errorf("this invitation was made at %s by the administrator's computer, but this computer's clock says it is only %s, so the two clocks disagree by %s. Set the date and time on this computer to automatic (System Settings > General > Date & Time) and try again",
			inv.Issued().Local().Format("2 Jan 15:04:05"), now.Local().Format("2 Jan 15:04:05"), inv.Issued().Sub(now).Round(time.Second))
	case errors.Is(err, enrollment.ErrBadSignature):
		return errors.New("this invitation was changed or cut short after it was made: copy the whole link again, without anything added or missing at either end")
	case errors.Is(err, enrollment.ErrMalformed):
		return errors.New("this is not a Werkbord Team invitation link, or part of it is missing: copy the whole link, which starts with werkbord://join/")
	case errors.Is(err, enrollment.ErrRefused):
		return errors.New("the workspace did not accept this invitation. An invitation works once, even when joining then fails part of the way, and it can also have been withdrawn or have run out. Ask your administrator for a new one")
	case errors.Is(err, enrollment.ErrNoEndpoint):
		return fmt.Errorf("this computer could not reach the workspace at any address in the invitation (the administrator's computer must be on, on the same network, and allow incoming connections for Werkbord Team): %w", err)
	}
	return err
}

type pendingJoin struct {
	Link         string               `json:"link"`
	Keys         []byte               `json:"keys"`
	EnrollmentID string               `json:"enrollmentId"`
	Name         string               `json:"name"`
	DeviceName   string               `json:"deviceName"`
	Response     *enrollment.Response `json:"response,omitempty"`
}

func (d *Daemon) pendingVault() (*pki.Vault, error) {
	cfg := d.o.Config
	cfg.DataDir = filepath.Join(cfg.DataDir, "pending")
	return d.setupVault(cfg)
}
func (d *Daemon) setupVault(cfg config.Config) (*pki.Vault, error) {
	s, err := SealerFor(cfg)
	if err != nil {
		return nil, err
	}
	return pki.OpenVault(cfg.PKIDir(), s)
}

func (d *Daemon) joinWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Link       string `json:"link"`
		Name       string `json:"name"`
		DeviceName string `json:"deviceName"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	inv, err := enrollment.Parse(in.Link, time.Now())
	if err != nil {
		daemonFail(w, invitationProblem(inv, err, time.Now()))
		return
	}
	if in.Name == "" || in.DeviceName == "" {
		daemonFail(w, errors.New("enter your name and a name for this computer"))
		return
	}
	err = d.launchOperation("Joining your workspace", func(ctx context.Context) error {
		d.control.Lock()
		defer d.control.Unlock()
		if _, err := os.Stat(d.workspaceConfig().DataDir); !errors.Is(err, os.ErrNotExist) {
			return errors.New("this computer has already joined a workspace")
		}
		if _, err := os.Stat(filepath.Join(d.o.Config.DataDir, "pending-join")); err == nil {
			return errors.New("an enrollment is already waiting for approval")
		}
		keys, err := pki.NewHostKeys()
		if err != nil {
			return err
		}
		kb, err := keys.Marshal()
		if err != nil {
			return err
		}
		p := pendingJoin{Link: in.Link, Keys: kb, Name: in.Name, DeviceName: in.DeviceName}
		v, err := d.pendingVault()
		if err != nil {
			return err
		}
		save := func() error {
			b, err := json.Marshal(p)
			if err != nil {
				return err
			}
			return v.SaveSecret("join", b)
		}
		if err := save(); err != nil {
			return err
		}
		_, pub, err := keys.NetworkKeyPEMs()
		if err != nil {
			return err
		}
		res, err := enrollment.Join(ctx, inv, enrollment.JoinParams{Signer: keys, MemberName: in.Name, DeviceName: in.DeviceName, NetworkPublicKeyPEM: string(pub), SealingPublicKey: keys.SealingPublicKey()})
		if err != nil && !errors.Is(err, enrollment.ErrPending) {
			return invitationProblem(inv, err, time.Now())
		}
		p.EnrollmentID = res.EnrollmentID
		p.Response = &res.Response
		if err := save(); err != nil {
			return err
		}
		if err := writeDaemonFile(filepath.Join(d.o.Config.DataDir, "pending-join"), []byte("waiting\n")); err != nil {
			return err
		}
		if errors.Is(err, enrollment.ErrPending) {
			return nil
		}
		return d.finishJoin(inv, keys, res)
	})
	if err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 202, map[string]bool{"started": true})
}

func (d *Daemon) resumeJoin(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(d.o.Config.DataDir, "pending-join")); err != nil {
		return nil
	}
	d.control.Lock()
	defer d.control.Unlock()
	v, err := d.pendingVault()
	if err != nil {
		return err
	}
	raw, err := v.Secret("join")
	if err != nil {
		return err
	}
	var p pendingJoin
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	// The link was valid when redeemed. Its expiry does not discard an enrollment already waiting for approval.
	inv, err := enrollment.ParseSaved(p.Link)
	if err != nil {
		return err
	}
	keys, err := pki.ParseHostKeys(p.Keys)
	if err != nil {
		return err
	}
	// A crash after the final rename must finish cleanup, never replace the installed identity.
	if _, err := os.Stat(filepath.Join(d.workspaceConfig().PKIDir(), "workspace.json")); err == nil {
		installed, err := d.setupVault(d.workspaceConfig())
		if err != nil {
			return err
		}
		mat, err := installed.Load(time.Now())
		if err != nil || mat.Meta.WorkspaceID != inv.WorkspaceID || mat.Host.DeviceID() != keys.DeviceID() {
			return errors.New("a different workspace is already installed; its data is preserved")
		}
		return d.clearPendingJoin()
	}
	if p.Response != nil && p.Response.State == enrollment.StateApproved {
		return d.finishJoin(inv, keys, &enrollment.Result{Response: *p.Response, Network: p.Response.Network, State: p.Response.State})
	}
	res, err := enrollment.Resume(ctx, inv, enrollment.JoinParams{Signer: keys}, p.EnrollmentID)
	if errors.Is(err, enrollment.ErrPending) {
		return nil
	}
	if err != nil {
		return err
	}
	// Save the one-time response before installing it, so a disk error does not consume the credential irretrievably.
	p.Response = &res.Response
	b, _ := json.Marshal(p)
	if err := v.SaveSecret("join", b); err != nil {
		return err
	}
	return d.finishJoin(inv, keys, res)
}

func (d *Daemon) finishJoin(inv enrollment.Invitation, keys *pki.HostKeys, res *enrollment.Result) error {
	if res.Network == nil {
		return errors.New("the workspace sent no network settings")
	}
	nc, err := DecodeNodeConfig(res.Network.Node)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(d.o.Config.DataDir, ".workspace-join-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	cfg := d.workspaceConfig()
	cfg.DataDir = stage
	v, err := d.setupVault(cfg)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(nc.OverlayAddr)
	if err != nil {
		return err
	}
	prefix := netip.PrefixFrom(addr, nc.PrefixBits).Masked()
	if err := pki.CheckNetworkRange(prefix); err != nil {
		return errors.New("the invitation supplied an unsupported private network")
	}
	if d.o.NetworkGuard != nil {
		if err := d.o.NetworkGuard(prefix); err != nil {
			return err
		}
	}
	if nc.DeviceID != keys.DeviceID() || res.Response.DeviceID != keys.DeviceID() || res.Response.MemberID == "" {
		return errors.New("the enrollment response belongs to a different device")
	}
	meta := pki.Meta{WorkspaceID: inv.WorkspaceID, WorkspaceName: inv.WorkspaceName, Fingerprint: inv.Fingerprint, NetworkPrefix: prefix.String()}
	if err := v.CreateJoined(meta, keys, []byte(res.Network.CACertificate)); err != nil {
		return err
	}
	if err := v.SaveDeviceToken(res.Response.DeviceToken); err != nil {
		return err
	}
	if err := v.SaveSecret("member", []byte(res.Response.MemberID)); err != nil {
		return err
	}
	if err := v.SaveJoinInfo(pki.JoinInfo{Endpoints: inv.Endpoints, APIAddrs: res.Network.APIAddrs, APIPort: apiPortOr(d.o.Config.Addr), Node: res.Network.Node}); err != nil {
		return err
	}
	if err := writeDaemonFile(filepath.Join(cfg.NodeDir(), "node.crt"), []byte(res.Network.NodeCertificate)); err != nil {
		return err
	}
	b, _ := json.Marshal(localEndpoints())
	if err := writeDaemonFile(filepath.Join(stage, "endpoints.json"), b); err != nil {
		return err
	}
	if err := os.Rename(stage, d.workspaceConfig().DataDir); err != nil {
		return err
	}
	return d.clearPendingJoin()
}

func (d *Daemon) clearPendingJoin() error {
	if err := os.Remove(filepath.Join(d.o.Config.DataDir, "pending-join")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.RemoveAll(filepath.Join(d.o.Config.DataDir, "pending"))
}

func (d *Daemon) cancelJoin(w http.ResponseWriter, r *http.Request) {
	err := d.launchOperation("Cancelling this enrollment", func(context.Context) error {
		d.control.Lock()
		defer d.control.Unlock()
		if _, err := os.Stat(d.workspaceConfig().DataDir); !errors.Is(err, os.ErrNotExist) {
			return errors.New("this device has already joined; leave the workspace safely in Settings")
		}
		return d.clearPendingJoin()
	})
	if err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 202, map[string]bool{"started": true})
}

func (d *Daemon) removalSafety(ctx context.Context) error {
	d.mu.RLock()
	host, mat := d.host, d.mat
	d.mu.RUnlock()
	if mat == nil {
		return nil
	}
	if !mat.Meta.Authority {
		return nil
	}
	if host == nil {
		return errors.New("the workspace must be online before this host can leave; its data is preserved")
	}
	var st domain.StorageStatus
	if err := host.Do(ctx, "GET", "/storage", nil, &st); err != nil {
		return fmt.Errorf("cannot prove another workspace copy is safe: %w", err)
	}
	return domain.CheckRemoval(st.Hosts, mat.Host.DeviceID())
}

func (d *Daemon) removalPlan(w http.ResponseWriter, r *http.Request) {
	err := d.removalSafety(r.Context())
	out := map[string]any{"canLeave": err == nil, "canRemoveData": err == nil, "choices": []string{"remove_gui_only", "leave_service", "stop_service", "leave_workspace", "remove_local_data"}}
	if err != nil {
		out["reason"] = "This device may hold the last workspace copy. Add another healthy Workspace Host before leaving or removing data."
	}
	httpkit.WriteJSON(w, 200, out)
}

func (d *Daemon) leaveWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RemoveData bool   `json:"removeData"`
		Confirm    string `json:"confirm"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	if in.Confirm != "leave workspace" {
		daemonFail(w, errors.New("confirm leaving this workspace"))
		return
	}
	if err := d.removalSafety(r.Context()); err != nil {
		daemonFail(w, err)
		return
	}
	err := d.launchOperation("Leaving the workspace", func(ctx context.Context) error {
		d.control.Lock()
		defer d.control.Unlock()
		d.mu.RLock()
		host, mat := d.host, d.mat
		d.mu.RUnlock()
		if mat == nil {
			return nil
		}
		if err := d.removalSafety(ctx); err != nil {
			return err
		}
		// The host is removed from replication before its credential is revoked; the cluster enforces quorum safety too.
		if mat.Meta.Authority {
			if err := host.Do(ctx, "DELETE", "/devices/"+mat.Host.DeviceID()+"/replica", nil, nil); err != nil {
				return err
			}
		}
		// Persist the destination before revocation. Recovery can finish after revocation, a directory move or a crash.
		plan := leavePlan{RemoveData: in.RemoveData, Archive: fmt.Sprintf("left-workspace-%d", time.Now().UnixNano())}
		b, _ := json.Marshal(plan)
		if err := writeDaemonFile(filepath.Join(d.o.Config.DataDir, "leaving"), b); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 202, map[string]bool{"started": true})
}

type leavePlan struct {
	RemoveData bool   `json:"removeData"`
	Archive    string `json:"archive"`
	Revoked    bool   `json:"revoked"`
}

// finishLeave is idempotent across a restart, including one after the atomic archive move.
// The loop stops sidecars before calling it. The plan is written only after checked cluster removal.
func (d *Daemon) finishLeave(ctx context.Context) error {
	d.control.Lock()
	defer d.control.Unlock()
	path := filepath.Join(d.o.Config.DataDir, "leaving")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var plan leavePlan
	if err := json.Unmarshal(b, &plan); err != nil {
		return err
	}
	if !strings.HasPrefix(plan.Archive, "left-workspace-") || filepath.Base(plan.Archive) != plan.Archive {
		return errors.New("invalid saved workspace departure")
	}
	if !plan.Revoked {
		return errors.New("workspace departure is waiting for device revocation")
	}
	source := d.workspaceConfig().DataDir
	if plan.RemoveData {
		err = os.RemoveAll(source)
	} else if _, stat := os.Stat(source); stat == nil {
		err = os.Rename(source, filepath.Join(d.o.Config.DataDir, plan.Archive))
	} else if errors.Is(stat, os.ErrNotExist) {
		_, err = os.Stat(filepath.Join(d.o.Config.DataDir, plan.Archive))
	} else {
		err = stat
	}
	if err != nil {
		return err
	}
	if err := d.state.ResetWorkspace(); err != nil {
		return err
	}
	d.mu.Lock()
	d.mat, d.host, d.vault, d.bridge, d.handler = nil, nil, nil, nil, nil
	d.memberID, d.lastSync, d.runnerOK = "", time.Time{}, false
	d.mu.Unlock()
	return os.Remove(path)
}

func (d *Daemon) revokeLeavingDevice(ctx context.Context) error {
	d.control.Lock()
	defer d.control.Unlock()
	path := filepath.Join(d.o.Config.DataDir, "leaving")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var plan leavePlan
	if err := json.Unmarshal(b, &plan); err != nil {
		return err
	}
	if !plan.Revoked {
		d.mu.RLock()
		host, mat := d.host, d.mat
		d.mu.RUnlock()
		if host == nil || mat == nil {
			if err := d.loadDevice(); err != nil {
				return err
			}
			d.mu.RLock()
			host, mat = d.host, d.mat
			d.mu.RUnlock()
		}
		// A leaving Host must use another Host's API once its own database node has stopped.
		if mat.Meta.Authority {
			bases := host.Bases()
			var remote []string
			for _, base := range bases {
				if u, err := url.Parse(base); err == nil && !net.ParseIP(u.Hostname()).IsLoopback() {
					remote = append(remote, base)
				}
			}
			prefix, _ := netip.ParsePrefix(mat.Meta.NetworkPrefix)
			if err := host.SetBases(remote, prefix); err != nil {
				return err
			}
		}
		if err := host.Do(ctx, "POST", "/devices/"+mat.Host.DeviceID()+"/revoke", map[string]any{}, nil); err != nil {
			var rejected *hostclient.Error
			if !errors.As(err, &rejected) || rejected.Status != http.StatusUnauthorized {
				return err
			}
		}
		plan.Revoked = true
		b, _ = json.Marshal(plan)
		if err := writeDaemonFile(path, b); err != nil {
			return err
		}
	}
	return nil
}

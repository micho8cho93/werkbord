package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"devboard/internal/deviceid"
	"devboard/internal/envelope"
	"devboard/internal/httpkit"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/runnerlink"
)

func (d *Daemon) saveSettings(w http.ResponseWriter, r *http.Request) {
	var s devicestate.Settings
	if !daemonDecode(w, r, &s) {
		return
	}
	if s.WerkbordBase != "" {
		base, err := localwerkbord.CheckBase(s.WerkbordBase)
		if err != nil {
			daemonFail(w, err)
			return
		}
		s.WerkbordBase = base
	}
	if err := d.state.SetSettings(s); err != nil {
		daemonFail(w, err)
		return
	}
	d.mu.Lock()
	d.bridge = nil
	d.mu.Unlock()
	d.notify()
	w.WriteHeader(204)
}

func (d *Daemon) connectRunner(w http.ResponseWriter, r *http.Request) {
	if err := d.attachRunner(r.Context()); err != nil {
		daemonFail(w, err)
		return
	}
	d.notify()
	w.WriteHeader(204)
}

func (d *Daemon) attachRunner(ctx context.Context) error {
	d.control.Lock()
	defer d.control.Unlock()
	d.mu.RLock()
	v, mat, host := d.vault, d.mat, d.host
	d.mu.RUnlock()
	if v == nil {
		return errors.New("join or create a workspace before connecting your runner")
	}
	path := d.o.RunnerConfigPath
	if path == "" {
		return errors.New("open or install the free Werkbord app on this computer, then connect its runner")
	}
	// This path comes from the native installer, never a network request. A temporary full credential is exchanged for narrow access.
	raw, err := os.ReadFile(path)
	var cfg struct {
		Addr  string `json:"addr"`
		Token string `json:"token"`
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(raw) > 64<<10 {
		return errors.New("Werkbord's settings file is too large")
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return errors.New("Werkbord's settings file is unreadable")
		}
	}
	base := d.state.Settings().WerkbordBase
	if base == "" {
		base = localwerkbord.DefaultBase
		if cfg.Addr != "" {
			base = "http://" + cfg.Addr
		}
	}
	base, err = localwerkbord.CheckBase(base)
	if err != nil {
		return err
	}
	if _, err := localwerkbord.Probe(ctx, base); err != nil {
		return err
	}
	if cfg.Token == "" {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(path), "token"))
		if err != nil {
			return errors.New("Werkbord's local access credential is missing; open Werkbord to finish its setup")
		}
		cfg.Token = strings.TrimSpace(string(b))
	}
	// A previously revoked token is not silently reissued: reconnection is the explicit local button.
	token, err := localwerkbord.ConnectExecution(ctx, base, cfg.Token, "Team execution "+mat.Host.DeviceID())
	cfg.Token = ""
	if err != nil {
		return err
	}
	if err := v.SaveSecret("runner-access", []byte(token)); err != nil {
		return err
	}
	bridge, err := localwerkbord.New(base, token)
	if err != nil {
		return err
	}
	set := d.state.Settings()
	set.WerkbordBase = base
	if err := d.state.SetSettings(set); err != nil {
		return err
	}
	d.mu.Lock()
	d.bridge = bridge
	d.mu.Unlock()
	if host != nil {
		ds, err := host.Devices(ctx)
		if err == nil {
			for _, dv := range ds {
				if dv.ID == mat.Host.DeviceID() && !dv.Has(domain.CapabilityRunner) {
					caps := append(append([]domain.Capability(nil), dv.Capabilities...), domain.CapabilityRunner)
					if err := host.Do(ctx, "PUT", "/devices/"+dv.ID+"/capabilities", map[string]any{"capabilities": caps}, nil); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (d *Daemon) trustSender(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PublicKey string `json:"publicKey"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	if _, err := deviceid.ParsePublicKey(in.PublicKey); err != nil {
		daemonFail(w, err)
		return
	}
	id := r.PathValue("id")
	for _, s := range d.state.Senders() {
		if s.DeviceID == id {
			if s.PublicKey != in.PublicKey {
				daemonFail(w, errors.New("this device's identity changed while you were reviewing it; review it again"))
				return
			}
			if err := d.state.Approve(id); err != nil {
				daemonFail(w, err)
				return
			}
			w.WriteHeader(204)
			return
		}
	}
	daemonFail(w, errors.New("this is not one of your devices"))
}
func (d *Daemon) revokeSender(w http.ResponseWriter, r *http.Request) {
	if err := d.state.Revoke(r.PathValue("id")); err != nil {
		daemonFail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (d *Daemon) approveTask(w http.ResponseWriter, r *http.Request) {
	daemonFail(w, errors.New("open this ticket's Agent controls and review its effective execution policy before approving"))
}

func (d *Daemon) sendRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Target  string          `json:"target"`
		Action  envelope.Action `json:"action"`
		Payload json.RawMessage `json:"payload"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	d.mu.RLock()
	mat, host, member := d.mat, d.host, d.memberID
	d.mu.RUnlock()
	if mat == nil || host == nil || member == "" {
		daemonFail(w, errors.New("your workspace is still reconnecting"))
		return
	}
	ds, err := host.Devices(r.Context())
	if err != nil {
		daemonFail(w, err)
		return
	}
	own := false
	for _, dv := range ds {
		if dv.ID == in.Target && dv.MemberID == member && dv.Has(domain.CapabilityRunner) && !dv.Revoked() {
			own = true
		}
	}
	if !own {
		daemonFail(w, errors.New("choose one of your own registered runners"))
		return
	}
	payload, ok := envelope.PayloadFor(in.Action)
	if !ok {
		daemonFail(w, envelope.ErrAction)
		return
	}
	dec := json.NewDecoder(bytes.NewReader(in.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(payload); err != nil {
		daemonFail(w, err)
		return
	}
	s := runnerlink.Sender{Signer: mat.Host, WorkspaceID: mat.Meta.WorkspaceID, UserID: member, Host: host}
	m, err := s.Ask(r.Context(), in.Target, in.Action, payload)
	if err != nil {
		daemonFail(w, err)
		return
	}
	d.notify()
	httpkit.WriteJSON(w, 202, m)
}

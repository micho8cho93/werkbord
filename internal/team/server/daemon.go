package server

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/envelope"
	"devboard/internal/httpkit"
	"devboard/internal/team/config"
	"devboard/internal/team/console"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/license"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/runnerlink"
)

// DaemonOptions describe only this computer's installation. None can be changed by a Workspace Host.
type DaemonOptions struct {
	Config    config.Config
	LocalAddr string
	// LocalKey is supplied by the native service installer; empty uses a key in device state (CLI/dev).
	LocalKey         string
	RunnerConfigPath string
	LicenseKey       ed25519.PublicKey
	Version          string
	Log              *slog.Logger
	// Slot names this workspace among the workspaces on this computer ("main" for the first and, on an installation
	// from before several were possible, the only one). HostingGuard and NetworkGuard are supplied by the Hub so that
	// workspaces on one computer cannot take the same host ports or private-network addresses. Both may be nil.
	Slot         string
	HostingGuard func() error
	NetworkGuard func(netip.Prefix) error
}

// Daemon owns one device and its connection to a workspace. Its local API is never served on the overlay.
type Daemon struct {
	o DaemonOptions
	// Internal transport substitution for unprivileged end-to-end tests; never configured by an API or flag.
	hostDial        func(context.Context, string, string) (net.Conn, error)
	state           *devicestate.State
	ctx             context.Context
	mu              sync.RWMutex
	operation       string
	lastError       string
	host            *hostclient.Client
	mat             *pki.Material
	vault           *pki.Vault
	memberID        string
	role            string
	bridge          *localwerkbord.Client
	handler         *runnerlink.Handler
	lastSync        time.Time
	lastRunnerCheck time.Time
	runnerOK        bool
	wake            chan struct{}
	job             sync.WaitGroup
	control         sync.Mutex
}

func NewDaemon(o DaemonOptions) (*Daemon, error) {
	o.Config.LicenseRequired = true
	o.Config.LicenseKey = append([]byte(nil), o.LicenseKey...)
	if o.LocalAddr == "" {
		o.LocalAddr = "127.0.0.1:7431"
	}
	host, _, err := net.SplitHostPort(o.LocalAddr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("the device service must listen on a literal loopback address")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if err := o.Config.Validate(); err != nil {
		return nil, err
	}
	st, err := devicestate.Open(filepath.Join(o.Config.DataDir, "local"))
	if err != nil {
		return nil, err
	}
	return &Daemon{o: o, state: st, wake: make(chan struct{}, 1)}, nil
}

func (d *Daemon) workspaceConfig() config.Config {
	c := d.o.Config
	c.DataDir = filepath.Join(c.DataDir, "workspace")
	c.SecureStorageID = c.DataDir
	c.LicenseFile = filepath.Join(d.o.Config.DataDir, "license.json")
	if c.BackupDir == "" {
		c.BackupDir = filepath.Join(d.o.Config.DataDir, "backups")
	}
	return c
}

func (d *Daemon) key() string {
	if d.o.LocalKey != "" {
		return d.o.LocalKey
	}
	return d.state.LocalKey()
}

// Run keeps infrastructure alive independently of the window. No context owned by a GUI reaches it.
func (d *Daemon) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.mu.Lock()
	d.ctx = runCtx
	d.mu.Unlock()
	ln, err := net.Listen("tcp", d.o.LocalAddr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: d.Handler(), BaseContext: func(net.Listener) context.Context { return runCtx }, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: time.Minute}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); d.deviceLoop(runCtx) }()
	d.o.Log.Info("Team device service is running", "version", d.o.Version, "addr", ln.Addr().String())
	select {
	case err = <-done:
		cancel()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	case <-ctx.Done():
		cancel()
		err = nil
	}
	stop, end := context.WithTimeout(context.Background(), 10*time.Second)
	defer end()
	_ = srv.Shutdown(stop)
	<-loopDone
	d.job.Wait()
	return err
}

// RunDevice keeps this workspace's infrastructure alive without a listener of its own: the Hub that owns several
// workspaces serves them all. It returns when ctx ends and everything it started has stopped.
func (d *Daemon) RunDevice(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.mu.Lock()
	d.ctx = runCtx
	d.mu.Unlock()
	d.deviceLoop(runCtx)
	d.job.Wait()
}

// Handler is used only on loopback. Workspace credentials never reach the renderer.
func (d *Daemon) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/device/v1/state", d.localState)
	mux.HandleFunc("GET /api/device/v1/summary", d.workspaceSummary)
	mux.HandleFunc("POST /api/device/v1/create", d.createWorkspace)
	mux.HandleFunc("POST /api/device/v1/invitation", d.inspectInvitation)
	mux.HandleFunc("POST /api/device/v1/join", d.joinWorkspace)
	mux.HandleFunc("POST /api/device/v1/join/cancel", d.cancelJoin)
	mux.HandleFunc("POST /api/device/v1/license", d.importLicense)
	mux.HandleFunc("PUT /api/device/v1/settings", d.saveSettings)
	mux.HandleFunc("POST /api/device/v1/execution/preview", d.previewExecution)
	mux.HandleFunc("POST /api/device/v1/execution/approve", d.approveExecution)
	mux.HandleFunc("POST /api/device/v1/execution/start", d.startExecution)
	mux.HandleFunc("GET /api/device/v1/execution/detail", d.executionDetail)
	mux.HandleFunc("POST /api/device/v1/execution/{action}", d.executionControl)
	mux.HandleFunc("POST /api/device/v1/runner/connect", d.connectRunner)
	mux.HandleFunc("POST /api/device/v1/runner/grant", d.acceptRunnerGrant)
	mux.HandleFunc("POST /api/device/v1/senders/{id}/trust", d.trustSender)
	mux.HandleFunc("POST /api/device/v1/senders/{id}/revoke", d.revokeSender)
	mux.HandleFunc("POST /api/device/v1/tasks/{id}/approve", d.approveTask)
	mux.HandleFunc("POST /api/device/v1/requests", d.sendRequest)
	mux.HandleFunc("GET /api/device/v1/removal", d.removalPlan)
	mux.HandleFunc("POST /api/device/v1/leave", d.leaveWorkspace)
	mux.HandleFunc("/api/team/v1/", d.workspaceRequest)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpkit.WriteError(w, 404, "not_found", "no such endpoint")
	})
	secured := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !bearer || len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(token), []byte(d.key())) != 1 {
			httpkit.WriteError(w, 401, "unauthorized", "open Werkbord Team to connect to this device")
			return
		}
		mux.ServeHTTP(w, r)
	})
	root := http.NewServeMux()
	root.Handle("/api/", secured)
	root.Handle("/", console.Handler())
	// The Werkbord desktop app shows this service's console in a frame next to a person's other workspaces: its window may.
	return httpkit.SecurityHeadersFramed(httpkit.FramedCSP(httpkit.DefaultCSP, d.o.Config.EmbedOrigins), daemonOrigin(root))
}

func daemonOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if err := sameHostOrigin(origin, r.Host); err != nil {
				httpkit.WriteError(w, 403, "forbidden_origin", "open Werkbord Team on this computer")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// sameHostOrigin accepts only a page that this same loopback service served.
func sameHostOrigin(origin, host string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Host != host {
		return errors.New("a page from somewhere else")
	}
	return nil
}

func daemonFail(w http.ResponseWriter, err error) {
	httpkit.WriteError(w, http.StatusConflict, "device", err.Error())
}
func daemonDecode(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := httpkit.DecodeJSON(w, r, out, 64<<10); err != nil {
		httpkit.WriteError(w, 400, "invalid", err.Error())
		return false
	}
	return true
}

func (d *Daemon) notify() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}
func (d *Daemon) problem(err error) {
	d.mu.Lock()
	if err == nil {
		d.lastError = ""
	} else {
		d.lastError = err.Error()
	}
	d.mu.Unlock()
}
func (d *Daemon) launchOperation(name string, fn func(context.Context) error) error {
	d.mu.Lock()
	if d.operation != "" {
		d.mu.Unlock()
		return errors.New("another setup step is still running")
	}
	if d.ctx == nil || d.ctx.Err() != nil {
		d.mu.Unlock()
		return errors.New("the device service is not running")
	}
	d.operation = name
	d.lastError = ""
	ctx := d.ctx
	d.job.Add(1)
	d.mu.Unlock()
	go func() {
		defer d.job.Done()
		err := fn(ctx)
		d.mu.Lock()
		d.operation = ""
		if err != nil {
			d.lastError = err.Error()
		}
		d.mu.Unlock()
		d.notify()
	}()
	return nil
}

func (d *Daemon) workspaceRequest(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	c := d.host
	d.mu.RUnlock()
	if c == nil {
		httpkit.WriteError(w, 503, "connecting", "the workspace is reconnecting; your device service is running")
		return
	}
	// The device's own credential replaces the GUI credential. No forwarding target or arbitrary headers are accepted.
	body, err := readBounded(r.Body, 64<<10)
	if err != nil {
		daemonFail(w, err)
		return
	}
	status, header, raw, err := c.Raw(r.Context(), r.Method, r.URL.RequestURI(), r.Header.Get("Content-Type"), body)
	if err != nil {
		httpkit.WriteError(w, 503, "unreachable", "the workspace is offline; keep a Workspace Host online and try again")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if retry := header.Get("Retry-After"); retry != "" {
		w.Header().Set("Retry-After", retry)
	}
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func (d *Daemon) licenseClaims() (license.Claims, error) {
	f, err := os.Open(filepath.Join(d.o.Config.DataDir, "license.json"))
	if err != nil {
		return license.Claims{}, err
	}
	defer f.Close()
	b, err := readBounded(f, 16384)
	if err != nil {
		return license.Claims{}, err
	}
	return license.Verify(b, d.o.LicenseKey, time.Now())
}

func (d *Daemon) importLicense(w http.ResponseWriter, r *http.Request) {
	var in struct {
		License  json.RawMessage `json:"license"`
		Document string          `json:"document"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	if in.Document != "" {
		in.License = json.RawMessage(in.Document)
	}
	c, err := license.Verify(in.License, d.o.LicenseKey, time.Now())
	if err != nil {
		daemonFail(w, err)
		return
	}
	d.mu.RLock()
	host := d.host
	d.mu.RUnlock()
	if host != nil {
		me, err := host.Me(r.Context())
		if err != nil {
			daemonFail(w, err)
			return
		}
		if me.Member.Role != domain.RoleOwner {
			daemonFail(w, errors.New("the workspace owner manages its license"))
			return
		}
		if err := host.Do(r.Context(), "PUT", "/license", map[string]any{"document": in.License}, nil); err != nil {
			daemonFail(w, err)
			return
		}
	}
	if err := writeDaemonFile(filepath.Join(d.o.Config.DataDir, "license.json"), in.License); err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 200, c)
}

func (d *Daemon) localState(w http.ResponseWriter, r *http.Request) {
	httpkit.WriteJSON(w, 200, d.stateView())
}

// stateView is everything the window may know about this device and its workspace. The Hub lists one per workspace.
func (d *Daemon) stateView() map[string]any {
	d.mu.RLock()
	op, problem, mat, bridge, lastSync, runnerCheck, runnerOK, role := d.operation, d.lastError, d.mat, d.bridge, d.lastSync, d.lastRunnerCheck, d.runnerOK, d.role
	d.mu.RUnlock()
	var senders []map[string]any
	for _, s := range d.state.Senders() {
		key, _ := deviceid.ParsePublicKey(s.PublicKey)
		senders = append(senders, map[string]any{"deviceId": s.DeviceID, "name": s.Name, "publicKey": s.PublicKey, "identity": deviceid.Fingerprint(key), "approved": s.Approved()})
	}
	out := map[string]any{"daemon": true, "version": d.o.Version, "operation": op, "error": problem, "settings": d.state.Settings(), "senders": senders, "opened": d.state.OpenedTasks(), "approvals": d.state.Approvals(), "runner": map[string]any{"connected": runnerOK && time.Since(runnerCheck) < time.Minute, "configured": bridge != nil}}
	if d.o.Slot != "" {
		out["slot"] = d.o.Slot
	}
	if c, err := d.licenseClaims(); err == nil {
		out["license"] = c
	} else {
		out["licenseError"] = "Import an active Werkbord Team license to create a workspace."
	}
	if mat != nil {
		out["workspace"] = map[string]any{"id": mat.Meta.WorkspaceID, "name": mat.Meta.WorkspaceName}
		out["deviceId"] = mat.Meta.HostDeviceID
		out["identity"] = deviceid.Fingerprint(mat.Host.PublicKey())
		out["workspaceHost"] = mat.Meta.Authority
		out["connected"] = !lastSync.IsZero() && time.Since(lastSync) < time.Minute
		if role != "" {
			out["role"] = role
		}
	}
	occ := d.occupancy()
	if occ.Enrolled {
		out["enrolled"] = true
	}
	if occ.Pending {
		out["pending"] = true
	}
	if occ.Leaving {
		out["leaving"] = true
	}
	if err := d.hostingAllowed(); err != nil {
		out["hostingBlocked"] = err.Error()
	}
	return out
}

// occupancy is what is on this workspace slot, read from the disk and not from any other slot, so it can be asked while the
// Hub holds its lock.
type occupancy struct{ Enrolled, Pending, Leaving bool }

func (d *Daemon) occupancy() occupancy {
	var o occupancy
	_, err := os.Stat(filepath.Join(d.workspaceConfig().PKIDir(), "workspace.json"))
	o.Enrolled = err == nil
	_, err = os.Stat(filepath.Join(d.o.Config.DataDir, "pending-join"))
	o.Pending = err == nil
	_, err = os.Stat(filepath.Join(d.o.Config.DataDir, "leaving"))
	o.Leaving = err == nil
	return o
}

var errHostingBlocked = errors.New("this computer already hosts another Team workspace, and a computer can host only one")

// hostingAllowed says whether this workspace may hold a Workspace Host or Connectivity Host role on this computer.
// A Host listens on fixed, workspace-wide ports; two workspaces on one computer would take the same ones.
func (d *Daemon) hostingAllowed() error {
	if d.o.HostingGuard == nil {
		return nil
	}
	return d.o.HostingGuard()
}

// workspaceDirectory uses a key pinned on this computer. A Host's rewritten registry cannot forge a trusted sender.
type workspaceDirectory struct {
	d        *Daemon
	client   *hostclient.Client
	self     *pki.Material
	memberID string
}

func (x workspaceDirectory) Device(ctx context.Context, ws, id string) (envelope.Device, error) {
	if ws != x.self.Meta.WorkspaceID {
		return envelope.Device{}, envelope.ErrUnknownKey
	}
	if id == x.self.Host.DeviceID() {
		return envelope.Device{ID: id, WorkspaceID: ws, OwnerID: x.memberID, PublicKey: x.self.Host.PublicKey()}, nil
	}
	for _, s := range x.d.state.Senders() {
		if s.DeviceID == id && s.Approved() {
			key, err := deviceid.ParsePublicKey(s.PublicKey)
			if err != nil {
				return envelope.Device{}, envelope.ErrUnknownKey
			}
			ds, err := x.client.Devices(ctx)
			if err != nil {
				return envelope.Device{}, err
			}
			for _, dv := range ds {
				if dv.ID == id {
					return envelope.Device{ID: id, WorkspaceID: ws, OwnerID: x.memberID, PublicKey: key, Revoked: dv.Revoked() || dv.MemberID != x.memberID || dv.PublicKey != s.PublicKey}, nil
				}
			}
		}
	}
	return envelope.Device{}, envelope.ErrUnknownKey
}

func (d *Daemon) loadDevice() error {
	cfg := d.workspaceConfig()
	if _, err := os.Stat(filepath.Join(cfg.PKIDir(), "workspace.json")); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	sealer, err := SealerFor(cfg)
	if err != nil {
		return err
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		return err
	}
	mat, err := v.Load(time.Now())
	if err != nil {
		return err
	}
	token, err := v.DeviceToken()
	if err != nil {
		return err
	}
	var bases []string
	if mat.Meta.Authority {
		bases = append(bases, "http://"+cfg.ClientAddr())
	}
	if j, err := v.JoinInfo(); err == nil {
		for _, a := range j.APIAddrs {
			bases = append(bases, "http://"+net.JoinHostPort(a, fmt.Sprint(j.APIPort)))
		}
	}
	prefix, err := netip.ParsePrefix(mat.Meta.NetworkPrefix)
	if err != nil {
		return err
	}
	member, err := v.Secret("member")
	if err != nil {
		return err
	}
	c, err := hostclient.New(hostclient.Options{Bases: bases, Token: token, Network: prefix, Timeout: 8 * time.Second, DialContext: d.hostDial, Signer: mat.Host, WorkspaceID: mat.Meta.WorkspaceID, UserID: string(member)})
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.mat, d.vault, d.host = mat, v, c
	d.handler = nil
	d.mu.Unlock()
	return nil
}

func (d *Daemon) profile() domain.DeviceProfile {
	s := d.state.Settings()
	form := domain.DeviceForm(s.Form)
	if form == "" {
		form = domain.FormUnknown
	}
	d.mu.RLock()
	authority := d.mat != nil && d.mat.Meta.Authority
	d.mu.RUnlock()
	// A device that does not host this workspace but already hosts another cannot be asked to host this one.
	conflict := !authority && d.hostingAllowed() != nil
	return domain.DeviceProfile{Platform: runtime.GOOS, Form: form, Sleeps: form == domain.FormLaptop, Version: d.o.Version, HostConflict: conflict}
}

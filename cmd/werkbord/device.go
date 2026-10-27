package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"werkbord/internal/clientaccess"
	"werkbord/internal/config"
	"werkbord/internal/controller"
	"werkbord/internal/daemon"
	"werkbord/internal/device"
	"werkbord/internal/netprivate"
	"werkbord/internal/remote"
	"werkbord/internal/webui"
)

// cmdDevice configures local capabilities. Client credentials are never supplied
// as command-line arguments, logged, or sent to the local-controller API.
func (a *app) cmdDevice(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: werkbord device <connect|local|start|stop|serve|status>")
	}
	fs := flag.NewFlagSet("device", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	force := fs.Bool("force", false, "stop this device runtime even with local work; inspect interrupted operations before restarting")
	pairAgain := fs.Bool("pair", false, "replace this desktop client grant using a fresh code")
	name := fs.String("name", "", "desktop display name")
	runner := fs.Bool("runner", false, "also run this machine's separately paired runner")
	commonFlags(fs, &a.cfg)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return flag.ErrHelp
	}
	sub := positional[0]
	positional = positional[1:]
	state, err := device.Load(a.cfg.DataDir)
	if err != nil {
		return err
	}
	wasRemote := state.Remote()
	if sub == "serve" {
		return serveDevice(a.cfg, state, a.out)
	}
	mgr := a.runnerDaemon(ctx) // adopts the existing runner service and tsnet directory
	if sub != "status" {
		if def, ok := a.describeRunner(ctx); ok && def.DataDir != "" && filepath.Clean(def.DataDir) != filepath.Clean(a.cfg.DataDir) {
			return errors.New("the runner service belongs to another data directory; use that installation rather than replacing its service")
		}
	}
	switch sub {
	case "status":
		st, err := mgr.Status(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(a.out).Encode(map[string]any{"mode": state.Mode, "controller": state.Controller, "runner": state.Runner, "service": st})
	case "stop":
		if !*force {
			if err := a.checkRunnerSettled(); err != nil {
				return err
			}
		}
		return mgr.Stop(ctx)
	case "start":
		if state.Mode != "remote" {
			return errors.New("finish connecting first with werkbord device connect")
		}
		return a.startDevice(ctx, mgr)
	case "connect", "local":
	default:
		return errors.New("usage: werkbord device <connect|local|start|stop|serve|status>")
	}
	if err := os.MkdirAll(filepath.Join(a.cfg.DataDir, "device"), 0700); err != nil {
		return err
	}
	unlock, err := controller.LockRunner(filepath.Join(a.cfg.DataDir, "device"))
	if err != nil {
		return err
	}
	defer unlock()
	if err := a.checkRunnerSettled(); err != nil {
		return err
	}
	if wasRemote {
		if _, up := a.healthy(ctx); up {
			c, e := a.client()
			if e != nil {
				return e
			}
			if e = c.do(ctx, "POST", "/api/device/switch/prepare", nil, nil); e != nil {
				return e
			}
			defer func() {
				if current, err := device.Load(a.cfg.DataDir); err == nil && current.Mode == "remote" {
					_ = c.do(context.Background(), "POST", "/api/device/switch/resume", nil, nil)
				}
			}()
		}
	}
	if sub == "local" {
		return a.deviceLocal(ctx, state, mgr)
	}
	if len(positional) != 1 {
		return errors.New("usage: werkbord device connect <controller-url> [--name NAME] [--runner]; the pairing code is read from stdin")
	}
	address := strings.TrimSuffix(positional[0], "/")
	if err := device.ValidateController(address); err != nil {
		return err
	}
	if !a.cfg.IsLoopback() || !a.cfg.RequireToken {
		return errors.New("desktop control requires authenticated loopback networking")
	}
	if state.Remote() && state.Controller != address {
		return errors.New("switch back with werkbord device local before selecting another controller")
	}
	if *runner {
		id, err := loadIdentity(a.cfg)
		if err != nil {
			return err
		}
		if id.Controller != address {
			return errors.New("pair the runner to this same controller first")
		}
	}
	credential, err := device.LoadCredential(a.cfg.DataDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !*pairAgain {
		return err
	}
	if *pairAgain || errors.Is(err, os.ErrNotExist) || credential.Controller != address {
		fmt.Fprintln(a.out, "Enter the desktop pairing code from the controller:")
		raw, err := bufio.NewReader(io.LimitReader(os.Stdin, 201)).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		code := strings.TrimSpace(raw)
		if len(code) != 64 {
			return errors.New("invalid desktop pairing code")
		}
		// Stop an idle runner before opening its network state. It is restarted on
		// failure so unsuccessful client enrollment cannot disable the runner role.
		before, err := mgr.Status(ctx)
		if err != nil {
			return err
		}
		if before.Running {
			if err = mgr.Stop(ctx); err != nil {
				return err
			}
			defer func() {
				if mode, e := device.Load(a.cfg.DataDir); e == nil && (!mode.Remote() || mode.Mode == "remote") {
					_ = mgr.Start(context.Background())
				}
			}()
		}
		hc, closeNode, err := privateRunnerClient(ctx, a.cfg, a.out, address)
		if err != nil {
			return err
		}
		id, err := device.DeviceID(a.cfg.DataDir)
		if err != nil {
			closeNode()
			return err
		}
		if *name == "" {
			*name, _ = os.Hostname()
		}
		request := map[string]string{"code": code, "name": *name, "deviceId": id}
		var reply struct {
			clientaccess.Entry
			Token string `json:"token"`
		}
		c := &client{base: address, http: hc}
		err = c.do(ctx, "POST", "/api/client/v1/enroll", request, &reply)
		closeNode()
		if err != nil {
			return err
		}
		credential = device.Credential{Controller: address, ID: reply.ID, DeviceID: id, Token: reply.Token}
		if !strings.HasPrefix(credential.Token, clientaccess.TokenPrefix) || reply.DeviceID != id {
			return errors.New("controller returned an invalid desktop grant")
		}
		if err = clientaccess.WriteJSON(device.CredentialPath(a.cfg.DataDir), credential); err != nil {
			return err
		}
	}
	// Preserve how local startup worked before entering the recoverable transition.
	if !state.Remote() {
		state = device.State{Mode: "switching", Controller: address, Runner: *runner}
		if _, installed := a.installed(ctx); installed {
			def, ok := a.describe(ctx)
			if !ok || def.Binary == "" {
				return errors.New("cannot preserve the existing controller service; inspect it before switching")
			}
			state.RestoreService = true
			state.RestoreBinary = def.Binary
			state.RestorePATH = a.path()
		}
	} else {
		state.Mode = "switching"
		state.Runner = *runner
	}
	if err = device.Save(a.cfg.DataDir, state); err != nil {
		return err
	}
	// A running controller fences starts under the same locks used for dispatch.
	if _, up := a.healthy(ctx); up && !wasRemote {
		c, err := a.client()
		if err != nil {
			return err
		}
		if err = c.do(ctx, "POST", "/api/device/switch/prepare", nil, nil); err != nil {
			if resumeErr := c.do(ctx, "POST", "/api/device/switch/resume", nil, nil); resumeErr == nil {
				_ = device.Save(a.cfg.DataDir, device.State{Mode: "local"})
			}
			return err
		}
	}
	if !wasRemote {
		if err = a.stop(ctx, false); err != nil {
			return err
		}
		if _, installed := a.installed(ctx); installed {
			if err = a.daemon(ctx).Uninstall(ctx); err != nil {
				return err
			}
		}
	}
	// Ensure no foreground controller still holds the data directory.
	release, err := controller.LockController(a.cfg.LockPath())
	if err != nil {
		return err
	}
	defer release()
	if err = mgr.Stop(ctx); err != nil {
		return err
	}
	state.Mode = "remote"
	if err = device.Save(a.cfg.DataDir, state); err != nil {
		return err
	}
	if err = a.startDevice(ctx, mgr); err != nil {
		return err
	}
	a.printf("Selected controller %s. Open Werkbord to check the connection. Local data is retained.\n", address)
	return nil
}
func (a *app) checkRunnerSettled() error {
	rawTerminals, err := os.ReadFile(filepath.Join(runnerDir(a.cfg), "terminals.json"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(rawTerminals) > 0 {
		var sessions []struct {
			State string `json:"state"`
		}
		if json.Unmarshal(rawTerminals, &sessions) != nil {
			return errors.New("cannot inspect runner terminals")
		}
		for _, session := range sessions {
			if session.State == "running" {
				return errors.New("finish runner terminals before switching")
			}
		}
	}
	receipts, err := filepath.Glob(filepath.Join(runnerDir(a.cfg), "operations", "*.json"))
	if err != nil {
		return err
	}
	for _, path := range receipts {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var receipt struct{ Result struct{ State string } }
		if json.Unmarshal(raw, &receipt) != nil || receipt.Result.State == "executing" {
			return fmt.Errorf("inspect interrupted device operation before switching: %s", filepath.Base(path))
		}
	}

	files, err := filepath.Glob(filepath.Join(runnerDir(a.cfg), "runs", "*.json"))
	if err != nil {
		return err
	}
	for _, f := range files {
		var r struct {
			Phase string `json:"phase"`
		}
		raw, err := os.ReadFile(f)
		if err != nil || json.Unmarshal(raw, &r) != nil || (r.Phase != "ended" && r.Phase != "resolved") {
			return fmt.Errorf("finish or resolve runner work before switching: %s", filepath.Base(f))
		}
	}
	return nil
}
func (a *app) startDevice(ctx context.Context, mgr daemon.Manager) error {
	binary, err := a.executable()
	if err != nil {
		return err
	}
	if _, err = a.cfg.ResolveToken(true); err != nil {
		return err
	}
	spec := daemon.Spec{Binary: binary, Args: []string{"device", "serve"}, DataDir: a.cfg.DataDir, LogFile: filepath.Join(runnerDir(a.cfg), "logs", "runner.log"), Path: a.path()}
	if err = mgr.Install(ctx, spec); err != nil {
		return err
	}
	return mgr.Start(ctx)
}
func (a *app) deviceLocal(ctx context.Context, state device.State, mgr daemon.Manager) error {
	if !state.Remote() {
		return nil
	}
	state.Mode = "restoring"
	if err := device.Save(a.cfg.DataDir, state); err != nil {
		return err
	}
	if err := mgr.Uninstall(ctx); err != nil {
		return err
	}
	// Keep the recovery record until the original service definition is restored.
	if state.RestoreService {
		if state.RestoreBinary == "" {
			return errors.New("saved controller executable missing")
		}
		spec := daemon.Spec{Binary: state.RestoreBinary, Args: []string{"serve"}, DataDir: a.cfg.DataDir, LogFile: filepath.Join(a.cfg.DataDir, "logs", "controller.log"), Path: state.RestorePATH}
		if err := a.daemon(ctx).Install(ctx, spec); err != nil {
			return err
		}
	}
	state.Mode = "local"
	if err := device.Save(a.cfg.DataDir, state); err != nil {
		return err
	}
	a.printf("Local controller selected. Remote credentials and local data are retained.\n")
	if _, up := a.healthy(ctx); up {
		if c, e := a.client(); e == nil {
			if e = c.do(ctx, "POST", "/api/device/switch/resume", nil, nil); e != nil {
				return e
			}
		}
	}
	return a.cmdStart(ctx, nil)
}

func serveDevice(cfg config.Config, state device.State, out io.Writer) error {
	if state.Mode != "remote" {
		return errors.New("device connection needs recovery; use werkbord device connect or werkbord device local")
	}
	if !cfg.IsLoopback() || !cfg.RequireToken {
		return errors.New("device gateway requires authenticated loopback")
	}
	credential, err := device.LoadCredential(cfg.DataDir)
	if err != nil {
		return err
	}
	if credential.Controller != state.Controller {
		return errors.New("desktop credential belongs to another controller")
	}
	if err := os.MkdirAll(runnerDir(cfg), 0700); err != nil {
		return err
	}
	release, err := controller.LockRunner(runnerDir(cfg))
	if err != nil {
		return err
	}
	defer release()
	token, err := cfg.ResolveToken(true)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	backend := netprivate.NewTailscale(netprivate.TailscaleOptions{Dir: filepath.Join(runnerDir(cfg), "tailscale"), Hostname: netprivate.DefaultHostname(host) + "-runner", ControlURL: cfg.Network.ControlURL, AuthKey: config.Getenv("TS_AUTHKEY")})
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = backend.Start(ctx); err != nil {
		return err
	}
	defer backend.Close()
	dialer, ok := backend.(interface {
		Dial(context.Context, string, string) (net.Conn, error)
	})
	if !ok {
		return errors.New("private network cannot dial")
	}
	transport := &http.Transport{DialContext: dialer.Dial, ResponseHeaderTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	var worker *remote.Worker
	runnerID := ""
	if state.Runner {
		id, err := loadIdentity(cfg)
		if err != nil {
			return err
		}
		if id.Controller != state.Controller {
			return errors.New("runner belongs to another controller")
		}
		agents, err := controller.NewAgents(cfg)
		if err != nil {
			return err
		}
		runnerID = id.RunnerID
		worker = &remote.Worker{TerminalCommands: map[string]string{"codex": cfg.Agents["codex"].Command, "claude-code": cfg.Agents["claude-code"].Command}, Version: version, Dir: runnerDir(cfg), Identity: id, Agents: agents, AllowClone: id.AllowClone, Client: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
		worker.LoadBindings = func() map[string]string {
			bindings := map[string]string{}
			if raw, e := os.ReadFile(filepath.Join(runnerDir(cfg), "repositories.json")); e == nil {
				_ = json.Unmarshal(raw, &bindings)
			}
			return bindings
		}
	}
	var prepareSwitch func() error
	var resumeSwitch func()
	if worker != nil {
		prepareSwitch = worker.PauseStarts
		resumeSwitch = worker.ResumeStarts
	}
	handler, err := device.Gateway(device.GatewayOptions{ResumeSwitch: resumeSwitch, PrepareSwitch: prepareSwitch, Credential: credential, Token: token, TokenSource: func() string { value, _ := cfg.ResolveToken(false); return value }, Transport: transport, Web: webui.Handler(), Version: version, RunnerID: runnerID})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	done := make(chan error, 1)
	go func() {
		e := server.Serve(listener)
		if errors.Is(e, http.ErrServerClosed) {
			e = nil
		}
		done <- e
	}()
	workerDone := make(chan error, 1)
	if worker != nil {
		go func() { workerDone <- worker.Run(ctx) }()
	}
	fmt.Fprintln(out, "Desktop gateway ready; remote coordination uses the private network.")
	workerExited := false
	select {
	case <-ctx.Done():
	case err = <-done:
	case err = <-workerDone:
		workerExited = true
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	_ = server.Shutdown(shutdown)
	if worker != nil && !workerExited {
		select {
		case <-workerDone:
		case <-shutdown.Done():
		}
	}
	return err
}

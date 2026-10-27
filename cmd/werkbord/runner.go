package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
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
	"runtime"
	"strings"
	"syscall"
	"time"

	"werkbord/internal/browser"
	"werkbord/internal/config"
	"werkbord/internal/controller"
	"werkbord/internal/daemon"
	"werkbord/internal/device"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/gitrepo"
	"werkbord/internal/netprivate"
	"werkbord/internal/remote"
	"werkbord/internal/runnerwire"
)

func runnerDir(cfg config.Config) string { return filepath.Join(cfg.DataDir, "runner") }
func loadIdentity(cfg config.Config) (remote.Identity, error) {
	var id remote.Identity
	b, e := os.ReadFile(filepath.Join(runnerDir(cfg), "identity.json"))
	if e != nil {
		return id, e
	}
	e = json.Unmarshal(b, &id)
	return id, e
}

// privateRunnerClient embeds a separate node in the user's own tailnet. Sign-in
// is interactive once, and the network identity remains local in the runner dir.
func privateRunnerClient(ctx context.Context, cfg config.Config, out io.Writer, address string) (*http.Client, func(), error) {
	uAddr, _, e := runnerwire.DecodeCode(runnerwire.EncodeCode(address, base64.RawURLEncoding.EncodeToString(make([]byte, 24))))
	if e != nil {
		return nil, nil, e
	}
	_ = uAddr
	host, _ := os.Hostname()
	backend := netprivate.NewTailscale(netprivate.TailscaleOptions{Dir: filepath.Join(runnerDir(cfg), "tailscale"), Hostname: netprivate.DefaultHostname(host) + "-runner", ControlURL: cfg.Network.ControlURL, AuthKey: config.Getenv("TS_AUTHKEY")})
	if e := backend.Start(ctx); e != nil {
		return nil, nil, e
	}
	closeNode := func() { _ = backend.Close() }
	opened := ""
	for {
		status, e := backend.Status(ctx)
		if e != nil {
			closeNode()
			return nil, nil, e
		}
		if status.State == "Running" {
			break
		}
		if status.AuthURL != "" && status.AuthURL != opened {
			opened = status.AuthURL
			fmt.Fprintf(out, "Sign in to the same private network as your controller:\n%s\n", status.AuthURL)
			_ = browser.Open(status.AuthURL)
		}
		select {
		case <-ctx.Done():
			closeNode()
			return nil, nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	dialer, ok := backend.(interface {
		Dial(context.Context, string, string) (net.Conn, error)
	})
	if !ok {
		closeNode()
		return nil, nil, fmt.Errorf("private network cannot dial controller")
	}
	transport := &http.Transport{DialContext: dialer.Dial}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, func() { transport.CloseIdleConnections(); closeNode() }, nil
}

func cmdJoin(cfg config.Config, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	fs.SetOutput(errOut)
	commonFlags(fs, &cfg)
	name := fs.String("name", "", "runner display name")
	allowClone := fs.Bool("allow-clone", false, "allow cloning owner-authorized repositories with this machine's own Git credentials")
	foreground := fs.Bool("foreground", false, "run here instead of installing a separate runner login service")
	positional, e := parseInterspersed(fs, args)
	if e != nil {
		return e
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: werkbord join <pairing-code> [--name NAME] [--allow-clone] [--foreground]")
	}
	if _, e := loadIdentity(cfg); e == nil {
		return fmt.Errorf("this machine is already paired: use werkbord runner start or werkbord runner serve")
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	address, secret, e := runnerwire.DecodeCode(positional[0])
	if e != nil {
		return e
	}
	pendingPath := filepath.Join(runnerDir(cfg), "pending-pair.json")
	pending := struct {
		Hash    string
		Private ed25519.PrivateKey
	}{}
	if b, err := os.ReadFile(pendingPath); err == nil {
		if err := json.Unmarshal(b, &pending); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if pending.Hash != runnerwire.SecretHash(positional[0]) || len(pending.Private) != ed25519.PrivateKeySize {
		_, pending.Private, e = ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		pending.Hash = runnerwire.SecretHash(positional[0])
		if e := remote.AtomicJSON(pendingPath, pending); e != nil {
			return e
		}
	}
	private := pending.Private
	public := private.Public().(ed25519.PublicKey)
	host, _ := os.Hostname()
	if *name == "" {
		*name = strings.TrimSuffix(host, ".local")
	}
	if *name == "" {
		*name = "Runner"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client, closeNode, e := privateRunnerClient(ctx, cfg, out, address)
	if e != nil {
		return e
	}
	in := runnerwire.Join{Secret: secret, PublicKey: base64.RawURLEncoding.EncodeToString(public), Name: *name, Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH, Version: version}
	body, e := json.Marshal(in)
	if e != nil {
		closeNode()
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", address+"/api/runner/join", strings.NewReader(string(body)))
	if e != nil {
		closeNode()
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Runner-Signature", runnerwire.Signature(private, body))
	res, e := client.Do(req)
	if e != nil {
		closeNode()
		return fmt.Errorf("cannot reach controller on private network")
	}
	defer res.Body.Close()
	if res.StatusCode != 201 {
		closeNode()
		return fmt.Errorf("pairing refused (HTTP %d); create a new code in Settings → Runners", res.StatusCode)
	}
	var r domain.Runner
	e = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&r)
	closeNode()
	if e != nil {
		return e
	}
	id := remote.Identity{Controller: address, RunnerID: r.ID, PrivateKey: private, Projects: r.Projects, AllowClone: *allowClone}
	if e := remote.AtomicJSON(filepath.Join(runnerDir(cfg), "identity.json"), id); e != nil {
		return e
	}
	_ = os.Remove(pendingPath)
	fmt.Fprintf(out, "Paired %s (%s). Project access is controlled in Settings → Runners.\n", r.Name, r.ID)
	if *foreground {
		return cmdRunner(cfg, []string{"serve"}, out, errOut)
	}
	return cmdRunner(cfg, []string{"start"}, out, errOut)
}

func cmdRunner(cfg config.Config, args []string, out, errOut io.Writer) error {
	if state, e := device.Load(cfg.DataDir); e != nil {
		return e
	} else if state.Remote() && len(args) > 0 && (args[0] == "serve" || args[0] == "start" || args[0] == "stop") {
		return newApp(cfg, out, errOut).cmdDevice(context.Background(), args)
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: werkbord runner <serve|start|stop|status|repo|resolve|terminals|operation>")
	}
	fs := flag.NewFlagSet("runner", flag.ContinueOnError)
	fs.SetOutput(errOut)
	commonFlags(fs, &cfg)
	terminalEnable := fs.Bool("enable", false, "allow a native coding-agent terminal on this runner")
	terminalSignedIn := fs.Bool("signed-in", false, "confirm that you have signed in to this agent CLI locally")
	controllerOperation := fs.Bool("controller", false, "inspect a local controller operation receipt instead of a remote runner receipt")
	inspected := fs.Bool("confirm-inspected", false, "confirm an interrupted operation outcome was inspected on this machine")
	confirmed := fs.Bool("confirm-stopped", false, "confirm the uncertain run has no remaining processes on this machine")
	positional, e := parseInterspersed(fs, args)
	if e != nil {
		return e
	}
	if len(positional) == 0 {
		return flag.ErrHelp
	}
	sub := positional[0]
	positional = positional[1:]
	dir := runnerDir(cfg)
	if sub == "terminals" {
		if len(positional) != 1 || (positional[0] != "codex" && positional[0] != "claude-code") {
			return fmt.Errorf("usage: werkbord runner terminals <codex|claude-code> [--enable --signed-in]")
		}
		prefs := map[string]domain.AgentSelectionPreference{}
		path := filepath.Join(dir, "terminal-permissions.json")
		if raw, err := os.ReadFile(path); err == nil {
			if err = json.Unmarshal(raw, &prefs); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		prefs[positional[0]] = domain.AgentSelectionPreference{DirectCLI: *terminalEnable, CLISignedIn: *terminalSignedIn}
		return remote.AtomicJSON(path, prefs)
	}
	if sub == "operation" {
		if len(positional) != 1 || !*inspected {
			return fmt.Errorf("inspect the owning checkout first, then: werkbord runner operation <operation-id> --confirm-inspected")
		}
		if *controllerOperation {
			dir = filepath.Join(cfg.DataDir, "device-operations")
		}
		path := filepath.Join(dir, "operations", positional[0]+".json")
		if !strings.HasPrefix(positional[0], "op_") || strings.ContainsAny(positional[0], "/\\.") {
			return fmt.Errorf("invalid operation id")
		}
		lock := func() (func(), error) {
			if *controllerOperation {
				return controller.LockController(cfg.LockPath())
			}
			return controller.LockRunner(dir)
		}
		release, err := lock()
		if err != nil {
			return err
		}
		defer release()
		var receipt struct {
			Hash   string            `json:"hash"`
			Result devicewire.Result `json:"result"`
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &receipt); err != nil {
			return err
		}
		if receipt.Result.State != "executing" {
			return fmt.Errorf("operation has no interrupted execution receipt")
		}
		receipt.Result.State = "uncertain"
		receipt.Result.Error = "owner inspected the interrupted operation; request ID remains fenced"
		return remote.AtomicJSON(path, receipt)
	}
	identity, e := loadIdentity(cfg)
	if e != nil {
		return fmt.Errorf("runner identity unavailable; pair this machine with werkbord join first")
	}
	if sub == "repo" {
		if len(positional) != 2 {
			return fmt.Errorf("usage: werkbord runner repo <project-id> <local-clone>")
		}
		if !containsProject(identity.Projects, positional[0]) {
			return fmt.Errorf("project is not authorized on this runner")
		}
		repo, e := (&gitrepo.CLI{}).Inspect(context.Background(), positional[1])
		if e != nil {
			return e
		}
		bindings := map[string]string{}
		raw, e := os.ReadFile(filepath.Join(dir, "repositories.json"))
		if e == nil {
			if e := json.Unmarshal(raw, &bindings); e != nil {
				return e
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		bindings[positional[0]] = repo.RootPath
		if e := remote.AtomicJSON(filepath.Join(dir, "repositories.json"), bindings); e != nil {
			return e
		}
		fmt.Fprintf(out, "Bound %s to %s. The runner loads this on its next heartbeat.\n", positional[0], repo.RootPath)
		return nil
	}
	manager := daemon.Detect(context.Background(), daemon.Options{DataDir: dir, Label: daemon.RunnerLabel})
	switch sub {
	case "status":
		st, e := manager.Status(context.Background())
		if e != nil {
			return e
		}
		var last json.RawMessage
		var health *remote.Health
		if data, err := os.ReadFile(filepath.Join(dir, "health.json")); err == nil {
			_ = json.Unmarshal(data, &health)
		}
		if data, err := os.ReadFile(filepath.Join(dir, "last-error.json")); err == nil {
			last = data
		}
		return json.NewEncoder(out).Encode(map[string]any{"service": st, "lastSyncError": last, "lastSuccessfulSync": health})
	case "stop":
		return manager.Stop(context.Background())
	case "start":
		binary, e := executable()
		if e != nil {
			return e
		}
		home, _ := os.UserHomeDir()
		spec := daemon.Spec{Binary: binary, Args: []string{"runner", "serve"}, DataDir: cfg.DataDir, LogFile: filepath.Join(dir, "logs", "runner.log"), Path: daemon.ServicePATH(os.Getenv("PATH"), home)}
		if e := manager.Install(context.Background(), spec); e != nil {
			return e
		}
		if e := manager.Start(context.Background()); e != nil {
			return e
		}
		fmt.Fprintln(out, "Runner service started. It reconnects through your private network.")
		return nil
	case "serve", "resolve":
		if e := os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		release, e := controller.LockRunner(dir)
		if e != nil {
			return e
		}
		defer release()
		agents, e := controller.NewAgents(cfg)
		if e != nil {
			return e
		}
		worker := &remote.Worker{TerminalCommands: map[string]string{"codex": cfg.Agents["codex"].Command, "claude-code": cfg.Agents["claude-code"].Command}, Version: version, Dir: dir, Identity: identity, Agents: agents, AllowClone: identity.AllowClone, Bindings: map[string]string{}}
		if sub == "resolve" {
			if len(positional) != 1 || !*confirmed {
				return fmt.Errorf("inspect the machine first, then: werkbord runner resolve <run-id> --confirm-stopped (stop the runner service before resolving)")
			}
			return worker.Resolve(positional[0])
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		client, closeNode, e := privateRunnerClient(ctx, cfg, out, identity.Controller)
		if e != nil {
			return e
		}
		defer closeNode()
		worker.Client = client
		worker.LoadBindings = func() map[string]string {
			bindings := map[string]string{}
			if b, e := os.ReadFile(filepath.Join(dir, "repositories.json")); e == nil {
				_ = json.Unmarshal(b, &bindings)
			}
			return bindings
		}
		return worker.Run(ctx)
	default:
		return fmt.Errorf("unknown runner command %q", sub)
	}
}
func containsProject(ids []string, id string) bool {
	for _, p := range ids {
		if p == id {
			return true
		}
	}
	return false
}

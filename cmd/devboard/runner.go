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

	"devboard/internal/browser"
	"devboard/internal/config"
	"devboard/internal/controller"
	"devboard/internal/daemon"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/netprivate"
	"devboard/internal/remote"
	"devboard/internal/runnerwire"
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
	backend := netprivate.NewTailscale(netprivate.TailscaleOptions{Dir: filepath.Join(runnerDir(cfg), "tailscale"), Hostname: netprivate.DefaultHostname(host) + "-runner", ControlURL: cfg.Network.ControlURL, AuthKey: os.Getenv("DEVBOARD_TS_AUTHKEY")})
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
		return fmt.Errorf("usage: devboard join <pairing-code> [--name NAME] [--allow-clone] [--foreground]")
	}
	if _, e := loadIdentity(cfg); e == nil {
		return fmt.Errorf("this machine is already paired: use devboard runner start or devboard runner serve")
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
	if len(args) == 0 {
		return fmt.Errorf("usage: devboard runner <serve|start|stop|status|repo|resolve>")
	}
	fs := flag.NewFlagSet("runner", flag.ContinueOnError)
	fs.SetOutput(errOut)
	commonFlags(fs, &cfg)
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
	identity, e := loadIdentity(cfg)
	if e != nil {
		return fmt.Errorf("runner identity unavailable; pair this machine with devboard join first")
	}
	if sub == "repo" {
		if len(positional) != 2 {
			return fmt.Errorf("usage: devboard runner repo <project-id> <local-clone>")
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
	manager := daemon.Detect(context.Background(), daemon.Options{DataDir: dir, Label: "dev.devboard.runner"})
	switch sub {
	case "status":
		st, e := manager.Status(context.Background())
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(st)
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
		worker := &remote.Worker{Dir: dir, Identity: identity, Agents: agents, AllowClone: identity.AllowClone, Bindings: map[string]string{}}
		if sub == "resolve" {
			if len(positional) != 1 || !*confirmed {
				return fmt.Errorf("inspect the machine first, then: devboard runner resolve <run-id> --confirm-stopped (stop the runner service before resolving)")
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

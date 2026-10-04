package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devboard/internal/daemon"
	"devboard/internal/netprivate"
	"devboard/internal/store/sqlite"
)

// envOn reports whether an environment variable asks for something: "1", "true" or "yes".
func envOn(name string) bool {
	switch strings.ToLower(os.Getenv(name)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

type setupFlags struct {
	noService, noStart, noOpen, noNetwork bool
	networkWait                           time.Duration
}

// cmdSetup is everything between "the binary is on this computer" and "Dev Board
// is open in the browser, working": the data directory, the database, the
// service, the controller, this computer as a runner, the private network. It is
// safe to run again: it updates the service for a new binary and restarts the
// controller, and changes nothing the user has chosen.
func (a *app) cmdSetup(ctx context.Context, args []string) error {
	var f setupFlags
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	fs.BoolVar(&f.noService, "no-service", envOn("DEVBOARD_NO_SERVICE"), "run in the background without installing a login service")
	fs.BoolVar(&f.noStart, "no-start", envOn("DEVBOARD_NO_START"), "set everything up but do not start the controller")
	fs.BoolVar(&f.noOpen, "no-open", envOn("DEVBOARD_NO_OPEN"), "do not open a browser")
	fs.BoolVar(&f.noNetwork, "no-network", envOn("DEVBOARD_NO_NETWORK"), "do not set up phone access (the private network)")
	fs.DurationVar(&f.networkWait, "network-wait", 3*time.Minute, "how long to wait for you to sign in to the private network")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: devboard setup [--no-service] [--no-start] [--no-open] [--no-network]")
	}

	a.printf("Setting up Dev Board %s\n", version)

	// 1. Directories and configuration.
	for _, d := range []string{a.cfg.DataDir, filepath.Join(a.cfg.DataDir, "logs"), a.cfg.WorktreesPath()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	if err := a.chooseAddr(ctx); err != nil {
		return err
	}
	a.step(markOK, "data directory", a.cfg.DataDir)

	// 2. The access token and the database, before anything is started, so that a
	// problem with either is reported here and not as a controller that will not start.
	if a.cfg.AuthRequired() {
		if _, err := a.cfg.ResolveToken(true); err != nil {
			return fmt.Errorf("create the access token: %w", err)
		}
	}
	if _, running := a.healthy(ctx); !running {
		db, err := sqlite.Open(ctx, a.cfg.DBPath(), nil)
		if err != nil {
			return fmt.Errorf("prepare the database: %w", err)
		}
		v, _ := db.SchemaVersion(ctx)
		_ = db.Close()
		a.step(markOK, "database", fmt.Sprintf("ready (schema v%d)", v))
	} else {
		a.step(markOK, "database", "in use by the running controller, which keeps it up to date")
	}

	// 3. The service.
	spec, err := a.spec()
	if err != nil {
		return err
	}
	var m daemon.Manager
	if f.noService {
		m = &daemon.Background{Options: daemon.Options{DataDir: a.cfg.DataDir}}
	} else {
		m = a.daemon(ctx)
	}
	if err := m.Install(ctx, spec); err != nil {
		return fmt.Errorf("install the service: %w", err)
	}
	_, isBG := m.(*daemon.Background)
	if isBG {
		a.step(markWarn, "service", "no login service on this system: Dev Board runs in the background and will not restart when you log in")
	} else {
		a.step(markOK, "service", fmt.Sprintf("installed (%s): starts when you log in", m.Name()))
	}
	if f.noStart {
		a.printf("\nDone. Start it with `devboard start`.\n")
		return nil
	}

	// 4. The controller. One that is already running is restarted, so that it is the
	// new binary with the new service definition, never a second one beside it.
	if _, running := a.healthy(ctx); running {
		if err := m.Restart(ctx); err != nil {
			return fmt.Errorf("restart the controller: %w", err)
		}
	} else if err := m.Start(ctx); err != nil {
		return fmt.Errorf("start the controller: %w", err)
	}
	v, err := a.waitHealthy(ctx, startTimeout)
	if err != nil {
		return a.whyNotStarted(err)
	}
	a.step(markOK, "controller", fmt.Sprintf("%s running at %s", v, a.controllerURL()))

	c, err := a.client()
	if err != nil {
		return err
	}

	// 5. This computer is a runner as soon as there is a controller; show that it is.
	if rs, err := c.runners(ctx); err == nil && len(rs) > 0 {
		a.step(markOK, "runner", fmt.Sprintf("this computer (%s, %s/%s) is registered", rs[0].Name, rs[0].OS, rs[0].Arch))
	} else {
		a.step(markWarn, "runner", "could not confirm that this computer is registered: run `devboard doctor`")
	}

	// 6. Phone access: the private network, signed in through the browser if needed.
	var net networkInfo
	if f.noNetwork {
		a.step("·", "phone access", "skipped (--no-network): turn it on later with `devboard open --phone`")
	} else {
		net, err = a.setupNetwork(ctx, c, f)
		if err != nil {
			a.step(markWarn, "phone access", err.Error())
		}
	}

	// 7. Open the app, to finish in the browser: GitHub, agents, repositories. The link that
	// signs the browser in carries the access token, so it goes to the browser and never to
	// the terminal (whose output is often kept, in an installer's log for one): if there is no
	// browser to open it in, `devboard open` is how to get one.
	url, err := a.signInURL()
	if err != nil {
		return err
	}
	a.printf("\n")
	if f.noOpen {
		a.printf("Run `devboard open` to finish setup in your browser (GitHub, your coding agents, your repositories).\n")
	} else if err := a.openBrowser(url); err != nil {
		a.printf("Could not open a browser here. Run `devboard open --print` on a computer with one, or `devboard open` here to try again.\n")
	} else {
		a.printf("Opened Dev Board in your browser to finish setup (GitHub, your coding agents, your repositories).\nIf it did not appear, run `devboard open`.\n")
	}
	if net.State == string(netprivate.StateConnected) {
		a.showPhone(ctx, c, net)
	}
	a.printf("\nUseful commands: devboard status · open · doctor · restart · stop · update\n")
	return nil
}

// chooseAddr settles which address the controller listens on. The default is
// 127.0.0.1:7420; if something else already holds it, the next free port is used
// and remembered in config.json, so that the service and every command agree.
func (a *app) chooseAddr(ctx context.Context) error {
	cfgFile := filepath.Join(a.cfg.DataDir, "config.json")
	if _, running := a.healthy(ctx); running {
		return nil // ours, where it is
	}
	if freeAddr(a.cfg.Addr) {
		if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
			return writeConfig(cfgFile, map[string]any{"addr": a.cfg.Addr})
		}
		return nil
	}
	host, port, _ := strings.Cut(a.cfg.Addr, ":")
	if a.cfg.Addr != "127.0.0.1:7420" {
		return fmt.Errorf("%s is already in use by something else; set a different addr in %s", a.cfg.Addr, cfgFile)
	}
	_ = port
	for p := 7421; p < 7440; p++ {
		cand := fmt.Sprintf("%s:%d", host, p)
		if freeAddr(cand) {
			a.cfg.Addr = cand
			a.step(markWarn, "port", fmt.Sprintf("7420 is in use by something else: using %d", p))
			return setConfigKey(cfgFile, "addr", cand)
		}
	}
	return errors.New("no free port between 7420 and 7439: set addr in " + cfgFile)
}

// setupNetwork brings the private network up, signing the user in to it through
// the browser if that is needed, and waits for them. Not finishing is not a
// failure: the app shows the same state and carries on from there.
func (a *app) setupNetwork(ctx context.Context, c *client, f setupFlags) (networkInfo, error) {
	n, err := c.network(ctx)
	if err != nil {
		return n, err
	}
	if n.Choice == "off" {
		a.step("·", "phone access", "off, as you left it: `devboard open --phone` turns it on")
		return n, nil
	}
	if n.Choice == "pinned_off" {
		a.step("·", "phone access", "turned off in config.json")
		return n, nil
	}
	n, err = a.ensureNetwork(ctx, c, f.networkWait, !f.noOpen)
	if err != nil {
		return n, err
	}
	switch n.State {
	case string(netprivate.StateConnected):
		a.step(markOK, "phone access", n.URL)
	case string(netprivate.StateNeedsLogin):
		a.step(markWait, "phone access", "waiting for you to sign in: Dev Board shows it in Settings, or run `devboard open --phone`")
	case string(netprivate.StateNeedsApproval):
		a.step(markWait, "phone access", "waiting for your tailnet's admin to approve this device")
	case string(netprivate.StateError):
		a.step(markWarn, "phone access", n.Error+": run `devboard doctor`")
	default:
		a.step(markWait, "phone access", "starting")
	}
	return n, nil
}

// showPhone prints the phone address and, on a terminal, its QR code.
func (a *app) showPhone(ctx context.Context, c *client, n networkInfo) {
	link, err := c.phone(ctx)
	if err != nil {
		return
	}
	a.printf("\nOn your phone (same Tailscale account): %s\n", link.URL)
	if a.isTerminal(a.out) {
		if text, err := netprivate.QRText(link.Link); err == nil {
			a.printf("Scan to open it already signed in:\n\n%s\n", indent(strings.TrimRight(text, "\n"), "  "))
		}
	}
	if !link.HTTPS && n.HTTPSHint != "" {
		a.printf("Note: %s\n", n.HTTPSHint)
	}
}

// writeConfig writes config.json atomically, readable by the user only.
func writeConfig(path string, v map[string]any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// setConfigKey sets one key in config.json, keeping everything else the user wrote.
func setConfigKey(path, key string, value any) error {
	cur := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cur); err != nil {
			return fmt.Errorf("%s is not valid JSON, so it was not changed: %w", path, err)
		}
	}
	cur[key] = value
	return writeConfig(path, cur)
}

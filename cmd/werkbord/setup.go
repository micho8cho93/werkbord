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

	"werkbord/internal/config"
	"werkbord/internal/daemon"
	"werkbord/internal/store/sqlite"
)

// envOn reports whether WERKBORD_<name> asks for something: "1", "true" or "yes".
func envOn(name string) bool {
	switch strings.ToLower(config.Getenv(name)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

type setupFlags struct {
	noService, noStart, noOpen bool
}

// cmdSetup is everything between "the binary is on this computer" and "Werkbord
// is open in the browser, working": the data directory, the database, the
// service, the controller, this computer as a runner, the private network. It is
// safe to run again: it updates the service for a new binary and restarts the
// controller, and changes nothing the user has chosen.
func (a *app) cmdSetup(ctx context.Context, args []string) error {
	var f setupFlags
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	fs.BoolVar(&f.noService, "no-service", envOn("NO_SERVICE"), "run in the background without installing a login service")
	fs.BoolVar(&f.noStart, "no-start", envOn("NO_START"), "set everything up but do not start the controller")
	fs.BoolVar(&f.noOpen, "no-open", envOn("NO_OPEN"), "do not open a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: werkbord setup [--no-service] [--no-start] [--no-open]")
	}

	a.printf("Setting up Werkbord %s\n", version)

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
		a.step(markWarn, "service", "no login service on this system: Werkbord runs in the background and will not restart when you log in")
	} else {
		a.step(markOK, "service", fmt.Sprintf("installed (%s): starts when you log in", m.Name()))
	}
	if f.noStart {
		a.printf("\nDone. Start it with `werkbord start`.\n")
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
		a.step(markWarn, "runner", "could not confirm that this computer is registered: run `werkbord doctor`")
	}

	// 6. Phone access comes through the relay: nothing to install or sign in to. Say whether it is up.
	a.setupPhones(ctx, c)

	// 7. Open the app, to finish in the browser: GitHub, agents, repositories. The link that
	// signs the browser in carries the access token, so it goes to the browser and never to
	// the terminal (whose output is often kept, in an installer's log for one): if there is no
	// browser to open it in, `werkbord open` is how to get one.
	url, err := a.signInURL()
	if err != nil {
		return err
	}
	a.printf("\n")
	if f.noOpen {
		a.printf("Run `werkbord open` to finish setup in your browser (GitHub, your coding agents, your repositories).\n")
	} else if err := a.openBrowser(url); err != nil {
		a.printf("Could not open a browser here. Run `werkbord open --print` on a computer with one, or `werkbord open` here to try again.\n")
	} else {
		a.printf("Opened Werkbord in your browser to finish setup (GitHub, your coding agents, your repositories).\nIf it did not appear, run `werkbord open`.\n")
	}
	a.printf("\nUseful commands: werkbord status · open · doctor · restart · stop · update\n")
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

// setupPhones reports phone access, which is the relay: the controller connects to it by itself, and a phone needs no app and
// no account. Not being connected yet is not a failure: it connects in the background.
func (a *app) setupPhones(ctx context.Context, c *client) {
	r, err := c.relay(ctx)
	switch {
	case err != nil:
		a.step(markWarn, "phone access", "could not check the relay: run `werkbord doctor`")
	case !r.Enabled:
		a.step("·", "phone access", "off: the relay is turned off in config.json")
	case r.Connected:
		a.step(markOK, "phone access", "ready: `werkbord phones add --qr` shows the code to scan")
	default:
		a.step(markWait, "phone access", "connecting to the relay: `werkbord phones add --qr` works once it is up")
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

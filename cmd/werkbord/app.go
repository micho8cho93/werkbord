package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"devboard/internal/browser"
	"devboard/internal/config"
	"devboard/internal/daemon"
	"devboard/internal/netprivate"
)

// app is what the service commands need from the outside world. Everything that
// touches the operating system sits behind a field, so that a test can start a
// real controller in-process under a fake service manager and drive the commands
// against it, without installing anything on the machine running the tests.
type app struct {
	cfg    config.Config
	out    io.Writer
	errOut io.Writer

	// daemon returns the service manager for this system.
	daemon func(ctx context.Context) daemon.Manager
	// runnerDaemon manages the separate runner process after a binary update.
	runnerDaemon func(ctx context.Context) daemon.Manager
	// installed returns the manager and whether the service is installed.
	installed func(ctx context.Context) (daemon.Manager, bool)
	// executable is the path of the running devboard.
	executable func() (string, error)
	// openBrowser opens a URL in the user's browser.
	openBrowser func(url string) error
	// isTerminal reports whether w is a terminal (so colour and QR codes make sense).
	isTerminal func(w io.Writer) bool
	// path is the PATH to give the service.
	path func() string
	// poll is how often to look at a controller that is starting.
	poll time.Duration
	// stopWait is how long a stopped controller has to stop answering.
	stopWait time.Duration
}

func newApp(cfg config.Config, out, errOut io.Writer) *app {
	a := &app{
		cfg: cfg, out: out, errOut: errOut,
		executable:  executable,
		openBrowser: browser.Open,
		isTerminal:  isTerminal,
		poll:        250 * time.Millisecond,
		stopWait:    40 * time.Second,
	}
	a.path = func() string {
		home, _ := os.UserHomeDir()
		return daemon.ServicePATH(os.Getenv("PATH"), home)
	}
	a.daemon = func(ctx context.Context) daemon.Manager {
		return daemon.Detect(ctx, daemon.Options{DataDir: cfg.DataDir})
	}
	a.runnerDaemon = func(ctx context.Context) daemon.Manager {
		return daemon.Detect(ctx, daemon.Options{DataDir: runnerDir(cfg), Label: daemon.RunnerLabel})
	}
	a.installed = func(ctx context.Context) (daemon.Manager, bool) {
		return daemon.Installed(ctx, daemon.Options{DataDir: cfg.DataDir})
	}
	return a
}

// executable returns the real path of the running devboard, symlinks resolved, so
// that a service points at the file and not at a shim that may move.
func executable() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return p, nil
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func (a *app) printf(format string, args ...any) { fmt.Fprintf(a.out, format, args...) }

// step prints one line of progress.
func (a *app) step(mark, what, detail string) {
	fmt.Fprintf(a.out, "  %s %-16s %s\n", mark, what, detail)
}

const (
	markOK   = "✓"
	markWarn = "!"
	markWait = "…"
	markFail = "✗"
)

// controllerURL is where the controller listens, as this computer reaches it.
func (a *app) controllerURL() string { return "http://" + a.cfg.ClientAddr() }

// signInURL is a link that opens the app on this computer already signed in.
func (a *app) signInURL() (string, error) {
	tok, err := a.cfg.ResolveToken(false)
	if err != nil {
		return "", err
	}
	if tok == "" || !a.cfg.AuthRequired() {
		return a.controllerURL() + "/", nil
	}
	return a.controllerURL() + "/#token=" + tok, nil
}

func (a *app) client() (*client, error) { return newClient(a.cfg) }

// healthy reports whether a controller answers at the configured address, and its version.
func (a *app) healthy(ctx context.Context) (string, bool) {
	c := &client{base: a.controllerURL()}
	return c.health(ctx)
}

// waitHealthy waits for the controller to answer.
func (a *app) waitHealthy(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if v, ok := a.healthy(ctx); ok {
			return v, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the controller did not answer at %s within %s", a.controllerURL(), timeout.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(a.poll):
		}
	}
}

// waitStopped waits for nothing to answer at the controller's address.
func (a *app) waitStopped(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, ok := a.healthy(ctx); !ok {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(a.poll):
		}
	}
	return false
}

// logPath is where the service writes the controller's log.
func (a *app) logPath() string { return filepath.Join(a.cfg.DataDir, "logs", "controller.log") }

// spec describes the service to install for this binary and this data directory.
func (a *app) spec() (daemon.Spec, error) {
	bin, err := a.executable()
	if err != nil {
		return daemon.Spec{}, err
	}
	return daemon.Spec{
		Binary: bin, Args: []string{"serve"}, DataDir: a.cfg.DataDir, LogFile: a.logPath(), Path: a.path(),
	}, nil
}

// tailLog returns the last n lines of the controller's log, for an error message
// that says why it did not start.
func (a *app) tailLog(n int) string {
	b, err := os.ReadFile(a.logPath())
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// whyNotStarted explains a controller that did not come up, with what its log said.
func (a *app) whyNotStarted(err error) error {
	if tail := a.tailLog(6); tail != "" {
		return fmt.Errorf("%w\nthe end of %s says:\n%s", err, a.logPath(), indent(tail, "  "))
	}
	return err
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

// ---- start, stop, restart ----

// startTimeout is how long a starting controller has: a database upgrade, and a
// private network coming up, both happen first.
const startTimeout = 45 * time.Second

func (a *app) cmdStart(ctx context.Context, args []string) error {
	if err := noArgs("start", args); err != nil {
		return err
	}
	return a.start(ctx, true)
}

// start makes sure exactly one controller is running: the service's if there is
// one, else a background process. It never starts a second beside one that is
// already answering.
func (a *app) start(ctx context.Context, say bool) error {
	if v, ok := a.healthy(ctx); ok {
		if say {
			a.printf("Werkbord %s is already running at %s.\n", v, a.controllerURL())
			if m, inst := a.installed(ctx); inst {
				if st, _ := m.Status(ctx); !st.Running {
					a.printf("It was not started by the service (%s), so `werkbord stop` will not stop it: it is probably running in a terminal.\n", m.Name())
				}
			}
		}
		return nil
	}
	m, inst := a.installed(ctx)
	if !inst {
		spec, err := a.spec()
		if err != nil {
			return err
		}
		m = a.daemon(ctx)
		if _, isBG := m.(*daemon.Background); !isBG {
			// A service manager exists but nothing is installed: run in the
			// background rather than installing a login item the user did not ask for.
			m = &daemon.Background{Options: daemon.Options{DataDir: a.cfg.DataDir}}
		}
		if err := m.Install(ctx, spec); err != nil {
			return err
		}
	}
	if err := m.Start(ctx); err != nil {
		return fmt.Errorf("start the controller: %w", err)
	}
	v, err := a.waitHealthy(ctx, startTimeout)
	if err != nil {
		return a.whyNotStarted(err)
	}
	if say {
		a.printf("Werkbord %s is running at %s (%s).\n", v, a.controllerURL(), m.Name())
		if !inst {
			a.printf("It is not set to start when you log in: run `werkbord setup` for that.\n")
		}
	}
	return nil
}

func (a *app) cmdStop(ctx context.Context, args []string) error {
	force, err := interruptionFlags("stop", args, a.errOut)
	if err != nil {
		return err
	}
	if err := a.checkInterruption(ctx, force); err != nil {
		return err
	}
	return a.stop(ctx, true)
}

func (a *app) stop(ctx context.Context, say bool) error {
	_, wasUp := a.healthy(ctx)
	stopped := false
	// The installed service, if any, and a background process if one was started.
	if m, inst := a.installed(ctx); inst {
		if err := m.Stop(ctx); err != nil {
			return err
		}
		stopped = true
	}
	bg := &daemon.Background{Options: daemon.Options{DataDir: a.cfg.DataDir}}
	if st, _ := bg.Status(ctx); st.Running {
		if err := bg.Stop(ctx); err != nil {
			return err
		}
		stopped = true
	}
	if !a.waitStopped(ctx, a.stopWait) {
		if stopped || wasUp {
			return fmt.Errorf("a controller is still answering at %s; it was not started by Werkbord's service (is `werkbord serve` running in a terminal?): stop it there", a.controllerURL())
		}
	}
	if say {
		if wasUp {
			a.printf("Werkbord stopped.\n")
		} else {
			a.printf("Werkbord was not running.\n")
		}
	}
	return nil
}

func (a *app) cmdRestart(ctx context.Context, args []string) error {
	force, err := interruptionFlags("restart", args, a.errOut)
	if err != nil {
		return err
	}
	if err := a.checkInterruption(ctx, force); err != nil {
		return err
	}
	if err := a.stop(ctx, false); err != nil {
		return err
	}
	return a.start(ctx, true)
}

func noArgs(cmd string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: werkbord %s", cmd)
	}
	return nil
}

// ---- status ----

func (a *app) cmdStatus(ctx context.Context, args []string) error {
	asJSON := false
	for _, x := range args {
		switch x {
		case "--json", "-json":
			asJSON = true
		default:
			return errors.New("usage: werkbord status [--json]")
		}
	}
	st := a.gatherStatus(ctx)
	if asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}
	a.printStatus(st)
	if !st.Running {
		return errStatusDown
	}
	return nil
}

// errStatusDown makes `werkbord status` exit non-zero when the controller is not
// running, so scripts can test it, without printing a second line of complaint.
var errStatusDown = silentError{}

type silentError struct{}

func (silentError) Error() string { return "" }

type statusReport struct {
	Version     string       `json:"version"`
	Running     bool         `json:"running"`
	Controller  string       `json:"controller,omitempty"`
	Service     daemon.State `json:"service"`
	URL         string       `json:"url"`
	DataDir     string       `json:"dataDir"`
	Network     *networkInfo `json:"network,omitempty"`
	Runner      *runnerInfo  `json:"runner,omitempty"`
	Agents      []agentInfo  `json:"agents,omitempty"`
	Projects    int          `json:"projects"`
	Unreachable string       `json:"unreachable,omitempty"`
}

func (a *app) gatherStatus(ctx context.Context) statusReport {
	st := statusReport{Version: version, URL: a.controllerURL(), DataDir: a.cfg.DataDir}
	m, inst := a.installed(ctx)
	if !inst {
		m = &daemon.Background{Options: daemon.Options{DataDir: a.cfg.DataDir}}
	}
	st.Service, _ = m.Status(ctx)
	v, ok := a.healthy(ctx)
	if !ok {
		return st
	}
	st.Running, st.Controller = true, v
	c, err := a.client()
	if err != nil {
		st.Unreachable = err.Error()
		return st
	}
	if n, err := c.network(ctx); err == nil {
		n.AuthURL = "" // a one-time sign-in link for the user: not for a status report that may be pasted or logged
		st.Network = &n
	}
	if rs, err := c.runners(ctx); err == nil && len(rs) > 0 {
		st.Runner = &rs[0]
	}
	if as, err := c.agents(ctx); err == nil {
		st.Agents = as
	}
	if ps, err := c.listProjects(ctx); err == nil {
		st.Projects = len(ps)
	} else if strings.Contains(err.Error(), "token") {
		st.Unreachable = "the controller refused this computer's access token"
	}
	return st
}

func (a *app) printStatus(st statusReport) {
	a.printf("Werkbord %s\n", st.Version)
	switch {
	case st.Running:
		how := st.Service.Manager
		if st.Service.Running && st.Service.PID > 0 {
			how = fmt.Sprintf("%s, pid %d", how, st.Service.PID)
		} else if !st.Service.Running {
			how = "not started by the service: probably running in a terminal"
		}
		a.step(markOK, "controller", fmt.Sprintf("running at %s (%s)", st.URL, how))
		if st.Controller != "" && st.Controller != st.Version {
			a.step(markWarn, "version", fmt.Sprintf("the running controller is %s, this werkbord is %s: run `werkbord restart`", st.Controller, st.Version))
		}
	default:
		a.step(markFail, "controller", "not running: start it with `werkbord start`")
	}
	switch {
	case st.Service.Installed:
		a.step(markOK, "starts at login", "yes ("+st.Service.Manager+")")
	default:
		a.step(markWarn, "starts at login", "no: run `werkbord setup` to install the service")
	}
	a.step("·", "data", st.DataDir)
	if st.Network != nil {
		switch st.Network.State {
		case string(netprivate.StateConnected):
			a.step(markOK, "phone access", st.Network.URL)
		case string(netprivate.StateOff):
			a.step("·", "phone access", "off: `werkbord open --phone` turns it on")
		case string(netprivate.StateNeedsLogin):
			a.step(markWait, "phone access", "waiting for you to sign in: run `werkbord open --phone`")
		case string(netprivate.StateNeedsApproval):
			a.step(markWait, "phone access", "waiting for your tailnet's admin to approve this device")
		case string(netprivate.StateError):
			a.step(markFail, "phone access", st.Network.Error)
		default:
			a.step(markWait, "phone access", st.Network.State)
		}
	}
	if st.Runner != nil {
		on := "online"
		if !st.Runner.Online {
			on = "offline"
		}
		a.step(markOK, "runner", fmt.Sprintf("this computer (%s, %s/%s) %s", st.Runner.Name, st.Runner.OS, st.Runner.Arch, on))
	}
	for _, ag := range st.Agents {
		switch {
		case ag.Available:
			a.step(markOK, ag.ID, ag.Version)
		case ag.Installed:
			a.step(markWarn, ag.ID, ag.Detail)
		default:
			a.step("·", ag.ID, "not installed")
		}
	}
	if st.Running {
		a.step("·", "projects", fmt.Sprint(st.Projects))
	}
	if st.Unreachable != "" {
		a.step(markWarn, "note", st.Unreachable)
	}
}

// ---- logs, uninstall ----

func (a *app) cmdLogs(_ context.Context, args []string) error {
	n := 80
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n":
			if i+1 >= len(args) {
				return errors.New("usage: werkbord logs [-n LINES]")
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &n); err != nil || n <= 0 {
				return errors.New("usage: werkbord logs [-n LINES]")
			}
			i++
		default:
			return errors.New("usage: werkbord logs [-n LINES]")
		}
	}
	tail := a.tailLog(n)
	if tail == "" {
		return fmt.Errorf("no log yet at %s: the service writes it once it has run", a.logPath())
	}
	a.printf("%s\n", tail)
	return nil
}

func (a *app) cmdUninstall(ctx context.Context, args []string) error {
	if err := noArgs("uninstall", args); err != nil {
		return err
	}
	if m, inst := a.installed(ctx); inst {
		if err := m.Uninstall(ctx); err != nil {
			return err
		}
		a.printf("Removed the %s service: Werkbord no longer starts when you log in.\n", m.Name())
	}
	bg := &daemon.Background{Options: daemon.Options{DataDir: a.cfg.DataDir}}
	if err := bg.Uninstall(ctx); err != nil {
		return err
	}
	a.printf("Your projects, tasks and settings are untouched, in %s.\nTo remove the program, delete the werkbord executable (and devboard beside it); to remove the data too, delete that directory.\n", a.cfg.DataDir)
	return nil
}

// ---- open ----

func (a *app) cmdOpen(ctx context.Context, args []string) error {
	phone, printOnly, qr := false, false, false
	for _, x := range args {
		switch x {
		case "--phone":
			phone = true
		case "--print":
			printOnly = true
		case "--qr":
			qr, phone = true, true
		default:
			return errors.New("usage: werkbord open [--phone] [--qr] [--print]")
		}
	}
	if _, ok := a.healthy(ctx); !ok {
		return errors.New("Werkbord is not running: start it with `werkbord start`")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if phone {
		return a.openPhone(ctx, c, printOnly, qr)
	}
	u, err := a.signInURL()
	if err != nil {
		return err
	}
	if printOnly {
		a.printf("%s\n", u)
		return nil
	}
	if err := a.openBrowser(u); err != nil {
		a.printf("Open this in your browser: %s\n", u)
		return nil
	}
	a.printf("Opened Werkbord in your browser (%s).\n", a.controllerURL())
	if n, err := c.network(ctx); err == nil && n.State == string(netprivate.StateConnected) {
		a.printf("On your phone: %s (`werkbord open --qr` shows a QR code).\n", n.URL)
	}
	return nil
}

// phoneTimeout is how long `open --phone` waits for the user to sign in to the network.
const phoneTimeout = 5 * time.Minute

// openPhone gets the private network running and signed in, then shows the
// address (and a QR code) for a phone.
func (a *app) openPhone(ctx context.Context, c *client, printOnly, qr bool) error {
	n, err := a.ensureNetwork(ctx, c, phoneTimeout, !printOnly)
	if err != nil {
		return err
	}
	if n.State != string(netprivate.StateConnected) {
		return fmt.Errorf("the private network is %s: finish signing in, then run this again", n.State)
	}
	link, err := c.phone(ctx)
	if err != nil {
		return err
	}
	a.printf("Open this on your phone (it must be on the same Tailscale account):\n\n  %s\n\n", link.URL)
	if !link.HTTPS && n.HTTPSHint != "" {
		a.printf("Note: %s\n\n", n.HTTPSHint)
	}
	if qr || a.isTerminal(a.out) {
		// The code carries the sign-in token, so it opens Werkbord already signed in.
		text, err := netprivate.QRText(link.Link)
		if err != nil {
			return err
		}
		a.printf("Or scan this to open it already signed in:\n\n%s\n", indent(strings.TrimRight(text, "\n"), "  "))
	}
	return nil
}

// ensureNetwork turns the private network on (unless the user turned it off) and,
// if it needs signing in, opens the sign-in page and waits for it to finish. It
// returns the status it ends with, and does not fail because the user has not
// signed in yet: that is a state, not an error.
func (a *app) ensureNetwork(ctx context.Context, c *client, wait time.Duration, open bool) (networkInfo, error) {
	n, err := c.network(ctx)
	if err != nil {
		return n, err
	}
	if n.State == string(netprivate.StateOff) {
		if n.Choice == "off" || n.Choice == "pinned_off" {
			if n.Choice == "pinned_off" {
				return n, errors.New("the private network is turned off in config.json (network.enabled)")
			}
			// The user turned it off in the app. Asking for the phone link is asking for it back on.
		}
		if n, err = c.setNetwork(ctx, true); err != nil {
			return n, err
		}
	}
	opened := false
	deadline := time.Now().Add(wait)
	for {
		switch n.State {
		case string(netprivate.StateConnected), string(netprivate.StateError):
			return n, nil
		case string(netprivate.StateNeedsLogin):
			if n.AuthURL != "" && !opened {
				opened = true
				a.printf("To reach Werkbord from your phone, sign in to Tailscale (a free account is enough; Werkbord has no account of its own).\n")
				if open && a.openBrowser(n.AuthURL) == nil {
					a.printf("Opened the sign-in page in your browser. If it did not appear, open:\n  %s\n", n.AuthURL)
				} else {
					a.printf("Open this link to sign in:\n  %s\n", n.AuthURL)
				}
			}
		case string(netprivate.StateNeedsApproval):
			a.printf("Your tailnet's admin has to approve this device: https://login.tailscale.com/admin/machines\n")
			return n, nil
		}
		if time.Now().After(deadline) {
			return n, nil
		}
		select {
		case <-ctx.Done():
			return n, ctx.Err()
		case <-time.After(time.Second):
		}
		if n, err = c.network(ctx); err != nil {
			return n, err
		}
	}
}

// ---- helpers for the commands below ----

// freeAddr reports whether nothing is listening on addr.
func freeAddr(addr string) bool {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// cleanExec runs a command and returns its trimmed output.
func cleanExec(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

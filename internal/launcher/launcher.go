// Package launcher is what the desktop app does before it shows anything: find the
// Werkbord controller that is on this computer, start it if it is not running, and
// say how to open its web app signed in.
//
// It does not run a controller and does not decide how one is run. Everything that
// installs, starts or updates goes through the werkbord program itself
// (`werkbord setup`, `start`, `install-release`, `update`), so that the desktop app
// and the terminal are two ways to ask for the same thing, answered by the same code:
// one service definition, one data directory, one database, and the one-controller
// guarantee those commands already give (a running controller is never started
// beside, and a second one on the same data directory refuses to start).
//
// What the desktop app adds is only what a program started from the Finder lacks. It
// has none of a terminal's environment, so the launcher
//
//   - joins the installation that is there: the executable and the data directory the
//     installed service was set up with, whoever set it up, rather than guessing;
//   - installs the program it ships with, when there is none, where the installer puts
//     it (~/.local/bin), and never runs the controller from inside the .app, where an
//     update would break the bundle's signature and deleting the app would break the
//     login service;
//   - gives `werkbord setup` the PATH of the user's login shell, because the service
//     keeps the PATH it was set up with and the coding agents are found through it.
//
// Nothing here weakens the API's authentication. The app reads the access token from
// the file the CLI reads it from, as the same user, and passes it the way
// `werkbord open` does: in the fragment of the address, which no server ever sees.
package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"devboard/internal/config"
	"devboard/internal/daemon"
	"devboard/internal/update"
)

// Phase says which part of getting ready the launcher is in, for a progress display.
type Phase string

const (
	PhaseLooking    Phase = "looking"    // looking for an installation and a running controller
	PhaseInstalling Phase = "installing" // putting Werkbord's program in place (first run only)
	PhaseUpgrading  Phase = "upgrading"  // replacing an older installed program with the one this app ships
	PhaseStarting   Phase = "starting"   // asking for a controller and waiting for it to answer
	PhaseConnecting Phase = "connecting" // checking the controller accepts this computer's token
)

// Step is one progress report.
type Step struct {
	Phase Phase  `json:"phase"`
	Text  string `json:"text"`
}

// Connection is a controller that is running and accepts this computer's token.
type Connection struct {
	// URL is where the controller is, as this computer reaches it.
	URL string `json:"url"`
	// SignInURL opens the web app signed in. It carries the access token (in the
	// fragment): it is for navigating to, and is never logged, shown or stored.
	SignInURL string `json:"signInUrl"`
	// Version is what the running controller reports.
	Version string `json:"version"`
	// Notice is something worth telling the person that did not stop the app opening,
	// such as an update that is waiting for the agents that are running to finish.
	Notice string `json:"notice,omitempty"`
}

// Runner runs a command and returns what it printed (standard output and error
// together). env is added to the environment the launcher itself has.
type Runner func(ctx context.Context, env []string, name string, args ...string) (string, error)

// Options say what the launcher works with. The zero value of everything but
// Version is right for the real system; tests and development set the rest.
type Options struct {
	// Version is the desktop app's own version, which is also the bundled program's.
	Version string
	// Bundled is the werkbord program shipped inside the app. Empty when there is none
	// (a build from source): the launcher then only joins an installation that exists.
	Bundled string
	// NoInstall uses Bundled where it is, and neither copies it anywhere nor ever
	// replaces an installed program with it. For development, from a source tree.
	NoInstall bool
	// NoService sets Werkbord up without a login service (`setup --no-service`): the
	// controller runs as a background process. For development and tests.
	NoService bool

	// Home is the user's home directory; InstallDir is where the program is installed
	// (default ~/.local/bin, where install.sh puts it). DataDir, if set, is used as it
	// is instead of being looked for.
	Home, InstallDir, DataDir string

	// Run runs a command; ShellPATH is the PATH of the user's login shell. Both default
	// to the real thing.
	Run       Runner
	ShellPATH func(ctx context.Context) string
	// Describe reads the installed service's definition (default: launchd's).
	Describe func(ctx context.Context) (daemon.Definition, bool)
	// ServiceInstalled says whether a login service is installed for this data directory.
	ServiceInstalled func(ctx context.Context, dataDir string) bool
	// HTTP is the client for the controller (default: a short timeout).
	HTTP *http.Client
	// Poll is how often a starting controller is looked at; StartTimeout how long it has.
	Poll, StartTimeout time.Duration
	// Source is where releases are looked for (default: GitHub's releases page).
	Source update.Source
}

// Launcher finds, starts and updates the controller. It is safe for concurrent use:
// connecting and updating take turns, so two windows or two clicks never start two things.
type Launcher struct {
	opt Options

	mu      sync.Mutex
	install Install // the installed program, as last found
	checker *update.Checker
	// A program was replaced while nothing was running. The program keeps the old executable beside the new one until
	// someone has seen the new controller come up, and refuses every later update while it is there. That someone is
	// the next connection that finds the controller running on the new version.
	unverified struct{ prev, version string }
	// adoptNoticed is the update (running>bundled) whose deferral the person has already been told about.
	adoptNoticed string
}

// Install is a werkbord program that is installed on this computer.
type Install struct {
	Path    string
	Version string
}

// New returns a launcher.
func New(opt Options) *Launcher {
	if opt.Home == "" {
		opt.Home, _ = os.UserHomeDir()
	}
	if opt.InstallDir == "" {
		opt.InstallDir = filepath.Join(opt.Home, ".local", "bin")
	}
	if opt.Run == nil {
		opt.Run = runCommand
	}
	if opt.ShellPATH == nil {
		opt.ShellPATH = loginShellPATH
	}
	if opt.Describe == nil {
		opt.Describe = func(ctx context.Context) (daemon.Definition, bool) {
			return daemon.Describe(ctx, daemon.Options{Home: opt.Home})
		}
	}
	if opt.ServiceInstalled == nil {
		opt.ServiceInstalled = func(ctx context.Context, dataDir string) bool {
			_, ok := daemon.Installed(ctx, daemon.Options{Home: opt.Home, DataDir: dataDir})
			return ok
		}
	}
	if opt.HTTP == nil {
		opt.HTTP = &http.Client{Timeout: 3 * time.Second}
	}
	if opt.Poll <= 0 {
		opt.Poll = 250 * time.Millisecond
	}
	if opt.StartTimeout <= 0 {
		opt.StartTimeout = 60 * time.Second
	}
	return &Launcher{opt: opt}
}

// ---- finding what is there ----

// Config is the configuration of the installation this computer has: the data
// directory the installed service was set up with unless the environment names one,
// and whatever that directory's config.json says.
func (l *Launcher) Config(ctx context.Context) (config.Config, error) {
	dir := l.opt.DataDir
	if dir == "" && config.Getenv("DATA_DIR") == "" {
		if def, ok := l.opt.Describe(ctx); ok && def.DataDir != "" {
			dir = def.DataDir
		}
	}
	var cfg config.Config
	var err error
	if dir == "" {
		cfg, err = config.Load()
	} else {
		cfg, err = config.LoadIn(dir)
	}
	if err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

// Health asks whether a Werkbord controller answers at base, and what version it
// says it is. /api/health is public, so this needs no token. Whatever else may be
// listening on the port, only something that answers like a controller counts.
func (l *Launcher) Health(ctx context.Context, base string) (version string, ok bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/health", nil)
	if err != nil {
		return "", false
	}
	resp, err := l.opt.HTTP.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var h struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&h) != nil {
		return "", false
	}
	if (h.Status != "ok" && h.Status != "degraded") || h.Version == "" {
		return "", false
	}
	return h.Version, true
}

// ErrAuth is returned when a controller answers but does not accept the token this
// computer holds: it is another installation (another data directory), or was started
// with a token of its own. Opening a window on it would show a sign-in prompt for a
// token the person has no way to find, and starting another controller would not help.
var ErrAuth = errors.New("the controller refused this computer's access token")

// checkAuth asks the controller for something only a signed-in client may have.
func (l *Launcher) checkAuth(ctx context.Context, cfg config.Config) error {
	if !cfg.AuthRequired() {
		return nil
	}
	tok, err := cfg.ResolveToken(false)
	if err != nil {
		return fmt.Errorf("read the access token: %w", err)
	}
	if tok == "" {
		return fmt.Errorf("%w: there is no token file at %s, so this computer has none to present", ErrAuth, cfg.TokenPath())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ControllerURL()+"/api/projects", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := l.opt.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: it is listening at %s but was not set up with the data in %s", ErrAuth, cfg.ControllerURL(), cfg.DataDir)
	case resp.StatusCode >= 300:
		return fmt.Errorf("the controller answered %s when asked for the project list", resp.Status)
	}
	return nil
}

var versionRE = regexp.MustCompile(`^(dev|v?\d+\.\d+\.\d+\S*)$`)

// versionOf runs a program's `version` command. A file that is not a werkbord
// program, or does not run, has none.
func (l *Launcher) versionOf(ctx context.Context, path string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := l.opt.Run(ctx, nil, path, "version")
	out = strings.TrimSpace(out)
	if err != nil || !versionRE.MatchString(out) {
		return "", false
	}
	return out, true
}

// FindInstall looks for the werkbord program that is installed: the one the login
// service runs, else where the installer puts it. Install.Path is empty when there
// is none that runs.
func (l *Launcher) FindInstall(ctx context.Context) Install {
	var candidates []string
	if l.opt.NoInstall && l.opt.Bundled != "" {
		candidates = []string{l.opt.Bundled}
	} else {
		if def, ok := l.opt.Describe(ctx); ok {
			candidates = append(candidates, def.Binary)
		}
		for _, dir := range []string{l.opt.InstallDir, "/opt/homebrew/bin", "/usr/local/bin"} {
			candidates = append(candidates, filepath.Join(dir, "werkbord"), filepath.Join(dir, "devboard"))
		}
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if fi, err := os.Stat(c); err != nil || fi.IsDir() || fi.Mode()&0o111 == 0 {
			continue
		}
		if v, ok := l.versionOf(ctx, c); ok {
			return Install{Path: c, Version: v}
		}
	}
	return Install{}
}

// ---- getting ready ----

// Connect makes sure a controller is running and accepts this computer's token, and
// returns how to open it. report, if not nil, is told what it is doing.
//
// A controller that is already answering is joined and left exactly as it is: it is
// not restarted, upgraded or started beside. Only when nothing answers does it
// install (if no program is installed), upgrade an older installed program to the one
// this app ships (nothing is running to interrupt), and start the controller.
func (l *Launcher) Connect(ctx context.Context, report func(Step)) (Connection, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	say := func(p Phase, text string) {
		if report != nil {
			report(Step{Phase: p, Text: text})
		}
	}

	say(PhaseLooking, "Looking for Werkbord…")
	cfg, err := l.Config(ctx)
	if err != nil {
		return Connection{}, fmt.Errorf("Werkbord's settings could not be read: %w", err)
	}
	inst := l.FindInstall(ctx)
	l.install = inst

	if v, ok := l.Health(ctx, cfg.ControllerURL()); ok {
		say(PhaseConnecting, "Connecting…")
		if err := l.checkAuth(ctx, cfg); err != nil {
			return Connection{}, err
		}
		notice := l.finishUpgrade(v)
		if l.shouldAdopt(inst, v) {
			// This app is newer than the program the running controller is (it was just updated, in place or by a new
			// disk image): bring the controller up to the program the app carries, the way the installer does, which
			// refuses while agents are working. When it refuses the controller is left exactly as it is.
			say(PhaseUpgrading, fmt.Sprintf("Updating Werkbord %s → %s…", v, l.opt.Version))
			nv, n, aerr := l.adopt(ctx, cfg, inst, v)
			if aerr != nil {
				return Connection{}, aerr
			}
			v = nv
			if n != "" {
				notice = strings.TrimSpace(notice + " " + n)
			}
		}
		return connection(cfg, v, notice)
	}

	// Nothing is answering: get a controller running.
	var notice string
	switch {
	case inst.Path == "" && l.opt.Bundled == "":
		return Connection{}, errors.New("Werkbord is not installed on this computer, and this copy of the app does not carry it. Install it from https://github.com/micho8cho93/werkbord")
	case inst.Path == "":
		say(PhaseInstalling, "Setting up Werkbord for the first time…")
		if inst, err = l.installBundled(ctx); err != nil {
			return Connection{}, err
		}
		l.install = inst
	case l.shouldUpgrade(inst):
		say(PhaseUpgrading, fmt.Sprintf("Updating Werkbord %s → %s…", inst.Version, l.opt.Version))
		if out, uerr := l.upgrade(ctx, cfg, inst); uerr != nil {
			notice = fmt.Sprintf("Werkbord %s is installed with this app, but could not replace %s yet: %s", l.opt.Version, inst.Version, lastLine(out, uerr))
		} else {
			inst.Version = l.opt.Version
			l.install = inst
			l.unverified.prev, l.unverified.version = inst.Path+".prev", inst.Version
		}
	}

	say(PhaseStarting, "Starting Werkbord…")
	args := []string{"start"}
	if l.needsSetup(ctx, cfg, inst) {
		args = []string{"setup", "--no-open", "--no-network"}
		if l.opt.NoService {
			args = append(args, "--no-service")
		}
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
	defer cancel()
	out, err := l.opt.Run(runCtx, l.childEnv(ctx, cfg), inst.Path, args...)
	if err != nil {
		return Connection{}, startError(args[0], out, err, cfg)
	}
	// setup may have moved the controller to another port, and start may have read a new config.json.
	if cfg, err = l.Config(ctx); err != nil {
		return Connection{}, err
	}
	v, err := l.waitHealthy(ctx, cfg.ControllerURL())
	if err != nil {
		return Connection{}, fmt.Errorf("%w: %s", err, tailOf(cfg.LogPath(), 6))
	}
	say(PhaseConnecting, "Connecting…")
	if err := l.checkAuth(ctx, cfg); err != nil {
		return Connection{}, err
	}
	if n := l.finishUpgrade(v); n != "" {
		notice = strings.TrimSpace(notice + " " + n)
	}
	return connection(cfg, v, notice)
}

// finishUpgrade completes an upgrade that was waiting for its controller to come up: now that one has, on the version
// that was installed (and has answered with this computer's token, which reads the database), the old executable is no
// longer needed, and is removed so that the next update is not refused for it. It returns a notice if it is not so.
func (l *Launcher) finishUpgrade(running string) string {
	if l.unverified.prev == "" {
		return ""
	}
	u := l.unverified
	if running != u.version {
		return fmt.Sprintf("The controller reports %s, not the %s that was installed, so the previous program is kept at %s.", running, u.version, u.prev)
	}
	l.unverified.prev, l.unverified.version = "", ""
	if err := os.Remove(u.prev); err != nil && !os.IsNotExist(err) {
		return fmt.Sprintf("The previous program at %s could not be removed (%v): remove it before the next update.", u.prev, err)
	}
	return ""
}

func connection(cfg config.Config, version, notice string) (Connection, error) {
	u, err := cfg.SignInURL()
	if err != nil {
		return Connection{}, err
	}
	return Connection{URL: cfg.ControllerURL(), SignInURL: u, Version: version, Notice: notice}, nil
}

// needsSetup: a computer Werkbord has never been set up on, or one whose login service
// still points at a program that is gone, is set up (which writes the service for the
// program that is there). Anything else is started, which changes nothing about how
// Werkbord was set up, including a choice not to have a login service.
func (l *Launcher) needsSetup(ctx context.Context, cfg config.Config, inst Install) bool {
	if def, ok := l.opt.Describe(ctx); ok && def.Binary != "" && def.Binary != inst.Path {
		if _, err := os.Stat(def.Binary); err != nil {
			return true // a service that would start nothing
		}
	}
	if l.opt.ServiceInstalled(ctx, cfg.DataDir) {
		return false
	}
	_, err := os.Stat(cfg.DBPath())
	return os.IsNotExist(err)
}

// shouldUpgrade: this app is a release, ships a program, and the installed one is
// older. A program that is not a release (built from source) is never replaced, and an
// installed program that is newer is never replaced by this older one.
func (l *Launcher) shouldUpgrade(inst Install) bool {
	return !l.opt.NoInstall && l.opt.Bundled != "" && update.Release(l.opt.Version) &&
		update.Release(inst.Version) && update.Compare(inst.Version, l.opt.Version) < 0
}

// upgrade installs the program this app ships over the installed one, by running
// that program's own `install-release`: the entry point the installer uses, which
// refuses while agents are running, takes a database snapshot first, replaces the
// executable, and keeps the old one until the new one has proved itself.
func (l *Launcher) upgrade(ctx context.Context, cfg config.Config, inst Install) (string, error) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Minute)
	defer cancel()
	return l.opt.Run(runCtx, l.childEnv(ctx, cfg), l.opt.Bundled, "install-release", inst.Path)
}

// shouldAdopt: the installed program is older than the one this app ships (shouldUpgrade) and so is the controller
// that is running. A controller that already runs the app's version, or a newer one, is left alone.
func (l *Launcher) shouldAdopt(inst Install, running string) bool {
	return l.shouldUpgrade(inst) && update.Release(running) && update.Compare(running, l.opt.Version) < 0
}

// adopt brings a controller that is running up to the program this app ships, by the same `install-release` the installer uses, and
// so with the same guarantees: it refuses while coding agents are working or a runner has unfinished work, takes a snapshot of the
// database first, and puts the old program, and the database, back if the new controller does not come up. When it does refuse, or
// has to put things back, the controller is as it was, and what is returned is that controller's version and a notice for
// the person, said once (the window reconnects, and this must not become a dialog on every reload).
func (l *Launcher) adopt(ctx context.Context, cfg config.Config, inst Install, running string) (version, notice string, err error) {
	out, uerr := l.upgrade(ctx, cfg, inst)
	v, werr := l.waitHealthy(ctx, cfg.ControllerURL())
	if werr != nil {
		why := werr.Error()
		if uerr != nil {
			why = lastLine(out, uerr)
		}
		return "", "", fmt.Errorf("Werkbord %s did not finish replacing %s, and Werkbord is not answering: %s", trimV(l.opt.Version), trimV(running), why)
	}
	if uerr != nil {
		key := running + ">" + l.opt.Version
		if l.adoptNoticed == key {
			return v, "", nil
		}
		l.adoptNoticed = key
		return v, fmt.Sprintf("Werkbord %s came with this app, but your Werkbord is still on %s: %s. It moves to the new one the next time you open Werkbord with nothing running, or when you choose Update now.",
			trimV(l.opt.Version), trimV(v), refusal(out, uerr)), nil
	}
	inst.Version = l.opt.Version
	l.install = inst
	l.checker = nil
	if v != l.opt.Version {
		return v, fmt.Sprintf("Werkbord %s was installed, but the controller reports %s.", trimV(l.opt.Version), trimV(v)), nil
	}
	return v, "", nil
}

// refusal is why `install-release` did not replace the program, in words a person can use.
func refusal(out string, err error) string {
	msg := lastLine(out, err)
	switch {
	case strings.Contains(msg, "is active on this computer"), strings.Contains(msg, "finish or resolve it"):
		return "coding agents are working (or a runner has unfinished work)"
	}
	return msg
}

func trimV(v string) string { return strings.TrimPrefix(v, "v") }

// PendingProgramUpdate says whether this app ships a newer program than the one installed: what the app
// brings with it, which needs no network and no download, and which is what finishes an update of the app.
func (l *Launcher) PendingProgramUpdate(ctx context.Context) (installed, bundled string, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	inst := l.FindInstall(ctx)
	if inst.Path == "" || !l.shouldUpgrade(inst) {
		return inst.Version, l.opt.Version, false
	}
	return inst.Version, l.opt.Version, true
}

// AdoptBundled installs the program this app ships over the installed one (see adopt), when the person asks. The
// returned text is what the program said, which is the reason when it refuses.
func (l *Launcher) AdoptBundled(ctx context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cfg, err := l.Config(ctx)
	if err != nil {
		return "", err
	}
	inst := l.FindInstall(ctx)
	if inst.Path == "" || !l.shouldUpgrade(inst) {
		return "", errors.New("the program installed on this computer is not older than the one in this app")
	}
	_, wasRunning := l.Health(ctx, cfg.ControllerURL())
	out, err := l.upgrade(ctx, cfg, inst)
	l.install = l.FindInstall(ctx)
	l.checker = nil
	if err != nil {
		return out, fmt.Errorf("%s", refusal(out, err))
	}
	if !wasRunning && l.install.Path != "" && l.install.Version != inst.Version {
		// As in ApplyUpdate: a controller that was not running is left stopped, with the old program kept beside the new
		// one until the new one has been seen to start, which the connection that follows is.
		l.unverified.prev, l.unverified.version = l.install.Path+".prev", l.install.Version
	}
	return out, nil
}

// installBundled puts the program this app ships where the installer puts it. The
// copy is staged beside its destination and renamed into place, so a partial file is
// never visible, and is run once before it is, to see that it is what it says.
func (l *Launcher) installBundled(ctx context.Context) (Install, error) {
	if l.opt.NoInstall {
		v, ok := l.versionOf(ctx, l.opt.Bundled)
		if !ok {
			return Install{}, fmt.Errorf("%s does not run", l.opt.Bundled)
		}
		return Install{Path: l.opt.Bundled, Version: v}, nil
	}
	dir := l.opt.InstallDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Install{}, fmt.Errorf("create %s: %w", dir, err)
	}
	staged := filepath.Join(dir, ".werkbord.new")
	if err := copyExecutable(l.opt.Bundled, staged); err != nil {
		return Install{}, fmt.Errorf("copy Werkbord to %s: %w", dir, err)
	}
	v, ok := l.versionOf(ctx, staged)
	if !ok || (update.Release(l.opt.Version) && v != l.opt.Version) {
		_ = os.Remove(staged)
		return Install{}, fmt.Errorf("the program inside this app did not report %s (it said %q): it was not installed", l.opt.Version, v)
	}
	dst := filepath.Join(dir, "werkbord")
	if err := os.Rename(staged, dst); err != nil {
		_ = os.Remove(staged)
		return Install{}, err
	}
	return Install{Path: dst, Version: v}, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (l *Launcher) waitHealthy(ctx context.Context, base string) (string, error) {
	deadline := time.Now().Add(l.opt.StartTimeout)
	for {
		if v, ok := l.Health(ctx, base); ok {
			return v, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("Werkbord did not answer at %s within %s", base, l.opt.StartTimeout.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(l.opt.Poll):
		}
	}
}

// childEnv is the environment the werkbord program is run with. A program started from
// the Finder has almost none: the data directory is stated, so that the program and
// this app cannot disagree about it, and PATH is the login shell's, which is what
// `werkbord setup` records for the service and the coding agents are found through.
func (l *Launcher) childEnv(ctx context.Context, cfg config.Config) []string {
	env := []string{"WERKBORD_DATA_DIR=" + cfg.DataDir}
	if p := l.opt.ShellPATH(ctx); p != "" {
		env = append(env, "PATH="+p)
	}
	return env
}

// startError says what failed in words a person can act on, with what the program said.
func startError(cmd, out string, err error, cfg config.Config) error {
	msg := lastLine(out, err)
	switch {
	case strings.Contains(out, "address already in use") || strings.Contains(out, "is already in use by something else"):
		return fmt.Errorf("another program is already using %s, so Werkbord cannot listen there. Quit it, or choose another address with \"addr\" in %s: %s", cfg.Addr, filepath.Join(cfg.DataDir, "config.json"), msg)
	case strings.Contains(out, "already using"):
		return fmt.Errorf("another Werkbord is using the data in %s: %s", cfg.DataDir, msg)
	}
	return fmt.Errorf("`werkbord %s` failed: %s", cmd, msg)
}

// ---- updates ----

// UpdateStatus says whether a newer stable release exists than the installed
// program. A build from source is never asked about, and neither is anything when the
// person turned looking for updates off (noUpdateCheck in config.json, or WERKBORD_NO_UPDATE_CHECK): the
// controller honours that, and so does the app, which must not ask the internet what the person said not to.
func (l *Launcher) UpdateStatus(ctx context.Context, force bool) update.Status {
	cfg, cerr := l.Config(ctx)
	l.mu.Lock()
	inst := l.install
	if inst.Path == "" {
		inst = l.FindInstall(ctx)
		l.install = inst
	}
	current := inst.Version
	if current == "" {
		current = l.opt.Version
	}
	if cerr != nil {
		l.mu.Unlock()
		return update.Status{Current: current, Release: update.Release(current), Error: "Werkbord's settings could not be read: " + cerr.Error()}
	}
	if l.checker == nil || l.checker.Current != current || l.checker.Disabled != cfg.NoUpdateCheck {
		l.checker = &update.Checker{Source: l.opt.Source, Current: current, Disabled: cfg.NoUpdateCheck}
	}
	checker := l.checker
	l.mu.Unlock()
	return checker.Status(ctx, force)
}

// UpdatesAllowed says whether looking for updates is allowed at all: not when the person turned it off (noUpdateCheck in
// config.json, or WERKBORD_NO_UPDATE_CHECK). If the settings cannot be read it is not allowed: nothing is asked on a guess.
func (l *Launcher) UpdatesAllowed(ctx context.Context) bool {
	cfg, err := l.Config(ctx)
	return err == nil && !cfg.NoUpdateCheck
}

// ApplyUpdate installs the newest release by running the installed program's own
// `update`: the checksum is verified, running agents stop it, the database is backed
// up first, and the old program is put back if the new one does not come up. The
// returned text is what that command said, which is the reason when it fails.
//
// Applying an update is always something this computer's own app does when its user
// asks; no request that reaches the controller can cause it.
func (l *Launcher) ApplyUpdate(ctx context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cfg, err := l.Config(ctx)
	if err != nil {
		return "", err
	}
	inst := l.FindInstall(ctx)
	if inst.Path == "" {
		return "", errors.New("the installed Werkbord program could not be found")
	}
	if !update.Release(inst.Version) {
		return "", fmt.Errorf("this Werkbord (%s) was built from source, not installed from a release: update it from its source tree", inst.Version)
	}
	_, wasRunning := l.Health(ctx, cfg.ControllerURL())
	// Not cancelled with the app: stopping half-way through replacing a program is the one thing to avoid.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Minute)
	defer cancel()
	out, err := l.opt.Run(runCtx, l.childEnv(ctx, cfg), inst.Path, "update")
	l.install = l.FindInstall(ctx)
	l.checker = nil
	if err != nil {
		return out, fmt.Errorf("%s", lastLine(out, err))
	}
	if !wasRunning && l.install.Path != "" && l.install.Version != inst.Version {
		// Updating a controller that was not running installs the program and leaves it stopped, with the old one kept
		// beside it until the new one has been seen to start. The connection that follows (the window reloads) is that.
		l.unverified.prev, l.unverified.version = l.install.Path+".prev", l.install.Version
	}
	return out, nil
}

// ---- diagnostics ----

// Diagnostics is what to look at when Werkbord does not open.
type Diagnostics struct {
	AppVersion       string
	BundledPath      string
	Install          Install
	DataDir          string
	Address          string
	ConfigError      string
	ServiceManager   string
	ServiceInstalled bool
	ServiceBinary    string
	Healthy          bool
	ControllerVer    string
	AuthError        string
	LogPath          string
	LogTail          string
}

// Diagnose gathers what is known, changing nothing.
func (l *Launcher) Diagnose(ctx context.Context) Diagnostics {
	d := Diagnostics{AppVersion: l.opt.Version, BundledPath: l.opt.Bundled}
	cfg, err := l.Config(ctx)
	if err != nil {
		d.ConfigError = err.Error()
	}
	d.Install = l.FindInstall(ctx)
	d.DataDir, d.Address, d.LogPath = cfg.DataDir, cfg.Addr, cfg.LogPath()
	if def, ok := l.opt.Describe(ctx); ok {
		d.ServiceInstalled, d.ServiceBinary = true, def.Binary
		d.ServiceManager = "launchd" // the only definition Describe reads
	}
	if v, ok := l.Health(ctx, cfg.ControllerURL()); ok {
		d.Healthy, d.ControllerVer = true, v
		if err := l.checkAuth(ctx, cfg); err != nil {
			d.AuthError = err.Error()
		}
	}
	d.LogTail = tailOf(cfg.LogPath(), 40)
	return d
}

// DiagnosticsText is Diagnose as plain text to read or paste into a bug report. The
// access token is never in it, and is scrubbed from anything quoted in it.
func (l *Launcher) DiagnosticsText(ctx context.Context) string {
	token := ""
	if cfg, err := l.Config(ctx); err == nil {
		token, _ = cfg.ResolveToken(false)
	}
	return l.Diagnose(ctx).Text(token)
}

// Text is the diagnostics as plain text, with token (and any token=… in it) removed.
func (d Diagnostics) Text(token string) string {
	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "%-20s %s\n", k+":", v) }
	row("App", d.AppVersion)
	row("Bundled program", orNone(d.BundledPath))
	row("Installed program", orNone(strings.TrimSpace(d.Install.Path+" "+d.Install.Version)))
	row("Data directory", orNone(d.DataDir))
	row("Address", orNone(d.Address))
	if d.ConfigError != "" {
		row("Settings error", d.ConfigError)
	}
	if d.ServiceInstalled {
		row("Login service", d.ServiceManager+" → "+orNone(d.ServiceBinary))
	} else {
		row("Login service", "not installed")
	}
	if d.Healthy {
		row("Controller", "answering, version "+d.ControllerVer)
	} else {
		row("Controller", "not answering")
	}
	if d.AuthError != "" {
		row("Sign-in", d.AuthError)
	}
	row("Log", orNone(d.LogPath))
	if d.LogTail != "" {
		b.WriteString("\n--- end of the log ---\n" + d.LogTail + "\n")
	}
	return Redact(b.String(), token)
}

var tokenInText = regexp.MustCompile(`(?i)((?:access_)?token=)[0-9A-Za-z._~+/=-]{8,}`)

// Redact removes the access token from text, wherever it appears: as the value
// itself, or as token=… in an address.
func Redact(text, token string) string {
	if len(token) >= 8 {
		text = strings.ReplaceAll(text, token, "<token>")
	}
	return tokenInText.ReplaceAllString(text, "${1}<token>")
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

// ---- helpers ----

func runCommand(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var pathMarkers = regexp.MustCompile(`__WERKBORD_PATH__(.*?)__WERKBORD_END__`)

// loginShellPATH is the PATH an interactive login shell has: where the person's own
// tools are, nvm's and npm's included. Asking is the only way to know, since a program
// started from the Finder is not given it. Markers keep whatever a startup file prints
// out of the answer, and a shell that does not answer in time is simply not used.
func loginShellPATH(ctx context.Context) string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-ilc", `printf '__WERKBORD_PATH__%s__WERKBORD_END__' "$PATH"`).Output()
	if err != nil {
		return ""
	}
	if m := pathMarkers.FindSubmatch(out); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// lastLine is the most useful line of what a failed command printed: the program
// ends its output with the reason.
func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < 4; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			keep = append([]string{l}, keep...)
		}
	}
	if len(keep) == 0 {
		return err.Error()
	}
	return strings.Join(keep, " · ")
}

// tailOf returns the last n lines of a file, or "".
func tailOf(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

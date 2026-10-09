package launcher

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/config"
	"devboard/internal/controller"
	"devboard/internal/daemon"
	"devboard/internal/update"
)

var bg = context.Background()

// world is a computer for the launcher to find things on: a home directory, a data
// directory, a fake `werkbord` program that records what it is asked to do, and, when
// that program is told to start the controller, a real controller in this process.
type world struct {
	t    *testing.T
	home string
	cfg  config.Config

	mu    sync.Mutex
	calls []string            // every command the launcher ran, "path arg arg"
	envs  map[string][]string // the environment given to each command that was run
	ctrl  *controller.Controller
	// the answers of the commands that change things
	startErr, upgradeErr, updateErr error
	// def is the login service's definition, if one is installed
	def       *daemon.Definition
	serviceOn bool
	startedAs string // the version the controller reports when the fake program starts it
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WERKBORD_DATA_DIR", "")
	t.Setenv("DEVBOARD_DATA_DIR", "")
	t.Setenv("WERKBORD_ADDR", "")
	t.Setenv("DEVBOARD_ADDR", "")
	cfg := config.Default()
	cfg.DataDir = filepath.Join(home, "data")
	cfg.Addr = freeAddr(t)
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// What the installed service says, as setup would have recorded it: a fresh machine has none.
	w := &world{t: t, home: home, cfg: cfg, envs: map[string][]string{}, startedAs: "v1.0.0"}
	t.Setenv("WERKBORD_DATA_DIR", cfg.DataDir)
	t.Setenv("WERKBORD_ADDR", cfg.Addr)
	t.Cleanup(func() {
		if w.ctrl != nil {
			_ = w.ctrl.Shutdown(bg)
		}
	})
	return w
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// program makes a file that is executable, at path, which reports version when asked: the
// version is in the file, as it is in a real program, so a copy of it reports the same.
func (w *world) program(path, version string) string {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n# "+version+"\n"), 0o755); err != nil {
		w.t.Fatal(err)
	}
	return path
}

// versionOf is what the program at path says its version is; a missing file does not run.
func (w *world) versionOf(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	_, v, ok := strings.Cut(strings.TrimSpace(string(b)), "\n# ")
	return v, ok
}

func (w *world) installDir() string { return filepath.Join(w.home, ".local", "bin") }

// startController runs a real controller on the configured address, as the service would.
func (w *world) startController(version string) {
	w.t.Helper()
	if w.ctrl != nil {
		w.t.Fatal("a second controller was started on one data directory by the test's own fake program")
	}
	c := controller.New(w.cfg, slog.New(slog.DiscardHandler), version)
	if err := c.Start(bg); err != nil {
		w.t.Fatal(err)
	}
	w.ctrl = c
}

func (w *world) count(prefix string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, c := range w.calls {
		if strings.Contains(c, prefix) {
			n++
		}
	}
	return n
}

func (w *world) mutations() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, c := range w.calls {
		if !strings.HasSuffix(c, " version") {
			out = append(out, c)
		}
	}
	return out
}

// run is the fake werkbord program and the Runner the launcher is given.
func (w *world) run(_ context.Context, env []string, name string, args ...string) (string, error) {
	w.mu.Lock()
	w.calls = append(w.calls, name+" "+strings.Join(args, " "))
	w.envs[name+" "+strings.Join(args, " ")] = env
	w.mu.Unlock()
	v, runs := w.versionOf(name)
	if !runs {
		return "", errors.New("fork/exec " + name + ": no such file or directory")
	}
	if len(args) == 0 {
		return "", errors.New("no command")
	}
	switch args[0] {
	case "version":
		return v + "\n", nil
	case "start", "setup":
		if w.startErr != nil {
			return "werkbord: " + w.startErr.Error() + "\n", w.startErr
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.ctrl == nil {
			c := controller.New(w.cfg, slog.New(slog.DiscardHandler), w.startedAs)
			if err := c.Start(bg); err != nil {
				return "", err
			}
			w.ctrl = c
		}
		if args[0] == "setup" {
			w.serviceOn = !contains(args, "--no-service")
		}
		return "running\n", nil
	case "install-release":
		if w.upgradeErr != nil {
			return "werkbord: " + w.upgradeErr.Error() + "\n", w.upgradeErr
		}
		w.mu.Lock()
		old := w.ctrl
		w.ctrl = nil
		w.mu.Unlock()
		if old != nil {
			// A controller that is up, as the real one handles it: stop it, replace the program, start the new one, and
			// remove the old program once the new controller has come up.
			_ = old.Shutdown(bg)
			w.program(args[1], v) // the installed program becomes the one that ran this
			c := controller.New(w.cfg, slog.New(slog.DiscardHandler), v)
			if err := c.Start(bg); err != nil {
				return "", err
			}
			w.mu.Lock()
			w.ctrl = c
			w.mu.Unlock()
			return "Installed\n", nil
		}
		// As the real one does for one that is not: the old program is kept beside the new one until the new controller is seen to start.
		if err := os.Rename(args[1], args[1]+".prev"); err != nil {
			return "", err
		}
		w.program(args[1], v) // the installed program becomes the one that ran this
		return "Installed\n", nil
	case "update":
		if w.updateErr != nil {
			return "werkbord: " + w.updateErr.Error() + "\n", w.updateErr
		}
		w.program(name, "v9.9.9")
		return "Downloading v9.9.9…\n", nil
	}
	return "", errors.New("unexpected command " + args[0])
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// launcher returns a launcher for the world. Everything the real system provides (the shell's
// PATH, launchd's definition) is the world's, so nothing on the machine running the tests is read.
func (w *world) launcher(opt Options) *Launcher {
	opt.Home = w.home
	opt.Run = w.run
	opt.ShellPATH = func(context.Context) string { return "/shell/bin:/usr/bin" }
	opt.Poll = 10 * time.Millisecond
	opt.StartTimeout = 3 * time.Second
	opt.Describe = func(context.Context) (daemon.Definition, bool) {
		if w.def == nil {
			return daemon.Definition{}, false
		}
		return *w.def, true
	}
	opt.ServiceInstalled = func(context.Context, string) bool { return w.def != nil || w.serviceOn }
	if opt.HTTP == nil {
		opt.HTTP = &http.Client{Timeout: time.Second}
	}
	return New(opt)
}

func (w *world) token() string {
	w.t.Helper()
	tok, err := w.cfg.ResolveToken(false)
	if err != nil || tok == "" {
		w.t.Fatalf("token: %q, %v", tok, err)
	}
	return tok
}

// ---- joining a controller that is running ----

func TestAControllerThatIsRunningIsJoinedAndLeftAlone(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.1.0")
	w.startController("v1.1.0")
	// The program the app carries is the one that is installed and running: nothing is to be changed.
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	l := w.launcher(Options{Version: "v1.1.0", Bundled: bundled})

	var steps []Phase
	conn, err := l.Connect(bg, func(s Step) { steps = append(steps, s.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	if got := w.mutations(); len(got) != 0 {
		t.Fatalf("Connect changed things on a computer where the controller was running: %v", got)
	}
	if conn.URL != "http://"+w.cfg.Addr || conn.Version != "v1.1.0" || conn.Notice != "" {
		t.Fatalf("connection = %+v", conn)
	}
	if conn.SignInURL != "http://"+w.cfg.Addr+"/#token="+w.token() {
		t.Fatalf("sign-in link = %q: the token goes in the fragment, which no server sees", conn.SignInURL)
	}
	for _, p := range steps {
		if p == PhaseInstalling || p == PhaseUpgrading || p == PhaseStarting {
			t.Fatalf("it reported %s with a controller running (%v)", p, steps)
		}
	}
}

func TestTheLoginServiceDecidesWhichInstallationIsJoined(t *testing.T) {
	// The app was started from the Finder, so it has none of the environment `werkbord setup` ran in.
	// The service says which program and which data it was set up with: that is the installation there is.
	w := newWorld(t)
	other := filepath.Join(w.home, "elsewhere", "data")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WERKBORD_DATA_DIR", "") // as the Finder leaves it
	t.Setenv("WERKBORD_ADDR", "")
	tool := w.program(filepath.Join(w.home, "tools", "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: tool, DataDir: other}
	w.cfg.DataDir = other
	w.cfg.Addr = freeAddr(t)
	if err := os.WriteFile(filepath.Join(other, "config.json"), []byte(`{"addr":"`+w.cfg.Addr+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w.serviceOn = true
	l := w.launcher(Options{Version: "v1.0.0"})
	if _, err := l.Connect(bg, nil); err != nil {
		t.Fatal(err)
	}
	if w.count(tool+" start") != 1 || w.count("setup") != 0 {
		t.Fatalf("calls: %v", w.calls)
	}
	env := w.envs[tool+" start"]
	if !contains(env, "WERKBORD_DATA_DIR="+other) {
		t.Fatalf("the program was not told the data directory it was set up with: %v", env)
	}
	// There is one database, in the installation's data directory, and none in the default place.
	if _, err := os.Stat(filepath.Join(w.home, "Library", "Application Support", "werkbord")); err == nil {
		t.Fatal("a second, default data directory was created")
	}
}

func TestADataDirectoryNamedInTheEnvironmentWinsOverTheService(t *testing.T) {
	w := newWorld(t) // WERKBORD_DATA_DIR is w.cfg.DataDir
	w.def = &daemon.Definition{Binary: w.program(filepath.Join(w.home, "tools", "werkbord"), "v1.0.0"), DataDir: filepath.Join(w.home, "not-this-one")}
	l := w.launcher(Options{Version: "v1.0.0"})
	cfg, err := l.Config(bg)
	if err != nil || cfg.DataDir != w.cfg.DataDir {
		t.Fatalf("data dir = %q, %v", cfg.DataDir, err)
	}
}

// ---- starting one ----

func TestAControllerThatIsNotRunningIsStartedOnceByTheInstalledProgram(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
	l := w.launcher(Options{Version: "v1.0.0"})

	var steps []Phase
	conn, err := l.Connect(bg, func(s Step) { steps = append(steps, s.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	if w.count(installed+" start") != 1 || len(w.mutations()) != 1 {
		t.Fatalf("calls: %v", w.mutations())
	}
	if conn.Version != "v1.0.0" || !strings.HasPrefix(conn.SignInURL, conn.URL+"/#token=") {
		t.Fatalf("connection = %+v", conn)
	}
	if steps[0] != PhaseLooking || steps[len(steps)-1] != PhaseConnecting {
		t.Fatalf("progress = %v", steps)
	}
	// It is the installed program that was asked: it knows the service, the lock and the database.
	if env := w.envs[installed+" start"]; !contains(env, "PATH=/shell/bin:/usr/bin") || !contains(env, "WERKBORD_DATA_DIR="+w.cfg.DataDir) {
		t.Fatalf("environment = %v", env)
	}
	// Opening the app again finds it running and does nothing.
	if _, err := l.Connect(bg, nil); err != nil || len(w.mutations()) != 1 {
		t.Fatalf("second connect: %v %v", err, w.mutations())
	}
}

func TestStartingWithoutALoginServiceDoesNotInstallOne(t *testing.T) {
	// Someone who ran `werkbord setup --no-service` chose that: opening the app must not undo it.
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	if err := os.WriteFile(w.cfg.DBPath(), []byte("x"), 0o600); err != nil { // set up before: there is a database
		t.Fatal(err)
	}
	l := w.launcher(Options{Version: "v1.0.0"})
	if _, err := l.Connect(bg, nil); err != nil {
		t.Fatal(err)
	}
	if w.count(installed+" start") != 1 || w.count("setup") != 0 {
		t.Fatalf("calls: %v", w.mutations())
	}
}

// ---- first run ----

func TestFirstRunInstallsTheBundledProgramWhereTheInstallerPutsItAndSetsUp(t *testing.T) {
	w := newWorld(t)
	bundledDir := t.TempDir() // stands for Werkbord.app/Contents/Helpers
	bundled := w.program(filepath.Join(bundledDir, "werkbord"), "v1.1.0")
	w.startedAs = "v1.1.0"
	l := w.launcher(Options{Version: "v1.1.0", Bundled: bundled})

	var steps []Phase
	conn, err := l.Connect(bg, func(s Step) { steps = append(steps, s.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(w.installDir(), "werkbord")
	want, _ := os.ReadFile(bundled)
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(want) {
		t.Fatalf("the program was not copied to %s: %v", dst, err)
	}
	if fi, _ := os.Stat(dst); fi.Mode()&0o111 == 0 {
		t.Fatal("the installed program is not executable")
	}
	if _, err := os.Stat(filepath.Join(w.installDir(), ".werkbord.new")); err == nil {
		t.Fatal("a staging file was left behind")
	}
	// The service runs the installed copy, never the one inside the .app.
	if w.count(dst+" setup --no-open --no-network") != 1 || w.count(bundled+" setup") != 0 || w.count(bundled+" start") != 0 {
		t.Fatalf("calls: %v", w.mutations())
	}
	if !contains(w.envs[dst+" setup --no-open --no-network"], "PATH=/shell/bin:/usr/bin") {
		t.Fatalf("setup was not given the login shell's PATH, so the service could not find the agents: %v", w.envs)
	}
	if conn.Version != "v1.1.0" || !contains(phases(steps), "installing") {
		t.Fatalf("conn %+v steps %v", conn, steps)
	}
}

func phases(ps []Phase) []string {
	var out []string
	for _, p := range ps {
		out = append(out, string(p))
	}
	return out
}

func TestFirstRunWithoutALoginServiceWhenAskedNotTo(t *testing.T) {
	w := newWorld(t)
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	l := w.launcher(Options{Version: "v1.1.0", Bundled: bundled, NoService: true})
	if _, err := l.Connect(bg, nil); err != nil {
		t.Fatal(err)
	}
	if w.count("setup --no-open --no-network --no-service") != 1 {
		t.Fatalf("calls: %v", w.mutations())
	}
}

func TestADevelopmentBuildRunsItsProgramInPlaceAndInstallsNothing(t *testing.T) {
	w := newWorld(t)
	dev := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0-3-gabc1234-dirty")
	l := w.launcher(Options{Version: "v1.1.0-3-gabc1234-dirty", Bundled: dev, NoInstall: true, NoService: true})
	if _, err := l.Connect(bg, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.installDir()); err == nil {
		t.Fatal("a development run installed something into the home directory")
	}
	if w.count(dev+" setup") != 1 {
		t.Fatalf("calls: %v", w.mutations())
	}
}

func TestWithNothingInstalledAndNothingBundledItSaysSo(t *testing.T) {
	w := newWorld(t)
	_, err := w.launcher(Options{Version: "dev"}).Connect(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v", err)
	}
	if len(w.mutations()) != 0 {
		t.Fatalf("calls: %v", w.mutations())
	}
}

func TestAServiceThatPointsAtAProgramThatIsGoneIsSetUpAgain(t *testing.T) {
	// The app was deleted and put back: the service still names the old copy inside it.
	w := newWorld(t)
	w.def = &daemon.Definition{Binary: filepath.Join(w.home, "Applications", "Gone.app", "Helpers", "werkbord"), DataDir: w.cfg.DataDir}
	if err := os.WriteFile(w.cfg.DBPath(), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	l := w.launcher(Options{Version: "v1.1.0", Bundled: bundled})
	if _, err := l.Connect(bg, nil); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(w.installDir(), "werkbord")
	if w.count(dst+" setup --no-open --no-network") != 1 {
		t.Fatalf("a service that would start nothing was not replaced: %v", w.mutations())
	}
}

// ---- upgrading ----

func TestAnOlderInstalledProgramIsUpgradedOnlyWhenNothingIsRunning(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	w.startedAs = "v1.1.0"
	l := w.launcher(Options{Version: "v1.1.0", Bundled: bundled})

	var steps []Phase
	if _, err := l.Connect(bg, func(s Step) { steps = append(steps, s.Phase) }); err != nil {
		t.Fatal(err)
	}
	// The new program's own recovery implementation does the replacing, as the installer has it do.
	if w.count(bundled+" install-release "+installed) != 1 || w.count(installed+" start") != 1 {
		t.Fatalf("calls: %v", w.mutations())
	}
	if !contains(phases(steps), "upgrading") {
		t.Fatalf("steps %v", steps)
	}
	// The controller came up on the new program, so the old one that was kept for that check is not needed, and would
	// make the next update be refused.
	if _, err := os.Stat(installed + ".prev"); err == nil {
		t.Fatal("the old program was left behind after the new controller came up")
	}
}

func TestTheOldProgramIsKeptWhenTheControllerDoesNotComeUpOnTheNewOne(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	w.startedAs = "v1.0.0" // what answers is not the program that was installed
	conn, err := w.launcher(Options{Version: "v1.1.0", Bundled: bundled}).Connect(bg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installed + ".prev"); err != nil {
		t.Fatalf("the old program, which is the way back, was removed although the new one was not seen to run: %v", err)
	}
	if !strings.Contains(conn.Notice, "kept at "+installed+".prev") {
		t.Fatalf("notice = %q", conn.Notice)
	}
}

// ---- an app that is newer than the controller it finds running (it was just updated) ----

func TestAnAppNewerThanTheRunningControllerBringsItUpToDateWhenNothingIsWorking(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	w.startController("v1.0.0")
	first := w.ctrl

	var steps []Phase
	conn, err := w.launcher(Options{Version: "v1.1.0", Bundled: bundled}).Connect(bg, func(s Step) { steps = append(steps, s.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	// By the installer's own entry point, run from the new program: refusal while agents work, a snapshot, a way back.
	if w.count(bundled+" install-release "+installed) != 1 || w.count(installed+" start") != 0 {
		t.Fatalf("calls: %v", w.mutations())
	}
	if conn.Version != "v1.1.0" || conn.Notice != "" {
		t.Fatalf("connection = %+v: the window should open on the new controller, with nothing to say", conn)
	}
	if w.ctrl == first {
		t.Fatal("the controller was not restarted on the new program")
	}
	if v, _ := w.versionOf(installed); v != "v1.1.0" {
		t.Fatalf("the installed program is %s", v)
	}
	if !contains(phases(steps), "upgrading") || contains(phases(steps), "installing") {
		t.Fatalf("steps %v", steps)
	}
	if _, err := os.Stat(installed + ".prev"); err == nil {
		t.Fatal("the old program was left behind after the new controller came up")
	}
	// Opening the app again changes nothing more.
	before := len(w.mutations())
	if _, err := w.launcher(Options{Version: "v1.1.0", Bundled: bundled}).Connect(bg, nil); err != nil || len(w.mutations()) != before {
		t.Fatalf("a second opening changed things: %v %v", err, w.mutations())
	}
}

func TestWhenAgentsAreWorkingTheRunningControllerIsLeftAloneAndTheAppSaysSoOnce(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	w.startController("v1.0.0")
	first := w.ctrl
	w.upgradeErr = errors.New("run run_1 is active on this computer; finish or stop it in Werkbord first, or explicitly use --force to interrupt it and retain its work")
	l := w.launcher(Options{Version: "v1.1.0", Bundled: bundled})

	conn, err := l.Connect(bg, nil)
	if err != nil {
		t.Fatalf("an update that was refused made the app unusable: %v", err)
	}
	if conn.Version != "v1.0.0" || !strings.Contains(conn.Notice, "coding agents are working") || !strings.Contains(conn.Notice, "still on 1.0.0") {
		t.Fatalf("connection = %+v", conn)
	}
	if strings.Contains(conn.Notice, "--force") {
		t.Fatalf("a window must not suggest --force: %q", conn.Notice)
	}
	if w.ctrl != first || w.count(installed+" start") != 0 {
		t.Fatalf("a refused update disturbed the controller: %v", w.mutations())
	}
	if v, _ := w.versionOf(installed); v != "v1.0.0" {
		t.Fatalf("the installed program is %s", v)
	}
	// The window reconnects (View → Reload): it is not told again. It is tried again, though.
	conn2, err := l.Connect(bg, nil)
	if err != nil || conn2.Notice != "" || conn2.Version != "v1.0.0" {
		t.Fatalf("second connection = %+v, %v", conn2, err)
	}
	if n := w.count(bundled + " install-release"); n != 2 {
		t.Fatalf("install-release was tried %d times: it is tried at each opening (and each refusal is the program's own)", n)
	}
	// The agents finish: the next opening completes the update.
	w.upgradeErr = nil
	conn3, err := l.Connect(bg, nil)
	if err != nil || conn3.Version != "v1.1.0" || conn3.Notice != "" {
		t.Fatalf("third connection = %+v, %v", conn3, err)
	}
	if w.ctrl == first {
		t.Fatal("the controller was never moved to the new program")
	}
}

func TestAnAppUpdateNeverMovesAControllerBackwards(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.2.0")
	w.startController("v1.2.0")
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	conn, err := w.launcher(Options{Version: "v1.1.0", Bundled: bundled}).Connect(bg, nil)
	if err != nil || conn.Version != "v1.2.0" || len(w.mutations()) != 0 {
		t.Fatalf("an older app changed a newer controller: %+v %v %v", conn, err, w.mutations())
	}
}

func TestAnAppBuiltFromSourceNeverReplacesAControllerThatIsRunning(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.startController("v1.0.0")
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0-2-gabc1234")
	if _, err := w.launcher(Options{Version: "v1.1.0-2-gabc1234", Bundled: bundled}).Connect(bg, nil); err != nil || len(w.mutations()) != 0 {
		t.Fatalf("a development build disturbed a running controller: %v %v", err, w.mutations())
	}
}

func TestADevelopmentRunNeverReplacesAControllerEither(t *testing.T) {
	w := newWorld(t)
	w.startController("v1.0.0")
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	if _, err := w.launcher(Options{Version: "v1.1.0", Bundled: bundled, NoInstall: true}).Connect(bg, nil); err != nil || len(w.mutations()) != 0 {
		t.Fatalf("NoInstall disturbed a running controller: %v %v", err, w.mutations())
	}
}

func TestThePendingProgramUpdateIsWhatTheAppCarriesAndTheComputerLacks(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	w.startController("v1.0.0")
	l := w.launcher(Options{Version: "v1.1.0", Bundled: bundled})
	if i, b, ok := l.PendingProgramUpdate(bg); !ok || i != "v1.0.0" || b != "v1.1.0" {
		t.Fatalf("pending = %q %q %v", i, b, ok)
	}
	if _, err := l.AdoptBundled(bg); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := l.PendingProgramUpdate(bg); ok {
		t.Fatal("still pending after it was installed")
	}
	if _, err := l.AdoptBundled(bg); err == nil {
		t.Fatal("installing what is already installed must say so, not run the installer again")
	}
	w2 := newWorld(t)
	w2.program(filepath.Join(w2.installDir(), "werkbord"), "v1.1.0")
	if _, _, ok := w2.launcher(Options{Version: "v1.1.0", Bundled: w2.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")}).PendingProgramUpdate(bg); ok {
		t.Fatal("nothing is pending when the versions agree")
	}
}

func TestAdoptBundledExplainsAnUpdateThatWasRefused(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	w.startController("v1.0.0")
	w.upgradeErr = errors.New("runner journal r1.json has running work; finish or resolve it before interruption, or use --force")
	_, err := w.launcher(Options{Version: "v1.1.0", Bundled: bundled}).AdoptBundled(bg)
	if err == nil || !strings.Contains(err.Error(), "coding agents are working") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnInstalledProgramIsNeverReplacedByAnOlderOneOrBySourceBuilds(t *testing.T) {
	for _, tc := range []struct{ name, installed, app string }{
		{"newer installed", "v1.2.0", "v1.1.0"},
		{"same version", "v1.1.0", "v1.1.0"},
		{"app built from source", "v1.0.0", "v1.1.0-2-gabc1234"},
		{"installed built from source", "v1.0.0-2-gabc1234", "v1.1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			installed := w.program(filepath.Join(w.installDir(), "werkbord"), tc.installed)
			w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
			bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), tc.app)
			w.startedAs = tc.installed
			if _, err := w.launcher(Options{Version: tc.app, Bundled: bundled}).Connect(bg, nil); err != nil {
				t.Fatal(err)
			}
			if w.count("install-release") != 0 {
				t.Fatalf("calls: %v", w.mutations())
			}
		})
	}
}

func TestAnUpgradeThatCannotHappenYetDoesNotStopTheAppOpening(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.def = &daemon.Definition{Binary: installed, DataDir: w.cfg.DataDir}
	bundled := w.program(filepath.Join(t.TempDir(), "werkbord"), "v1.1.0")
	w.upgradeErr = errors.New("runner journal r1.json has running work; finish or resolve it before interruption, or use --force")
	conn, err := w.launcher(Options{Version: "v1.1.0", Bundled: bundled}).Connect(bg, nil)
	if err != nil {
		t.Fatalf("an upgrade that was refused made the app unusable: %v", err)
	}
	if !strings.Contains(conn.Notice, "could not replace v1.0.0") || !strings.Contains(conn.Notice, "finish or resolve") {
		t.Fatalf("notice = %q", conn.Notice)
	}
	if v, _ := w.versionOf(installed); v != "v1.0.0" || w.count(installed+" start") != 1 {
		t.Fatalf("the old program should carry on: %v", w.mutations())
	}
}

// ---- things that are wrong ----

func TestAControllerThatRefusesTheToken(t *testing.T) {
	// Another installation answers on the address: opening a window on it would show a sign-in prompt
	// for a token nobody can find, and starting yet another controller would not help.
	w := newWorld(t)
	otherData := t.TempDir()
	cfg := config.Default()
	cfg.DataDir, cfg.Addr = otherData, w.cfg.Addr
	c := controller.New(cfg, slog.New(slog.DiscardHandler), "v1.0.0")
	if err := c.Start(bg); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(bg)
	if _, err := w.cfg.ResolveToken(true); err != nil { // this computer's own, different, token
		t.Fatal(err)
	}
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	_, err := w.launcher(Options{Version: "v1.0.0"}).Connect(bg, nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v", err)
	}
	if got := w.mutations(); len(got) != 0 {
		t.Fatalf("it did something about it: %v", got)
	}
}

func TestSomethingElseOnThePortIsNotAController(t *testing.T) {
	w := newWorld(t)
	l, err := net.Listen("tcp", w.cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = rw.Write([]byte(`{"status":"ok"}`)) // no version: not Werkbord
	}))
	srv.Listener = l
	srv.Start()
	defer srv.Close()
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	if err := os.WriteFile(w.cfg.DBPath(), []byte("x"), 0o600); err != nil { // set up before
		t.Fatal(err)
	}
	w.startErr = errors.New("listen on " + w.cfg.Addr + ": bind: address already in use")
	_, err = w.launcher(Options{Version: "v1.0.0"}).Connect(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "another program is already using "+w.cfg.Addr) {
		t.Fatalf("err = %v", err)
	}
	if w.count(installed+" start") != 1 {
		t.Fatalf("calls: %v", w.mutations())
	}
}

func TestAControllerThatDoesNotComeUpIsReportedWithItsLog(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	if err := os.WriteFile(w.cfg.DBPath(), []byte("x"), 0o600); err != nil { // set up before
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(w.cfg.LogPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.cfg.LogPath(), []byte("old line\nlevel=ERROR msg=\"open database: disk full\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The program says it started but nothing ever answers.
	l := w.launcher(Options{Version: "v1.0.0"})
	l.opt.Run = func(ctx context.Context, env []string, name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "start" {
			return "", nil
		}
		return w.run(ctx, env, name, args...)
	}
	l.opt.StartTimeout = 150 * time.Millisecond
	_, err := l.Connect(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "did not answer") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
}

func TestAFailingStartSaysWhatTheProgramSaid(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.startErr = errors.New("another werkbord is already using the data in /x")
	_, err := w.launcher(Options{Version: "v1.0.0"}).Connect(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "another Werkbord is using the data") {
		t.Fatalf("err = %v", err)
	}
}

// ---- finding the installed program ----

func TestOnlyAProgramThatRunsAndSaysAVersionIsAnInstall(t *testing.T) {
	w := newWorld(t)
	dir := w.installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Not executable, and an executable that is something else.
	if err := os.WriteFile(filepath.Join(dir, "werkbord"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := w.program(filepath.Join(dir, "devboard"), "hello world, I am something else")
	l := w.launcher(Options{Version: "v1.0.0"})
	if inst := l.FindInstall(bg); inst.Path != "" {
		t.Fatalf("found %+v", inst)
	}
	w.program(other, "v0.9.0")
	if inst := l.FindInstall(bg); inst.Path != other || inst.Version != "v0.9.0" {
		t.Fatalf("an install under the old name was not found: %+v", inst)
	}
}

// ---- updates ----

func TestUpdateStatusUsesTheInstalledProgramsVersion(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			http.Redirect(rw, r, "/tag/werkbord-v1.2.0", http.StatusFound)
			return
		}
		http.NotFound(rw, r)
	}))
	defer ts.Close()
	l := w.launcher(Options{Version: "v1.1.0", Source: update.Source{Base: ts.URL}})
	st := l.UpdateStatus(bg, false)
	if !st.Available || st.Current != "v1.0.0" || st.Latest != "v1.2.0" {
		t.Fatalf("status = %+v", st)
	}
}

func TestApplyUpdateRunsTheInstalledProgramsOwnUpdateAndNothingElse(t *testing.T) {
	w := newWorld(t)
	installed := w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	l := w.launcher(Options{Version: "v1.0.0"})
	out, err := l.ApplyUpdate(bg)
	if err != nil || !strings.Contains(out, "Downloading") {
		t.Fatalf("%q, %v", out, err)
	}
	if got := w.mutations(); len(got) != 1 || got[0] != installed+" update" {
		t.Fatalf("calls: %v: the update must be exactly `werkbord update`, with its checksum, backup and rollback", got)
	}
	if v := l.FindInstall(bg).Version; v != "v9.9.9" {
		t.Fatalf("the install was not looked at again: %s", v)
	}
}

func TestApplyUpdateExplainsAnUpdateThatWasRefused(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.updateErr = errors.New("run r_1 is active on this computer; finish or stop it in Werkbord first, or explicitly use --force to interrupt it and retain its work")
	_, err := w.launcher(Options{Version: "v1.0.0"}).ApplyUpdate(bg)
	if err == nil || !strings.Contains(err.Error(), "is active on this computer") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyUpdateRefusesABuildFromSource(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0-5-gabc1234-dirty")
	_, err := w.launcher(Options{Version: "dev"}).ApplyUpdate(bg)
	if err == nil || !strings.Contains(err.Error(), "built from source") || w.count(" update") != 0 {
		t.Fatalf("err = %v, calls %v", err, w.mutations())
	}
}

// ---- diagnostics ----

func TestDiagnosticsNeverContainTheToken(t *testing.T) {
	w := newWorld(t)
	tok, _ := w.cfg.ResolveToken(true)
	if err := os.MkdirAll(filepath.Dir(w.cfg.LogPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	log := "level=INFO msg=\"http request\" path=/api/events\nGET /api/events?access_token=" + tok + " 200\nlink http://127.0.0.1:7420/#token=" + tok + "\nBearer " + tok + "\n"
	if err := os.WriteFile(w.cfg.LogPath(), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	w.startController("v1.0.0")
	text := w.launcher(Options{Version: "v1.0.0"}).DiagnosticsText(bg)
	if strings.Contains(text, tok) {
		t.Fatalf("the diagnostics contain the access token:\n%s", text)
	}
	for _, want := range []string{"v1.0.0", w.cfg.DataDir, "answering", "<token>", "end of the log"} {
		if !strings.Contains(text, want) {
			t.Errorf("diagnostics lack %q:\n%s", want, text)
		}
	}
}

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"token=abcdef0123456789 and more": "token=<token> and more",
		"?access_token=abcdef0123456789":  "?access_token=<token>",
		"/#token=abcdef0123456789":        "/#token=<token>",
		"nothing here":                    "nothing here",
		"short token=abc":                 "short token=abc",
	} {
		if got := Redact(in, ""); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Redact("the secret is s3cr3t-value-123", "s3cr3t-value-123"); strings.Contains(got, "s3cr3t") {
		t.Errorf("Redact left the token in %q", got)
	}
}

// ---- the login shell's PATH ----

func TestLoginShellPATHIgnoresWhatStartupFilesPrint(t *testing.T) {
	dir := t.TempDir()
	sh := filepath.Join(dir, "sh")
	script := "#!/bin/sh\necho 'Welcome back!'\nPATH=/home/me/.nvm/bin:/usr/bin\nprintf '__WERKBORD_PATH__%s__WERKBORD_END__' \"$PATH\"\necho 'bye'\n"
	if err := os.WriteFile(sh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", sh)
	if got := loginShellPATH(bg); got != "/home/me/.nvm/bin:/usr/bin" {
		t.Fatalf("PATH = %q", got)
	}
	if err := os.WriteFile(sh, []byte("#!/bin/sh\necho no markers\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := loginShellPATH(bg); got != "" {
		t.Fatalf("a shell that did not answer properly gave %q", got)
	}
	t.Setenv("SHELL", filepath.Join(dir, "missing"))
	if got := loginShellPATH(bg); got != "" {
		t.Fatalf("a missing shell gave %q", got)
	}
}

// The person turned looking for updates off (noUpdateCheck in config.json, or WERKBORD_NO_UPDATE_CHECK): the controller
// honours that, and the app, which asks the same releases page, must too: not one request leaves this computer.
func TestUpdateStatusAsksNobodyWhenLookingForUpdatesIsTurnedOff(t *testing.T) {
	for _, how := range []string{"config.json", "environment"} {
		t.Run(how, func(t *testing.T) {
			w := newWorld(t)
			w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
			var asked int32
			ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&asked, 1)
				http.Redirect(rw, r, "/tag/werkbord-v1.2.0", http.StatusFound)
			}))
			defer ts.Close()
			if how == "config.json" {
				if err := os.WriteFile(filepath.Join(w.cfg.DataDir, "config.json"), []byte(`{"noUpdateCheck": true}`), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("WERKBORD_NO_UPDATE_CHECK", "1")
			}
			l := w.launcher(Options{Version: "v1.1.0", Source: update.Source{Base: ts.URL}})
			for _, force := range []bool{false, true} {
				st := l.UpdateStatus(bg, force)
				if !st.Disabled || st.Available || st.Latest != "" {
					t.Fatalf("force=%v status = %+v", force, st)
				}
			}
			if n := atomic.LoadInt32(&asked); n != 0 {
				t.Fatalf("%d requests were made although looking for updates is turned off", n)
			}
		})
	}
	// and turning it back on is noticed without restarting the app
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v1.0.0")
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, "/tag/werkbord-v1.2.0", http.StatusFound)
	}))
	defer ts.Close()
	cfgFile := filepath.Join(w.cfg.DataDir, "config.json")
	_ = os.WriteFile(cfgFile, []byte(`{"noUpdateCheck": true}`), 0o600)
	l := w.launcher(Options{Version: "v1.1.0", Source: update.Source{Base: ts.URL}})
	if !l.UpdateStatus(bg, true).Disabled {
		t.Fatal("expected it to be off")
	}
	_ = os.WriteFile(cfgFile, []byte(`{}`), 0o600)
	if st := l.UpdateStatus(bg, true); st.Disabled || !st.Available {
		t.Fatalf("after turning it back on: %+v", st)
	}
}

// What the app asks of the controller it joins, written down: a new shell must keep working against an older
// controller (and the other way round), so the calls they share are a short list that a change to the launcher
// cannot grow by accident. The web app calls the shell through desktop/internal/shell, which has its own list.
func TestTheLauncherAsksTheControllerForOnlyTheseThings(t *testing.T) {
	allowed := map[string]bool{
		"/api/health":         true, // public; says it is a controller and what version
		"/api/control-center": true, // read-only desktop update preflight; backend replacement rechecks independently
		"/api/projects":       true, // needs the token: does this controller accept this computer's?
	}
	src, err := os.ReadFile("launcher.go")
	if err != nil {
		t.Fatal(err)
	}
	safety, err := os.ReadFile("safety.go")
	if err != nil {
		t.Fatal(err)
	}
	src = append(src, safety...)
	for _, m := range regexp.MustCompile(`"(/api/[a-zA-Z0-9/_-]*)`).FindAllStringSubmatch(string(src), -1) {
		if !allowed[m[1]] {
			t.Errorf("launcher.go asks the controller for %s: a call the app and an older controller share is a decision, "+
				"not an accident; add it to this list and to docs/DESKTOP.md (\"What the shell and the controller share\")", m[1])
		}
	}
	// and the commands it runs on the installed program, which older programs have too
	cmds := map[string]bool{"version": true, "start": true, "setup": true, "install-release": true, "update": true}
	for _, m := range regexp.MustCompile(`(?:Run\(\w+,[^,]+,[^,]+, |args = \[\]string\{|opt\.Run\([^)]*?)"([a-z-]+)"`).FindAllStringSubmatch(string(src), -1) {
		if !cmds[m[1]] {
			t.Errorf("launcher.go runs `werkbord %s`", m[1])
		}
	}
}

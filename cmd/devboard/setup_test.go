package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/store/sqlite"
)

// signInWhenOpened makes the fake private network sign in once the browser has been
// sent to its sign-in link, as a user does.
func (e *testEnv) signInWhenOpened() {
	e.browseFn = func(url string) error {
		if url == e.node.authURL {
			go func() {
				time.Sleep(50 * time.Millisecond)
				e.node.signIn()
			}()
		}
		return nil
	}
}

func TestSetupFromACleanMachine(t *testing.T) {
	e := newTestEnv(t)
	e.signInWhenOpened()
	// A computer with nothing: no data directory content but what the test put in config.
	if _, err := os.Stat(filepath.Join(e.dataDir, "devboard.db")); err == nil {
		t.Fatal("the database exists before setup")
	}

	if err := e.app.cmdSetup(bg, nil); err != nil {
		t.Fatalf("setup: %v\n%s", err, e.output())
	}
	out := e.output()

	// Directories, database, token, service, controller.
	for _, d := range []string{"logs", "worktrees"} {
		if fi, err := os.Stat(filepath.Join(e.dataDir, d)); err != nil || !fi.IsDir() {
			t.Errorf("%s was not created", d)
		}
	}
	info, err := sqlite.Inspect(bg, filepath.Join(e.dataDir, "devboard.db"))
	if err != nil || !info.Exists || info.Version != info.Latest || info.Integrity != "ok" {
		t.Fatalf("database = %+v, %v", info, err)
	}
	if fi, err := os.Stat(filepath.Join(e.dataDir, "token")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file = %v, %v", fi, err)
	}
	st, _ := e.mgr.Status(bg)
	if !st.Installed || !st.Running || e.mgr.starts != 1 {
		t.Fatalf("service = %+v after %d start(s)", st, e.mgr.starts)
	}
	// The service is told what it needs: this binary, the controller, the data dir, the PATH setup was run with, a log.
	if e.mgr.spec.Binary == "" || len(e.mgr.spec.Args) != 1 || e.mgr.spec.Args[0] != "serve" || e.mgr.spec.DataDir != e.dataDir ||
		!strings.Contains(e.mgr.spec.Path, "/captured/by/setup") || e.mgr.spec.LogFile != filepath.Join(e.dataDir, "logs", "controller.log") {
		t.Fatalf("spec = %+v", e.mgr.spec)
	}
	if !e.running() {
		t.Fatal("the controller is not running")
	}

	// This computer is already a runner.
	c, _ := e.app.client()
	rs, err := c.runners(bg)
	if err != nil || len(rs) != 1 || !rs[0].Online || rs[0].Name == "" {
		t.Fatalf("runners = %+v, %v", rs, err)
	}

	// Private networking: the browser was sent to sign in, and then to the app.
	opened := e.opened()
	if len(opened) != 2 || opened[0] != e.node.authURL {
		t.Fatalf("browser opened %v: first the sign-in page", opened)
	}
	// The Tailscale sign-in link is shown to the user (they need it if the browser does not open) and the access token never is.
	if !strings.HasPrefix(opened[1], e.app.controllerURL()+"/#token="+e.token()) {
		t.Fatalf("the app was opened at %s", opened[1])
	}
	n, err := c.network(bg)
	if err != nil || n.State != "connected" || n.URL != "https://devboard-test.tail1234.ts.net/" || n.Choice != "on" {
		t.Fatalf("network = %+v, %v", n, err)
	}
	for _, want := range []string{"database", "service", "controller", "runner", "phone access", "https://devboard-test.tail1234.ts.net/", "Opened Dev Board in your browser"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// What was printed never includes the access token.
	if strings.Contains(out, e.token()) {
		t.Errorf("setup printed the access token:\n%s", out)
	}
	// config.json keeps what the user had and records the address.
	var cfg map[string]any
	b, _ := os.ReadFile(filepath.Join(e.dataDir, "config.json"))
	_ = json.Unmarshal(b, &cfg)
	if cfg["agents"] == nil {
		t.Errorf("setup dropped the user's configuration: %s", b)
	}
}

func TestSetupSkipsWhatIsAskedToBeSkipped(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatalf("%v\n%s", err, e.output())
	}
	if len(e.opened()) != 0 {
		t.Fatalf("a browser was opened: %v", e.opened())
	}
	c, _ := e.app.client()
	if n, _ := c.network(bg); n.State != "off" || n.Choice != "unset" {
		t.Fatalf("network = %+v: it should not have been touched", n)
	}
	// With no browser opened it says how to get there, and never prints the link that signs in: it carries the token.
	if !strings.Contains(e.output(), "devboard open") || strings.Contains(e.output(), e.token()) {
		t.Fatalf("output:\n%s", e.output())
	}
}

func TestSetupWithNoStartOnlyPreparesEverything(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-start"}); err != nil {
		t.Fatal(err)
	}
	st, _ := e.mgr.Status(bg)
	if !st.Installed || st.Running || e.running() {
		t.Fatalf("service = %+v: installed, not started", st)
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "devboard.db")); err != nil {
		t.Fatal("the database was not prepared")
	}
}

func TestSetupHonoursTheEnvironmentFlagsTheInstallerUses(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("DEVBOARD_NO_OPEN", "1")
	t.Setenv("DEVBOARD_NO_NETWORK", "true")
	if err := e.app.cmdSetup(bg, nil); err != nil {
		t.Fatal(err)
	}
	if len(e.opened()) != 0 || !e.running() {
		t.Fatalf("opened %v, running %v", e.opened(), e.running())
	}
	if !strings.Contains(e.output(), "skipped") {
		t.Fatalf("output:\n%s", e.output())
	}
}

func TestSetupTwiceIsAnUpgradeNotASecondController(t *testing.T) {
	e := newTestEnv(t)
	args := []string{"--no-network", "--no-open"}
	if err := e.app.cmdSetup(bg, args); err != nil {
		t.Fatal(err)
	}
	c, _ := e.app.client()
	repo := newGitRepo(t)
	if _, err := c.registerProject(bg, repo, "keep"); err != nil {
		t.Fatal(err)
	}
	tokenBefore := e.token()

	e.out.Reset()
	if err := e.app.cmdSetup(bg, args); err != nil {
		t.Fatalf("%v\n%s", err, e.output())
	}
	if e.mgr.starts != 2 { // the first start, and the restart: one controller at a time, replaced
		t.Fatalf("controllers started: %d", e.mgr.starts)
	}
	ps, err := c.listProjects(bg)
	if err != nil || len(ps) != 1 || ps[0].Name != "keep" {
		t.Fatalf("projects after setup again = %+v, %v", ps, err)
	}
	if e.token() != tokenBefore {
		t.Fatal("setting up again changed the access token, signing out every phone")
	}
}

func TestSetupDoesNotTurnBackOnWhatTheUserTurnedOff(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	c, _ := e.app.client()
	if _, err := c.setNetwork(bg, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.setNetwork(bg, false); err != nil {
		t.Fatal(err)
	}
	e.out.Reset()
	if err := e.app.cmdSetup(bg, []string{"--no-open"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.network(bg); n.State != "off" || n.Choice != "off" {
		t.Fatalf("network = %+v", n)
	}
	if !strings.Contains(e.output(), "as you left it") {
		t.Fatalf("output:\n%s", e.output())
	}
}

func TestSetupWhenTheUserDoesNotSignInYetCarriesOn(t *testing.T) {
	e := newTestEnv(t) // nobody signs in
	if err := e.app.cmdSetup(bg, []string{"--network-wait", "300ms"}); err != nil {
		t.Fatalf("%v\n%s", err, e.output())
	}
	opened := e.opened()
	if len(opened) != 2 || opened[0] != e.node.authURL {
		t.Fatalf("opened %v", opened)
	}
	c, _ := e.app.client()
	if n, _ := c.network(bg); n.State != "needs_login" {
		t.Fatalf("network = %+v", n)
	}
	if !strings.Contains(e.output(), "waiting for you to sign in") {
		t.Fatalf("output:\n%s", e.output())
	}
	// Signing in later is picked up with nothing more to do.
	e.node.signIn()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if n, _ := c.network(bg); n.State == "connected" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the network did not come up after signing in")
}

func TestSetupMovesToAnotherPortWhenTheDefaultIsTaken(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:7420")
	if err != nil {
		t.Skip("7420 is in use on this machine, so it cannot be taken for the test")
	}
	defer l.Close()
	e := newTestEnv(t)
	t.Setenv("DEVBOARD_ADDR", "127.0.0.1:7420")
	e.app.cfg.Addr = "127.0.0.1:7420"
	// With the user's own config.json holding no addr, the default is what is in use.
	if err := e.app.chooseAddr(bg); err != nil {
		t.Fatal(err)
	}
	if e.app.cfg.Addr == "127.0.0.1:7420" || !strings.HasPrefix(e.app.cfg.Addr, "127.0.0.1:74") {
		t.Fatalf("addr = %s", e.app.cfg.Addr)
	}
	b, _ := os.ReadFile(filepath.Join(e.dataDir, "config.json"))
	var cfg map[string]any
	_ = json.Unmarshal(b, &cfg)
	if cfg["addr"] != e.app.cfg.Addr || cfg["agents"] == nil {
		t.Fatalf("config.json = %s: the new address is remembered and the rest kept", b)
	}
	// An address the user chose is not quietly replaced.
	e.app.cfg.Addr = "127.0.0.1:7499"
	l2, err := net.Listen("tcp", "127.0.0.1:7499")
	if err != nil {
		t.Skip("7499 in use")
	}
	defer l2.Close()
	if err := e.app.chooseAddr(bg); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("a chosen address in use: %v", err)
	}
}

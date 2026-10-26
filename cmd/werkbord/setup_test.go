package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/store/sqlite"
)

func TestSetupFromACleanMachine(t *testing.T) {
	e := newTestEnv(t)
	// A computer with nothing: no data directory content but what the test put in config.
	if _, err := os.Stat(filepath.Join(e.dataDir, "werkbord.db")); err == nil {
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
	info, err := sqlite.Inspect(bg, filepath.Join(e.dataDir, "werkbord.db"))
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

	// The browser was sent to the app, signed in. Setup does not touch the private network (Tailscale): phones come in
	// through the relay, and the private network is only for other computers, which the user turns on on purpose.
	opened := e.opened()
	if len(opened) != 1 || !strings.HasPrefix(opened[0], e.app.controllerURL()+"/#token="+e.token()) {
		t.Fatalf("browser opened %v: the app, signed in", opened)
	}
	n, err := c.network(bg)
	if err != nil || n.State != "off" || n.Choice != "unset" {
		t.Fatalf("network = %+v, %v: setup must not join a network the user did not ask for", n, err)
	}
	for _, want := range []string{"database", "service", "controller", "runner", "phone access", "Opened Werkbord in your browser"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Tailscale") {
		t.Errorf("setup mentions Tailscale:\n%s", out)
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
	if err := e.app.cmdSetup(bg, []string{"--no-open"}); err != nil {
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
	if !strings.Contains(e.output(), "werkbord open") || strings.Contains(e.output(), e.token()) {
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
	if _, err := os.Stat(filepath.Join(e.dataDir, "werkbord.db")); err != nil {
		t.Fatal("the database was not prepared")
	}
}

func TestSetupHonoursTheEnvironmentFlagsTheInstallerUses(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("WERKBORD_NO_OPEN", "1")
	if err := e.app.cmdSetup(bg, nil); err != nil {
		t.Fatal(err)
	}
	if len(e.opened()) != 0 || !e.running() {
		t.Fatalf("opened %v, running %v", e.opened(), e.running())
	}
	if !strings.Contains(e.output(), "phone access") {
		t.Fatalf("output:\n%s", e.output())
	}
}

func TestSetupTwiceIsAnUpgradeNotASecondController(t *testing.T) {
	e := newTestEnv(t)
	args := []string{"--no-open"}
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

func TestSetupLeavesThePrivateNetworkAlone(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-open"}); err != nil {
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
	if strings.Contains(e.output(), "Tailscale") {
		t.Fatalf("setup talked about the private network:\n%s", e.output())
	}
}

func TestSetupMovesToAnotherPortWhenTheDefaultIsTaken(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:7420")
	if err != nil {
		t.Skip("7420 is in use on this machine, so it cannot be taken for the test")
	}
	defer l.Close()
	e := newTestEnv(t)
	t.Setenv("WERKBORD_ADDR", "127.0.0.1:7420")
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

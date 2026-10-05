package main

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"devboard/internal/config"
	"devboard/internal/controller"
)

func TestServiceLifecycleManagesTheOneController(t *testing.T) {
	e := newTestEnv(t)
	_ = e.mgr.Install(bg, e.mgr.spec) // as setup would have

	if err := e.app.cmdStart(bg, nil); err != nil {
		t.Fatal(err)
	}
	if !e.running() || e.mgr.starts != 1 || !strings.Contains(e.output(), "is running at") {
		t.Fatalf("running=%v starts=%d\n%s", e.running(), e.mgr.starts, e.output())
	}

	// Starting again starts nothing.
	e.out.Reset()
	if err := e.app.cmdStart(bg, nil); err != nil || e.mgr.starts != 1 || !strings.Contains(e.output(), "already running") {
		t.Fatalf("second start: %v starts=%d\n%s", err, e.mgr.starts, e.output())
	}

	// Status says what is going on, and its JSON form is for scripts.
	e.out.Reset()
	if err := e.app.cmdStatus(bg, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"controller", "running at " + e.app.controllerURL(), "fake service, pid 4321", "starts at login", "yes", "projects", "claude-code", "codex"} {
		if !strings.Contains(e.output(), want) {
			t.Errorf("status lacks %q:\n%s", want, e.output())
		}
	}
	e.out.Reset()
	if err := e.app.cmdStatus(bg, []string{"--json"}); err != nil {
		t.Fatal(err)
	}
	var st statusReport
	if err := json.Unmarshal(e.out.Bytes(), &st); err != nil || !st.Running || !st.Service.Installed || st.Controller != "test-controller" {
		t.Fatalf("status json = %+v, %v\n%s", st, err, e.output())
	}
	if strings.Contains(e.output(), e.token()) {
		t.Fatal("status printed the access token")
	}
	// Nor the private network's one-time sign-in link, in either form.
	if _, err := mustClient(t, e).setNetwork(bg, true); err != nil {
		t.Fatal(err)
	}
	waitNeedsLogin(t, e)
	for _, args := range [][]string{nil, {"--json"}} {
		e.out.Reset()
		_ = e.app.cmdStatus(bg, args)
		if strings.Contains(e.output(), "one-time-link") || strings.Contains(e.output(), e.token()) {
			t.Fatalf("status %v printed a credential:\n%s", args, e.output())
		}
	}

	// Restart replaces it with a new one.
	e.out.Reset()
	if err := e.app.cmdRestart(bg, nil); err != nil || e.mgr.starts != 2 || !e.running() {
		t.Fatalf("restart: %v starts=%d", err, e.mgr.starts)
	}

	// Stop stops it, and says so; stopping again is not an error.
	e.out.Reset()
	if err := e.app.cmdStop(bg, nil); err != nil || e.running() || !strings.Contains(e.output(), "stopped") {
		t.Fatalf("stop: %v running=%v\n%s", err, e.running(), e.output())
	}
	e.out.Reset()
	if err := e.app.cmdStop(bg, nil); err != nil || !strings.Contains(e.output(), "was not running") {
		t.Fatalf("second stop: %v\n%s", err, e.output())
	}
	// Status of a stopped controller says so, and exits non-zero for scripts.
	e.out.Reset()
	if err := e.app.cmdStatus(bg, nil); err == nil || !strings.Contains(e.output(), "not running") {
		t.Fatalf("status when stopped: %v\n%s", err, e.output())
	}
}

func TestStartNeverDuplicatesAControllerStartedByHand(t *testing.T) {
	e := newTestEnv(t)
	_ = e.mgr.Install(bg, e.mgr.spec)
	// `werkbord serve` in a terminal.
	cfg, _ := config.Load()
	by := controller.New(cfg, slog.New(slog.DiscardHandler), "by-hand")
	by.SetNetworkBackend(e.node)
	if err := by.Start(bg); err != nil {
		t.Fatal(err)
	}
	defer by.Shutdown(bg)

	if err := e.app.cmdStart(bg, nil); err != nil {
		t.Fatal(err)
	}
	if e.mgr.starts != 0 {
		t.Fatalf("the service started a second controller beside the one running")
	}
	if !strings.Contains(e.output(), "not started by the service") {
		t.Fatalf("it should say whose controller this is:\n%s", e.output())
	}
	// And stop does not pretend to have stopped it.
	e.out.Reset()
	err := e.app.cmdStop(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "still answering") {
		t.Fatalf("stop: %v", err)
	}
}

func TestOpenOnThisComputerAndForAPhone(t *testing.T) {
	e := newTestEnv(t)
	e.signInWhenOpened()
	if err := e.app.cmdOpen(bg, nil); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("open with nothing running: %v", err)
	}
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	e.out.Reset()

	// --print gives the sign-in link without opening anything.
	if err := e.app.cmdOpen(bg, []string{"--print"}); err != nil || len(e.opened()) != 0 {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(e.output()); got != e.app.controllerURL()+"/#token="+e.token() {
		t.Fatalf("link = %q", got)
	}
	e.out.Reset()
	if err := e.app.cmdOpen(bg, nil); err != nil {
		t.Fatal(err)
	}
	if o := e.opened(); len(o) != 1 || !strings.Contains(o[0], "#token=") || strings.Contains(e.output(), e.token()) {
		t.Fatalf("opened %v; output:\n%s", o, e.output())
	}

	// --phone turns the network on, sends the browser to sign in, waits, and shows the address and a QR code.
	e.out.Reset()
	if err := e.app.cmdOpen(bg, []string{"--qr"}); err != nil {
		t.Fatalf("%v\n%s", err, e.output())
	}
	out := e.output()
	if !strings.Contains(out, "https://devboard-test.tail1234.ts.net/") || !strings.Contains(out, "scan this") || !strings.Contains(out, "█") {
		t.Fatalf("phone output:\n%s", out)
	}
	if strings.Contains(out, e.token()) {
		t.Fatalf("the token was printed (it is in the QR code, not as text):\n%s", out)
	}
	if o := e.opened(); o[len(o)-1] != e.node.authURL {
		t.Fatalf("the sign-in page was not opened: %v", o)
	}
	if n, _ := mustClient(t, e).network(bg); n.Choice != "on" {
		t.Fatalf("choice = %s", n.Choice)
	}
	// With the network connected, plain `open` mentions the phone address.
	e.out.Reset()
	if err := e.app.cmdOpen(bg, nil); err != nil || !strings.Contains(e.output(), "On your phone: https://devboard-test") {
		t.Fatalf("%v\n%s", err, e.output())
	}
	if err := e.app.cmdOpen(bg, []string{"--bogus"}); err == nil {
		t.Fatal("an unknown flag was accepted")
	}
}

func waitNeedsLogin(t *testing.T, e *testEnv) {
	t.Helper()
	c := mustClient(t, e)
	for i := 0; i < 300; i++ {
		if n, err := c.network(bg); err == nil && n.State == "needs_login" && n.AuthURL != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the network never asked for sign-in")
}

func mustClient(t *testing.T, e *testEnv) *client {
	t.Helper()
	c, err := e.app.client()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUninstallRemovesTheServiceAndKeepsTheData(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	e.out.Reset()
	if err := e.app.cmdUninstall(bg, nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.mgr.Status(bg); st.Installed || st.Running {
		t.Fatalf("service = %+v", st)
	}
	if !strings.Contains(e.output(), "untouched") || !strings.Contains(e.output(), e.dataDir) {
		t.Fatalf("output:\n%s", e.output())
	}
	if _, err := e.app.client(); err != nil {
		t.Fatal(err)
	}
}

func TestLogsShowsTheEndOfTheLog(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdLogs(bg, nil); err == nil || !strings.Contains(err.Error(), "no log yet") {
		t.Fatalf("no log: %v", err)
	}
	writeLog(t, e, "one\ntwo\nthree\nfour\n")
	if err := e.app.cmdLogs(bg, []string{"-n", "2"}); err != nil || strings.TrimSpace(e.output()) != "three\nfour" {
		t.Fatalf("%v: %q", err, e.output())
	}
	if err := e.app.cmdLogs(bg, []string{"-n", "x"}); err == nil {
		t.Fatal("a bad count was accepted")
	}
}

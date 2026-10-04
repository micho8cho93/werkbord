package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/config"
	"devboard/internal/controller"
	"devboard/internal/daemon"
	"devboard/internal/netprivate"
)

var bg = context.Background()

// freePort returns a loopback port nothing is listening on.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// fakeNode is a private network node a test controls: it needs sign-in until the
// test says it has happened.
type fakeNode struct {
	mu       sync.Mutex
	loggedIn bool
	authURL  string
	lns      []net.Listener
}

func (f *fakeNode) signIn() { f.mu.Lock(); f.loggedIn = true; f.mu.Unlock() }

func (f *fakeNode) Start(context.Context) error { return nil }
func (f *fakeNode) Status(context.Context) (netprivate.BackendStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.loggedIn {
		return netprivate.BackendStatus{State: "NeedsLogin", AuthURL: f.authURL}, nil
	}
	return netprivate.BackendStatus{State: "Running", DNSName: "devboard-test.tail1234.ts.net.", HTTPS: true,
		IPs: []netip.Addr{netip.MustParseAddr("100.64.0.9")}, Tailnet: "me@example.com"}, nil
}
func (f *fakeNode) Listen(string, bool) (net.Listener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	f.mu.Lock()
	f.lns = append(f.lns, l)
	f.mu.Unlock()
	return l, err
}
func (f *fakeNode) Close() error { return nil }

// inprocManager is a service manager that runs the controller in this process, so
// that the commands are tested against a real controller without installing
// anything on the machine running the tests.
type inprocManager struct {
	t    *testing.T
	cfg  config.Config
	node *fakeNode

	mu         sync.Mutex
	installed  bool
	spec       daemon.Spec
	ctl        *controller.Controller
	starts     int
	startCheck func() error // makes Start fail while it returns an error, to test rollback
	name       string
}

func (m *inprocManager) Name() string {
	if m.name != "" {
		return m.name
	}
	return "fake service"
}

func (m *inprocManager) Install(_ context.Context, spec daemon.Spec) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installed, m.spec = true, spec
	return nil
}

func (m *inprocManager) Uninstall(ctx context.Context) error {
	_ = m.Stop(ctx)
	m.mu.Lock()
	m.installed = false
	m.mu.Unlock()
	return nil
}

func (m *inprocManager) Start(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.installed {
		return daemon.ErrNotInstalled
	}
	if m.startCheck != nil {
		if err := m.startCheck(); err != nil {
			return err
		}
	}
	if m.ctl != nil {
		return nil // already running: starting it again starts nothing
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	c := controller.New(cfg, slog.New(slog.DiscardHandler), "test-controller")
	c.SetNetworkBackend(m.node)
	if err := c.Start(bg); err != nil {
		return err
	}
	m.ctl = c
	m.starts++
	return nil
}

func (m *inprocManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	c := m.ctl
	m.ctl = nil
	m.mu.Unlock()
	if c != nil {
		return c.Shutdown(ctx)
	}
	return nil
}

func (m *inprocManager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	return m.Start(ctx)
}

func (m *inprocManager) Status(context.Context) (daemon.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return daemon.State{Manager: m.Name(), Installed: m.installed, Running: m.ctl != nil, PID: map[bool]int{true: 4321}[m.ctl != nil]}, nil
}

type testEnv struct {
	t        *testing.T
	app      *app
	mgr      *inprocManager
	node     *fakeNode
	out      *bytes.Buffer
	dataDir  string
	mu       sync.Mutex
	browsed  []string
	browseFn func(url string) error
}

func (e *testEnv) opened() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.browsed...)
}

// newTestEnv isolates the CLI in a temporary data directory on a free port, with
// a fake service manager, a fake browser and a fake private network. Both agents
// are pointed at commands that do not exist unless a test installs one.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	port := freePort(t)
	t.Setenv("DEVBOARD_DATA_DIR", dir)
	t.Setenv("DEVBOARD_ADDR", fmt.Sprintf("127.0.0.1:%d", port))
	t.Setenv("DEVBOARD_TOKEN", "")
	t.Setenv("DEVBOARD_REQUIRE_TOKEN", "")
	t.Setenv("DEVBOARD_NETWORK", "")
	t.Setenv("DEVBOARD_NO_SERVICE", "")
	t.Setenv("DEVBOARD_NO_OPEN", "")
	t.Setenv("DEVBOARD_NO_NETWORK", "")
	t.Setenv("DEVBOARD_NO_START", "")
	t.Setenv("HOME", dir)                        // nothing reads the real home directory
	if err := os.Chmod(dir, 0o700); err != nil { // as the controller creates its data directory
		t.Fatal(err)
	}
	oldVersion := version
	version = "test-controller"
	t.Cleanup(func() { version = oldVersion })
	missing := `{"agents":{"claude-code":{"command":"no-such-claude"},"codex":{"command":"no-such-codex"}},"github":{"command":"no-such-gh"}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(missing), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	e := &testEnv{t: t, dataDir: dir, out: &bytes.Buffer{}, node: &fakeNode{authURL: "https://login.example/a/one-time-link"}}
	e.mgr = &inprocManager{t: t, cfg: cfg, node: e.node}
	e.app = newApp(cfg, e.out, io.Discard)
	e.app.poll = 10 * time.Millisecond
	e.app.stopWait = 500 * time.Millisecond
	e.app.daemon = func(context.Context) daemon.Manager { return e.mgr }
	e.app.installed = func(ctx context.Context) (daemon.Manager, bool) {
		st, _ := e.mgr.Status(ctx)
		return e.mgr, st.Installed
	}
	bin := filepath.Join(dir, "bin", "devboard")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("#!/bin/sh\necho old\n"), 0o755)
	e.app.executable = func() (string, error) { return bin, nil }
	e.app.path = func() string { return "/usr/bin:/bin:/captured/by/setup" }
	e.app.isTerminal = func(io.Writer) bool { return false }
	e.app.openBrowser = func(url string) error {
		e.mu.Lock()
		e.browsed = append(e.browsed, url)
		fn := e.browseFn
		e.mu.Unlock()
		if fn != nil {
			return fn(url)
		}
		return nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		_ = e.mgr.Stop(ctx)
	})
	return e
}

func (e *testEnv) output() string { return e.out.String() }

func (e *testEnv) token() string {
	b, _ := os.ReadFile(filepath.Join(e.dataDir, "token"))
	return strings.TrimSpace(string(b))
}

func (e *testEnv) running() bool {
	_, ok := e.app.healthy(bg)
	return ok
}

// fakeAgent installs a script as an agent's command in config.json and reloads the config.
func (e *testEnv) fakeAgent(id, script string) {
	e.t.Helper()
	path := filepath.Join(e.dataDir, "bin-"+id)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		e.t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(e.dataDir, "config.json"))
	var cfg map[string]any
	_ = json.Unmarshal(raw, &cfg)
	agents := cfg["agents"].(map[string]any)
	agents[id] = map[string]any{"command": path}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(e.dataDir, "config.json"), b, 0o600); err != nil {
		e.t.Fatal(err)
	}
	c, err := config.Load()
	if err != nil {
		e.t.Fatal(err)
	}
	e.app.cfg, e.mgr.cfg = c, c
}

const fakeCodex = "#!/bin/sh\ncase \"$1\" in --version) echo 'codex-cli 7.7.7';; login) echo 'Logged in using ChatGPT';; esac\nexit 0\n"
const fakeClaude = "#!/bin/sh\ncase \"$1\" in --version) echo '9.9.9 (Claude Code)';; auth) echo '{\"loggedIn\": true}';; esac\nexit 0\n"

func TestRunDispatchesTheNewCommands(t *testing.T) {
	newTestEnv(t)
	for _, tc := range []struct{ args []string }{{[]string{"start", "extra"}}, {[]string{"stop", "x"}}, {[]string{"restart", "x"}}, {[]string{"uninstall", "x"}}} {
		var out, errOut bytes.Buffer
		if err := run(tc.args, &out, &errOut); err == nil || !strings.Contains(err.Error(), "usage: devboard "+tc.args[0]) {
			t.Errorf("%v: err = %v", tc.args, err)
		}
	}
	var out, errOut bytes.Buffer
	if err := run([]string{"help"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"setup", "start", "stop", "restart", "status", "open", "doctor", "update", "uninstall", "logs"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage does not mention %s", want)
		}
	}
}

// newGitRepo makes a repository with a commit.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git: %v %s", err, out)
		}
	}
	return dir
}

func writeLog(t *testing.T, e *testEnv, body string) {
	t.Helper()
	path := filepath.Join(e.dataDir, "logs", "controller.log")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

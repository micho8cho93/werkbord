package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var bg = context.Background()

// recorder is an Exec that records commands and answers from a script.
type recorder struct {
	calls  []string
	answer func(call string) (string, error)
}

func (r *recorder) exec(_ context.Context, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if r.answer != nil {
		return r.answer(call)
	}
	return "", nil
}

func (r *recorder) took(prefix string) int {
	n := 0
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func spec(dir string) Spec {
	return Spec{
		Binary: filepath.Join(dir, "bin", "devboard"), Args: []string{"serve"}, DataDir: filepath.Join(dir, "data"),
		LogFile: filepath.Join(dir, "data", "logs", "controller.log"), Path: "/usr/local/bin:/home/me/.nvm/versions/node/v22/bin:/usr/bin",
	}
}

func TestLaunchdAgentDefinition(t *testing.T) {
	home := t.TempDir()
	l := &Launchd{Options: Options{Home: home, UID: 501, Exec: (&recorder{}).exec}}
	if err := l.Install(bg, spec(home)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", "dev.werkbord.controller.plist"))
	if err != nil {
		t.Fatal(err)
	}
	plist := string(b)
	for _, want := range []string{
		"<string>dev.werkbord.controller</string>",
		"<string>" + filepath.Join(home, "bin", "devboard") + "</string>",
		"<string>serve</string>",
		"<key>PATH</key>", "/home/me/.nvm/versions/node/v22/bin", // the agents are found through the captured PATH
		"<key>DEVBOARD_DATA_DIR</key>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>SuccessfulExit</key>\n\t\t<false/>", // restarted after a crash, not after a clean stop
		filepath.Join(home, "data", "logs", "controller.log"),
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
	// The path of a binary with markup in it cannot break the file.
	s := spec(home)
	s.Binary = filepath.Join(home, "a&b<c>", "devboard")
	if err := l.Install(bg, s); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(l.plistPath())
	if !strings.Contains(string(b), "a&amp;b&lt;c&gt;") {
		t.Errorf("plist does not escape the binary path:\n%s", b)
	}
}

func TestLaunchdLifecycle(t *testing.T) {
	home := t.TempDir()
	loaded := false
	r := &recorder{}
	r.answer = func(call string) (string, error) {
		switch {
		case strings.HasPrefix(call, "launchctl print"):
			if !loaded {
				return "Could not find service", errors.New("exit status 113")
			}
			return "dev.werkbord.controller = {\n\tstate = running\n\tpid = 4242\n}", nil
		case strings.HasPrefix(call, "launchctl bootstrap"):
			loaded = true
		case strings.HasPrefix(call, "launchctl bootout"):
			loaded = false
		}
		return "", nil
	}
	l := &Launchd{Options: Options{Home: home, UID: 501, Exec: r.exec}}

	if err := l.Start(bg); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("starting what is not installed: %v", err)
	}
	if st, _ := l.Status(bg); st.Installed || st.Running {
		t.Fatalf("status before install = %+v", st)
	}
	if err := l.Install(bg, spec(home)); err != nil {
		t.Fatal(err)
	}
	if st, _ := l.Status(bg); !st.Installed || st.Running {
		t.Fatalf("installed but not loaded = %+v", st)
	}
	if err := l.Start(bg); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "dev.werkbord.controller.plist")
	if !contains(r.calls, "launchctl bootstrap gui/501 "+plist) {
		t.Fatalf("calls = %v", r.calls)
	}
	if st, _ := l.Status(bg); !st.Installed || !st.Running || st.PID != 4242 || st.Manager != "launchd" {
		t.Fatalf("running = %+v", st)
	}
	// Starting a running service starts nothing new: it is kickstarted, which does nothing to a running job.
	if err := l.Start(bg); err != nil || r.took("launchctl bootstrap") != 1 {
		t.Fatalf("second start: %v, calls %v", err, r.calls)
	}
	if err := l.Restart(bg); err != nil || r.took("launchctl bootout gui/501/dev.werkbord.controller") != 1 || r.took("launchctl bootstrap") != 2 {
		t.Fatalf("restart: %v, calls %v", err, r.calls)
	}
	if err := l.Stop(bg); err != nil || loaded {
		t.Fatalf("stop: %v loaded=%v", err, loaded)
	}
	if err := l.Stop(bg); err != nil { // stopping a stopped service is fine
		t.Fatal(err)
	}
	if err := l.Uninstall(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatal("uninstall left the plist")
	}
}

func contains(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func TestSystemdUnitDefinition(t *testing.T) {
	home := t.TempDir()
	s := &Systemd{Options: Options{Home: home, Exec: (&recorder{}).exec}}
	sp := spec(home)
	sp.Binary = filepath.Join(home, "my bin", "devboard") // a space needs quoting
	unit, err := s.Unit(sp)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ExecStart="` + filepath.Join(home, "my bin", "devboard") + `" serve`,
		"Environment=PATH=/usr/local/bin:/home/me/.nvm/versions/node/v22/bin:/usr/bin",
		"Environment=DEVBOARD_DATA_DIR=" + filepath.Join(home, "data"),
		"Restart=on-failure", "WantedBy=default.target", "TimeoutStopSec=30",
		"StandardOutput=append:" + filepath.Join(home, "data", "logs", "controller.log"),
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "User=") || strings.Contains(unit, "WantedBy=multi-user") {
		t.Errorf("a user service must not name a user or a system target:\n%s", unit)
	}
}

func TestSystemdLifecycle(t *testing.T) {
	home := t.TempDir()
	active := false
	r := &recorder{}
	r.answer = func(call string) (string, error) {
		switch {
		case strings.HasPrefix(call, "systemctl --user show werkbord.service"):
			if active {
				return "ActiveState=active\nSubState=running\nMainPID=777", nil
			}
			return "ActiveState=inactive\nSubState=dead\nMainPID=0", nil
		case strings.HasPrefix(call, "systemctl --user start"), strings.HasPrefix(call, "systemctl --user restart"):
			active = true
		case strings.HasPrefix(call, "systemctl --user stop"):
			active = false
		}
		return "", nil
	}
	t.Setenv("USER", "me")
	s := &Systemd{Options: Options{Home: home, Exec: r.exec}}
	if err := s.Start(bg); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("starting what is not installed: %v", err)
	}
	if err := s.Install(bg, spec(home)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"systemctl --user daemon-reload", "systemctl --user enable werkbord.service", "loginctl enable-linger me"} {
		if !contains(r.calls, want) {
			t.Errorf("missing %q in %v", want, r.calls)
		}
	}
	if st, _ := s.Status(bg); !st.Installed || st.Running {
		t.Fatalf("installed, stopped = %+v", st)
	}
	if err := s.Start(bg); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(bg); !st.Running || st.PID != 777 || st.Manager != "systemd" {
		t.Fatalf("running = %+v", st)
	}
	if err := s.Restart(bg); err != nil || !contains(r.calls, "systemctl --user restart werkbord.service") {
		t.Fatalf("restart: %v %v", err, r.calls)
	}
	if err := s.Stop(bg); err != nil || active {
		t.Fatalf("stop: %v", err)
	}
	if err := s.Uninstall(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.unitPath()); !os.IsNotExist(err) {
		t.Fatal("uninstall left the unit")
	}
	// A failing systemctl surfaces what it said.
	r.answer = func(string) (string, error) { return "Failed to connect to bus", errors.New("exit status 1") }
	if err := s.Install(bg, spec(home)); err == nil || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("a failing systemctl: %v", err)
	}
}

func TestSchtasksLifecycle(t *testing.T) {
	exists, running := false, false
	r := &recorder{}
	r.answer = func(call string) (string, error) {
		switch {
		case strings.HasPrefix(call, "schtasks /Create"):
			exists = true
		case strings.HasPrefix(call, "schtasks /Query"):
			if !exists {
				return "ERROR: The system cannot find the file specified.", errors.New("exit status 1")
			}
			status := "Ready"
			if running {
				status = "Running"
			}
			return "TaskName:    \\Werkbord\nStatus:      " + status, nil
		case strings.HasPrefix(call, "schtasks /Run"):
			running = true
		case strings.HasPrefix(call, "schtasks /End"):
			running = false
		case strings.HasPrefix(call, "schtasks /Delete"):
			exists = false
		}
		return "", nil
	}
	s := &Schtasks{Options: Options{Exec: r.exec}}
	if err := s.Start(bg); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("start before install: %v", err)
	}
	sp := Spec{Binary: `C:\Users\me\bin\devboard.exe`, Args: []string{"serve"}, DataDir: `C:\Users\me\AppData\Roaming\devboard`, LogFile: filepath.Join(t.TempDir(), "logs", "c.log")}
	if err := s.Install(bg, sp); err != nil {
		t.Fatal(err)
	}
	var create string
	for _, c := range r.calls {
		if strings.HasPrefix(c, "schtasks /Create") {
			create = c
		}
	}
	for _, want := range []string{"/SC ONLOGON", "/RL LIMITED", "/TN Werkbord", `devboard.exe" serve`, "DEVBOARD_DATA_DIR=C:\\Users\\me\\AppData\\Roaming\\devboard"} {
		if !strings.Contains(create, want) {
			t.Errorf("create command lacks %q: %s", want, create)
		}
	}
	if err := s.Start(bg); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(bg); !st.Installed || !st.Running {
		t.Fatalf("running = %+v", st)
	}
	if err := s.Restart(bg); err != nil || !running {
		t.Fatalf("restart: %v running=%v", err, running)
	}
	if err := s.Stop(bg); err != nil || running {
		t.Fatalf("stop: %v", err)
	}
	if err := s.Uninstall(bg); err != nil || exists {
		t.Fatalf("uninstall: %v", err)
	}
}

func TestBackgroundProcessIsNeverStartedTwice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	dir := t.TempDir()
	// A controller stand-in that runs until told to stop and records that it was started.
	script := filepath.Join(dir, "fake-devboard")
	startsFile := filepath.Join(dir, "starts")
	body := "#!/bin/sh\necho started >> '" + startsFile + "'\ntrap 'exit 0' TERM\nwhile true; do sleep 0.1; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &Background{Options: Options{DataDir: filepath.Join(dir, "data")}}
	if err := b.Start(bg); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("start with nothing recorded: %v", err)
	}
	sp := Spec{Binary: script, Args: []string{"serve"}, DataDir: filepath.Join(dir, "data"), LogFile: filepath.Join(dir, "data", "logs", "c.log"), Path: os.Getenv("PATH")}
	if err := b.Install(bg, sp); err != nil {
		t.Fatal(err)
	}
	if st, _ := b.Status(bg); st.Running || st.Installed {
		t.Fatalf("before start = %+v: a background process is never 'installed'", st)
	}
	if err := b.Start(bg); err != nil {
		t.Fatal(err)
	}
	st, _ := b.Status(bg)
	if !st.Running || st.PID == 0 || st.Manager != "background process" {
		t.Fatalf("after start = %+v", st)
	}
	first := st.PID
	for i := 0; i < 3; i++ {
		if err := b.Start(bg); err != nil {
			t.Fatal(err)
		}
	}
	if st, _ := b.Status(bg); st.PID != first {
		t.Fatalf("a second controller was started: pid %d then %d", first, st.PID)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, _ := os.ReadFile(startsFile); strings.Count(string(raw), "started") >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if raw, _ := os.ReadFile(startsFile); strings.Count(string(raw), "started") != 1 {
		t.Fatalf("started %d times", strings.Count(string(raw), "started"))
	}

	ctx, cancel := context.WithTimeout(bg, 20*time.Second)
	defer cancel()
	if err := b.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := b.Status(bg); !st.Running || st.PID == first {
		t.Fatalf("after restart = %+v (was %d)", st, first)
	}
	if err := b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := b.Status(bg); st.Running {
		t.Fatalf("after stop = %+v", st)
	}
	if err := b.Stop(ctx); err != nil { // stopping a stopped one is fine
		t.Fatal(err)
	}
}

func TestDetectChoosesByPlatform(t *testing.T) {
	ok := func(context.Context, string, ...string) (string, error) { return "", nil }
	fail := func(context.Context, string, ...string) (string, error) { return "", errors.New("no") }
	for _, tc := range []struct {
		goos string
		exec Exec
		want string
	}{
		{"darwin", ok, "launchd"},
		{"windows", ok, "task scheduler"},
		{"linux", ok, "systemd"},
		{"linux", fail, "background process"}, // no systemd user session: a container, WSL1
		{"freebsd", ok, "background process"},
	} {
		if got := Detect(bg, Options{GOOS: tc.goos, Exec: tc.exec, Home: t.TempDir(), DataDir: t.TempDir()}).Name(); got != tc.want {
			t.Errorf("%s: manager = %s, want %s", tc.goos, got, tc.want)
		}
	}
	// A background process is never reported as an installed service.
	if _, installed := Installed(bg, Options{GOOS: "freebsd", Exec: ok, Home: t.TempDir(), DataDir: t.TempDir()}); installed {
		t.Error("the background manager reported itself installed")
	}
}

func TestServicePATHKeepsTheUsersPATHFirstAndAddsWhereAgentsLive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}
	got := ServicePATH("/home/me/.nvm/bin:/usr/bin:/home/me/.nvm/bin", "/home/me")
	parts := strings.Split(got, ":")
	if parts[0] != "/home/me/.nvm/bin" || parts[1] != "/usr/bin" {
		t.Fatalf("PATH = %s: the user's own entries come first", got)
	}
	seen := map[string]bool{}
	for _, p := range parts {
		if seen[p] {
			t.Errorf("%s appears twice in %s", p, got)
		}
		seen[p] = true
	}
	for _, want := range []string{"/home/me/.local/bin", "/opt/homebrew/bin", "/usr/local/bin"} {
		if !seen[want] {
			t.Errorf("PATH lacks %s: %s", want, got)
		}
	}
}

func TestLogsAreRotated(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "c.log")
	if err := os.WriteFile(log, make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	rotateLog(log, 1000)
	if _, err := os.Stat(log); err != nil {
		t.Fatal("a small log was rotated")
	}
	rotateLog(log, 50)
	if _, err := os.Stat(log); err == nil {
		t.Fatal("a big log was not rotated")
	}
	if _, err := os.Stat(log + ".1"); err != nil {
		t.Fatal("the old log was lost")
	}
}

// A computer set up before the rename keeps its service, under the old label, until
// setup moves it; a new one gets the new label. A legacy runner unit is never taken
// for the controller (on Linux they shared one unit name).
func TestAServiceInstalledBeforeTheRenameIsStillFound(t *testing.T) {
	ok := func(context.Context, string, ...string) (string, error) { return "", nil }

	home := t.TempDir()
	agents := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agents, 0o700); err != nil {
		t.Fatal(err)
	}
	if m := Detect(bg, Options{GOOS: "darwin", Exec: ok, Home: home, UID: 501}); IsLegacy(m) {
		t.Fatal("a computer with no service got the old label")
	}
	if err := os.WriteFile(filepath.Join(agents, "dev.devboard.controller.plist"), []byte("<string>serve</string>"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := Detect(bg, Options{GOOS: "darwin", Exec: ok, Home: home, UID: 501})
	if !IsLegacy(m) {
		t.Fatal("the service installed before the rename was not found")
	}
	if err := os.WriteFile(filepath.Join(agents, "dev.werkbord.controller.plist"), []byte("<string>serve</string>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m := Detect(bg, Options{GOOS: "darwin", Exec: ok, Home: home, UID: 501}); IsLegacy(m) {
		t.Fatal("the new service lost to the old one")
	}

	linux := t.TempDir()
	units := filepath.Join(linux, ".config", "systemd", "user")
	if err := os.MkdirAll(units, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(units, "devboard.service"), []byte("ExecStart=/home/me/bin/devboard runner serve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m := Detect(bg, Options{GOOS: "linux", Exec: ok, Home: linux}); IsLegacy(m) {
		t.Fatal("a legacy runner unit was taken for the controller")
	}
	if m := Detect(bg, Options{GOOS: "linux", Exec: ok, Home: linux, Label: RunnerLabel}); !IsLegacy(m) {
		t.Fatal("the legacy runner unit was not found for the runner")
	}
}

// A program the Finder started has none of the environment `werkbord setup` ran in. The service
// definition is where the install records which executable it runs and where its data is.
func TestDescribeReadsWhatSetupInstalled(t *testing.T) {
	home := t.TempDir()
	l := &Launchd{Options: Options{Home: home, UID: 501, Exec: (&recorder{}).exec}}
	sp := spec(home)
	sp.Binary = filepath.Join(home, "My Tools & Co", "bin", "werkbord") // XML-escaped in the plist
	sp.DataDir = filepath.Join(home, "Library", "Application Support", "my <data>")
	if err := l.Install(bg, sp); err != nil {
		t.Fatal(err)
	}
	def, ok := Describe(bg, Options{Home: home, UID: 501, GOOS: "darwin", Exec: (&recorder{}).exec})
	if !ok || def.Binary != sp.Binary || def.DataDir != sp.DataDir {
		t.Fatalf("Describe = %+v, %v; want %q and %q", def, ok, sp.Binary, sp.DataDir)
	}
}

func TestDescribeIsEmptyWhenNothingIsInstalledOrWhenThereIsNoLaunchd(t *testing.T) {
	home := t.TempDir()
	if def, ok := Describe(bg, Options{Home: home, UID: 501, GOOS: "darwin", Exec: (&recorder{}).exec}); ok {
		t.Fatalf("Describe found %+v in an empty home", def)
	}
	// A definition with no data directory in it leaves the data directory to the default.
	if def, ok := parsePlist(`<array><string>x</string></array><key>ProgramArguments</key><array><string>/a/werkbord</string><string>serve</string></array>`); !ok || def.Binary != "/a/werkbord" || def.DataDir != "" {
		t.Fatalf("parsePlist = %+v, %v", def, ok)
	}
	// On a system without launchd nothing is read, and nothing is run.
	rec := &recorder{}
	if _, ok := Describe(bg, Options{Home: home, GOOS: "linux", Exec: rec.exec}); ok {
		t.Fatal("Describe claimed a definition on linux")
	}
}

// An install from before the rename (dev.devboard.controller) is the install there is.
func TestDescribeFindsAServiceInstalledUnderTheOldLabel(t *testing.T) {
	home := t.TempDir()
	old := &Launchd{Options: Options{Home: home, UID: 501, Label: "dev.devboard.controller", Exec: (&recorder{}).exec}}
	sp := spec(home)
	if err := old.Install(bg, sp); err != nil {
		t.Fatal(err)
	}
	def, ok := Describe(bg, Options{Home: home, UID: 501, GOOS: "darwin", Exec: (&recorder{}).exec})
	if !ok || def.Binary != sp.Binary || def.DataDir != sp.DataDir {
		t.Fatalf("Describe = %+v, %v", def, ok)
	}
}

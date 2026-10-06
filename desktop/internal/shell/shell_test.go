package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"devboard/internal/config"
	"devboard/internal/launcher"
	"devboard/internal/update"
)

// fakeLauncher is the launcher, answering from a script and recording what it is asked.
type fakeLauncher struct {
	mu      sync.Mutex
	cfg     config.Config
	conn    launcher.Connection
	connErr error
	status  update.Status
	applyOK bool
	applyEr error
	applied int
	steps   []launcher.Step
}

func (f *fakeLauncher) Connect(_ context.Context, report func(launcher.Step)) (launcher.Connection, error) {
	if report != nil {
		report(launcher.Step{Phase: launcher.PhaseLooking, Text: "Looking for Werkbord…"})
	}
	return f.conn, f.connErr
}
func (f *fakeLauncher) Config(context.Context) (config.Config, error)    { return f.cfg, nil }
func (f *fakeLauncher) UpdateStatus(context.Context, bool) update.Status { return f.status }
func (f *fakeLauncher) ApplyUpdate(context.Context) (string, error) {
	f.mu.Lock()
	f.applied++
	f.mu.Unlock()
	return "Downloading v1.2.0…\nWerkbord v1.2.0 is running again.\n", f.applyEr
}
func (f *fakeLauncher) DiagnosticsText(context.Context) string { return "Controller: answering\n" }

// fakeUI is the window system: it answers dialogs from a script and records the rest.
type fakeUI struct {
	mu      sync.Mutex
	answers []string // what each dialog is answered with, in order
	asked   []Dialog
	opened  []string
	shown   []string
	files   []string
	reloads int
	events  []string
}

func (u *fakeUI) Ask(d Dialog) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.asked = append(u.asked, d)
	if len(u.answers) == 0 {
		return d.Cancel
	}
	a := u.answers[0]
	u.answers = u.answers[1:]
	return a
}
func (u *fakeUI) OpenURL(url string)   { u.mu.Lock(); u.opened = append(u.opened, url); u.mu.Unlock() }
func (u *fakeUI) Reveal(path string)   { u.mu.Lock(); u.shown = append(u.shown, path); u.mu.Unlock() }
func (u *fakeUI) OpenFile(path string) { u.mu.Lock(); u.files = append(u.files, path); u.mu.Unlock() }
func (u *fakeUI) Reload()              { u.mu.Lock(); u.reloads++; u.mu.Unlock() }
func (u *fakeUI) Emit(event string, _ any) {
	u.mu.Lock()
	u.events = append(u.events, event)
	u.mu.Unlock()
}

func newShell(t *testing.T, l *fakeLauncher, ui *fakeUI) *Shell {
	t.Helper()
	if l.cfg.DataDir == "" {
		l.cfg = config.Default()
		l.cfg.DataDir = t.TempDir()
	}
	return New(Options{Launcher: l, UI: ui, Version: "v1.1.0", Platform: "darwin", AppLog: "/logs/desktop.log"})
}

// ---- what a page may ask ----

// Wails makes every exported method of the struct it binds callable from a page the controller
// served. This list is that surface: a new method is a decision, made here, not an accident.
func TestThePageFacingSurfaceIsExactlyWhatIsListed(t *testing.T) {
	want := []string{"CheckForUpdates", "Connect", "Diagnostics", "Info", "OpenExternal", "OpenInBrowser", "OpenLogs", "Reload", "RequestUpdate", "ShowDiagnostics", "UpdateStatus"}
	var got []string
	typ := reflect.TypeOf(&Shell{})
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Shell's exported methods are the app's page-callable API.\n got %v\nwant %v\nIf a method is meant to be callable from a web page, add it here and say why it is safe in its doc comment.", got, want)
	}
	// What the web app actually calls (web/src/lib/desktop*.ts, UpdateBanner.svelte) must be there.
	for _, m := range []string{"Info", "OpenExternal", "RequestUpdate"} {
		if _, ok := typ.MethodByName(m); !ok {
			t.Errorf("the web app calls main.App.%s, which does not exist", m)
		}
	}
}

func TestInfoSaysWhichAppThisIs(t *testing.T) {
	s := newShell(t, &fakeLauncher{}, &fakeUI{})
	if got := s.Info(); got != (AppInfo{Version: "v1.1.0", Platform: "darwin"}) {
		t.Fatalf("Info = %+v", got)
	}
}

func TestOnlyWebAddressesAreOpenedInTheBrowser(t *testing.T) {
	ui := &fakeUI{}
	s := newShell(t, &fakeLauncher{}, ui)
	for _, ok := range []string{"https://github.com/o/r/pull/1", "http://127.0.0.1:7421/", "https://login.tailscale.com/admin/machines?x=1#y"} {
		if err := s.OpenExternal(ok); err != nil {
			t.Errorf("OpenExternal(%q) = %v", ok, err)
		}
	}
	bad := []string{"", "file:///etc/passwd", "javascript:alert(1)", "mailto:a@b.c", "ssh://host", "werkbord://update", "x-apple.systempreferences:", "https://", "//evil.example", "data:text/html,hi", "https://" + strings.Repeat("a", 5000)}
	for _, raw := range bad {
		if err := s.OpenExternal(raw); err == nil {
			t.Errorf("OpenExternal(%q) was accepted", raw)
		}
	}
	if len(ui.opened) != 3 {
		t.Fatalf("opened %v", ui.opened)
	}
}

func TestConnectReportsProgressAndReturnsTheSignInLink(t *testing.T) {
	l := &fakeLauncher{conn: launcher.Connection{URL: "http://127.0.0.1:7420", SignInURL: "http://127.0.0.1:7420/#token=secret", Version: "v1.1.0"}}
	ui := &fakeUI{}
	got, err := newShell(t, l, ui).Connect()
	if err != nil || got.SignInURL != l.conn.SignInURL || got.Version != "v1.1.0" {
		t.Fatalf("Connect = %+v, %v", got, err)
	}
	if len(ui.events) != 1 || ui.events[0] != "progress" {
		t.Fatalf("events = %v", ui.events)
	}
}

func TestAConnectionThatFailsIsReturnedAsItIs(t *testing.T) {
	l := &fakeLauncher{connErr: errors.New("another program is already using 127.0.0.1:7420")}
	_, err := newShell(t, l, &fakeUI{}).Connect()
	if err == nil || !strings.Contains(err.Error(), "already using") {
		t.Fatalf("err = %v", err)
	}
}

func TestDiagnosticsNameTheAppLogToo(t *testing.T) {
	got := newShell(t, &fakeLauncher{}, &fakeUI{}).Diagnostics()
	if !strings.Contains(got, "Controller: answering") || !strings.Contains(got, "/logs/desktop.log") {
		t.Fatalf("diagnostics = %q", got)
	}
}

func TestOpenLogsRevealsTheControllersLog(t *testing.T) {
	l := &fakeLauncher{}
	ui := &fakeUI{}
	s := newShell(t, l, ui)
	// No log yet: its folder, or the data directory, is shown instead of nothing.
	s.OpenLogs()
	if len(ui.shown) != 1 || ui.shown[0] != l.cfg.DataDir {
		t.Fatalf("shown = %v", ui.shown)
	}
	if err := os.MkdirAll(filepath.Dir(l.cfg.LogPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.cfg.LogPath(), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.OpenLogs()
	if ui.shown[1] != l.cfg.LogPath() {
		t.Fatalf("shown = %v", ui.shown)
	}
}

func TestShowDiagnosticsWritesAPrivateFileAndOpensIt(t *testing.T) {
	ui := &fakeUI{}
	dir := t.TempDir()
	s := New(Options{Launcher: &fakeLauncher{}, UI: ui, AppLog: filepath.Join(dir, "logs", "desktop.log")})
	s.ShowDiagnostics()
	path := filepath.Join(dir, "logs", "diagnostics.txt")
	if len(ui.files) != 1 || ui.files[0] != path {
		t.Fatalf("opened %v", ui.files)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "Controller: answering") {
		t.Fatalf("%q, %v", b, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("the report is %v: it names paths and log lines and is for its owner only", fi.Mode().Perm())
	}
}

func TestOpenInBrowserOpensTheSameControllerSignedIn(t *testing.T) {
	l := &fakeLauncher{}
	l.cfg = config.Default()
	l.cfg.DataDir = t.TempDir()
	l.cfg.Addr = "127.0.0.1:7421"
	tok, err := l.cfg.ResolveToken(true)
	if err != nil {
		t.Fatal(err)
	}
	ui := &fakeUI{}
	newShell(t, l, ui).OpenInBrowser()
	if len(ui.opened) != 1 || ui.opened[0] != "http://127.0.0.1:7421/#token="+tok {
		t.Fatalf("opened %v", ui.opened)
	}
}

// ---- updating ----

func available() update.Status {
	return update.Status{Current: "v1.1.0", Latest: "v1.2.0", Available: true, Release: true}
}

func TestUpdateNeedsTheirYesInANativeDialogAndThenRunsTheOrdinaryUpdate(t *testing.T) {
	l := &fakeLauncher{status: available()}
	ui := &fakeUI{answers: []string{"Update now"}}
	res := newShell(t, l, ui).RequestUpdate()
	if !res.OK || l.applied != 1 || ui.reloads != 1 {
		t.Fatalf("result %+v, applied %d, reloads %d", res, l.applied, ui.reloads)
	}
	if len(ui.asked) != 1 || ui.asked[0].Kind != Question || !strings.Contains(ui.asked[0].Title, "1.2.0") ||
		!strings.Contains(ui.asked[0].Message, "backed up") || !strings.Contains(ui.asked[0].Message, "agents are working") {
		t.Fatalf("the confirmation does not say what the update does: %+v", ui.asked)
	}
	if res.Message != "Werkbord v1.2.0 is running again." {
		t.Fatalf("message = %q", res.Message)
	}
}

func TestLaterChangesNothing(t *testing.T) {
	l := &fakeLauncher{status: available()}
	ui := &fakeUI{answers: []string{"Later"}}
	res := newShell(t, l, ui).RequestUpdate()
	if res.OK || !res.Declined || l.applied != 0 || ui.reloads != 0 {
		t.Fatalf("result %+v, applied %d, reloads %d", res, l.applied, ui.reloads)
	}
	// Dismissing the dialog some other way is not a yes either.
	ui = &fakeUI{answers: []string{""}}
	if res := newShell(t, l, ui).RequestUpdate(); res.OK || l.applied != 0 {
		t.Fatalf("a dismissed dialog installed: %+v", res)
	}
}

func TestAnUpdateThatIsRefusedIsReportedAndNothingReloads(t *testing.T) {
	l := &fakeLauncher{status: available(), applyEr: errors.New("run r_1 is active on this computer; finish or stop it in Werkbord first")}
	ui := &fakeUI{answers: []string{"Update now"}}
	res := newShell(t, l, ui).RequestUpdate()
	if res.OK || !strings.Contains(res.Message, "is active on this computer") || ui.reloads != 0 {
		t.Fatalf("result %+v reloads %d", res, ui.reloads)
	}
	// From the page, the page shows why; no second dialog on top of it.
	if len(ui.asked) != 1 {
		t.Fatalf("dialogs: %+v", ui.asked)
	}
	// From the menu there is no page, so a dialog says why.
	ui = &fakeUI{answers: []string{"Update now", "OK"}}
	newShell(t, l, ui).CheckForUpdates()
	if len(ui.asked) != 2 || ui.asked[1].Kind != Failure || !strings.Contains(ui.asked[1].Message, "is active on this computer") || !strings.Contains(ui.asked[1].Message, "put back") {
		t.Fatalf("dialogs: %+v", ui.asked)
	}
}

func TestThereIsNothingToInstallWhenThereIsNothingToInstall(t *testing.T) {
	for name, tc := range map[string]struct {
		st   update.Status
		want string
	}{
		"up to date":        {update.Status{Current: "v1.2.0", Latest: "v1.2.0", Release: true}, "newest release"},
		"built from source": {update.Status{Current: "v1.1.0-3-gabc1234", Release: false}, "built from source"},
		"offline":           {update.Status{Current: "v1.1.0", Release: true, Error: "dial tcp: no route to host"}, "no route to host"},
		"turned off":        {update.Status{Current: "v1.1.0", Release: true, Disabled: true}, "turned off"},
	} {
		t.Run(name, func(t *testing.T) {
			l := &fakeLauncher{status: tc.st}
			ui := &fakeUI{}
			s := newShell(t, l, ui)
			s.CheckForUpdates()
			if l.applied != 0 || len(ui.asked) != 1 || !strings.Contains(ui.asked[0].Message, tc.want) {
				t.Fatalf("applied %d, dialogs %+v", l.applied, ui.asked)
			}
			// The page asking says it in its own words and shows no dialog.
			ui2 := &fakeUI{}
			res := newShell(t, l, ui2).RequestUpdate()
			if len(ui2.asked) != 0 || res.Message == "" || l.applied != 0 {
				t.Fatalf("page: %+v, dialogs %+v", res, ui2.asked)
			}
		})
	}
}

func TestTwoUpdatesAreNeverRunAtOnce(t *testing.T) {
	l := &fakeLauncher{status: available()}
	block := make(chan string)
	ui := &blockingUI{fakeUI: &fakeUI{}, answer: block, asking: make(chan struct{})}
	s := newShell(t, l, ui.fakeUI)
	s.o.UI = ui
	done := make(chan UpdateResult)
	go func() { done <- s.RequestUpdate() }()
	<-ui.asking // the first is waiting for its answer
	if res := s.RequestUpdate(); res.OK || !strings.Contains(res.Message, "already running") {
		t.Fatalf("a second update started: %+v", res)
	}
	block <- "Later"
	<-done
}

// blockingUI holds a dialog open until the test answers it.
type blockingUI struct {
	*fakeUI
	answer chan string
	asking chan struct{}
}

func (b *blockingUI) Ask(Dialog) string {
	b.asking <- struct{}{}
	return <-b.answer
}

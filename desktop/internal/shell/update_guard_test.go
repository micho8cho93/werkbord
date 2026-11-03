package shell

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"werkbord/internal/device"
	"werkbord/internal/update"
)

func TestBusyAgentsAndHostsBlockAppUpdateAndRelaunch(t *testing.T) {
	for _, reason := range []string{"active agents", "Workspace Host maintenance", "unsafe cluster transition"} {
		t.Run(reason, func(t *testing.T) {
			l := &fakeLauncher{status: update.Status{Release: true, Current: "v1.8.0", Latest: "v1.9.0", Available: true}}
			u := &fakeUpdater{active: true, probe: Probe{Found: true, Version: "v1.9.0"}}
			busy := true
			s := New(Options{Launcher: l, UI: &fakeUI{}, Version: "v1.8.0", Updater: u, UpdateGuard: func(context.Context) error {
				if busy {
					return errors.New(reason)
				}
				return nil
			}})
			if res := s.RequestUpdate(); res.OK || res.Message != reason || u.shown != 0 || l.applied != 0 {
				t.Fatalf("unsafe update: %+v", res)
			}
			if !u.busy() {
				t.Fatal("relaunch bypassed current safety check")
			}
			busy = false
			if u.busy() {
				t.Fatal("safe relaunch remained blocked")
			}
		})
	}
}

func TestOpenTerminalsAreNamedAndCanBeStoppedFromTheUpdateDialog(t *testing.T) {
	open := &device.TerminalsRunning{Terminals: []device.RunningTerminal{{ID: "t1", ProjectID: "p1", Agent: "codex", Repository: "test-repo"}}}
	status := update.Status{Release: true, Current: "v1.8.0", Latest: "v1.9.0", Available: true}
	for _, tc := range []struct {
		name        string
		answers     []string
		guard       func(calls int) error
		stopErr     error
		wantStopped int
		wantApplied int
		wantMessage string
		declined    bool
	}{
		{name: "stop and update", answers: []string{"Stop and update", "Update now"}, guard: func(n int) error {
			if n == 1 {
				return open
			}
			return nil
		}, wantStopped: 1, wantApplied: 1},
		{name: "not now", answers: []string{"Not now"}, guard: func(int) error { return open }, declined: true},
		{name: "stopping fails", answers: []string{"Stop and update"}, guard: func(int) error { return open }, stopErr: errors.New("it would not stop"), wantStopped: 1, wantMessage: "it would not stop"},
		{name: "something else is in the way", answers: []string{"Stop and update"}, guard: func(n int) error {
			if n == 1 {
				return open
			}
			return errors.New("update deferred: coding agents are active on this computer")
		}, wantStopped: 1, wantMessage: "update deferred: coding agents are active on this computer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &fakeLauncher{status: status, stopEr: tc.stopErr}
			ui := &fakeUI{answers: tc.answers}
			calls := 0
			s := New(Options{Launcher: l, UI: ui, Version: "v1.8.0", UpdateGuard: func(context.Context) error {
				calls++
				return tc.guard(calls)
			}})
			res := s.RequestUpdate()
			if len(l.stopped) != tc.wantStopped || l.applied != tc.wantApplied || res.Declined != tc.declined || res.Message != tc.wantMessage && tc.wantApplied == 0 {
				t.Fatalf("stopped %d applied %d result %+v", len(l.stopped), l.applied, res)
			}
			if len(ui.asked) == 0 || ui.asked[0].Title != "Terminals are still open" || !strings.Contains(ui.asked[0].Message, "Codex in test-repo") {
				t.Fatalf("the dialog did not name the terminal: %+v", ui.asked)
			}
			if tc.wantStopped == 1 && (len(l.stopped[0]) != 1 || l.stopped[0][0].ID != "t1") {
				t.Fatalf("stopped the wrong terminals: %+v", l.stopped)
			}
		})
	}
}

func TestAnotherRefusalIsNotOfferedAsStoppable(t *testing.T) {
	l := &fakeLauncher{status: update.Status{Release: true, Current: "v1.8.0", Latest: "v1.9.0", Available: true}}
	ui := &fakeUI{}
	s := New(Options{Launcher: l, UI: ui, Version: "v1.8.0", UpdateGuard: func(context.Context) error { return errors.New("update deferred: device operations are pending") }})
	if res := s.RequestUpdate(); res.OK || res.Message != "update deferred: device operations are pending" || len(l.stopped) != 0 {
		t.Fatalf("%+v", res)
	}
}

// lockedAlerter is an Alerter that a background update may write to while the test reads it.
type lockedAlerter struct {
	mu    sync.Mutex
	shown []shownAlert
}

func (a *lockedAlerter) Status() string { return "granted" }
func (a *lockedAlerter) Show(title, body string, urgent, force bool) bool {
	a.mu.Lock()
	a.shown = append(a.shown, shownAlert{title, body, urgent, force})
	a.mu.Unlock()
	return true
}
func (a *lockedAlerter) OpenSettings() {}
func (a *lockedAlerter) alerts() []shownAlert {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]shownAlert(nil), a.shown...)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestUpdateWaitsForTheTerminalsAndThenFinishesByItself(t *testing.T) {
	old := readyPoll
	readyPoll = 5 * time.Millisecond
	t.Cleanup(func() { readyPoll = old })

	open := &device.TerminalsRunning{Terminals: []device.RunningTerminal{{ID: "t1", ProjectID: "p1", Agent: "codex", Repository: "test-repo"}}}
	for _, tc := range []struct {
		name       string
		adoptErr   error
		wantAlert  string
		wantReload int
	}{
		{name: "it finishes", wantAlert: "Werkbord updated", wantReload: 1},
		{name: "it cannot", adoptErr: errors.New("the new program did not start"), wantAlert: "Werkbord was not updated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &fakeLauncher{pending: true, installed: "v1.8.0", bundled: "v1.9.0", adoptEr: tc.adoptErr,
				status: update.Status{Release: true, Current: "v1.8.0", Latest: "v1.9.0", Available: true}}
			ui := &fakeUI{answers: []string{"Update when they finish"}}
			alerts := &lockedAlerter{}
			var mu sync.Mutex
			stillOpen := true
			guards := 0
			s := New(Options{Launcher: l, UI: ui, Alerter: alerts, Version: "v1.9.0", UpdateGuard: func(context.Context) error {
				mu.Lock()
				defer mu.Unlock()
				guards++
				if stillOpen {
					return open
				}
				return nil
			}})

			res := s.RequestUpdate()
			if !res.Scheduled || res.OK || res.Declined || res.Message == "" {
				t.Fatalf("choosing to wait should say it is scheduled: %+v", res)
			}
			// While a terminal is open nothing is installed, however many times it looks.
			time.Sleep(60 * time.Millisecond)
			l.mu.Lock()
			early := l.adopted
			l.mu.Unlock()
			if early != 0 {
				t.Fatal("installed while a terminal was still open")
			}
			mu.Lock()
			stillOpen = false
			mu.Unlock()
			waitFor(t, "the update", func() bool { return len(alerts.alerts()) > 0 })
			got := alerts.alerts()
			if len(got) != 1 || got[0].title != tc.wantAlert || !got[0].forced {
				t.Fatalf("alerts: %+v", got)
			}
			l.mu.Lock()
			adopted := l.adopted
			l.mu.Unlock()
			ui.mu.Lock()
			reloads := ui.reloads
			ui.mu.Unlock()
			if adopted != 1 || reloads != tc.wantReload {
				t.Fatalf("adopted %d times, reloaded %d times", adopted, reloads)
			}
			// It is done with, and a second request can wait again.
			waitFor(t, "the watcher to end", func() bool { return !s.waiting.Load() })
		})
	}
}

func TestOnlyOneUpdateWaitsAtATime(t *testing.T) {
	old := readyPoll
	readyPoll = time.Hour // it never looks: only how many watchers there are matters
	t.Cleanup(func() { readyPoll = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := New(Options{Launcher: &fakeLauncher{}, UI: &fakeUI{}, Ctx: ctx, UpdateGuard: func(context.Context) error { return nil }})
	s.updateWhenReady()
	if !s.waiting.Load() {
		t.Fatal("no watcher started")
	}
	s.updateWhenReady() // would panic or double up if a second one started and ended the first's state
	cancel()
	waitFor(t, "the watcher to end with the app", func() bool { return !s.waiting.Load() })
}

func TestAnUpdateThatNeedsMoreThanTheAppStillAsksItsUsualQuestion(t *testing.T) {
	old := readyPoll
	readyPoll = 5 * time.Millisecond
	t.Cleanup(func() { readyPoll = old })
	open := &device.TerminalsRunning{Terminals: []device.RunningTerminal{{ID: "t1", ProjectID: "p1", Agent: "codex", Repository: "test-repo"}}}
	// Nothing newer is carried by the app: this is a download, which is the usual update with its usual question.
	l := &fakeLauncher{status: update.Status{Release: true, Current: "v1.8.0", Latest: "v1.9.0", Available: true}}
	ui := &fakeUI{answers: []string{"Update when they finish", "Update now"}}
	var mu sync.Mutex
	stillOpen := true
	s := New(Options{Launcher: l, UI: ui, Version: "v1.8.0", UpdateGuard: func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if stillOpen {
			return open
		}
		return nil
	}})
	if res := s.RequestUpdate(); !res.Scheduled {
		t.Fatalf("%+v", res)
	}
	mu.Lock()
	stillOpen = false
	mu.Unlock()
	waitFor(t, "the usual update", func() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.applied == 1 })
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if len(ui.asked) != 2 || !strings.HasPrefix(ui.asked[1].Title, "Update to Werkbord") {
		t.Fatalf("the usual question was skipped: %+v", ui.asked)
	}
}

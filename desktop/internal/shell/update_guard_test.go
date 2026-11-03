package shell

import (
	"context"
	"errors"
	"strings"
	"testing"
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

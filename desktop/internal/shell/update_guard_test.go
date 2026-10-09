package shell

import (
	"context"
	"devboard/internal/update"
	"errors"
	"testing"
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

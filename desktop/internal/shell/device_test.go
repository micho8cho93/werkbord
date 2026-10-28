package shell

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"werkbord/internal/device"
	"werkbord/internal/launcher"
)

type deviceLauncher struct {
	fakeLauncher
	dmu      sync.Mutex
	status   launcher.DeviceStatus
	setups   []device.SetupRequest
	locals   int
	bindings [][2]string
}

func (d *deviceLauncher) DeviceStatus(context.Context) (launcher.DeviceStatus, error) {
	d.dmu.Lock()
	defer d.dmu.Unlock()
	return d.status, nil
}
func (d *deviceLauncher) StartDeviceSetup(_ context.Context, r device.SetupRequest) (launcher.DeviceJob, error) {
	d.dmu.Lock()
	defer d.dmu.Unlock()
	d.setups = append(d.setups, r)
	d.status.Job = launcher.DeviceJob{Action: "connect", Phase: "done"}
	return launcher.DeviceJob{Running: true, Action: "connect", Phase: "working"}, nil
}
func (d *deviceLauncher) UseLocalController(context.Context) (launcher.DeviceJob, error) {
	d.dmu.Lock()
	defer d.dmu.Unlock()
	d.locals++
	d.status.Job = launcher.DeviceJob{Action: "local", Phase: "done"}
	return launcher.DeviceJob{Running: true, Action: "local", Phase: "working"}, nil
}
func (d *deviceLauncher) BindDeviceRepository(_ context.Context, project, path string) (launcher.DeviceJob, error) {
	d.dmu.Lock()
	defer d.dmu.Unlock()
	d.bindings = append(d.bindings, [2]string{project, path})
	return launcher.DeviceJob{Action: "bind", Phase: "done"}, nil
}

type folderUI struct {
	*fakeUI
	dir string
}

func (u *folderUI) ChooseDirectory() (string, error) { return u.dir, nil }

func deviceRig(t *testing.T, ui UI, answers ...string) (*rig, *deviceLauncher) {
	t.Helper()
	r := newRig(t, answers...)
	l := &deviceLauncher{}
	opt := r.Shell.o
	opt.Launcher = l
	if ui != nil {
		opt.UI = ui
	}
	r.Shell = New(opt)
	return r, l
}

const pairing = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestOnlyAPersonalPageMayAskAboutDevices(t *testing.T) {
	r, l := deviceRig(t, nil, "Connect")
	for _, m := range []string{"DeviceStatus", "DeviceConnect", "DeviceLocal", "DeviceBind"} {
		if _, err := r.Relay("team:main", m, nil); err == nil {
			t.Errorf("a Team page asked for %s", m)
		}
	}
	if len(l.setups) != 0 || l.locals != 0 || len(l.bindings) != 0 || len(r.ui.asked) != 0 {
		t.Fatal("a refused call still did something")
	}
}

func TestConnectingNeedsThePersonsConfirmationAndKeepsCodesOutOfTheDialog(t *testing.T) {
	req := map[string]any{"controller": "https://home.tailnet.ts.net:8443", "name": "Laptop", "code": pairing, "runner": false}
	r, l := deviceRig(t, nil, "Cancel")
	if _, err := r.Relay("personal", "DeviceConnect", args(req)); err == nil || len(l.setups) != 0 {
		t.Fatalf("a declined dialog still connected: %v %v", err, l.setups)
	}
	r, l = deviceRig(t, nil, "Connect")
	if _, err := r.Relay("personal", "DeviceConnect", args(req)); err != nil {
		t.Fatal(err)
	}
	if len(l.setups) != 1 || l.setups[0].Code != pairing || l.setups[0].Controller != "https://home.tailnet.ts.net:8443" {
		t.Fatalf("unexpected request %+v", l.setups)
	}
	for _, d := range r.ui.asked {
		if strings.Contains(d.Message+d.Title, pairing) {
			t.Fatal("the pairing code was shown in a dialog")
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.ui.mu.Lock()
		n := r.ui.reloads
		r.ui.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the window was not reloaded onto the new controller")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAnInvalidConnectRequestIsRefusedBeforeAnyDialog(t *testing.T) {
	r, l := deviceRig(t, nil, "Connect")
	for _, bad := range []map[string]any{
		{"controller": "file:///etc/passwd", "name": "x", "code": pairing},
		{"controller": "https://home.tailnet.ts.net:8443", "name": "x", "code": "short"},
		{"controller": "https://home.tailnet.ts.net:8443", "name": "x\nrm", "code": pairing},
		{"controller": "https://home.tailnet.ts.net:8443", "name": "x", "code": pairing, "runner": false, "runnerCode": "abc"},
	} {
		if _, err := r.Relay("personal", "DeviceConnect", args(bad)); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if len(l.setups) != 0 || len(r.ui.asked) != 0 {
		t.Fatalf("invalid requests reached a dialog or the launcher: %v", r.ui.asked)
	}
}

func TestSwitchingBackAsksFirst(t *testing.T) {
	r, l := deviceRig(t, nil, "Cancel")
	if _, err := r.Relay("personal", "DeviceLocal", nil); err == nil || l.locals != 0 {
		t.Fatal("declined switch ran")
	}
	r, l = deviceRig(t, nil, "Switch back")
	if _, err := r.Relay("personal", "DeviceLocal", nil); err != nil || l.locals != 1 {
		t.Fatalf("%v %d", err, l.locals)
	}
}

func TestTheFolderForARepositoryComesFromThePersonNotThePage(t *testing.T) {
	ui := &folderUI{fakeUI: &fakeUI{answers: []string{"Use folder"}}, dir: "/Users/me/code/app"}
	r, l := deviceRig(t, ui)
	// A path supplied by the page is not a field of the request, so it cannot be used.
	if _, err := r.Relay("personal", "DeviceBind", args(map[string]any{"project": "prj_1", "path": "/etc"})); err != nil {
		t.Fatal(err)
	}
	if len(l.bindings) != 1 || l.bindings[0] != [2]string{"prj_1", "/Users/me/code/app"} {
		t.Fatalf("unexpected binding %v", l.bindings)
	}
	r, l = deviceRig(t, &folderUI{fakeUI: &fakeUI{answers: []string{"Use folder"}}, dir: "relative/path"})
	if _, err := r.Relay("personal", "DeviceBind", args(map[string]any{"project": "prj_1"})); err == nil || len(l.bindings) != 0 {
		t.Fatal("a relative folder was bound")
	}
	r, l = deviceRig(t, &folderUI{fakeUI: &fakeUI{answers: []string{"Cancel"}}, dir: "/Users/me/code/app"})
	if _, err := r.Relay("personal", "DeviceBind", args(map[string]any{"project": "prj_1"})); err == nil || len(l.bindings) != 0 {
		t.Fatal("a declined binding ran")
	}
}

func TestStatusIsReadOnlyAndNeedsNoDialog(t *testing.T) {
	r, l := deviceRig(t, nil)
	l.status = launcher.DeviceStatus{Mode: "remote", Controller: "https://home.tailnet.ts.net:8443"}
	v, err := r.Relay("personal", "DeviceStatus", nil)
	if err != nil || v.(launcher.DeviceStatus).Mode != "remote" || len(r.ui.asked) != 0 {
		t.Fatalf("%v %v %v", v, err, r.ui.asked)
	}
	if _, err := r.Relay("personal", "DeviceStatus", args("extra")); err == nil {
		t.Fatal("status accepted an argument")
	}
}

func TestABuildWithoutDeviceSetupSaysSo(t *testing.T) {
	r := newRig(t)
	if _, err := r.Relay("personal", "DeviceStatus", nil); err == nil {
		t.Fatal("a launcher without device setup answered")
	}
}

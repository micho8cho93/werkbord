package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/clientaccess"
	"werkbord/internal/daemon"
	"werkbord/internal/device"
)

const setupCode = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func setupJSON(t *testing.T, v any) *strings.Reader {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReader(string(raw))
}

func TestConfigureRejectsMalformedRequestsWithoutChangingAnything(t *testing.T) {
	e := newTestEnv(t)
	m := &deviceManager{}
	e.app.runnerDaemon = func(context.Context) daemon.Manager { return m }
	good := map[string]any{"controller": "http://home.test", "name": "Laptop", "code": setupCode}
	for name, input := range map[string]*strings.Reader{
		"not json":       strings.NewReader("{"),
		"trailing data":  strings.NewReader(`{"controller":"http://home.test","code":"` + setupCode + `"} {}`),
		"unknown field":  setupJSON(t, map[string]any{"controller": "http://home.test", "code": setupCode, "command": "rm"}),
		"loopback":       setupJSON(t, map[string]any{"controller": "http://127.0.0.1:1", "code": setupCode}),
		"bad code":       setupJSON(t, map[string]any{"controller": "http://home.test", "code": "zz"}),
		"no code at all": setupJSON(t, map[string]any{"controller": "http://home.test"}),
		"runner no code": setupJSON(t, map[string]any{"controller": "http://home.test", "code": setupCode, "runner": true}),
		"runner other":   setupJSON(t, map[string]any{"controller": "http://home.test", "code": setupCode, "runner": true, "runnerCode": "not-a-runner-code"}),
	} {
		if err := e.app.configureDevice(bg, nil, input); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := e.app.configureDevice(bg, []string{"extra"}, setupJSON(t, good)); err == nil {
		t.Error("a positional argument was accepted")
	}
	state, err := device.Load(e.dataDir)
	if err != nil || state.Remote() || m.starts+m.stops != 0 {
		t.Fatalf("a refused request changed state: %+v %v %+v", state, err, m)
	}
	if _, err := os.Stat(device.CredentialPath(e.dataDir)); !os.IsNotExist(err) {
		t.Fatal("a credential was saved by a refused request")
	}
}

func TestConfigureUsesASavedCredentialAndNeverPrintsIt(t *testing.T) {
	e := newTestEnv(t)
	spec, err := e.app.spec()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.mgr.Install(bg, spec); err != nil {
		t.Fatal(err)
	}
	if err = e.mgr.Start(bg); err != nil {
		t.Fatal(err)
	}
	e.app.describe = func(context.Context) (daemon.Definition, bool) {
		return daemon.Definition{Binary: spec.Binary, DataDir: e.dataDir}, true
	}
	remoteMgr := &deviceManager{}
	e.app.runnerDaemon = func(context.Context) daemon.Manager { return remoteMgr }
	credential := device.Credential{Controller: "http://home.test", ID: "cl_laptop", DeviceID: strings.Repeat("a", 64), Token: clientaccess.TokenPrefix + strings.Repeat("b", 64)}
	if err = clientaccess.WriteJSON(device.CredentialPath(e.dataDir), credential); err != nil {
		t.Fatal(err)
	}
	// Without a code, a previously paired controller is reused: this is how a retry avoids consuming a one-time code twice.
	if err = e.app.configureDevice(bg, nil, setupJSON(t, map[string]any{"controller": "http://home.test", "name": "Laptop"})); err != nil {
		t.Fatal(err)
	}
	state, err := device.Load(e.dataDir)
	if err != nil || !state.Remote() || state.Controller != "http://home.test" || e.running() {
		t.Fatalf("%+v %v running=%v", state, err, e.running())
	}
	if strings.Contains(e.output(), credential.Token) || strings.Contains(e.output(), setupCode) {
		t.Fatal("a credential was printed")
	}
	// Another controller cannot be selected without first switching back.
	err = e.app.configureDevice(bg, nil, setupJSON(t, map[string]any{"controller": "http://other.test", "code": setupCode}))
	if err == nil || !strings.Contains(err.Error(), "switch back") {
		t.Fatalf("selecting a second controller while connected: %v", err)
	}
}

func TestConfigureWithoutACredentialNeedsADesktopCode(t *testing.T) {
	e := newTestEnv(t)
	err := e.app.configureDevice(bg, nil, setupJSON(t, map[string]any{"controller": "http://home.test", "name": "Laptop"}))
	if err == nil || !strings.Contains(err.Error(), "pairing code") {
		t.Fatalf("%v", err)
	}
}

func TestBindDeviceRepositoryTakesOnlyAProjectAndAPath(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.bindDeviceRepository(bg, nil, strings.NewReader(`{"Project":"prj_1","Path":"/x","Extra":1}`)); err == nil {
		t.Error("unknown field accepted")
	}
	if err := e.app.bindDeviceRepository(bg, nil, strings.NewReader(`{"Project":"prj_1","Path":"/x"} {}`)); err == nil {
		t.Error("trailing data accepted")
	}
	if err := e.app.bindDeviceRepository(bg, []string{"prj_1"}, strings.NewReader(`{}`)); err == nil {
		t.Error("positional argument accepted")
	}
	// Binding refuses while the runner has unsettled work, as every other change to a runner does.
	if err := clientaccess.WriteJSON(filepath.Join(runnerDir(e.app.cfg), "runs", "run_busy.json"), map[string]string{"phase": "active"}); err != nil {
		t.Fatal(err)
	}
	if err := e.app.bindDeviceRepository(bg, nil, strings.NewReader(`{"Project":"prj_1","Path":"`+t.TempDir()+`"}`)); err == nil || !strings.Contains(err.Error(), "finish or resolve") {
		t.Fatalf("%v", err)
	}
}

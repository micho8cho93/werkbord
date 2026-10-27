package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/clientaccess"
	"werkbord/internal/daemon"
	"werkbord/internal/device"
)

type deviceManager struct {
	spec               daemon.Spec
	installed, running bool
	starts, stops      int
}

func (m *deviceManager) Name() string { return "device test" }
func (m *deviceManager) Install(_ context.Context, s daemon.Spec) error {
	m.spec = s
	m.installed = true
	return nil
}
func (m *deviceManager) Uninstall(ctx context.Context) error { m.installed = false; return m.Stop(ctx) }
func (m *deviceManager) Start(context.Context) error         { m.running = true; m.starts++; return nil }
func (m *deviceManager) Stop(context.Context) error          { m.running = false; m.stops++; return nil }
func (m *deviceManager) Restart(ctx context.Context) error   { _ = m.Stop(ctx); return m.Start(ctx) }
func (m *deviceManager) Status(context.Context) (daemon.State, error) {
	return daemon.State{Installed: m.installed, Running: m.running}, nil
}
func TestDeviceSwitchPreservesDatabaseAndRestoresController(t *testing.T) {
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
	original := e.token()
	e.app.describe = func(context.Context) (daemon.Definition, bool) {
		return daemon.Definition{Binary: spec.Binary, DataDir: e.dataDir}, true
	}
	remoteMgr := &deviceManager{}
	e.app.runnerDaemon = func(context.Context) daemon.Manager { return remoteMgr }
	credential := device.Credential{Controller: "http://home.test", ID: "cl_laptop", DeviceID: strings.Repeat("a", 64), Token: clientaccess.TokenPrefix + strings.Repeat("b", 64)}
	if err = clientaccess.WriteJSON(device.CredentialPath(e.dataDir), credential); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(e.dataDir, "keep.txt")
	if err = os.WriteFile(sentinel, []byte("existing local data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.app.cmdDevice(bg, []string{"connect", "http://home.test"}); err != nil {
		t.Fatal(err)
	}
	state, err := device.Load(e.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != "remote" || !state.RestoreService || e.running() || e.mgr.installed || !remoteMgr.running {
		t.Fatalf("unsafe transition: %+v local=%v device=%+v", state, e.running(), remoteMgr)
	}
	if strings.Join(remoteMgr.spec.Args, " ") != "device serve" {
		t.Fatal("service did not adopt shared device runtime")
	}
	if _, err = os.Stat(e.app.cfg.DBPath()); err != nil {
		t.Fatal("local database removed", err)
	}
	raw, _ := os.ReadFile(sentinel)
	if string(raw) != "existing local data" {
		t.Fatal("local files changed")
	}
	if strings.Contains(e.output(), credential.Token) {
		t.Fatal("client credential printed")
	}
	if err = e.app.cmdDevice(bg, []string{"local"}); err != nil {
		t.Fatal(err)
	}
	state, err = device.Load(e.dataDir)
	if err != nil || state.Remote() || !e.running() || !e.mgr.installed || remoteMgr.installed {
		t.Fatalf("restore: %+v %v", state, err)
	}
	if e.token() != original {
		t.Fatal("local controller token changed")
	}
}
func TestDeviceSwitchRejectsUnsettledRunnerBeforeStoppingAnything(t *testing.T) {
	e := newTestEnv(t)
	m := &deviceManager{running: true, installed: true}
	e.app.runnerDaemon = func(context.Context) daemon.Manager { return m }
	if err := clientaccess.WriteJSON(filepath.Join(runnerDir(e.app.cfg), "runs", "run_busy.json"), map[string]string{"phase": "active"}); err != nil {
		t.Fatal(err)
	}
	if err := e.app.cmdDevice(bg, []string{"connect", "http://home.test"}); err == nil || !strings.Contains(err.Error(), "finish or resolve") {
		t.Fatal(err)
	}
	if m.stops != 0 || !m.running {
		t.Fatal("active runner stopped")
	}
	state, err := device.Load(e.dataDir)
	if err != nil || state.Remote() {
		t.Fatal("failed switch changed mode")
	}
}
func TestClientOnlyAndRunnerCapabilitiesRemainExplicit(t *testing.T) {
	e := newTestEnv(t)
	m := &deviceManager{}
	e.app.runnerDaemon = func(context.Context) daemon.Manager { return m }
	if err := device.Save(e.dataDir, device.State{Mode: "remote", Controller: "http://home.test"}); err != nil {
		t.Fatal(err)
	}
	if err := e.app.cmdStart(bg, nil); err != nil {
		t.Fatal(err)
	}
	if e.mgr.starts != 0 || m.starts != 1 {
		t.Fatal("remote start launched controller")
	}
	if err := e.app.checkInterruption(bg, false); err == nil {
		t.Fatal("updater could operate on remote workspace")
	}
	if err := e.app.cmdDevice(bg, []string{"connect", "http://home.test", "--runner"}); err == nil {
		t.Fatal("runner capability enabled without independent pairing")
	}
}

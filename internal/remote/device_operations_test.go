//go:build darwin || linux

package remote

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"werkbord/internal/api"
	"werkbord/internal/clientaccess"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/service"
)

func operationQueue(t *testing.T, e *workerEnv) *service.DeviceOperations {
	t.Helper()
	root, err := filepath.EvalSymlinks(e.worker.Bindings[e.project.ID])
	if err != nil {
		t.Fatal(err)
	}
	e.worker.Bindings[e.project.ID] = root
	d := &service.DeviceOperations{Deps: e.svc.Deps, Runners: e.svc, ActorAllowed: func(id string) bool { return id == "desktop" }}
	e.svc.Operations = d
	return d
}
func completeOperation(t *testing.T, e *workerEnv, d *service.DeviceOperations, actor string, r devicewire.Request) *devicewire.Record {
	t.Helper()
	r.RequestID = domain.NewID("request")
	r.RunnerID = e.runner.ID
	r.ProjectID = e.project.ID
	item, err := d.Submit(testCtx, actor, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		e.worker.mu.Lock()
		defer e.worker.mu.Unlock()
		_, ok := e.worker.opResults[item.Operation.ID]
		return ok
	})
	if err = e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	got, err := d.Get(testCtx, actor, item.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func TestGitOperationsUseSignedHeartbeatWhileAgentRemainsActive(t *testing.T) {
	e := workerFixture(t)
	d := operationQueue(t, e)
	run := e.start()
	r := completeOperation(t, e, d, "desktop", devicewire.Request{Kind: "git.status"})
	if r.Result.State != "done" || r.Result.Git == nil {
		t.Fatalf("status %+v", r)
	}
	r = completeOperation(t, e, d, "desktop", devicewire.Request{Kind: "git.prepare", Action: "commit", Message: "unsafe during run", Files: []string{"test.txt"}})
	if r.Result.State != "failed" {
		t.Fatalf("write allowed during active execution %+v", r)
	}
	current, err := e.runs.Get(testCtx, run.ID)
	if err != nil || current.State.Terminal() {
		t.Fatalf("agent interrupted %+v %v", current, err)
	}
}
func TestNativeTerminalAcrossRunnerGrantsAndOfflineReconnect(t *testing.T) {
	e := workerFixture(t)
	e.fake.Info = domain.Agent{ID: "codex", Installed: true, Available: true, SignIn: domain.SignedIn}
	command := filepath.Join(e.worker.Dir, "native-fixture")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf 'READY\\n'\nwhile IFS= read -r line; do printf 'REPLY:%s\\n' \"$line\"; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := clientaccess.WriteJSON(filepath.Join(e.worker.Dir, "terminal-permissions.json"), map[string]domain.AgentSelectionPreference{"codex": {DirectCLI: true}}); err != nil {
		t.Fatal(err)
	}
	e.worker.Operations.Terminals.Close()
	e.worker.TerminalCommands = map[string]string{"codex": command}
	if err := e.worker.initDeviceOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.worker.Operations.Terminals.Close)
	d := operationQueue(t, e)
	created := completeOperation(t, e, d, "owner", devicewire.Request{Kind: "terminal.create", AgentID: "codex", Cols: 80, Rows: 24})
	if created.Result.State != "done" || created.Result.Terminal == nil {
		t.Fatalf("create %+v", created)
	}
	session := created.Result.Terminal
	if !strings.HasPrefix(session.ID, e.runner.ID+"_") {
		t.Fatal("unscoped terminal ID")
	}
	clientStore, err := clientaccess.Open(filepath.Join(t.TempDir(), "clients.json"))
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := clientStore.NewCode()
	entry, token, err := clientStore.Enroll(code, "Laptop", "desktop-device-123456")
	if err != nil {
		t.Fatal(err)
	}
	clientHandler := api.New(api.Options{Store: e.db, Distributed: e.svc, DeviceOperations: d, Clients: clientStore, ClientListener: true, AuthRequired: true}).Handler()
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/device/terminals", "", 200},
		{"POST", "/api/projects/" + e.project.ID + "/terminals", `{}`, 404},
		{"PUT", "/api/projects/" + e.project.ID + "/terminals/" + session.ID + "/clients/" + entry.ID, `{}`, 404},
		{"POST", "/api/device/operations", `{"kind":"terminal.stop"}`, 403},
	} {
		r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		clientHandler.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s %s: %d %s", test.method, test.path, w.Code, w.Body.String())
		}
		if test.method == http.MethodGet && strings.TrimSpace(w.Body.String()) != "[]" {
			t.Fatal("ungranted terminal leaked")
		}
	}
	request := devicewire.Request{Kind: "terminal.output", RunnerID: e.runner.ID, ProjectID: e.project.ID, SessionID: session.ID, RequestID: domain.NewID("request")}
	if _, err := d.Submit(testCtx, "desktop", request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("ungranted terminal accessible %v", err)
	}
	if err := d.GrantTerminal(testCtx, e.runner.ID, e.project.ID, session.ID, "desktop", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		out, _ := e.worker.Operations.Terminals.OutputNow(testCtx, e.project.ID, session.ID, 0)
		return strings.Contains(string(out.Data), "READY")
	})
	// No Tick while offline: the PTY remains alive after the old lease expires.
	e.clock.Add(120000)
	if list := e.worker.Operations.Terminals.List(); len(list) != 1 || list[0].State != "running" {
		t.Fatalf("offline stopped terminal %+v", list)
	}
	if err := e.worker.Tick(testCtx); err != nil {
		t.Fatal(err)
	}
	out := completeOperation(t, e, d, "desktop", devicewire.Request{Kind: "terminal.output", SessionID: session.ID})
	if out.Result.State != "done" || !strings.Contains(string(out.Result.Output.Data), "READY") {
		t.Fatalf("output %+v", out)
	}
	input := devicewire.Request{Kind: "terminal.input", SessionID: session.ID, Client: "stable_client_123456", Sequence: 1, Data: "hello\n", Rendered: out.Result.Output.Next}
	sent := completeOperation(t, e, d, "desktop", input)
	if sent.Result.State != "done" {
		t.Fatalf("input %+v", sent)
	}
	// A lost response repeats the original ID, not the PTY write.
	if sent.Operation.Request.Data != "" {
		t.Fatal("completed input retained terminal bytes in the controller")
	}
	replay := sent.Operation.Request
	replay.Data = input.Data
	same, err := d.Submit(testCtx, "desktop", replay)
	if err != nil || same.Result.State != "done" {
		t.Fatalf("input replay %+v %v", same, err)
	}
	waitFor(t, func() bool {
		o, _ := e.worker.Operations.Terminals.OutputNow(testCtx, e.project.ID, session.ID, out.Result.Output.Next)
		return strings.Contains(string(o.Data), "REPLY:hello")
	})
	if err := d.GrantTerminal(testCtx, e.runner.ID, e.project.ID, session.ID, "desktop", false); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Get(testCtx, "desktop", sent.Operation.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("revoked terminal result readable %v", err)
	}
	if err = e.worker.Operations.Terminals.Stop(e.project.ID, session.ID); err != nil {
		t.Fatal(err)
	}
}

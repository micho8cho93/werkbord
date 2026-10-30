//go:build darwin || linux

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"werkbord/internal/agent/fake"
	"werkbord/internal/domain"
	"werkbord/internal/service"
	"werkbord/internal/terminal"
)

type sessionFixture struct {
	url, token, projectID, taskID, runID, device string
	m                                            *terminal.Manager
	handler                                      http.Handler
}

// sessionServer is a controller with one repository, one task that has a run Salie launched, and native terminals.
func sessionServer(t *testing.T) *sessionFixture {
	t.Helper()
	f := &sessionFixture{token: "owner-token"}
	ts := newTestServer(t, func(o *Options) {
		ctx := context.Background()
		p, e := o.Projects.Register(ctx, gitRepo(t), "App")
		if e != nil {
			t.Fatal(e)
		}
		f.projectID = p.ID
		if e = o.Agents.Register(&fake.Adapter{Name: "codex", Info: domain.Agent{ID: "codex", Installed: true, Available: true, SignIn: domain.SignedIn}}); e != nil {
			t.Fatal(e)
		}
		if _, e = o.Settings.SetSelection(ctx, domain.SelectionSettings{Agents: map[string]domain.AgentSelectionPreference{"codex": {DirectCLI: true, Billing: "auto"}}}); e != nil {
			t.Fatal(e)
		}
		local, e := o.Settings.RegisterRunner(ctx)
		if e != nil {
			t.Fatal(e)
		}
		f.device = local.ID
		f.m, e = terminal.New(terminal.Options{Projects: o.Projects, Settings: o.Settings, Agents: o.Agents, Commands: map[string]string{"codex": "/bin/cat"}, Device: local.ID})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(f.m.Close)
		o.Terminals = f.m
		task, e := o.Tasks.Create(ctx, p.ID, "Fix the login", "details")
		if e != nil {
			t.Fatal(e)
		}
		f.taskID = task.ID
		run, e := o.Runs.Create(ctx, service.NewRun{TaskID: task.ID, AgentID: "codex", Prompt: "fix it", Origin: domain.SalieOrigin("ast_conv")})
		if e != nil {
			t.Fatal(e)
		}
		f.runID = run.ID
		o.AuthRequired = true
		o.Token = f.token
		f.handler = New(*o).Handler()
	})
	f.url = ts.URL
	return f
}

func (f *sessionFixture) list(t *testing.T, query string) agentSessionList {
	t.Helper()
	status, b := call(t, "GET", f.url+"/api/agent-sessions"+query, f.token, "")
	if status != 200 {
		t.Fatalf("list %s: %d %s", query, status, b)
	}
	var out agentSessionList
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *sessionFixture) launch(t *testing.T, body string) (int, terminal.Metadata) {
	t.Helper()
	status, b := call(t, "POST", f.url+"/api/projects/"+f.projectID+"/terminals", f.token, body)
	var m terminal.Metadata
	if status == 201 {
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
	}
	return status, m
}

func byKey(s []domain.AgentSession) map[string]domain.AgentSession {
	out := map[string]domain.AgentSession{}
	for _, x := range s {
		out[x.Key] = x
	}
	return out
}

func TestEachExecutionHasOneIdentityAcrossRunsAndTerminals(t *testing.T) {
	f := sessionServer(t)
	linkedStatus, linked := f.launch(t, `{"agentId":"codex","device":"`+f.device+`","cols":80,"rows":24,"taskId":"`+f.taskID+`"}`)
	standaloneStatus, standalone := f.launch(t, `{"agentId":"codex","device":"`+f.device+`","cols":80,"rows":24}`)
	if linkedStatus != 201 || standaloneStatus != 201 {
		t.Fatalf("launch: %d %d", linkedStatus, standaloneStatus)
	}
	if linked.TaskID != f.taskID || standalone.TaskID != "" {
		t.Fatalf("association: %+v %+v", linked, standalone)
	}

	list := f.list(t, "")
	if !list.Terminals || len(list.Sessions) != 3 {
		t.Fatalf("sessions = %+v", list)
	}
	sessions := byKey(list.Sessions)
	if len(sessions) != 3 {
		t.Fatalf("a key was listed twice: %+v", list.Sessions)
	}
	run, ok := sessions["run:"+f.runID]
	if !ok || run.TaskID != f.taskID || run.LaunchedBy != "ast_conv" || run.Title != "Fix the login" || !run.Capabilities.Feed || run.Capabilities.Terminal {
		t.Fatalf("run session = %+v", run)
	}
	term, ok := sessions["terminal:"+linked.ID]
	if !ok || term.TaskID != f.taskID || term.Title != "Fix the login" || !term.Capabilities.Terminal || term.Capabilities.Feed || !term.Live {
		t.Fatalf("task terminal = %+v", term)
	}
	solo, ok := sessions["terminal:"+standalone.ID]
	if !ok || solo.TaskID != "" || solo.Title != "App" {
		t.Fatalf("standalone terminal = %+v", solo)
	}

	// Asking again, or from any place, names the same executions: nothing is created by looking.
	again := f.list(t, "")
	if len(again.Sessions) != 3 {
		t.Fatalf("listing created sessions: %+v", again.Sessions)
	}
	for k := range byKey(again.Sessions) {
		if _, ok := sessions[k]; !ok {
			t.Fatalf("a new identity appeared: %s", k)
		}
	}

	// A task's page asks for its own: the run and the terminal linked to it, not the standalone one.
	forTask := byKey(f.list(t, "?task="+f.taskID).Sessions)
	if len(forTask) != 2 || forTask["run:"+f.runID].Key == "" || forTask["terminal:"+linked.ID].Key == "" {
		t.Fatalf("task sessions = %+v", forTask)
	}
	if got := f.list(t, "?project=prj_other").Sessions; len(got) != 0 {
		t.Fatalf("another project: %+v", got)
	}
}

func TestATerminalIsLinkedAndUnlinkedWithoutTouchingItsProcess(t *testing.T) {
	f := sessionServer(t)
	_, s := f.launch(t, `{"agentId":"codex","device":"`+f.device+`","cols":80,"rows":24}`)
	path := f.url + "/api/projects/" + f.projectID + "/terminals/" + s.ID + "/task"

	if status, b := call(t, "PUT", path, f.token, `{"taskId":"tsk_missing"}`); status != 404 {
		t.Fatalf("linking to a task that is not in the project: %d %s", status, b)
	}
	if status, b := call(t, "PUT", path, f.token, `{"taskId":"`+f.taskID+`"}`); status != 200 {
		t.Fatalf("link: %d %s", status, b)
	}
	if got := byKey(f.list(t, "").Sessions)["terminal:"+s.ID]; got.TaskID != f.taskID || got.State != "running" {
		t.Fatalf("linked: %+v", got)
	}
	if status, b := call(t, "PUT", path, f.token, `{"taskId":""}`); status != 200 {
		t.Fatalf("unlink: %d %s", status, b)
	}
	if got := byKey(f.list(t, "").Sessions)["terminal:"+s.ID]; got.TaskID != "" || got.State != "running" {
		t.Fatalf("unlinked: %+v", got)
	}
	if status, _ := call(t, "PUT", f.url+"/api/projects/"+f.projectID+"/terminals/missing/task", f.token, `{"taskId":""}`); status != 404 {
		t.Fatalf("an unknown terminal: %d", status)
	}
}

func TestALaunchForATaskOutsideTheProjectStartsNoProcess(t *testing.T) {
	f := sessionServer(t)
	before := len(f.m.List())
	status, _ := f.launch(t, `{"agentId":"codex","device":"`+f.device+`","cols":80,"rows":24,"taskId":"tsk_elsewhere"}`)
	if status != 404 {
		t.Fatalf("status = %d", status)
	}
	if got := len(f.m.List()); got != before {
		t.Fatalf("a terminal was started for a task that does not exist: %d -> %d", before, got)
	}
}

func TestTerminalsAreListedOnlyToTheLocalOwnerButRunsAreNot(t *testing.T) {
	f := sessionServer(t)
	_, s := f.launch(t, `{"agentId":"codex","device":"`+f.device+`","cols":80,"rows":24}`)
	req := httptest.NewRequest("GET", "http://localhost/api/agent-sessions", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("Authorization", "Bearer "+f.token)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	var out agentSessionList
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Terminals || strings.Contains(w.Body.String(), s.ID) {
		t.Fatalf("a connection that is not the local owner was shown a terminal: %s", w.Body.String())
	}
	if _, ok := byKey(out.Sessions)["run:"+f.runID]; !ok {
		t.Fatalf("runs are still listed: %+v", out.Sessions)
	}
	unauth := httptest.NewRequest("GET", "http://localhost/api/agent-sessions", nil)
	unauth.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, unauth)
	if w.Code != 401 && w.Code != 403 {
		t.Fatalf("without a token: %d", w.Code)
	}
}

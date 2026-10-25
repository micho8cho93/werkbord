//go:build darwin || linux

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/agent/fake"
	"devboard/internal/domain"
	"devboard/internal/localaccess"
	"devboard/internal/phoneaccess"
	"devboard/internal/terminal"
)

func TestTerminalOwnerAuthorizationAndClosedCommandSurface(t *testing.T) {
	var m *terminal.Manager
	var projectID, programToken, phoneToken, phoneID string
	ts := newTestServer(t, func(o *Options) {
		ctx := context.Background()
		p, e := o.Projects.Register(ctx, gitRepo(t), "Native")
		if e != nil {
			t.Fatal(e)
		}
		projectID = p.ID
		if e = o.Agents.Register(&fake.Adapter{Name: "codex", Info: domain.Agent{ID: "codex", Installed: true, Available: true, SignIn: domain.SignedIn}}); e != nil {
			t.Fatal(e)
		}
		_, e = o.Settings.SetSelection(ctx, domain.SelectionSettings{Agents: map[string]domain.AgentSelectionPreference{"codex": {DirectCLI: true, Billing: "auto"}}})
		if e != nil {
			t.Fatal(e)
		}
		local, e := o.Settings.RegisterRunner(ctx)
		if e != nil {
			t.Fatal(e)
		}
		m, e = terminal.New(terminal.Options{Projects: o.Projects, Settings: o.Settings, Agents: o.Agents, Commands: map[string]string{"codex": "/bin/cat"}, Device: local.ID})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(m.Close)
		o.Terminals = m
		o.LocalAccess, e = localaccess.Open(filepath.Join(t.TempDir(), "access.json"))
		if e != nil {
			t.Fatal(e)
		}
		_, programToken, e = o.LocalAccess.Create("Program")
		if e != nil {
			t.Fatal(e)
		}
		o.Phones, e = phoneaccess.Open(filepath.Join(t.TempDir(), "phones.json"))
		if e != nil {
			t.Fatal(e)
		}
		code, _, e := o.Phones.NewCode()
		if e != nil {
			t.Fatal(e)
		}
		entry, token, e := o.Phones.Enroll(code, "Phone")
		phoneID = entry.ID
		if e != nil {
			t.Fatal(e)
		}
		phoneToken = token
		o.AuthRequired = true
		o.Token = "terminal-owner"
	})
	// Even an owner token cannot attach through a non-loopback connection.
	handler := New(Options{Terminals: m, AuthRequired: true, Token: "owner"}).Handler()
	for _, in := range []struct {
		remote string
		want   int
	}{{"127.0.0.1:1234", 200}, {"192.0.2.1:1234", 403}} {
		req := httptest.NewRequest("GET", "http://localhost/api/terminals", nil)
		req.RemoteAddr = in.remote
		req.Header.Set("Authorization", "Bearer owner")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != in.want {
			t.Fatalf("terminal attachment from %s: %d", in.remote, w.Code)
		}
	}

	noAuth := New(Options{Terminals: m}).Handler()
	request := httptest.NewRequest("GET", "http://localhost/api/terminals", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	noAuth.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatalf("unauthenticated terminal listener: %d", response.Code)
	}

	base := "/api/projects/" + projectID + "/terminals"
	for _, in := range []string{
		`{"agentId":"/bin/sh","device":"local-device","cols":80,"rows":24}`,
		`{"agentId":"codex","device":"remote-device","cols":80,"rows":24}`,
		`{"agentId":"codex","device":"local-device","cols":80,"rows":24,"command":"sh"}`,
		`{"agentId":"codex","device":"local-device","cols":80,"rows":24,"args":["exec"]}`,
		`{"agentId":"codex","device":"local-device","cols":80,"rows":24,"cwd":"/tmp"}`,
	} {
		status, b := call(t, "POST", ts.URL+base, "terminal-owner", strings.ReplaceAll(in, "local-device", m.Device()))
		if status != 400 && status != 403 {
			t.Fatalf("accepted launch: %d %s", status, b)
		}
	}
	status, b := call(t, "POST", ts.URL+base, "terminal-owner", `{"agentId":"codex","device":"`+m.Device()+`","cols":80,"rows":24}`)
	if status != 201 {
		t.Fatalf("launch: %d %s", status, b)
	}
	var session terminal.Metadata
	if e := json.Unmarshal(b, &session); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "process") || strings.Contains(string(b), "/bin/cat") {
		t.Fatal("private process configuration exposed")
	}
	path := base + "/" + session.ID
	routes := []struct{ method, path, body string }{{"GET", "/api/terminals", ""}, {"POST", base, `{}`}, {"GET", path + "/output?after=0", ""}, {"POST", path + "/input", `{"data":"hi\n"}`}, {"POST", path + "/resize", `{"cols":90,"rows":25}`}, {"POST", path + "/stop", `{}`}}
	for _, route := range routes {
		for _, token := range []string{"", "wrong", programToken, phoneToken} {
			status, b := call(t, route.method, ts.URL+route.path, token, route.body)
			if token == phoneToken && route.path == "/api/terminals" {
				if status != 200 || string(b) != "[]\n" {
					t.Fatalf("ungranted list: %d %s", status, b)
				}
				continue
			}
			if status != 401 && status != 403 {
				t.Fatalf("%s %s token %q: %d %s", route.method, route.path, token, status, b)
			}
		}
	}
	if code, _ := call(t, "POST", ts.URL+"/api/projects/other/terminals/"+session.ID+"/input", "terminal-owner", `{"data":"attack"}`); code != 404 {
		t.Fatalf("cross project input: %d", code)
	}
	if code, _ := call(t, "GET", ts.URL+"/api/projects/other/terminals/"+session.ID+"/output?after=0", "terminal-owner", ""); code != 404 {
		t.Fatalf("cross project output: %d", code)
	}

	// Owner explicitly enables one existing session for one enrolled phone.
	grant := `{"phoneId":"` + phoneID + `","enabled":true}`
	if code, b := call(t, "PUT", ts.URL+path+"/phone", phoneToken, grant); code != 403 {
		t.Fatalf("phone self grant: %d %s", code, b)
	}
	if code, b := call(t, "PUT", ts.URL+path+"/phone", "terminal-owner", grant); code != 200 {
		t.Fatalf("owner grant: %d %s", code, b)
	}
	if code, b := call(t, "GET", ts.URL+"/api/terminals", phoneToken, ""); code != 200 || !strings.Contains(string(b), session.ID) {
		t.Fatalf("granted list: %d %s", code, b)
	}
	for _, in := range []struct {
		body string
		want int
	}{
		{`{"data":"unique\n","client":"phone-client-0001","sequence":1}`, 200},
		{`{"data":"unique\n","client":"phone-client-0001","sequence":1}`, 200},
		{`{"data":"different\n","client":"phone-client-0001","sequence":1}`, 409},
		{`{"data":"out of order\n","client":"phone-client-0001","sequence":3}`, 409},
		{`{"data":"without receipt"}`, 400},
	} {
		if code, b := call(t, "POST", ts.URL+path+"/input", phoneToken, in.body); code != in.want {
			t.Fatalf("input: %d %s", code, b)
		}
	}
	if code, b := call(t, "GET", ts.URL+path+"/output?after=0", phoneToken, ""); code != 200 || strings.Contains(string(b), phoneID) {
		t.Fatalf("output: %d %s", code, b)
	}
	if code, _ := call(t, "POST", ts.URL+"/api/projects/other/terminals/"+session.ID+"/input", phoneToken, `{"data":"attack","client":"phone-client-0001","sequence":2}`); code != 404 {
		t.Fatal(code)
	}
	if code, _ := call(t, "POST", ts.URL+path+"/resize", phoneToken, `{"cols":40,"rows":12}`); code != 200 {
		t.Fatal(code)
	}
	if code, _ := call(t, "PUT", ts.URL+path+"/phone", "terminal-owner", `{"phoneId":"`+phoneID+`","enabled":false}`); code != 200 {
		t.Fatal(code)
	}
	if code, _ := call(t, "POST", ts.URL+path+"/stop", phoneToken, `{}`); code != 403 {
		t.Fatalf("removed grant still controls: %d", code)
	}
	call(t, "PUT", ts.URL+path+"/phone", "terminal-owner", grant)
	if code, _ := call(t, "DELETE", ts.URL+"/api/phones/"+phoneID, "terminal-owner", ""); code != 204 {
		t.Fatal(code)
	}
	for _, route := range routes {
		if code, _ := call(t, route.method, ts.URL+route.path, phoneToken, route.body); code != 401 {
			t.Fatalf("revoked phone: %s %d", route.path, code)
		}
	}
	// A disconnected output request cannot stop a session.
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+path+"/output?after=0", nil)
	req.Header.Set("Authorization", "Bearer terminal-owner")
	cancel()
	_, _ = http.DefaultClient.Do(req)
	if e := m.Input(projectID, session.ID, []byte("still here\n")); e != nil {
		t.Fatal(e)
	}
	if code, _ := call(t, "POST", ts.URL+path+"/stop", "terminal-owner", `{}`); code != 200 {
		t.Fatalf("stop: %d", code)
	}
}
func TestTerminalsRefuseNetworkAndCrossOrigin(t *testing.T) {
	handler := New(Options{AuthRequired: true, Token: "owner"}).Handler()
	for _, remote := range []string{"127.0.0.1:1234", "192.0.2.1:1234"} {
		req := httptest.NewRequest("GET", "http://localhost/api/terminals", nil)
		req.RemoteAddr = remote
		req.Header.Set("Authorization", "Bearer owner")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	req := httptest.NewRequest("POST", "http://localhost/api/projects/p/terminals", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer owner")
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

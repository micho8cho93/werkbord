package workspaces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPersonalTransportBoundsOwnerRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer owner-only" {
			t.Error("missing owner authentication")
		}
		if r.Method == "PUT" {
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"stale revision"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	p := NewIndividual(func(context.Context) (Access, error) { return Access{Base: server.URL, Token: "owner-only"}, nil })
	for _, path := range []string{"/api/personal/v1/workspaces?limit=100", "/api/personal/v1/workspaces/pws_abcdefghijklmnop", "/api/personal/v1/collections/pcl_abcdefghijklmnop/records?limit=100&after=prec_abcdefghijklmnop"} {
		if _, err := p.PersonalWorkspaceCall(bg, "GET", path, nil); err != nil {
			t.Fatal(err)
		}
	}
	result, err := p.PersonalWorkspaceCall(bg, "PUT", "/api/personal/v1/workspaces/pws_abcdefghijklmnop", json.RawMessage(`{"revision":1}`))
	if err != nil || result.Status != 409 {
		t.Fatal("lost conflict", result, err)
	}
	before := calls
	for _, path := range []string{"https://example.test/api/personal/v1/workspaces", "/api/projects", "/api/personal/v1/workspaces/../collections", "/api/personal/v1/workspaces?token=owner-only", "/api/personal/v1/workspaces?limit=999", "/api/personal/v1/workspaces#x"} {
		if _, err := p.PersonalWorkspaceCall(bg, "GET", path, nil); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
	if _, err := p.PersonalWorkspaceCall(bg, "PATCH", "/api/personal/v1/workspaces", nil); err == nil {
		t.Fatal("arbitrary method")
	}
	if _, err := p.PersonalWorkspaceCall(bg, "GET", "/api/personal/v1/workspaces", json.RawMessage(`{}`)); err == nil {
		t.Fatal("read body")
	}
	if calls != before {
		t.Fatal("invalid request reached HTTP")
	}
}

func TestPersonalLifecycleTransportFiniteMethods(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer owner-only" {
			t.Error("lost owner scope")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	p := NewIndividual(func(context.Context) (Access, error) { return Access{Base: server.URL, Token: "owner-only"}, nil })
	for _, suffix := range []string{"history", "revisions/1", "deletion-preview", "rollback"} {
		method := "GET"
		var body json.RawMessage
		if suffix == "deletion-preview" || suffix == "rollback" {
			method = "POST"
			body = json.RawMessage(`{}`)
		}
		path := "/api/personal/v1/workspaces/pws_abcdefghijklmnop/" + suffix
		if _, err := p.PersonalWorkspaceCall(bg, method, path, body); err != nil {
			t.Fatal(suffix, err)
		}
		if _, err := p.PersonalWorkspaceCall(bg, "DELETE", path, json.RawMessage(`{}`)); err == nil {
			t.Fatal("unsupported lifecycle method accepted")
		}
	}
	if calls != 4 {
		t.Fatal(calls)
	}
}

func TestPersonalRuntimeTransportAllowsOnlyFiniteActionAndBindingMethods(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer owner-only" {
			t.Error("lost owner credential")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	p := NewIndividual(func(context.Context) (Access, error) { return Access{Base: server.URL, Token: "owner-only"}, nil })
	action := "/api/personal/v1/workspaces/pws_abcdefghijklmnop/views/leads/actions/contact"
	binding := "/api/personal/v1/workspaces/pws_abcdefghijklmnop/bindings/tickets"
	if _, err := p.PersonalWorkspaceCall(bg, "POST", action, json.RawMessage(`{"confirmed":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PersonalWorkspaceCall(bg, "GET", binding, nil); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, path string }{{"GET", action}, {"DELETE", action}, {"POST", binding}, {"PUT", binding}, {"POST", action + "?token=x"}, {"POST", action + "/../run"}, {"POST", action + "-invalid"}} {
		if _, err := p.PersonalWorkspaceCall(bg, test.method, test.path, json.RawMessage(`{}`)); err == nil {
			t.Fatal("accepted arbitrary transport", test)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

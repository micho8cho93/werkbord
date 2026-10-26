package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/service"
)

func TestMemoryAPIHumanControlsAndScope(t *testing.T) {
	ts := newTestServer(t, func(o *Options) {
		m, e := service.NewMemory(service.Deps{Store: o.Store}, "individual", domain.MemoryLimits{})
		if e != nil {
			t.Fatal(e)
		}
		o.Memory = &service.Knowledge{Memory: m}
		o.AuthRequired = true
		o.Token = ownerToken
	})
	var p, other domain.Project
	for _, out := range []*domain.Project{&p, &other} {
		code, b := call(t, "POST", ts.URL+"/api/projects", ownerToken, fmt.Sprintf(`{"path":%q}`, gitRepo(t)))
		if code != 201 {
			t.Fatal(code, string(b))
		}
		if e := json.Unmarshal(b, out); e != nil {
			t.Fatal(e)
		}
	}
	path := ts.URL + "/api/projects/" + p.ID + "/memory"
	if code, _ := call(t, "POST", path, "", `{"kind":"decision","summary":"Use SQLite"}`); code != 401 {
		t.Fatal(code)
	}
	if code, _ := call(t, "POST", path, ownerToken, `{"kind":"fact","summary":"passed","verifiedAt":"2026-01-01T00:00:00Z"}`); code != 400 {
		t.Fatal("client verification accepted", code)
	}
	code, b := call(t, "POST", path, ownerToken, `{"kind":"decision","summary":"Use SQLite"}`)
	if code != 201 {
		t.Fatal(code, string(b))
	}
	var m domain.Memory
	if e := json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	if m.Source.ApprovedBy != "individual" || m.VerifiedAt == nil {
		t.Fatal(m)
	}
	wrong := ts.URL + "/api/projects/" + other.ID + "/memory/" + m.ID
	for _, method := range []string{"PATCH", "DELETE"} {
		if code, _ = call(t, method, wrong, ownerToken, fmt.Sprintf(`{"version":%d}`, m.Version)); code != 404 {
			t.Fatal(method, code)
		}
	}
	if code, _ = call(t, "GET", wrong+"/sources", ownerToken, ""); code != 404 {
		t.Fatal(code)
	}
	if code, _ = call(t, "GET", path+"/context?q=SQLite&tokens=512", ownerToken, ""); code != 200 {
		t.Fatal(code)
	}
	code, b = call(t, "PATCH", path+"/"+m.ID, ownerToken, fmt.Sprintf(`{"version":%d,"pinned":true}`, m.Version))
	if code != 200 {
		t.Fatal(code, string(b))
	}
	if code, _ = call(t, "PATCH", path+"/"+m.ID, ownerToken, fmt.Sprintf(`{"version":%d,"archive":true}`, m.Version)); code != 409 {
		t.Fatal("stale edit accepted", code)
	}
	if e := json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	code, b = call(t, "GET", path+"/"+m.ID+"/sources", ownerToken, "")
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, b = call(t, "DELETE", path+"/"+m.ID, ownerToken, fmt.Sprintf(`{"version":%d}`, m.Version))
	if code != 204 {
		t.Fatal(code, string(b))
	}
	if code, _ = call(t, "GET", path+"?status=made-up", ownerToken, ""); code != 400 {
		t.Fatal(code)
	}
}
func TestMemoryUnavailableOnOtherListeners(t *testing.T) {
	ts := newTestServer(t, nil)
	if code := do(t, "GET", ts.URL+"/api/projects/p/memory", "", nil); code != 503 {
		t.Fatal(code)
	}
}

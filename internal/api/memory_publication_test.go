package api

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"werkbord/internal/domain"
	"werkbord/internal/localaccess"
	"werkbord/internal/service"
)

func TestMemoryPublicationGrantCannotReadOrApprovePrivateData(t *testing.T) {
	ts := newTestServer(t, func(o *Options) {
		m, err := service.NewMemory(service.Deps{Store: o.Store}, "individual", domain.MemoryLimits{})
		if err != nil {
			t.Fatal(err)
		}
		o.Memory = &service.Knowledge{Memory: m}
		o.AuthRequired = true
		o.Token = ownerToken
		o.LocalAccess, err = localaccess.Open(filepath.Join(t.TempDir(), "grants.json"))
		if err != nil {
			t.Fatal(err)
		}
	})
	code, raw := call(t, "POST", ts.URL+"/api/local-access", ownerToken, `{"name":"Memory","scope":"memory-v1","workspaceId":"ws_one"}`)
	if code != 201 {
		t.Fatalf("grant: %d %s", code, raw)
	}
	var grant struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatal(err)
	}
	if code, raw = call(t, "GET", ts.URL+"/api/memory/v1/publication/events?workspaceId=ws_one", grant.Token, ""); code != 200 {
		t.Fatalf("own journal: %d %s", code, raw)
	}
	for _, test := range []struct{ method, path, body string }{
		{"GET", "/api/memory/v1/publication/events?workspaceId=ws_other", ""},
		{"GET", "/api/memory/v1/publication/grants", ""},
		{"GET", "/api/memory/v1/publication?workspaceId=ws_one", ""},
		{"PUT", "/api/memory/v1/publication/grants", `{"workspaceId":"ws_one"}`},
		{"POST", "/api/memory/v1/publication/review", `{"workspaceId":"ws_one","summary":"no approval"}`},
		{"POST", "/api/memory/v1/publication/events?workspaceId=ws_one", `{"eventId":"fake","sequence":1,"record":{},"summary":"smuggled"}`},
	} {
		if code, raw = call(t, test.method, ts.URL+test.path, grant.Token, test.body); code != 403 {
			t.Fatalf("grant escaped %s %s: %d %s", test.method, test.path, code, raw)
		}
	}
}

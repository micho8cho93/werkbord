package controller

import (
	"devboard/internal/appops"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPersonalDataIsWiredOnlyOnTheOwnerLoopbackListener(t *testing.T) {
	node := newFakeNode()
	cfg := defaultConfig(t)
	on := true
	cfg.Network.Enabled = &on
	c := startWith(t, cfg, node)
	waitFor(t, "the private listener", func() bool { return node.addr() != "" })
	token, err := c.cfg.ResolveToken(false)
	if err != nil {
		t.Fatal(err)
	}
	local, private := "http://"+c.Addr(), "http://"+node.addr()
	for _, path := range []string{"/api/personal/v1/workspaces", "/api/personal/v1/collections", "/api/personal/v1/capabilities"} {
		if code, body := get(t, local+path, token); code != 200 {
			t.Fatal(path, code, body)
		}
		// The fake private transport also uses loopback. A source-IP check alone
		// cannot pass this test; the service must be absent from private wiring.
		if code, body := get(t, private+path, token); code != 503 {
			t.Fatal(path, code, body)
		}
	}
	spec := `{"name":"Private","icon":"board","uiSchemaVersion":1,"visibility":{"individual":true},"definition":{"layout":{"type":"heading","text":"Private workspace"}}}`
	send := func(base string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest("POST", base+"/api/personal/v1/workspaces", strings.NewReader(spec))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}
	if code, body := send(private); code != 503 {
		t.Fatal(code, string(body))
	}
	code, body := send(local)
	if code != 201 {
		t.Fatal(code, string(body))
	}
	var saved struct {
		ID       string
		Revision int
	}
	if err = json.Unmarshal(body, &saved); err != nil || saved.ID == "" || saved.Revision != 1 {
		t.Fatal(saved, err)
	}
	if code, body := get(t, local+"/api/personal/v1/workspaces/"+saved.ID, token); code != 200 || !strings.Contains(body, "Private workspace") {
		t.Fatal(code, body)
	}
}

func TestTokenlessControllerDoesNotExposePersonalAssistantTools(t *testing.T) {
	cfg := defaultConfig(t)
	cfg.RequireToken = false
	if cfg.AuthRequired() {
		t.Fatal("fixture must use tokenless loopback")
	}
	c := startWith(t, cfg, newFakeNode())
	p := appops.Principal{Via: "assistant", Grants: appops.AllPermissions}
	hasProjects := false
	for _, op := range c.appOps.Catalog(p) {
		if strings.Contains(op.Name, "personal_workspace") {
			t.Fatal("personal data available without owner authentication", op.Name)
		}
		if op.Name == "list_projects" {
			hasProjects = true
		}
	}
	if !hasProjects {
		t.Fatal("existing board permissions changed")
	}
}

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
		// The fake private transport also uses loopback. A source-IP check alone cannot pass this test: the private
		// listener must refuse the controller's own token, whatever address it comes from.
		if code, body := get(t, private+path, token); code != 403 {
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
	if code, body := send(private); code != 403 {
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

// What is made at the computer is open on an enrolled phone, and what the phone writes is what the computer reads: one
// database behind both, reached through the private network by the phone's own credential and nothing else.
func TestAnEnrolledPhoneWorksWithPersonalDataOnThePrivateNetwork(t *testing.T) {
	node := newFakeNode()
	cfg := defaultConfig(t)
	on := true
	cfg.Network.Enabled = &on
	c := startWith(t, cfg, node)
	waitFor(t, "the private listener", func() bool { return node.addr() != "" })
	token, _ := c.cfg.ResolveToken(false)
	local, private := "http://"+c.Addr(), "http://"+node.addr()
	waitFor(t, "connected", func() bool {
		code, body := get(t, local+"/api/network", token)
		return code == 200 && strings.Contains(body, `"connected"`)
	})
	_, codeBody := post(t, local+"/api/phones/codes", token)
	var link struct{ Link string }
	_ = json.Unmarshal([]byte(codeBody), &link)
	secret := link.Link[strings.Index(link.Link, "#enroll=")+len("#enroll="):]
	resp, err := http.Post(private+"/api/phones/enroll", "application/json", strings.NewReader(`{"code":"`+secret+`","name":"Phone"}`))
	if err != nil {
		t.Fatal(err)
	}
	var enrolled struct{ Token string }
	_ = json.NewDecoder(resp.Body).Decode(&enrolled)
	resp.Body.Close()
	if resp.StatusCode != 201 || enrolled.Token == "" {
		t.Fatalf("enroll = %d", resp.StatusCode)
	}
	do := func(base, method, path, tok, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	code, body := do(local, "POST", "/api/personal/v1/collections", token, `{"name":"To-dos","schemaVersion":1,"fields":[{"key":"title","name":"Title","type":"text","required":true}]}`)
	var col struct{ ID string }
	if _ = json.Unmarshal([]byte(body), &col); code != 201 {
		t.Fatal(code, body)
	}
	spec := `{"name":"Made at the computer","icon":"check","uiSchemaVersion":1,"visibility":{"universal":true,"individual":true},"collectionIds":["` + col.ID + `"],"readBindings":[],"definition":{"views":[{"id":"list","name":"To-dos","kind":"list","collectionId":"` + col.ID + `","fields":["title"],"operations":["create","update","delete"]}],"layout":{"type":"view","viewId":"list"}}}`
	code, body = do(local, "POST", "/api/personal/v1/workspaces", token, spec)
	var ws struct{ ID string }
	if _ = json.Unmarshal([]byte(body), &ws); code != 201 {
		t.Fatal(code, body)
	}
	// The phone sees it at once, with no step in between.
	if code, body = do(private, "GET", "/api/personal/v1/workspaces", enrolled.Token, ""); code != 200 || !strings.Contains(body, ws.ID) {
		t.Fatalf("the phone's list of workspaces = %d %s", code, body)
	}
	if code, body = do(private, "POST", "/api/personal/v1/collections/"+col.ID+"/records", enrolled.Token, `{"values":{"title":"From the phone"}}`); code != 201 {
		t.Fatalf("a phone's record = %d %s", code, body)
	}
	if code, body = do(local, "GET", "/api/personal/v1/collections/"+col.ID+"/records", token, ""); code != 200 || !strings.Contains(body, "From the phone") {
		t.Fatalf("the computer does not read the phone's record: %d %s", code, body)
	}
	// What a workspace is stays at the computer.
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/personal/v1/workspaces"}, {"PUT", "/api/personal/v1/workspaces/" + ws.ID}, {"DELETE", "/api/personal/v1/workspaces/" + ws.ID},
	} {
		if code, body = do(private, c.method, c.path, enrolled.Token, spec); code != 403 {
			t.Errorf("a phone %s %s = %d %s", c.method, c.path, code, body)
		}
	}
	// And the controller's own token still does not open it from the network.
	if code, body = do(private, "GET", "/api/personal/v1/workspaces", token, ""); code != 403 {
		t.Errorf("the controller's token over the private network = %d %s", code, body)
	}
}

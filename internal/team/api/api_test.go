package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"devboard/internal/team/config"
	"devboard/internal/team/server"
)

type client struct {
	t     *testing.T
	base  string
	token string
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, svc, err := server.Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ts := httptest.NewServer(server.Handler(db, svc, nil, "v9.9.9"))
	t.Cleanup(ts.Close)
	// Workspaces are created by the host's CLI, never over HTTP; do the same here.
	c, err := svc.CreateWorkspace(context.Background(), "Acme", "Ada", "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ownerToken = c.Token
	return ts
}

// ownerToken is the token of the workspace newServer made.
var ownerToken string

func (c client) do(method, path string, body any) (int, map[string]any, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		var b []byte
		if s, ok := body.(string); ok {
			b = []byte(s)
		} else {
			b, _ = json.Marshal(body)
		}
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, m, raw
}

func (c client) want(status int, method, path string, body any) map[string]any {
	c.t.Helper()
	got, m, raw := c.do(method, path, body)
	if got != status {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, got, status, raw)
	}
	return m
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

const v1 = "/api/team/v1"

func TestHealthIsPublicAndEverythingElseNeedsAToken(t *testing.T) {
	ts := newServer(t)
	anon := client{t: t, base: ts.URL}
	h := anon.want(200, "GET", v1+"/health", nil)
	if h["status"] != "ok" || h["product"] != "werkbord-team" || h["version"] != "v9.9.9" {
		t.Fatalf("health: %v", h)
	}
	for _, p := range []string{"/me", "/workspace", "/members", "/projects", "/roles", "/nonsense"} {
		m := anon.want(401, "GET", v1+p, nil)
		if errCode(m) != "unauthorized" {
			t.Errorf("%s: %v", p, m)
		}
	}
	bad := client{t: t, base: ts.URL, token: "wbt_" + strings.Repeat("0", 64)}
	bad.want(401, "GET", v1+"/me", nil)
	// An unknown path under /api is a JSON 404, not the console.
	owner := client{t: t, base: ts.URL, token: ownerToken}
	if errCode(owner.want(404, "GET", v1+"/nonsense", nil)) != "not_found" || errCode(owner.want(404, "GET", "/api/other", nil)) != "not_found" {
		t.Fatal("unknown API routes should be JSON not_found")
	}
}

func TestTheWholeFlowOverHTTP(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}

	me := owner.want(200, "GET", v1+"/me", nil)
	if me["member"].(map[string]any)["role"] != "owner" || len(me["permissions"].([]any)) < 5 {
		t.Fatalf("me: %v", me)
	}
	owner.want(200, "GET", v1+"/workspace", nil)
	if m := owner.want(200, "PATCH", v1+"/workspace", `{"name":"Acme Corp"}`); m["name"] != "Acme Corp" {
		t.Fatalf("rename: %v", m)
	}
	roles, _, raw := owner.do("GET", v1+"/roles", nil)
	if roles != 200 || !strings.Contains(string(raw), `"owner"`) || !strings.Contains(string(raw), `"member"`) {
		t.Fatalf("roles: %s", raw)
	}

	added := owner.want(201, "POST", v1+"/members", `{"name":"Bo","email":"bo@example.com"}`)
	bo := client{t: t, base: ts.URL, token: added["token"].(string)}
	boID := added["member"].(map[string]any)["id"].(string)
	if !strings.HasPrefix(bo.token, "wbt_") || added["member"].(map[string]any)["role"] != "member" {
		t.Fatalf("added: %v", added)
	}
	// The listing never carries a token.
	if _, _, raw := owner.do("GET", v1+"/members", nil); strings.Contains(string(raw), "wbt_") || strings.Contains(string(raw), "tokenHash") {
		t.Fatalf("the member list leaks a token: %s", raw)
	}

	proj := owner.want(201, "POST", v1+"/projects", `{"name":"Billing","repository":"https://github.com/acme/billing"}`)
	pid := proj["id"].(string)
	if list, _, raw := bo.do("GET", v1+"/projects", nil); list != 200 || strings.Contains(string(raw), pid) {
		t.Fatalf("Bo sees a project they are not on: %s", raw)
	}
	bo.want(404, "GET", v1+"/projects/"+pid, nil)
	owner.want(204, "PUT", v1+"/projects/"+pid+"/members/"+boID, nil)
	bo.want(200, "GET", v1+"/projects/"+pid, nil)
	if _, _, raw := bo.do("GET", v1+"/projects/"+pid+"/members", nil); !strings.Contains(string(raw), "Bo") || !strings.Contains(string(raw), "Ada") {
		t.Fatalf("project members: %s", raw)
	}
	if errCode(bo.want(403, "PATCH", v1+"/projects/"+pid, `{"archived":true}`)) != "forbidden" {
		t.Fatal("a member archived a project")
	}
	if m := owner.want(200, "PATCH", v1+"/projects/"+pid, `{"archived":true}`); m["archived"] != true {
		t.Fatalf("archive: %v", m)
	}
	owner.want(204, "DELETE", v1+"/projects/"+pid+"/members/"+boID, nil)
	owner.want(404, "DELETE", v1+"/projects/"+pid+"/members/"+boID, nil)

	// A member cannot do what only the owner can.
	bo.want(403, "POST", v1+"/members", `{"name":"Cy"}`)
	bo.want(403, "POST", v1+"/projects", `{"name":"Mine"}`)
	bo.want(403, "PATCH", v1+"/workspace", `{"name":"Mine"}`)
	bo.want(403, "DELETE", v1+"/members/"+me["member"].(map[string]any)["id"].(string), nil)

	// Reissuing signs out the old token.
	fresh := bo.want(200, "POST", v1+"/members/"+boID+"/token", nil)
	bo.want(401, "GET", v1+"/me", nil)
	bo2 := client{t: t, base: ts.URL, token: fresh["token"].(string)}
	bo2.want(200, "GET", v1+"/me", nil)

	owner.want(204, "DELETE", v1+"/members/"+boID, nil)
	bo2.want(401, "GET", v1+"/me", nil)
	owner.want(409, "DELETE", v1+"/members/"+me["member"].(map[string]any)["id"].(string), nil) // the owner stays
}

func TestRequestsAreValidatedStrictly(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	for name, body := range map[string]string{
		"unknown field":    `{"name":"X","admin":true}`,
		"not json":         `name=X`,
		"two objects":      `{"name":"X"}{"name":"Y"}`,
		"empty name":       `{"name":""}`,
		"password in repo": `{"name":"X","repository":"https://u:p@github.com/a/b"}`,
		"oversized":        `{"name":"X","description":"` + strings.Repeat("x", 70<<10) + `"}`,
	} {
		if m := owner.want(400, "POST", v1+"/projects", body); errCode(m) != "invalid" {
			t.Errorf("%s: %v", name, m)
		}
	}
	owner.want(400, "POST", v1+"/members", `{"name":"Cy","role":"owner"}`)
	owner.want(400, "POST", v1+"/members", `{"name":"Cy","role":"superuser"}`)
	owner.want(404, "DELETE", v1+"/projects/x", nil) // there is no way to delete a project
}

func TestTheOwnerCannotBeCreatedOverHTTP(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	anon := client{t: t, base: ts.URL}
	anon.want(401, "POST", v1+"/workspaces", `{"name":"Mine","owner":"Eve"}`)
	owner.want(404, "POST", v1+"/workspaces", `{"name":"Mine","owner":"Eve"}`)
	for _, c := range []client{anon, owner} {
		c.want(404, "POST", "/api/team/workspaces", `{"name":"Mine"}`)
	}
}

func TestACrossSiteWriteIsRefused(t *testing.T) {
	ts := newServer(t)
	req, _ := http.NewRequest("POST", ts.URL+v1+"/projects", strings.NewReader(`{"name":"X"}`))
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestTheConsoleIsServedWithSecurityHeaders(t *testing.T) {
	ts := newServer(t)
	for _, p := range []string{"/", "/console.js", "/console.css"} {
		res, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || len(body) == 0 {
			t.Fatalf("%s: %d", p, res.StatusCode)
		}
		if res.Header.Get("Content-Security-Policy") == "" || res.Header.Get("X-Frame-Options") != "DENY" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s is missing security headers: %v", p, res.Header)
		}
		if p == "/" && !strings.Contains(string(body), "Werkbord Team") {
			t.Errorf("/ is not the console: %.80s", body)
		}
	}
	// The console never builds HTML from what the server says.
	js, _ := http.Get(ts.URL + "/console.js")
	b, _ := io.ReadAll(js.Body)
	js.Body.Close()
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
		if strings.Contains(string(b), banned) {
			t.Errorf("console.js uses %s", banned)
		}
	}
}

func TestSoleOwnerCanRotateTheirCredentialAndContinue(t *testing.T) {
	ts := newServer(t)
	old := client{t: t, base: ts.URL, token: ownerToken}
	me := old.want(200, "GET", v1+"/me", nil)
	id := me["member"].(map[string]any)["id"].(string)
	next := old.want(200, "POST", v1+"/members/"+id+"/token", nil)
	fresh := client{t: t, base: ts.URL, token: next["token"].(string)}
	old.want(401, "GET", v1+"/me", nil)
	fresh.want(200, "GET", v1+"/me", nil)
}

func TestPartialPRFieldsKeepPreviouslyReportedEvidence(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	p := owner.want(201, "POST", v1+"/projects", map[string]any{"name": "Shop", "repository": "https://github.com/acme/shop"})
	pid := p["id"].(string)
	path := v1 + "/projects/" + pid + "/tickets"
	k := owner.want(201, "POST", path, map[string]any{"title": "Work", "status": "available"})
	tid := k["id"].(string)
	path += "/" + tid
	owner.want(200, "POST", path+"/claim", nil)
	owner.want(200, "PUT", path+"/git", map[string]any{"pullRequest": map[string]any{"number": 7, "url": "https://github.com/acme/shop/pull/7", "state": "open", "draft": true, "mergeable": "conflicting", "baseBranch": "main", "ahead": 8, "behind": 3}})
	changed := owner.want(200, "PUT", path+"/git", map[string]any{"pullRequest": map[string]any{"state": "closed"}})
	pr := changed["pullRequest"].(map[string]any)
	if pr["draft"] != true || pr["baseBranch"] != "main" || pr["mergeable"] != "conflicting" || pr["behind"] != float64(3) || pr["number"] != float64(7) {
		t.Fatalf("partial edit erased PR facts: %v", pr)
	}
}

// Admin is a role like the others over HTTP: the owner appoints one, who then
// administers members but cannot appoint another admin or reach the owner.
func TestAdminsOverHTTP(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	me := owner.want(200, "GET", v1+"/me", nil)
	ownerID := me["member"].(map[string]any)["id"].(string)

	added := owner.want(201, "POST", v1+"/members", `{"name":"Ann","role":"admin"}`)
	if added["member"].(map[string]any)["role"] != "admin" {
		t.Fatalf("added: %v", added)
	}
	ann := client{t: t, base: ts.URL, token: added["token"].(string)}
	annMe := ann.want(200, "GET", v1+"/me", nil)
	for _, p := range annMe["permissions"].([]any) {
		if p == "workspace.ownership" || p == "admins.manage" {
			t.Fatalf("an admin has %v", p)
		}
	}
	if _, _, raw := owner.do("GET", v1+"/roles", nil); !strings.Contains(string(raw), `"admin"`) {
		t.Fatalf("roles: %s", raw)
	}

	ann.want(201, "POST", v1+"/members", `{"name":"Bo"}`)
	if errCode(ann.want(403, "POST", v1+"/members", `{"name":"Cy","role":"admin"}`)) != "forbidden" {
		t.Fatal("an admin appointed an admin")
	}
	ann.want(403, "POST", v1+"/members/"+ownerID+"/token", nil)
	ann.want(409, "DELETE", v1+"/members/"+ownerID, nil)
	ann.want(201, "POST", v1+"/projects", `{"name":"Admin's"}`)
	owner.want(200, "GET", v1+"/me", nil) // the owner's token still works
}

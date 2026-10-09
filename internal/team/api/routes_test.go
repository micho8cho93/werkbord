package api_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"devboard/internal/team/config"
	"devboard/internal/team/server"
)

// The security audit of Team's HTTP surface, as tests.
//
// Team members exchange coordination metadata and nothing else. So the surface is
// listed here, in full: a route that is not in this list fails the build until
// someone reads it, adds it, and says in the review that it cannot start a
// process, read a file, or reach another member's computer. The same list drives
// the authorization matrix below.

// publicRoutes need no token.
var publicRoutes = map[string]bool{
	"GET /health":          true,
	"POST /invites/redeem": true, // the invite code is the credential
}

// projectRoutes are scoped to one project: a person who is not on it must not be
// able to tell it exists. Path parameters: {id} project, {tid} ticket.
var projectRoutes = []string{
	"PUT /projects/{id}/tickets/{tid}/progress", "GET /projects/{id}/tickets/{tid}/progress", // metadata observations only; no execution effects
	"GET /projects/{id}", "PATCH /projects/{id}",
	"GET /projects/{id}/members", "PUT /projects/{id}/members/{memberId}", "DELETE /projects/{id}/members/{memberId}",
	"GET /projects/{id}/board", "GET /projects/{id}/people", "GET /projects/{id}/sync",
	// Archive routes only change stored ticket metadata; they cannot execute work or reach a member.
	"POST /projects/{id}/tickets/archive-done", "POST /projects/{id}/tickets/{tid}/archive",
	"POST /projects/{id}/tickets", "GET /projects/{id}/tickets/{tid}", "PATCH /projects/{id}/tickets/{tid}",
	"POST /projects/{id}/tickets/{tid}/move", "POST /projects/{id}/tickets/{tid}/claim", "POST /projects/{id}/tickets/{tid}/release",
	"POST /projects/{id}/tickets/{tid}/assign", "POST /projects/{id}/tickets/{tid}/submit", "POST /projects/{id}/tickets/{tid}/request-changes",
	"POST /projects/{id}/tickets/{tid}/complete", "PUT /projects/{id}/tickets/{tid}/git", "POST /projects/{id}/tickets/{tid}/handoff",
	"GET /projects/{id}/repository", "POST /projects/{id}/repository/branches", "GET /projects/{id}/activity",
	"GET /projects/{id}/invites", "POST /projects/{id}/invites", "DELETE /projects/{id}/invites/{inviteId}",
}

// workspaceRoutes act on the signed-in member's own workspace, whichever project.
var workspaceRoutes = []string{
	"PUT /license", // installs only an owner-approved, vendor-signed offline entitlement
	"GET /me", "GET /roles", "GET /workspace", "PATCH /workspace",
	"GET /members", "POST /members", "DELETE /members/{id}", "POST /members/{id}/token", "PUT /members/{id}/role",
	"GET /projects", "POST /projects",
	"GET /overview", "GET /my-work", "GET /reviews", "GET /sync",
	"GET /search", // bounded, read-only lookup of visible projects and stored tickets; cannot execute or reach a computer
	"POST /invites/join",
	// The private network and the devices on it: addresses, roles, reachability, invitations
	// and requests to join. A device's own routes are authenticated by its own credential.
	"GET /devices", "POST /devices/{id}/revoke", "PUT /devices/{id}/network", "GET /devices/{id}/network", "PUT /devices/{id}/capabilities", "PUT /devices/{id}/name", "POST /devices/{id}/provision", "DELETE /devices/{id}/replica",
	// Where the workspace's data is kept (how many hosts hold it, whether it is read-only) and backing it up: coordination
	// metadata, and a request to the host to back up to the directory its own configuration names.
	"GET /storage", "POST /storage/backup",
	// How resilient the workspace is, in words (read-only), and a device saying, with its own credential, that it is there and what
	// kind of machine it is (the profile holds a platform, a kind, whether it sleeps, a count and a version; nothing else).
	"GET /resilience", "POST /device/heartbeat",
	// Signed requests between a person's own devices (an action from a closed list, with identifiers for a payload): the server stores
	// one exactly as the sender signed it and hands it to the device it is for, which checks the signature itself. Nothing here makes,
	// changes or acts on a request, and only the owner of both devices can send or read one.
	"POST /messages", "GET /messages", "GET /messages/{id}", "GET /device/messages", "POST /device/messages/{id}/ack",
	"GET /network", "PUT /network/approval", "GET /network/config", "POST /network/certificate", "POST /network/checks",
	"GET /network/provision", "POST /network/provision/ack",
	"GET /enrollment-invitations", "POST /enrollment-invitations", "DELETE /enrollment-invitations/{id}",
	"GET /enrollments", "POST /enrollments/{id}/approve", "POST /enrollments/{id}/deny",
}

// ownerOnly are project routes a plain member of the project must be refused (403).
var ownerOnly = []string{
	"POST /projects/{id}/tickets/archive-done",
	"PATCH /projects/{id}", "PUT /projects/{id}/members/{memberId}", "DELETE /projects/{id}/members/{memberId}",
	"POST /projects/{id}/tickets/{tid}/assign", "POST /projects/{id}/tickets/{tid}/complete", "POST /projects/{id}/tickets/{tid}/request-changes",
	"GET /projects/{id}/invites", "POST /projects/{id}/invites", "DELETE /projects/{id}/invites/{inviteId}",
}

// forbiddenWords must not appear in any route: nothing in Team starts a process,
// opens a terminal, serves a file, or addresses a member's computer.
var forbiddenWords = []string{"exec", "shell", "terminal", "pty", "run", "runner", "process", "command", "script", "file", "fs", "path", "env", "secret",
	"credential", "key", "ssh", "proxy", "forward", "tunnel", "socket", "ws", "agent", "machine", "host", "remote", "download", "upload", "eval"}

func allRoutes() []string {
	out := append([]string{}, projectRoutes...)
	out = append(out, workspaceRoutes...)
	for r := range publicRoutes {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// The routes the server registers are exactly the routes listed here.
func TestTheRouteSurfaceIsExactlyTheReviewedList(t *testing.T) {
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?:HandleFunc|Handle)\("([A-Z]+) /api/team/v1([^"]*)"`)
	var registered []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		registered = append(registered, m[1]+" "+m[2])
	}
	sort.Strings(registered)
	want := allRoutes()
	if fmt.Sprint(registered) != fmt.Sprint(want) {
		have := map[string]bool{}
		for _, r := range registered {
			have[r] = true
		}
		for _, r := range want {
			delete(have, r)
		}
		var extra, missing []string
		for r := range have {
			extra = append(extra, r)
		}
		wantSet := map[string]bool{}
		for _, r := range want {
			wantSet[r] = true
		}
		for _, r := range registered {
			delete(wantSet, r)
		}
		for r := range wantSet {
			missing = append(missing, r)
		}
		sort.Strings(extra)
		sort.Strings(missing)
		t.Fatalf("api.go registers routes that differ from the reviewed list.\n  not reviewed: %v\n  reviewed but not registered: %v\n"+
			"Read the new route; it must exchange coordination metadata only (no process, file, shell, credential or path on anyone's computer). Then add it to routes_test.go.", extra, missing)
	}
}

func TestNoRouteCanReachAComputer(t *testing.T) {
	wordRE := func(w string) *regexp.Regexp {
		return regexp.MustCompile(`(^|[^a-z])` + regexp.QuoteMeta(w) + `($|[^a-z])`)
	}
	for _, r := range allRoutes() {
		path := strings.ToLower(strings.SplitN(r, " ", 2)[1])
		path = regexp.MustCompile(`\{[a-zA-Z]+\}`).ReplaceAllString(path, "")
		for _, w := range forbiddenWords {
			if wordRE(w).MatchString(path) {
				t.Errorf("route %q contains %q: Team has no such capability", r, w)
			}
		}
	}
	// And the things themselves are simply not there, with a valid token.
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	for _, path := range []string{"/exec", "/shell", "/terminal", "/runners", "/runner", "/files", "/fs", "/env", "/secrets", "/credentials", "/tokens", "/agents",
		"/machines", "/proxy", "/ws", "/websocket", "/events", "/stream", "/projects/x/exec", "/projects/x/files", "/projects/x/runner",
		"/projects/x/tickets/y/run", "/projects/x/tickets/y/exec", "/members/x/runner", "/members/x/files", "/members/x/shell"} {
		for _, method := range []string{"GET", "POST", "PUT"} {
			if code, _, _ := owner.do(method, v1+path, `{}`); code != 404 && code != 405 {
				t.Errorf("%s %s answered %d", method, path, code)
			}
		}
	}
}

// bodyFor is a body that passes validation for the route, so that what a test
// observes is the authorization and not a complaint about the payload.
func bodyFor(route string) string {
	switch route {
	case "POST /projects/{id}/tickets/{tid}/archive":
		return `{"version":1,"archived":true}`
	case "POST /projects/{id}/tickets":
		return `{"title":"x"}`
	case "POST /projects/{id}/tickets/{tid}/move":
		return `{"status":"available"}`
	case "POST /projects/{id}/tickets/{tid}/assign":
		return `{"memberId":"tmb_x"}`
	case "POST /projects/{id}/repository/branches":
		return `{"branches":[]}`
	case "POST /projects/{id}/invites", "POST /projects/{id}/tickets/{tid}/submit", "POST /projects/{id}/tickets/{tid}/request-changes":
		return `{}`
	}
	return `{}`
}

func fill(route string, ids map[string]string) (method, path string) {
	method, path, _ = strings.Cut(route, " ")
	for k, v := range ids {
		path = strings.ReplaceAll(path, "{"+k+"}", v)
	}
	return method, v1 + path
}

// Every route that needs a token refuses a missing or wrong one.
func TestEveryRouteExceptTheTwoPublicOnesNeedsAToken(t *testing.T) {
	ts := newServer(t)
	anon := client{t: t, base: ts.URL}
	bad := client{t: t, base: ts.URL, token: "wbt_" + strings.Repeat("a", 64)}
	ids := map[string]string{"id": "tpj_x", "tid": "ttk_x", "memberId": "tmb_x", "inviteId": "tiv_x"}
	for _, r := range allRoutes() {
		if publicRoutes[r[strings.Index(r, " ")+1:]] || publicRoutes[r] {
			continue
		}
		method, path := fill(r, ids)
		for name, c := range map[string]client{"no token": anon, "a wrong token": bad} {
			if code, _, _ := c.do(method, path, `{}`); code != 401 {
				t.Errorf("%s with %s answered %d", r, name, code)
			}
		}
	}
	// The public ones are public, and nothing more.
	if code, _, _ := anon.do("GET", v1+"/health", nil); code != 200 {
		t.Errorf("health: %d", code)
	}
	if code, _, _ := anon.do("POST", v1+"/invites/redeem", `{"code":"wbi_nothing","name":"X"}`); code != 404 {
		t.Errorf("redeem: %d", code)
	}
}

// A project is invisible to people outside it, on every route that names it: those
// of another workspace and those of the same workspace who are not on it.
func TestEveryProjectRouteHidesTheProjectFromOutsiders(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	proj := owner.want(201, "POST", v1+"/projects", `{"name":"Secret"}`)
	pid := str(proj, "id")
	k := owner.want(201, "POST", v1+"/projects/"+pid+"/tickets", `{"title":"Hidden","status":"available"}`)
	ids := map[string]string{"id": pid, "tid": str(k, "id"), "memberId": "tmb_x", "inviteId": "tiv_x"}

	eve := client{t: t, base: ts.URL, token: str(owner.want(201, "POST", v1+"/members", `{"name":"Eve"}`), "token")}
	for _, r := range projectRoutes {
		method, path := fill(r, ids)
		if code, _, raw := eve.do(method, path, bodyFor(r)); code != 404 {
			t.Errorf("%s: a member of the workspace who is not on the project got %d: %s", r, code, raw)
		}
	}
}

// Another workspace's owner, who can see every project of their own, cannot see or
// touch one of ours by naming its id.
func TestEveryProjectRouteHidesTheProjectFromOtherWorkspaces(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, svc, err := server.Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ts := httptest.NewServer(server.Handler(db, svc, nil, "test"))
	t.Cleanup(ts.Close)
	a, _ := svc.CreateWorkspace(context.Background(), "Acme", "Ada", "")
	b, _ := svc.CreateWorkspace(context.Background(), "Rival", "Rex", "")
	acme := client{t: t, base: ts.URL, token: a.Token}
	rival := client{t: t, base: ts.URL, token: b.Token}
	pid := str(acme.want(201, "POST", v1+"/projects", `{"name":"Secret"}`), "id")
	k := acme.want(201, "POST", v1+"/projects/"+pid+"/tickets", `{"title":"Hidden","status":"available"}`)
	ids := map[string]string{"id": pid, "tid": str(k, "id"), "memberId": a.Owner.ID, "inviteId": "tiv_x"}
	for _, r := range projectRoutes {
		method, path := fill(r, ids)
		if code, _, raw := rival.do(method, path, bodyFor(r)); code != 404 {
			t.Errorf("%s: another workspace's owner got %d: %s", r, code, raw)
		}
	}
	// Their own lists show nothing of ours.
	for _, path := range []string{"/projects", "/overview", "/my-work", "/reviews", "/members", "/search?q=Secret", "/search?q=Ada"} {
		if _, _, raw := rival.do("GET", v1+path, nil); strings.Contains(string(raw), "Secret") || strings.Contains(string(raw), "Ada") {
			t.Errorf("GET %s shows another workspace: %s", path, raw)
		}
	}
	// A member id from our workspace means nothing in theirs.
	if code, _, _ := rival.do("POST", v1+"/members/"+a.Owner.ID+"/token", nil); code != 404 {
		t.Errorf("reissuing another workspace's token: %d", code)
	}
	if code, _, _ := rival.do("DELETE", v1+"/members/"+a.Owner.ID, nil); code != 404 {
		t.Errorf("removing another workspace's member: %d", code)
	}
}

// A plain member of a project is refused what only a project owner may do.
func TestProjectOwnerRoutesRefuseAPlainMember(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	pid := str(owner.want(201, "POST", v1+"/projects", `{"name":"Shop"}`), "id")
	bo := owner.want(201, "POST", v1+"/members", `{"name":"Bo"}`)
	boC := client{t: t, base: ts.URL, token: str(bo, "token")}
	owner.want(204, "PUT", v1+"/projects/"+pid+"/members/"+str(sub(bo, "member"), "id"), `{}`)
	k := owner.want(201, "POST", v1+"/projects/"+pid+"/tickets", `{"title":"Work","status":"available"}`)
	ids := map[string]string{"id": pid, "tid": str(k, "id"), "memberId": str(sub(bo, "member"), "id"), "inviteId": "tiv_x"}
	for _, r := range ownerOnly {
		method, path := fill(r, ids)
		if code, _, raw := boC.do(method, path, bodyFor(r)); code != 403 {
			t.Errorf("%s: a plain member got %d: %s", r, code, raw)
		}
	}
	// ...and workspace-level management is the workspace owner's.
	for _, r := range []string{"POST /members", "PATCH /workspace", "DELETE /members/{id}", "POST /projects"} {
		method, path := fill(r, map[string]string{"id": str(sub(bo, "member"), "id")})
		if code, _, raw := boC.do(method, path, `{"name":"X"}`); code != 403 {
			t.Errorf("%s: a plain member got %d: %s", r, code, raw)
		}
	}
	// Reissuing someone else's token is refused too.
	if code, _, _ := boC.do("POST", v1+"/members/"+str(sub(owner.want(200, "GET", v1+"/me", nil), "member"), "id")+"/token", nil); code != 403 {
		t.Errorf("Bo reissued the owner's token: %d", code)
	}
}

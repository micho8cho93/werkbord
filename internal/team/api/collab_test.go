package api_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func sub(m map[string]any, k string) map[string]any { s, _ := m[k].(map[string]any); return s }
func str(m map[string]any, k string) string         { s, _ := m[k].(string); return s }

// TestCollaborativeWorkflowOverHTTP walks a ticket from an invite to Done with two
// developers, as the console and the developers' own Werkbords would.
func TestCollaborativeWorkflowOverHTTP(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}

	proj := owner.want(201, "POST", v1+"/projects", `{"name":"Shop","description":"The shop","repository":"https://github.com/acme/shop"}`)
	pid := str(proj, "id")
	p := v1 + "/projects/" + pid

	// The owner invites a developer with a link; the developer needs no account first.
	inv := owner.want(201, "POST", p+"/invites", `{"role":"member","maxUses":2}`)
	code := str(inv, "code")
	if !strings.HasPrefix(code, "wbi_") {
		t.Fatalf("%v", inv)
	}
	anon := client{t: t, base: ts.URL}
	anon.want(400, "POST", v1+"/invites/redeem", fmt.Sprintf(`{"code":%q,"name":" "}`, code))
	anon.want(404, "POST", v1+"/invites/redeem", `{"code":"wbi_nope","name":"Bo"}`)
	joined := anon.want(201, "POST", v1+"/invites/redeem", fmt.Sprintf(`{"code":%q,"name":"Bo","email":"bo@example.com"}`, code))
	bo := client{t: t, base: ts.URL, token: str(joined, "token")}
	boID := str(sub(joined, "member"), "id")
	cyJoined := anon.want(201, "POST", v1+"/invites/redeem", fmt.Sprintf(`{"code":%q,"name":"Cy"}`, code))
	cy := client{t: t, base: ts.URL, token: str(cyJoined, "token")}
	anon.want(404, "POST", v1+"/invites/redeem", fmt.Sprintf(`{"code":%q,"name":"Di"}`, code)) // used up
	// The listing of invites never shows the code.
	if _, _, raw := owner.do("GET", p+"/invites", nil); strings.Contains(string(raw), code) {
		t.Fatalf("an invite listing shows the code: %s", raw)
	}
	bo.want(403, "GET", p+"/invites", nil)
	bo.want(403, "POST", p+"/invites", `{}`)

	// Tickets.
	k := owner.want(201, "POST", p+"/tickets", `{"title":"Authentication error","description":"Login fails","requirements":"Keep sessions","status":"available"}`)
	tid := str(k, "id")
	tk := p + "/tickets/" + tid
	if str(k, "key") != "WB-1" || str(k, "status") != "available" {
		t.Fatalf("%v", k)
	}
	bo.want(400, "POST", p+"/tickets", `{"title":""}`)
	bo.want(400, "POST", p+"/tickets", `{"title":"x","bogus":1}`) // unknown fields are refused

	// Two developers race for it: one wins, one is told who has it.
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i, c := range []client{bo, cy} {
		wg.Add(1)
		go func() { defer wg.Done(); codes[i], _, _ = c.do("POST", tk+"/claim", nil) }()
	}
	wg.Wait()
	if !((codes[0] == 200 && codes[1] == 409) || (codes[0] == 409 && codes[1] == 200)) {
		t.Fatalf("claim statuses %v", codes)
	}
	holder, other := bo, cy
	if codes[1] == 200 {
		holder, other = cy, bo
	}
	board := owner.want(200, "GET", p+"/board", nil)
	tickets := board["tickets"].([]any)
	got := tickets[0].(map[string]any)
	if got["status"] != "in_progress" || got["assigneeId"] == "" || str(got, "branch") != "wb-1-authentication-error" {
		t.Fatalf("%v", got)
	}
	if len(board["statuses"].([]any)) != 5 || board["role"] != "owner" {
		t.Fatalf("%v", board)
	}

	// Only the holder opens it in their runner, and what they get carries no machine or credential.
	other.want(403, "POST", tk+"/handoff", nil)
	h := holder.want(200, "POST", tk+"/handoff", nil)
	if h["schema"] != "werkbord-team.handoff/v1" || str(sub(h, "git"), "branch") != "wb-1-authentication-error" || str(sub(h, "git"), "repository") != "https://github.com/acme/shop" {
		t.Fatalf("%v", h)
	}
	if !strings.Contains(str(h, "prompt"), "Keep sessions") {
		t.Fatalf("%v", h["prompt"])
	}

	// The holder reports Git work; somebody else cannot.
	report := `{"commits":[{"sha":"0123456","subject":"Fix it","author":"Bo","committedAt":"2026-06-01T10:00:00Z"}],
	  "pullRequest":{"number":9,"url":"https://github.com/acme/shop/pull/9","mergeable":"mergeable","baseBranch":"main","behind":4,"ahead":1},
	  "state":{"headSha":"0123456789abcdef","baseBranch":"main","ahead":1,"behind":4,"lastCommitAt":"2026-06-01T10:00:00Z","files":["auth.go"]}}`
	other.want(403, "PUT", tk+"/git", report)
	holder.want(400, "PUT", tk+"/git", `{"pullRequest":{"url":"http://github.com/x/y/pull/1"}}`)
	r := holder.want(200, "PUT", tk+"/git", report)
	if pr := sub(r, "pullRequest"); pr["number"] != float64(9) || pr["behind"] != float64(4) {
		t.Fatalf("%v", r)
	}
	repo := cy.want(200, "GET", p+"/repository", nil)
	if len(repo["pullRequests"].([]any)) != 1 || len(repo["attention"].([]any)) == 0 {
		t.Fatalf("%v", repo)
	}

	// Submit for review; the open PR keeps it from being completed.
	holder.want(200, "POST", tk+"/submit", `{}`)
	owner.want(409, "POST", tk+"/complete", nil)
	other.want(403, "POST", tk+"/complete", nil)
	holder.want(200, "PUT", tk+"/git", `{"pullRequest":{"url":"https://github.com/acme/shop/pull/9","state":"merged"}}`)
	done := owner.want(200, "POST", tk+"/complete", nil)
	if done["status"] != "done" {
		t.Fatalf("%v", done)
	}

	acts, _, raw := owner.do("GET", p+"/activity?limit=100", nil)
	if acts != 200 {
		t.Fatal(string(raw))
	}
	for _, kind := range []string{"ticket.created", "ticket.claimed", "ticket.handed_off", "ticket.pull_request_created", "ticket.work_submitted", "ticket.review_requested", "ticket.completed", "project.member_joined"} {
		if !strings.Contains(string(raw), `"`+kind+`"`) {
			t.Errorf("no %s in the activity", kind)
		}
	}
	// None of it carries a token or hash.
	for _, banned := range []string{"wbt_", "wbi_", "tokenHash", "codeHash"} {
		for _, body := range []string{string(raw)} {
			if strings.Contains(body, banned) {
				t.Errorf("the activity contains %q", banned)
			}
		}
	}
	_ = boID
}

func TestSyncLongPollWakesOnAnotherMembersChange(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	pid := str(owner.want(201, "POST", v1+"/projects", `{"name":"Shop"}`), "id")
	p := v1 + "/projects/" + pid
	rev := owner.want(200, "GET", p+"/board", nil)["revision"].(float64)

	if m := owner.want(200, "GET", fmt.Sprintf("%s/sync?since=%d", p, int64(rev)), nil); m["changed"] != false {
		t.Fatalf("%v", m)
	}
	done := make(chan map[string]any, 1)
	go func() {
		_, m, _ := owner.do("GET", fmt.Sprintf("%s/sync?since=%d&wait=10", p, int64(rev)), nil)
		done <- m
	}()
	time.Sleep(100 * time.Millisecond)
	owner.want(201, "POST", p+"/tickets", `{"title":"New"}`)
	select {
	case m := <-done:
		if m["changed"] != true || m["revision"].(float64) <= rev {
			t.Fatalf("%v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the long poll did not return on a change")
	}
}

func TestWorkRoutesAreAuthenticatedAndScoped(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	anon := client{t: t, base: ts.URL}
	pid := str(owner.want(201, "POST", v1+"/projects", `{"name":"Shop"}`), "id")
	p := v1 + "/projects/" + pid
	tid := str(owner.want(201, "POST", p+"/tickets", `{"title":"T","status":"available"}`), "id")
	outsider := client{t: t, base: ts.URL, token: str(owner.want(201, "POST", v1+"/members", `{"name":"Eve"}`), "token")}

	for _, route := range [][2]string{
		{"GET", p + "/board"}, {"GET", p + "/people"}, {"GET", p + "/sync"}, {"GET", p + "/activity"}, {"GET", p + "/repository"},
		{"POST", p + "/tickets"}, {"GET", p + "/tickets/" + tid}, {"POST", p + "/tickets/" + tid + "/claim"}, {"POST", p + "/tickets/" + tid + "/handoff"},
		{"PUT", p + "/tickets/" + tid + "/git"}, {"GET", p + "/invites"}, {"POST", p + "/invites/join"}, {"POST", v1 + "/invites/join"},
	} {
		body := "{}"
		if route[1] == p+"/tickets" {
			body = `{"title":"x"}`
		}
		anon.want(401, route[0], route[1], body)
		// A member who is not on the project cannot even tell it exists.
		if route[1] == v1+"/invites/join" {
			continue
		}
		if c, m, _ := outsider.do(route[0], route[1], body); c != 404 {
			t.Errorf("%s %s as an outsider: %d %v", route[0], route[1], c, m)
		}
	}
	// Redeeming is public; joining needs a sign-in, and a bad code is a plain 404.
	outsider.want(404, "POST", v1+"/invites/join", `{"code":"wbi_nope"}`)
	inv := owner.want(201, "POST", p+"/invites", `{"maxUses":2}`)
	outsider.want(200, "POST", v1+"/invites/join", fmt.Sprintf(`{"code":%q}`, str(inv, "code")))
	outsider.want(200, "GET", p+"/board", nil)
	outsider.want(409, "POST", v1+"/invites/join", fmt.Sprintf(`{"code":%q}`, str(inv, "code")))
	// A new member cannot manage the project.
	outsider.want(403, "POST", p+"/tickets/"+tid+"/assign", `{"memberId":"x"}`)
	outsider.want(403, "PUT", p+"/members/"+str(sub(owner.want(200, "GET", "/api/team/v1/me", nil), "member"), "id"), `{"role":"owner"}`)
}

// The views that span projects need a token like everything else, are scoped to
// the signed-in member, and the sync tells a client what it missed.
func TestCrossProjectViewsAndSyncOverHTTP(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	anon := client{t: t, base: ts.URL}
	for _, path := range []string{"/overview", "/my-work", "/reviews", "/sync"} {
		anon.want(401, "GET", v1+path, nil)
		(client{t: t, base: ts.URL, token: "wbt_" + strings.Repeat("0", 64)}).want(401, "GET", v1+path, nil)
	}
	proj := owner.want(201, "POST", v1+"/projects", `{"name":"Shop","repository":"https://github.com/acme/shop"}`)
	pid := str(proj, "id")
	bo := client{t: t, base: ts.URL, token: str(owner.want(201, "POST", v1+"/members", `{"name":"Bo"}`), "token")}

	// Bo is on no project yet: nothing to see, and no way to see the project through the views.
	for _, path := range []string{"/my-work", "/reviews", "/overview"} {
		m := bo.want(200, "GET", v1+path, nil)
		if raw := fmt.Sprint(m); strings.Contains(raw, "Shop") && path != "/overview" {
			t.Fatalf("%s leaks a project Bo is not on: %v", path, m)
		}
	}
	if list, _ := bo.want(200, "GET", v1+"/overview", nil)["projects"].([]any); len(list) != 0 {
		t.Fatalf("overview shows a project Bo is not on: %v", list)
	}

	// The sync starts with no events, then reports what happened after the cursor it was given.
	start := owner.want(200, "GET", v1+"/sync?wait=0", nil)
	rev, cursor := start["revision"].(float64), start["cursor"].(float64)
	k := owner.want(201, "POST", v1+"/projects/"+pid+"/tickets", `{"title":"A ticket","status":"available"}`)
	res := owner.want(200, "GET", fmt.Sprintf("%s/sync?since=%d&after=%d&wait=0", v1, int64(rev), int64(cursor)), nil)
	evs, _ := res["events"].([]any)
	if res["changed"] != true || len(evs) != 1 || str(evs[0].(map[string]any), "kind") != "ticket.created" || str(evs[0].(map[string]any), "ticketKey") != str(k, "key") {
		t.Fatalf("%v", res)
	}
	// Without a cursor there are no events, only the revision; and a nonsense number is just "no cursor".
	res = owner.want(200, "GET", fmt.Sprintf("%s/sync?since=%d&after=nonsense&wait=0", v1, int64(rev)), nil)
	if evs, _ := res["events"].([]any); len(evs) != 0 || res["changed"] != true {
		t.Fatalf("%v", res)
	}
	// A client ahead of the server is told to start again.
	res = owner.want(200, "GET", v1+"/sync?since=999999&wait=0", nil)
	if res["reset"] != true {
		t.Fatalf("%v", res)
	}
	// A long poll is woken by someone else's change, well before its wait is over.
	rev2, cur2 := res["revision"].(float64), res["cursor"].(float64)
	done := make(chan map[string]any, 1)
	began := time.Now()
	go func() {
		_, m, _ := owner.do("GET", fmt.Sprintf("%s/sync?since=%d&after=%d&wait=15", v1, int64(rev2), int64(cur2)), nil)
		done <- m
	}()
	time.Sleep(100 * time.Millisecond)
	owner.want(200, "POST", v1+"/projects/"+pid+"/tickets/"+str(k, "id")+"/claim", nil)
	select {
	case m := <-done:
		if m["changed"] != true || time.Since(began) > 3*time.Second {
			t.Fatalf("%v after %s", m, time.Since(began))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the long poll was not woken")
	}
}

// Too many open sync requests for one member are refused with a 429 and a Retry-After.
func TestTooManyOpenSyncRequestsAreRefused(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	start := owner.want(200, "GET", v1+"/sync?wait=0", nil)
	rev, cursor := int64(start["revision"].(float64)), int64(start["cursor"].(float64))
	var wg sync.WaitGroup
	codes := make(chan int, 16)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _, _ := owner.do("GET", fmt.Sprintf("%s/sync?since=%d&after=%d&wait=1", v1, rev, cursor), nil)
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	busy := 0
	for c := range codes {
		if c == 429 {
			busy++
		}
	}
	if busy == 0 {
		t.Fatal("twelve simultaneous waits were all accepted")
	}
	// A member who is not holding any open can ask again at once.
	owner.want(200, "GET", v1+"/sync?wait=0", nil)
}

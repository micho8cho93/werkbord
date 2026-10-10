package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/server"
)

// This file is the whole V2 story in one test, over real HTTP:
//
//	Developer A creates a project → invites Developer B → the owner creates tickets
//	→ B claims one → it is unavailable to A → B takes its handoff to B's own Werkbord
//	→ B works (branch, commits, pull request reported) → the ticket enters Review
//	→ A reviews → the merge happens on the Git host → the ticket is Done
//
// with every member's view kept current by the sync, and a race for one ticket.

type person struct {
	t     *testing.T
	base  string
	token string
}

func (p person) call(method, path string, body any) (int, map[string]any) {
	p.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.base+"/api/team/v1"+path, rd)
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m
}

func (p person) ok(method, path string, body any) map[string]any {
	p.t.Helper()
	code, m := p.call(method, path, body)
	if code < 200 || code > 299 {
		p.t.Fatalf("%s %s = %d %v", method, path, code, m)
	}
	return m
}

func (p person) status(method, path string, body any) int {
	p.t.Helper()
	code, _ := p.call(method, path, body)
	return code
}

func list(m map[string]any, k string) []map[string]any {
	var out []map[string]any
	if a, ok := m[k].([]any); ok {
		for _, v := range a {
			if o, ok := v.(map[string]any); ok {
				out = append(out, o)
			}
		}
	}
	return out
}
func obj(m map[string]any, k string) map[string]any { o, _ := m[k].(map[string]any); return o }
func text(m map[string]any, k string) string        { s, _ := m[k].(string); return s }

// watcher keeps one workspace sync open for a person, the way a console does, and
// collects the events it is told about.
type watcher struct {
	mu     sync.Mutex
	events []string
	stop   context.CancelFunc
	done   chan struct{}
}

func watch(t *testing.T, p person) *watcher {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w := &watcher{stop: cancel, done: make(chan struct{})}
	start := p.ok("GET", "/sync?wait=0", nil)
	rev, cursor := int64(start["revision"].(float64)), int64(start["cursor"].(float64))
	go func() {
		defer close(w.done)
		for ctx.Err() == nil {
			req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/team/v1/sync?since=%d&after=%d&wait=5", p.base, rev, cursor), nil)
			req.Header.Set("Authorization", "Bearer "+p.token)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.NewDecoder(res.Body).Decode(&m)
			res.Body.Close()
			if m["changed"] != true {
				continue
			}
			rev, cursor = int64(m["revision"].(float64)), int64(m["cursor"].(float64))
			w.mu.Lock()
			for _, e := range list(m, "events") {
				w.events = append(w.events, text(e, "actorName")+" "+text(e, "kind")+" "+text(e, "ticketKey"))
			}
			w.mu.Unlock()
		}
	}()
	t.Cleanup(func() { cancel(); <-w.done })
	return w
}

func (w *watcher) saw(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		for _, e := range w.events {
			if e == want {
				w.mu.Unlock()
				return
			}
		}
		w.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	t.Fatalf("never told %q; was told %v", want, w.events)
}

func TestTheWholeTeamWorkflowFromProjectToDone(t *testing.T) {
	// A Team server, and a stand-in for Developer B's own Werkbord (on B's computer).
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, svc, err := server.Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ts := httptest.NewServer(server.Handler(db, svc, nil, "test"))
	t.Cleanup(ts.Close)
	created, err := svc.CreateWorkspace(context.Background(), "Acme", "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	a := person{t, ts.URL, created.Token}

	// 1. A creates the project, and invites B with a link.
	proj := a.ok("POST", "/projects", map[string]any{"name": "Shop", "description": "The shop", "repository": "https://github.com/acme/shop"})
	pid := text(proj, "id")
	p := "/projects/" + pid
	invite := a.ok("POST", p+"/invites", map[string]any{"role": "member", "maxUses": 1})
	anon := person{t, ts.URL, ""}
	joined := anon.ok("POST", "/invites/redeem", map[string]any{"code": text(invite, "code"), "name": "Bo"})
	b := person{t, ts.URL, text(joined, "token")}
	if b.token == "" {
		t.Fatal("B got no token")
	}
	// A third developer, Cy, is invited for the race below.
	cyInvite := a.ok("POST", p+"/invites", map[string]any{"role": "member"})
	cy := person{t, ts.URL, text(anon.ok("POST", "/invites/redeem", map[string]any{"code": text(cyInvite, "code"), "name": "Cy"}), "token")}

	// Everyone's views are open from now on.
	aSees, bSees := watch(t, a), watch(t, b)

	// 2. The owner creates tickets.
	var tid, tid2 string
	for i, title := range []string{"Authentication error", "Add audit log"} {
		k := a.ok("POST", p+"/tickets", map[string]any{"title": title, "description": "Details of " + title, "requirements": "Keep sessions", "status": "available"})
		if i == 0 {
			tid = text(k, "id")
		} else {
			tid2 = text(k, "id")
		}
	}
	bSees.saw(t, "Ada ticket.created WB-1")
	tk := p + "/tickets/" + tid

	// 3. B claims it. A, who is also a project owner, can no longer claim it.
	claimed := b.ok("POST", tk+"/claim", nil)
	if text(claimed, "status") != "in_progress" || text(claimed, "branch") != "wb-1-authentication-error" {
		t.Fatalf("%v", claimed)
	}
	if code, m := a.call("POST", tk+"/claim", nil); code != 409 || !strings.Contains(fmt.Sprint(m), "already claimed by Bo") {
		t.Fatalf("A could claim B's ticket: %d %v", code, m)
	}
	aSees.saw(t, "Bo ticket.claimed WB-1") // A's view heard about it without asking

	// ...and simultaneously: B, A and Cy all go for the second ticket. Exactly one gets it.
	var wg sync.WaitGroup
	start := make(chan struct{})
	var mu sync.Mutex
	winners, losers := 0, 0
	for _, who := range []person{a, b, cy} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			code, _ := who.call("POST", p+"/tickets/"+tid2+"/claim", nil)
			mu.Lock()
			defer mu.Unlock()
			switch code {
			case 200:
				winners++
			case 409:
				losers++
			default:
				t.Errorf("claim answered %d", code)
			}
		}()
	}
	close(start)
	wg.Wait()
	if winners != 1 || losers != 2 {
		t.Fatalf("%d winners, %d losers", winners, losers)
	}
	// Put it back, so the rest of the story is about one ticket.
	a.ok("POST", p+"/tickets/"+tid2+"/release", nil)

	// 4. B's My Work shows it; A's does not.
	mine := b.ok("GET", "/my-work", nil)
	if ip := list(mine, "inProgress"); len(ip) != 1 || text(obj(ip[0], "ticket"), "key") != "WB-1" || text(obj(ip[0], "project"), "name") != "Shop" {
		t.Fatalf("B's My Work: %v", mine)
	}
	if ap := a.ok("GET", "/my-work", nil); len(list(ap, "inProgress")) != 0 {
		t.Fatalf("A's My Work shows B's ticket: %v", ap)
	}

	// 5. B takes the ticket's context into B's own Werkbord. Team only hands over text (the ticket's key, branch and prompt);
	// it holds no credential and starts nothing. Importing it into B's Werkbord is B's computer's business (the connector).
	hf := b.ok("POST", tk+"/handoff", nil)
	if hf["schema"] != "werkbord-team.handoff/v1" || !strings.Contains(fmt.Sprint(hf["prompt"]), "wb-1-authentication-error") || !strings.Contains(fmt.Sprint(hf["ticket"]), "WB-1") {
		t.Fatalf("the handoff does not carry the ticket's context: %v", hf)
	}
	// A cannot open B's ticket, anywhere.
	if code := a.status("POST", tk+"/handoff", nil); code < 400 {
		t.Fatalf("A opened B's ticket in a runner: %d", code)
	}

	// 6. B works. B's Werkbord reports the branch, a commit and the pull request.
	at := time.Now().UTC().Format(time.RFC3339)
	b.ok("PUT", tk+"/git", map[string]any{
		"branch":      "wb-1-authentication-error",
		"commits":     []map[string]any{{"sha": "0123456789abcdef0123456789abcdef01234567", "subject": "Fix the token check", "author": "Bo", "committedAt": at}},
		"pullRequest": map[string]any{"number": 12, "url": "https://github.com/acme/shop/pull/12", "state": "open", "mergeable": "mergeable", "baseBranch": "main", "behind": 0, "ahead": 1},
		"state":       map[string]any{"headSha": "0123456789abcdef0123456789abcdef01234567", "baseBranch": "main", "ahead": 1, "behind": 0, "lastCommitAt": at, "files": []string{"auth/token.go"}},
	})
	mine = b.ok("GET", "/my-work", nil)
	if prs := list(mine, "pullRequests"); len(prs) != 1 || text(obj(prs[0], "links"), "branch") != "https://github.com/acme/shop/tree/wb-1-authentication-error" {
		t.Fatalf("B's open pull requests: %v", mine["pullRequests"])
	}
	// 7. B submits it; it enters Review.
	submitted := b.ok("POST", tk+"/submit", map[string]any{})
	if text(submitted, "status") != "review" {
		t.Fatalf("%v", submitted)
	}
	aSees.saw(t, "Bo ticket.review_requested WB-1")

	// 8. A reviews: the queue shows ticket, author, branch, pull request, commits and mergeability.
	q := a.ok("GET", "/reviews", nil)
	items := list(q, "items")
	if len(items) != 1 {
		t.Fatalf("A's review queue: %v", q)
	}
	it := items[0]
	k := obj(it, "ticket")
	if text(k, "key") != "WB-1" || text(it, "author") != "Bo" || text(k, "branch") != "wb-1-authentication-error" || text(it, "merge") != "mergeable" ||
		len(list(k, "commits")) != 1 || text(obj(k, "pullRequest"), "url") != "https://github.com/acme/shop/pull/12" || it["canComplete"] != false {
		t.Fatalf("%v", it)
	}
	// The merge has not happened yet: Team will not call it done.
	if code, m := a.call("POST", tk+"/complete", nil); code != 409 || !strings.Contains(fmt.Sprint(m), "still open") {
		t.Fatalf("%d %v", code, m)
	}
	// B cannot sign off B's own work either.
	if code := b.status("POST", tk+"/complete", nil); code != 403 {
		t.Fatalf("B completed own ticket: %d", code)
	}

	// 9. The merge happens on the Git host; A records it. B's My Work says so.
	a.ok("PUT", tk+"/git", map[string]any{"pullRequest": map[string]any{"number": 12, "url": "https://github.com/acme/shop/pull/12", "state": "merged"}})
	bSees.saw(t, "Ada ticket.pull_request_merged WB-1")
	if n := b.ok("GET", "/my-work", nil); text(list(n, "needsAction")[0], "kind") != "merged" {
		t.Fatalf("%v", n["needsAction"])
	}
	if it := list(a.ok("GET", "/reviews", nil), "items")[0]; it["canComplete"] != true {
		t.Fatalf("%v", it)
	}

	// 10. A completes it. The ticket is Done, and the views agree.
	done := a.ok("POST", tk+"/complete", nil)
	if text(done, "status") != "done" || done["completedAt"] == nil {
		t.Fatalf("%v", done)
	}
	bSees.saw(t, "Ada ticket.completed WB-1")
	if bm := b.ok("GET", "/my-work", nil); len(list(bm, "inProgress"))+len(list(bm, "submitted")) != 0 {
		t.Fatalf("a finished ticket is still in B's work: %v", bm)
	}
	if aq := a.ok("GET", "/reviews", nil); len(list(aq, "items")) != 0 {
		t.Fatalf("a finished ticket is still in the review queue: %v", aq)
	}
	ov := a.ok("GET", "/overview", nil)
	counts := obj(list(ov, "projects")[0], "counts")
	if counts["done"] != float64(1) || counts["available"] != float64(1) || counts["in_progress"] != float64(0) || counts["review"] != float64(0) {
		t.Fatalf("the overview disagrees: %v", counts)
	}

	// 11. The history tells the whole story, in order.
	req, _ := http.NewRequest("GET", ts.URL+"/api/team/v1"+p+"/activity?limit=100", nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var acts []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&acts); err != nil {
		t.Fatal(err)
	}
	var kinds []string // oldest first
	for i := len(acts) - 1; i >= 0; i-- {
		if text(acts[i], "ticketKey") == "WB-1" {
			kinds = append(kinds, text(acts[i], "kind"))
		}
	}
	want := "ticket.created ticket.claimed ticket.handed_off ticket.pull_request_created ticket.work_submitted ticket.review_requested ticket.pull_request_merged ticket.completed"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("history\n got %s\nwant %s", got, want)
	}
}

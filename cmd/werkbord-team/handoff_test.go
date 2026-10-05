package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devboard/internal/team/config"
	"devboard/internal/team/server"
	"devboard/internal/team/service"
)

const localToken = "local-werkbord-token-not-a-team-token"

// handoffWorld is a Team server with a project and a ticket claimed by Bo, and a
// stand-in for Bo's own local Werkbord.
type handoffWorld struct {
	teamURL  string
	boToken  string
	cyToken  string
	local    *httptest.Server
	mu       sync.Mutex
	localReq []*http.Request
	localBod []map[string]any
}

func newHandoffWorld(t *testing.T, repo string, remotes ...string) *handoffWorld {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, svc, err := server.Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ts := httptest.NewServer(server.Handler(db, svc, nil, "test"))
	t.Cleanup(ts.Close)

	ctx := context.Background()
	c, _ := svc.CreateWorkspace(ctx, "Acme", "Ada", "")
	owner, _ := svc.Authenticate(ctx, c.Token)
	bo, _ := svc.AddMember(ctx, owner, "Bo", "", "")
	cy, _ := svc.AddMember(ctx, owner, "Cy", "", "")
	p, err := svc.CreateProject(ctx, owner, service.ProjectInput{Name: "Shop", Repository: repo, Description: "The shop"})
	if err != nil {
		t.Fatal(err)
	}
	boA, _ := svc.Authenticate(ctx, bo.Token)
	cyA, _ := svc.Authenticate(ctx, cy.Token)
	_ = svc.AddProjectMember(ctx, owner, p.ID, boA.Member.ID, "")
	_ = svc.AddProjectMember(ctx, owner, p.ID, cyA.Member.ID, "")
	k, err := svc.CreateTicket(ctx, owner, p.ID, service.TicketInput{Title: "Authentication error", Description: "Login fails", Requirements: "Keep sessions", Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimTicket(ctx, boA, p.ID, k.ID); err != nil {
		t.Fatal(err)
	}

	w := &handoffWorld{teamURL: ts.URL, boToken: bo.Token, cyToken: cy.Token}
	w.local = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.localReq = append(w.localReq, r)
		if r.Header.Get("Authorization") != "Bearer "+localToken {
			http.Error(rw, `{"error":{"message":"unauthorized"}}`, 401)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			var projects []map[string]any
			for i, u := range remotes {
				projects = append(projects, map[string]any{"id": "prj_" + string(rune('a'+i)), "name": "local",
					"repository": map[string]any{"remotes": []map[string]any{{"name": "origin", "url": u}}}})
			}
			projects = append(projects, map[string]any{"id": "prj_norepo", "name": "no repo"})
			_ = json.NewEncoder(rw).Encode(map[string]any{"projects": projects})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/tasks"):
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			b["path"] = r.URL.Path
			w.localBod = append(w.localBod, b)
			rw.WriteHeader(201)
			_ = json.NewEncoder(rw).Encode(map[string]any{"id": "tsk_local1"})
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(w.local.Close)
	return w
}

func (w *handoffWorld) env(token string) map[string]string {
	return map[string]string{"WERKBORD_TEAM_SERVER": w.teamURL, "WERKBORD_TEAM_TOKEN": token, "DEVBOARD_TOKEN": localToken}
}

func TestHandoffPrintsTheTicketContextForTheHolder(t *testing.T) {
	w := newHandoffWorld(t, "https://github.com/acme/shop")
	out, _, err := runCLI(t, w.env(w.boToken), "handoff", "--ticket", "wb-1")
	if err != nil {
		t.Fatal(err)
	}
	var h map[string]any
	if err := json.Unmarshal([]byte(out), &h); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	git := h["git"].(map[string]any)
	if h["schema"] != "werkbord-team.handoff/v1" || git["branch"] != "wb-1-authentication-error" || git["repository"] != "https://github.com/acme/shop" {
		t.Fatalf("%v", h)
	}
	for _, banned := range []string{w.boToken, w.cyToken, localToken} {
		if strings.Contains(out, banned) {
			t.Fatal("a token is in the handoff")
		}
	}
	// --prompt prints the task text alone; --out writes a private file.
	out, _, err = runCLI(t, w.env(w.boToken), "handoff", "--ticket", "WB-1", "--prompt")
	if err != nil || !strings.HasPrefix(out, "WB-1: Authentication error") || !strings.Contains(out, "Keep sessions") {
		t.Fatalf("%q %v", out, err)
	}
	file := filepath.Join(t.TempDir(), "h.json")
	if _, _, err := runCLI(t, w.env(w.boToken), "handoff", "--ticket", "WB-1", "--out", file); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(file); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", st, err)
	}
	// Nobody who does not hold the ticket gets one.
	if _, _, err := runCLI(t, w.env(w.cyToken), "handoff", "--ticket", "WB-1"); err == nil || !strings.Contains(err.Error(), "only the member who holds") {
		t.Fatalf("%v", err)
	}
}

func TestHandoffCreatesATaskInTheDevelopersOwnWerkbord(t *testing.T) {
	// The local project's remote is the same repository written differently.
	w := newHandoffWorld(t, "https://github.com/acme/shop.git", "git@github.com:other/thing.git", "git@GitHub.com:acme/shop")
	env := w.env(w.boToken)
	out, _, err := runCLI(t, env, "handoff", "--ticket", "WB-1", "--runner", w.local.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tsk_local1") || !strings.Contains(out, "wb-1-authentication-error") {
		t.Fatalf("%s", out)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.localBod) != 1 {
		t.Fatalf("%d tasks created", len(w.localBod))
	}
	task := w.localBod[0]
	if task["path"] != "/api/projects/prj_b/tasks" || task["title"] != "WB-1: Authentication error" || !strings.Contains(task["description"].(string), "Keep sessions") {
		t.Fatalf("%v", task)
	}
	// The local Werkbord only ever saw its own token; Team's token never went there.
	for _, r := range w.localReq {
		if got := r.Header.Get("Authorization"); got != "Bearer "+localToken {
			t.Errorf("the local Werkbord was sent %q", got)
		}
	}
}

func TestHandoffOnlyEverGoesToThisComputer(t *testing.T) {
	w := newHandoffWorld(t, "https://github.com/acme/shop", "https://github.com/acme/shop")
	for _, runner := range []string{"http://example.com:7420", "https://203.0.113.9", "http://192.168.1.20:7420", "http://[2001:db8::1]:7420", "ftp://127.0.0.1", "http://user:pw@127.0.0.1:1"} {
		_, _, err := runCLI(t, w.env(w.boToken), "handoff", "--ticket", "WB-1", "--runner", runner)
		if err == nil {
			t.Errorf("--runner %s was accepted", runner)
		}
	}
	w.mu.Lock()
	n := len(w.localReq)
	w.mu.Unlock()
	if n != 0 {
		t.Fatal("the local stand-in was called")
	}
	// localhost forms are fine.
	for _, ok := range []string{"http://localhost:7420", "http://127.0.0.1:7420", "http://[::1]:7420", "http://127.0.0.2:1"} {
		u, err := parseBase(ok)
		if err != nil || !isLoopbackHost(u) {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

func TestHandoffExplainsWhatIsMissing(t *testing.T) {
	w := newHandoffWorld(t, "https://github.com/acme/shop", "https://github.com/someone/else")
	for name, c := range map[string]struct {
		env  map[string]string
		args []string
		want string
	}{
		"no ticket":            {w.env(w.boToken), []string{"handoff"}, "--ticket is required"},
		"no team token":        {map[string]string{"WERKBORD_TEAM_SERVER": w.teamURL, "WERKBORD_TEAM_TOKEN": ""}, []string{"handoff", "--ticket", "WB-1"}, "WERKBORD_TEAM_TOKEN"},
		"unknown ticket":       {w.env(w.boToken), []string{"handoff", "--ticket", "WB-99"}, "no ticket"},
		"unknown project":      {w.env(w.boToken), []string{"handoff", "--ticket", "WB-1", "--project", "Nope"}, "no project"},
		"bad team token":       {w.env("wbt_" + strings.Repeat("0", 64)), []string{"handoff", "--ticket", "WB-1"}, "401"},
		"no local token":       {map[string]string{"WERKBORD_TEAM_SERVER": w.teamURL, "WERKBORD_TEAM_TOKEN": w.boToken, "DEVBOARD_TOKEN": ""}, []string{"handoff", "--ticket", "WB-1", "--runner", w.local.URL}, "DEVBOARD_TOKEN"},
		"no matching project":  {w.env(w.boToken), []string{"handoff", "--ticket", "WB-1", "--runner", w.local.URL}, "none of your local Werkbord projects"},
		"a bad server address": {w.env(w.boToken), []string{"handoff", "--ticket", "WB-1", "--server", "nonsense"}, "--server"},
	} {
		_, _, err := runCLI(t, c.env, c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (want %q)", name, err, c.want)
		}
	}
	// An explicit local project skips the matching.
	if _, _, err := runCLI(t, w.env(w.boToken), "handoff", "--ticket", "WB-1", "--runner", w.local.URL, "--local-project", "prj_a"); err != nil {
		t.Fatal(err)
	}
}

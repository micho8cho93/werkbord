package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/workspace"
)

var bg = context.Background()

// ---- fakes ----

type fake struct {
	name    string
	listed  []workspace.Listed
	listErr error
	sumErr  map[string]error
	slow    map[string]time.Duration
	lists   atomic.Int32
	frames  atomic.Int32
	sums    atomic.Int32
}

func (f *fake) Name() string { return f.name }
func (f *fake) List(context.Context) ([]workspace.Listed, error) {
	f.lists.Add(1)
	return f.listed, f.listErr
}
func (f *fake) Frame(_ context.Context, l workspace.Listed, join string) (Frame, error) {
	f.frames.Add(1)
	u := "http://127.0.0.1:1" + l.Root + "#token=T"
	if join != "" {
		u += "&join=" + join
	}
	return Frame{URL: u, Origin: "http://127.0.0.1:1"}, nil
}
func (f *fake) Summary(ctx context.Context, l workspace.Listed) (workspace.Summary, error) {
	f.sums.Add(1)
	if d := f.slow[l.Entry.ID]; d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return workspace.Summary{}, ctx.Err()
		}
	}
	if err := f.sumErr[l.Entry.ID]; err != nil {
		return workspace.Summary{}, err
	}
	return workspace.Summary{Schema: workspace.Schema, Workspace: l.Entry}, nil
}

func personalListed(state workspace.State) workspace.Listed {
	return workspace.Listed{Entry: workspace.Entry{ID: workspace.PersonalID, Kind: workspace.KindPersonal, Name: "Personal", State: state}, Root: "/", SummaryPath: "/api/workspace/v1/summary"}
}

func team(slot, name string, state workspace.State) workspace.Listed {
	return workspace.Listed{Entry: workspace.Entry{ID: workspace.TeamID(slot), Kind: workspace.KindTeam, Name: name, State: state, Role: "member"}, Root: "/w/" + slot + "/", SummaryPath: "/w/" + slot + "/api/device/v1/summary"}
}

func registry(t *testing.T, personal, teams *fake) (*Registry, *State) {
	t.Helper()
	st := OpenState(filepath.Join(t.TempDir(), "shell.json"))
	return NewRegistry(st, nil, personal, teams), st
}

// ---- tests ----

func TestPersonalIsAlwaysThereAndTeamsComeAndGoWithoutIt(t *testing.T) {
	p := &fake{name: "Werkbord", listed: []workspace.Listed{personalListed(workspace.StateReady)}}
	tm := &fake{name: "Team", listErr: ErrNotRunning}
	r, _ := registry(t, p, tm)
	v := r.Refresh(bg)
	if len(v.Items) != 1 || v.Items[0].ID != workspace.PersonalID || v.Selected != workspace.PersonalID {
		t.Fatalf("%+v", v)
	}
	if len(v.Problems) != 1 || v.Problems[0].Kind != "not_running" || v.Problems[0].Source != "Team" {
		t.Fatalf("problems = %+v", v.Problems)
	}
	// The Team service comes up with two workspaces.
	tm.listErr, tm.listed = nil, []workspace.Listed{team("main", "Acme", workspace.StateReady), team("ws_2", "Globex", workspace.StateOffline)}
	v = r.Refresh(bg)
	if len(v.Items) != 3 || v.Items[0].ID != workspace.PersonalID || len(v.Problems) != 0 {
		t.Fatalf("%+v", v)
	}
	// Personal stays when Personal's own controller is down: it is shown as unavailable, never dropped.
	p.listed = []workspace.Listed{personalListed(workspace.StateUnavailable)}
	v = r.Refresh(bg)
	if v.Items[0].ID != workspace.PersonalID || v.Items[0].State != workspace.StateUnavailable {
		t.Fatalf("%+v", v.Items[0])
	}
}

func TestTheLastWorkspaceOpensIfThePersonCanStillGetIntoItElsePersonal(t *testing.T) {
	p := &fake{name: "Werkbord", listed: []workspace.Listed{personalListed(workspace.StateReady)}}
	tm := &fake{name: "Team", listed: []workspace.Listed{team("main", "Acme", workspace.StateReady), team("ws_2", "Globex", workspace.StateReady)}}
	r, st := registry(t, p, tm)
	if v, err := r.Select(bg, "team:ws_2"); err != nil || v.Selected != "team:ws_2" {
		t.Fatalf("%+v %v", v, err)
	}
	// A new run of the app (new registry, same state file) opens it.
	r2 := NewRegistry(OpenState(st.path), nil, p, tm)
	if v := r2.Refresh(bg); v.Selected != "team:ws_2" {
		t.Fatalf("selected = %s", v.Selected)
	}
	// Offline and connecting are still the person's workspaces.
	tm.listed[1] = team("ws_2", "Globex", workspace.StateOffline)
	if v := r2.Refresh(bg); v.Selected != "team:ws_2" {
		t.Fatalf("an offline workspace was abandoned: %s", v.Selected)
	}
	for name, mutate := range map[string]func(){
		"left":         func() { tm.listed[1] = team("ws_2", "Globex", workspace.StateLeaving) },
		"gone":         func() { tm.listed = tm.listed[:1] },
		"service down": func() { tm.listErr = ErrNotRunning },
		"not set up": func() {
			tm.listed = []workspace.Listed{team("main", "Acme", workspace.StateReady), team("ws_2", "Globex", workspace.StateSetup)}
		},
	} {
		tm.listErr, tm.listed = nil, []workspace.Listed{team("main", "Acme", workspace.StateReady), team("ws_2", "Globex", workspace.StateReady)}
		mutate()
		if v := NewRegistry(OpenState(st.path), nil, p, tm).Refresh(bg); v.Selected != workspace.PersonalID {
			t.Errorf("%s: opened %s instead of Personal", name, v.Selected)
		}
	}
	// A state file nobody can read is an empty state, not a failure.
	if err := os.WriteFile(st.path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	tm.listErr, tm.listed = nil, []workspace.Listed{team("main", "Acme", workspace.StateReady)}
	if v := NewRegistry(OpenState(st.path), nil, p, tm).Refresh(bg); v.Selected != workspace.PersonalID {
		t.Fatalf("selected = %s", v.Selected)
	}
}

func TestChoosingAWorkspaceTouchesNothingButTheShellsOwnMemory(t *testing.T) {
	p := &fake{name: "Werkbord", listed: []workspace.Listed{personalListed(workspace.StateReady)}}
	tm := &fake{name: "Team", listed: []workspace.Listed{team("main", "Acme", workspace.StateReady)}}
	r, _ := registry(t, p, tm)
	for _, id := range []string{"team:main", workspace.PersonalID, "team:main"} {
		if _, err := r.Select(bg, id); err != nil {
			t.Fatal(err)
		}
	}
	// Switching only lists. It never opens, summarizes, starts, authorizes or revokes anything in either program, so what an
	// agent that is running was allowed to do is not touched by where the person is looking.
	if p.frames.Load()+tm.frames.Load()+p.sums.Load()+tm.sums.Load() != 0 {
		t.Fatalf("switching reached into a workspace: frames %d/%d summaries %d/%d", p.frames.Load(), tm.frames.Load(), p.sums.Load(), tm.sums.Load())
	}
	if _, err := r.Select(bg, "team:nope"); err == nil {
		t.Fatal("a workspace that is not here was selected")
	}
	tm.listed = []workspace.Listed{team("main", "Acme", workspace.StateLeaving)}
	if _, err := r.Select(bg, "team:main"); err == nil {
		t.Fatal("a workspace being left was selected")
	}
}

func TestSeveralTeamsAreSeparateWorkspacesWithSeparatePlaces(t *testing.T) {
	p := &fake{name: "Werkbord", listed: []workspace.Listed{personalListed(workspace.StateReady)}}
	tm := &fake{name: "Team", listed: []workspace.Listed{team("main", "Acme", workspace.StateReady), team("ws_2", "Globex", workspace.StateReady)}}
	r, _ := registry(t, p, tm)
	r.Refresh(bg)
	if err := r.Remember("team:main", "?tab=board&project=tpj_1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Remember("team:ws_2", "?tab=reviews"); err != nil {
		t.Fatal(err)
	}
	a, _ := r.Target(bg, "team:main", "")
	b, _ := r.Target(bg, "team:ws_2", "")
	if a.Place != "?tab=board&project=tpj_1" || b.Place != "?tab=reviews" || a.URL == b.URL || !strings.Contains(a.URL, "/w/main/") || !strings.Contains(b.URL, "/w/ws_2/") {
		t.Fatalf("%+v %+v", a, b)
	}
	// An invitation goes to the workspace being set up, nowhere else.
	j, _ := r.Target(bg, "team:ws_2", "werkbord://join/abc")
	if !strings.Contains(j.URL, "join=werkbord://join/abc") || strings.Contains(a.URL, "join=") {
		t.Fatalf("%s", j.URL)
	}
	// A page can make the shell remember a place inside itself and nothing else.
	for _, bad := range []string{"https://evil.example/", "//evil", "/x", "javascript:1", "", "#/../x"} {
		if err := r.Remember("team:main", bad); err == nil {
			t.Errorf("%q was remembered", bad)
		}
	}
	if err := r.Remember("team:unknown", "#/x"); err == nil {
		t.Error("a place in a workspace that is not here was remembered")
	}
	if k, _ := r.Kind("team:ws_2"); k != workspace.KindTeam {
		t.Fatal(k)
	}
	if k, _ := r.Kind(workspace.PersonalID); k != workspace.KindPersonal {
		t.Fatal(k)
	}
	if _, ok := r.Kind("team:unknown"); ok {
		t.Fatal("an unknown workspace has a kind")
	}
}

func TestOneWorkspaceThatDoesNotAnswerNeverHidesTheOthers(t *testing.T) {
	p := &fake{name: "Werkbord", listed: []workspace.Listed{personalListed(workspace.StateReady)}}
	tm := &fake{name: "Team", listed: []workspace.Listed{team("main", "Acme", workspace.StateReady), team("ws_2", "Globex", workspace.StateOffline), team("ws_3", "Initech", workspace.StateReady), team("ws_4", "Hooli", workspace.StateConnecting)},
		sumErr: map[string]error{"team:ws_2": ErrNotRunning}, slow: map[string]time.Duration{"team:ws_3": time.Minute}}
	r, _ := registry(t, p, tm)
	ctx, cancel := context.WithTimeout(bg, 2*time.Second)
	defer cancel()
	start := time.Now()
	o := r.Overview(ctx)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("a slow workspace held everything up for %s", time.Since(start))
	}
	if len(o.Entries) != 5 {
		t.Fatalf("%d entries", len(o.Entries))
	}
	byID := map[string]Entry{}
	for _, e := range o.Entries {
		byID[e.Workspace.ID] = e
	}
	if byID["personal"].Summary == nil || byID["team:main"].Summary == nil {
		t.Fatal("the workspaces that answered were lost")
	}
	if byID["team:ws_2"].Summary != nil || byID["team:ws_2"].Error == "" || byID["team:ws_3"].Error == "" {
		t.Fatalf("%+v %+v", byID["team:ws_2"], byID["team:ws_3"])
	}
	// A workspace that is only connecting has no summary to read and is not asked for one.
	if byID["team:ws_4"].Summary != nil || byID["team:ws_4"].Error != "" {
		t.Fatalf("%+v", byID["team:ws_4"])
	}
}

func TestADuplicateWorkspaceIdIsKeptFromTheFirstSourceOnly(t *testing.T) {
	p := &fake{name: "Werkbord", listed: []workspace.Listed{personalListed(workspace.StateReady)}}
	evil := &fake{name: "Impostor", listed: []workspace.Listed{personalListed(workspace.StateReady)}}
	evil.listed[0].Entry.Name = "Personal (impostor)"
	r := NewRegistry(OpenState(""), nil, p, evil)
	v := r.Refresh(bg)
	if len(v.Items) != 1 || v.Items[0].Name != "Personal" || v.Items[0].Source != "Werkbord" {
		t.Fatalf("%+v", v.Items)
	}
}

func TestStateKeepsAChoiceAndAPlaceAndNothingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d", "shell.json")
	s := OpenState(path)
	if err := s.SetLast("team:main"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlace("team:main", "?tab=board"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLast("not an id"); err == nil {
		t.Fatal("a bad id was kept")
	}
	for _, place := range []string{"?token=SECRET", "?tab=board&join=SECRET", "#/p/prj_1?invite=SECRET", "?%74oken=SECRET"} {
		if err := s.SetPlace("team:main", place); err == nil {
			t.Fatalf("credential route was remembered: %s", place)
		}
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "token") {
		t.Fatalf("the state holds a credential: %s", raw)
	}
	s2 := OpenState(path)
	if s2.Last() != "team:main" || s2.Place("team:main") != "?tab=board" {
		t.Fatal("not remembered")
	}
	// A file edited to hold something else is cleaned on the way in.
	_ = os.WriteFile(path, []byte(`{"version":1,"last":"x y","places":{"team:main":"https://evil.example","personal":"#/control"}}`), 0o600)
	s3 := OpenState(path)
	if s3.Last() != "" || s3.Place("team:main") != "" || s3.Place("personal") != "#/control" {
		t.Fatalf("%q %q %q", s3.Last(), s3.Place("team:main"), s3.Place("personal"))
	}
	s3.Forget("personal")
	if OpenState(path).Place("personal") != "" {
		t.Fatal("forgotten place came back")
	}
	_ = os.WriteFile(path, []byte(`{"version":1,"places":{"personal":"?token=SECRET","team:main":"?tab=board&join=SECRET"}}`), 0o600)
	if s := OpenState(path); len(s.data.Places) != 0 {
		t.Fatal("credential routes from disk were retained")
	}
}

// ---- over HTTP ----

func TestAServiceSpeaksTheNeutralProtocolOverLoopbackWithItsOwnCredential(t *testing.T) {
	var asked []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer KEY" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/device/v1/listing":
			_ = json.NewEncoder(w).Encode(workspace.Listing{Schema: workspace.Schema, Workspaces: []workspace.Listed{team("main", "Acme", workspace.StateReady)}})
		case "/w/main/api/device/v1/summary":
			_ = json.NewEncoder(w).Encode(workspace.Summary{Schema: workspace.Schema, Workspace: team("main", "Acme", workspace.StateReady).Entry, Work: []workspace.Item{{ID: "ttk_1", Title: "WB-1 Fix", Project: "App", Status: workspace.StatusDoing, Href: "?tab=board"}}})
		case "/w/other/api/device/v1/summary":
			_ = json.NewEncoder(w).Encode(workspace.Summary{Schema: workspace.Schema, Workspace: team("main", "Acme", workspace.StateReady).Entry}) // answers for another
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	svc, err := NewService("Team", srv.URL, "/api/device/v1/listing", func() (string, error) { return "KEY", nil })
	if err != nil {
		t.Fatal(err)
	}
	l, err := svc.List(bg)
	if err != nil || len(l) != 1 {
		t.Fatalf("%v %v", l, err)
	}
	f, err := svc.Frame(bg, l[0], "")
	if err != nil || f.URL != srv.URL+"/w/main/#token=KEY" || f.Origin != srv.URL {
		t.Fatalf("%+v %v", f, err)
	}
	s, err := svc.Summary(bg, l[0])
	if err != nil || len(s.Work) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	// A summary that answers for a different workspace than was asked about is refused.
	imp := l[0]
	imp.SummaryPath = "/w/other/api/device/v1/summary"
	imp.Entry.ID = "team:other"
	if _, err := svc.Summary(bg, imp); err == nil {
		t.Fatal("a service answered for another workspace")
	}
	// Refusal, an old service, and nothing listening are told apart.
	bad, _ := NewService("Team", srv.URL, "/api/device/v1/listing", func() (string, error) { return "WRONG", nil })
	if _, err := bad.List(bg); !errors.Is(err, ErrRefused) {
		t.Fatalf("wrong key: %v", err)
	}
	old, _ := NewService("Team", srv.URL, "/api/device/v1/nothing", func() (string, error) { return "KEY", nil })
	if _, err := old.List(bg); !errors.Is(err, ErrOutdated) {
		t.Fatalf("old: %v", err)
	}
	none, _ := NewService("Team", srv.URL, "/api/device/v1/listing", func() (string, error) { return "", errors.New("no key file") })
	if _, err := none.List(bg); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("no key: %v", err)
	}
	srv.Close()
	if _, err := svc.List(bg); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("closed: %v", err)
	}
}

func TestOnlyLiteralLoopbackAddressesAreEverSpokenTo(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:7431", "http://localhost:7420", "http://[::1]:7420"} {
		if _, err := CheckBase(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"https://127.0.0.1:7431", "http://192.168.1.5:7431", "http://example.com:80", "http://127.0.0.1", "http://127.0.0.1:1/x", "http://u@127.0.0.1:1", "http://127.0.0.1.evil.example:1"} {
		if _, err := CheckBase(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	if _, err := NewService("x", "http://10.0.0.5:7431", "/x", nil); err == nil {
		t.Error("a service on another computer was accepted")
	}
	// Even a hostname that resolves nowhere near loopback is never dialled.
	hc := loopbackClient(time.Second)
	if _, err := hc.Get("http://192.0.2.1:9/"); err == nil || !strings.Contains(err.Error(), "not on this computer") {
		t.Fatalf("%v", err)
	}
}

func TestPersonalIsReadWithTheControllersCredentialAndOpenedSignedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			_, _ = w.Write([]byte(`{"status":"ok","version":"1"}`))
		case "/api/workspace/v1/summary":
			if r.Header.Get("Authorization") != "Bearer CTL" {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(workspace.Summary{Schema: workspace.Schema, Workspace: personalListed(workspace.StateReady).Entry})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p := NewPersonal(func(context.Context) (Access, error) { return Access{Base: srv.URL, Token: "CTL"}, nil })
	l, err := p.List(bg)
	if err != nil || len(l) != 1 || l[0].Entry.State != workspace.StateReady {
		t.Fatalf("%+v %v", l, err)
	}
	f, _ := p.Frame(bg, l[0], "")
	if f.URL != srv.URL+"/#token=CTL" {
		t.Fatalf("frame = %s", f.URL)
	}
	if s, err := p.Summary(bg, l[0]); err != nil || s.Workspace.ID != workspace.PersonalID {
		t.Fatalf("%+v %v", s, err)
	}
	srv.Close()
	l, err = p.List(bg)
	if err != nil || l[0].Entry.State != workspace.StateUnavailable || l[0].Entry.ID != workspace.PersonalID {
		t.Fatalf("a controller that stopped made Personal disappear: %+v %v", l, err)
	}
	down := NewPersonal(func(context.Context) (Access, error) { return Access{}, errors.New("no installation") })
	if l, _ := down.List(bg); l[0].Entry.State != workspace.StateUnavailable {
		t.Fatalf("%+v", l)
	}
}

func TestAGrantIsNarrowNamedAndReplacesTheOneItReconnects(t *testing.T) {
	var mu sync.Mutex
	entries := []map[string]string{{"id": "lac_old", "name": "Team: Acme"}, {"id": "lac_other", "name": "Team: Globex"}}
	var deleted []string
	var minted map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer CTL" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/local-access":
			_ = json.NewEncoder(w).Encode(entries)
		case r.Method == "POST" && r.URL.Path == "/api/local-access":
			_ = json.NewDecoder(r.Body).Decode(&minted)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"token":"wba_newgrant","id":"lac_new"}`))
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/local-access/"):
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/api/local-access/"))
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p := NewPersonal(func(context.Context) (Access, error) { return Access{Base: srv.URL, Token: "CTL"}, nil })
	tok, base, err := p.MintExecutionGrant(bg, "Team: Acme")
	if err != nil || tok != "wba_newgrant" || base != srv.URL {
		t.Fatalf("%q %q %v", tok, base, err)
	}
	if minted["scope"] != "execution-local-v1" || minted["name"] != "Team: Acme" {
		t.Fatalf("minted %v", minted)
	}
	if len(deleted) != 1 || deleted[0] != "lac_old" {
		t.Fatalf("revoked %v: only the earlier grant of the same name goes", deleted)
	}
	if _, _, err := p.MintExecutionGrant(bg, ""); err == nil {
		t.Fatal("a nameless grant was made")
	}
	if _, _, err := NewPersonal(func(context.Context) (Access, error) { return Access{Base: srv.URL}, nil }).MintExecutionGrant(bg, "x"); err == nil {
		t.Fatal("a grant was made without the credential")
	}
}

func TestAnOutdatedPersonalIsListedWithAProblemInsteadOfABlankFrame(t *testing.T) {
	version := "v1.3.1-preview.1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","version":"` + version + `"}`))
	}))
	defer srv.Close()
	p := NewPersonal(func(context.Context) (Access, error) { return Access{Base: srv.URL, Token: "CTL"}, nil })
	l, err := p.List(bg)
	if !errors.Is(err, ErrOutdated) || len(l) != 1 || l[0].Entry.State != workspace.StateUnavailable || !strings.Contains(l[0].Entry.Detail, "1.3.1-preview.1") {
		t.Fatalf("%+v %v", l, err)
	}
	v := NewRegistry(OpenState(""), nil, p).Refresh(bg)
	if len(v.Items) != 1 || v.Items[0].State != workspace.StateUnavailable || len(v.Problems) != 1 || v.Problems[0].Kind != "outdated" || v.Problems[0].Source != "Werkbord" {
		t.Fatalf("an outdated Personal must stay listed and say why: %+v", v)
	}
	for _, ok := range []string{"v1.6.0", "1.9.0-preview.1", "dev", "v1.9.0-3-gabcdef1"} {
		version = ok
		if l, err := p.List(bg); err != nil || l[0].Entry.State != workspace.StateReady {
			t.Fatalf("%s: %+v %v", ok, l, err)
		}
	}
}

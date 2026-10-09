package workspace

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func good() Summary {
	return Summary{
		Schema:    Schema,
		Workspace: Entry{ID: PersonalID, Kind: KindPersonal, Name: "Personal", State: StateReady},
		Projects:  []Project{{ID: "prj_1", Name: "App", Href: "#/p/prj_1/board"}},
		Work:      []Item{{ID: "tsk_1", Title: "Fix login", Project: "App", Status: StatusDoing, Execution: ExecRunning, Href: "#/p/prj_1/task/tsk_1"}},
		Attention: []Attention{{ID: "q1", Kind: AttentionNeedsInput, Severity: SeverityWarning, Title: "Which database?", Href: "#/control"}},
		Schedule:  []Scheduled{{ID: "s1", Title: "Nightly", Project: "App", At: time.Unix(1_800_000_000, 0).UTC(), State: "scheduled", Href: "#/p/prj_1/calendar"}},
		At:        time.Unix(1_800_000_000, 0).UTC(),
	}
}

func encode(t *testing.T, s Summary) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAGoodSummaryRoundTrips(t *testing.T) {
	got, dropped, err := Decode(strings.NewReader(encode(t, good())), PersonalID)
	if err != nil || dropped != 0 {
		t.Fatalf("%v %d", err, dropped)
	}
	if len(got.Work) != 1 || got.Work[0].Execution != ExecRunning || got.Projects[0].Href != "#/p/prj_1/board" {
		t.Fatalf("%+v", got)
	}
}

func TestLinksPointOnlyInsideTheWorkspaceTheyCameFrom(t *testing.T) {
	for _, ok := range []string{"#/p/prj_1/board", "#/control", "?tab=board&project=tpj_1&ticket=ttk_9", "#"} {
		if !ValidHref(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "/p/x", "//evil.example/", "https://evil.example/", "javascript:alert(1)", "#/../x", "#//x", "?a=<b>", "#/a b", "data:text/html,x", "#" + strings.Repeat("a", 300)} {
		if ValidHref(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestOneBadRowIsDroppedNotTheWholeSummary(t *testing.T) {
	s := good()
	s.Work = append(s.Work, Item{ID: "tsk_2", Title: "Elsewhere", Project: "App", Status: StatusTodo, Href: "https://evil.example/"})
	s.Work = append(s.Work, Item{ID: "tsk 3", Title: "Space in id", Project: "App", Status: StatusTodo, Href: "#/x"})
	s.Work = append(s.Work, Item{ID: "tsk_4", Title: "Unknown status", Project: "App", Status: "wat", Href: "#/x"})
	s.Attention = append(s.Attention, Attention{ID: "a2", Kind: "other", Severity: "dire", Title: "bad severity"})
	got, dropped, err := Decode(strings.NewReader(encode(t, s)), PersonalID)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 4 || len(got.Work) != 1 || len(got.Attention) != 1 {
		t.Fatalf("dropped %d: %+v", dropped, got)
	}
}

func TestTextFromAPersonIsMadePlainAndShort(t *testing.T) {
	s := good()
	s.Work[0].Title = "Fix\nthe\x00 login‮ " + strings.Repeat("x", 500)
	got, _, err := Decode(strings.NewReader(encode(t, s)), PersonalID)
	if err != nil {
		t.Fatal(err)
	}
	title := got.Work[0].Title
	if strings.ContainsAny(title, "\n\x00") || len([]rune(title)) > MaxText {
		t.Fatalf("title = %q", title)
	}
}

func TestASummaryIsReadStrictly(t *testing.T) {
	good := encode(t, good())
	cases := map[string]string{
		"unknown field":     strings.Replace(good, `"schema"`, `"extra":1,"schema"`, 1),
		"another schema":    strings.Replace(good, Schema, "werkbord.workspace/v2", 1),
		"two documents":     good + good,
		"not json":          "<html>",
		"another workspace": strings.Replace(good, `"id":"personal"`, `"id":"team:main"`, 1),
	}
	for name, body := range cases {
		if _, _, err := Decode(strings.NewReader(body), PersonalID); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, _, err := Decode(strings.NewReader(strings.Repeat(" ", MaxBytes+1)+good), PersonalID); err == nil {
		t.Error("an oversized summary was accepted")
	}
	// A provider cannot answer for a workspace the caller did not ask about.
	team := good
	team = strings.Replace(team, `"id":"personal","kind":"personal"`, `"id":"team:ws_1","kind":"team"`, 1)
	if _, _, err := Decode(strings.NewReader(team), PersonalID); err == nil {
		t.Error("a team answered for personal")
	}
	if _, _, err := Decode(strings.NewReader(team), TeamID("ws_1")); err != nil {
		t.Errorf("a team answering for itself: %v", err)
	}
}

func TestOnlyPersonalIsPersonalAndTeamsAreNamedBySlot(t *testing.T) {
	if err := (Entry{ID: "team:main", Kind: KindPersonal, Name: "x", State: StateReady}).validate(); err == nil {
		t.Error("a personal workspace with a team's name")
	}
	if err := (Entry{ID: PersonalID, Kind: KindTeam, Name: "x", State: StateReady}).validate(); err == nil {
		t.Error("a team workspace with personal's name")
	}
	if err := (Entry{ID: "main", Kind: KindTeam, Name: "x", State: StateReady}).validate(); err == nil {
		t.Error("a team workspace not named by slot")
	}
	if err := (Entry{ID: TeamID("main"), Kind: KindTeam, Name: "Acme", State: StateReady, Role: "admin", DeviceRoles: []string{"runner", "workspace_host"}}).validate(); err != nil {
		t.Error(err)
	}
	if err := (Entry{ID: TeamID("main"), Kind: KindTeam, Name: "Acme", State: StateReady, DeviceRoles: []string{"root"}}).validate(); err == nil {
		t.Error("an invented device role")
	}
}

func TestAnInfrastructureReportCannotClaimMoreHostsOnlineThanExist(t *testing.T) {
	s := good()
	s.Workspace = Entry{ID: TeamID("main"), Kind: KindTeam, Name: "Acme", State: StateReady}
	s.Infra = &Infra{Writable: true, HostsConfigured: 1, HostsOnline: 3, Warnings: []string{"One host."}}
	got, _, err := Decode(strings.NewReader(encode(t, s)), TeamID("main"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Infra.HostsOnline != 1 {
		t.Fatalf("infra = %+v", got.Infra)
	}
}

func listing(t *testing.T, mutate func(*Listing)) string {
	t.Helper()
	l := Listing{Schema: Schema, Workspaces: []Listed{
		{Entry: Entry{ID: TeamID("main"), Kind: KindTeam, Name: "Acme", State: StateReady, Role: "member"}, Root: "/w/main/", SummaryPath: "/w/main/api/device/v1/summary"},
		{Entry: Entry{ID: TeamID("ws_ab12"), Kind: KindTeam, Name: "Globex", State: StateSetup}, Root: "/w/ws_ab12/", SummaryPath: "/w/ws_ab12/api/device/v1/summary"},
	}}
	if mutate != nil {
		mutate(&l)
	}
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAListingPointsOnlyAtItsProvider(t *testing.T) {
	got, err := DecodeListing(strings.NewReader(listing(t, nil)))
	if err != nil || len(got.Workspaces) != 2 || got.Workspaces[1].Root != "/w/ws_ab12/" {
		t.Fatalf("%+v %v", got, err)
	}
	bad := map[string]func(*Listing){
		"another host":           func(l *Listing) { l.Workspaces[0].Root = "//evil.example/" },
		"a scheme":               func(l *Listing) { l.Workspaces[0].SummaryPath = "http://evil.example/x" },
		"climbing":               func(l *Listing) { l.Workspaces[0].Root = "/w/../../" },
		"a root without a slash": func(l *Listing) { l.Workspaces[0].Root = "/w/main" },
		"a query":                func(l *Listing) { l.Workspaces[0].SummaryPath = "/x?y=1" },
		"the same twice":         func(l *Listing) { l.Workspaces[1].Entry.ID = l.Workspaces[0].Entry.ID },
		"personal named team":    func(l *Listing) { l.Workspaces[0].Entry.Kind = KindPersonal },
		"another schema":         func(l *Listing) { l.Schema = "nope" },
	}
	for name, mutate := range bad {
		if _, err := DecodeListing(strings.NewReader(listing(t, mutate))); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := DecodeListing(strings.NewReader(strings.Replace(listing(t, nil), `"schema"`, `"extra":1,"schema"`, 1))); err == nil {
		t.Error("an unknown field was accepted")
	}
}

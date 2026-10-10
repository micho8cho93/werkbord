package api_test

import (
	"fmt"
	"testing"
)

func TestLabelsOverHTTPFollowTheRoles(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	member := client{t: t, base: ts.URL, token: str(owner.want(201, "POST", v1+"/members", `{"name":"Bo"}`), "token")}
	adminM := owner.want(201, "POST", v1+"/members", `{"name":"Ad","role":"admin"}`)
	admin := client{t: t, base: ts.URL, token: str(adminM, "token")}

	l := owner.want(201, "POST", v1+"/labels", `{"name":"Q4 launch","color":"#3A7"}`)
	if str(l, "name") != "Q4 launch" || str(l, "color") != "#33aa77" {
		t.Fatalf("label = %v", l)
	}
	admin.want(201, "POST", v1+"/labels", `{"name":"Design","color":"#111111"}`)

	// A member sees the labels and may not change one.
	list := member.want(200, "GET", v1+"/labels", nil)
	if ls, _ := list["labels"].([]any); len(ls) != 2 {
		t.Fatalf("labels = %v", list)
	}
	id := str(l, "id")
	for _, c := range []struct {
		method, path, body string
	}{
		{"POST", "/labels", `{"name":"Mine","color":"#000000"}`},
		{"PATCH", "/labels/" + id, `{"name":"Mine","version":1}`},
		{"DELETE", "/labels/" + id, ``},
	} {
		if code, _, raw := member.do(c.method, v1+c.path, c.body); code != 403 {
			t.Errorf("a member: %s %s answered %d: %s", c.method, c.path, code, raw)
		}
	}

	// Bad input and stale edits.
	for body, want := range map[string]int{
		`{"name":"","color":"#000000"}`:            400,
		`{"name":"x","color":"teal"}`:              400,
		`{"name":"q4  LAUNCH","color":"#000000"}`:  409,
		`{"name":"x","color":"#000000","extra":1}`: 400,
	} {
		if code, _, raw := owner.do("POST", v1+"/labels", body); code != want {
			t.Errorf("%s: %d, want %d: %s", body, code, want, raw)
		}
	}
	renamed := owner.want(200, "PATCH", v1+"/labels/"+id, `{"name":"Launch","version":1}`)
	if str(renamed, "name") != "Launch" {
		t.Fatalf("renamed = %v", renamed)
	}
	if code, _, _ := owner.do("PATCH", v1+"/labels/"+id, `{"name":"stale","version":1}`); code != 409 {
		t.Errorf("stale: %d", code)
	}
	if code, _, _ := owner.do("PATCH", v1+"/labels/"+id, `{"name":"no version"}`); code != 400 {
		t.Errorf("no version: %d", code)
	}
	owner.want(204, "DELETE", v1+"/labels/"+id, nil)
	if code, _, _ := owner.do("DELETE", v1+"/labels/"+id, nil); code != 404 {
		t.Errorf("delete twice: %d", code)
	}
}

func TestTicketsCarryLabelsModeDatesAndDependenciesOverHTTP(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	pid := str(owner.want(201, "POST", v1+"/projects", `{"name":"Launch week"}`), "id") // no repository: not code
	press := str(owner.want(201, "POST", v1+"/labels", `{"name":"Press","color":"#aa0000"}`), "id")
	base := v1 + "/projects/" + pid + "/tickets"

	a := owner.want(201, "POST", base, fmt.Sprintf(`{"title":"Write release","workMode":"human","labelIds":[%q],"plan":{"start":"2026-10-05","end":"2026-10-09"}}`, press))
	if str(a, "workMode") != "human" || fmt.Sprint(a["labelIds"]) != "["+press+"]" {
		t.Fatalf("a = %v", a)
	}
	b := owner.want(201, "POST", base, fmt.Sprintf(`{"title":"Launch","plan":{"start":"2026-10-07","milestone":true},"dependencies":[%q]}`, str(a, "id")))
	if str(b, "workMode") != "agent" {
		t.Fatalf("an unset mode is agent work, as before: %v", b)
	}
	for body, want := range map[string]int{
		`{"title":"x","workMode":"robot"}`:                               400,
		`{"title":"x","labelIds":["tlb_nope"]}`:                          400,
		`{"title":"x","plan":{"start":"2026-10-09","end":"2026-10-05"}}`: 400,
		`{"title":"x","dependencies":["ttk_nope"]}`:                      400,
	} {
		if code, _, raw := owner.do("POST", base, body); code != want {
			t.Errorf("%s: %d, want %d: %s", body, code, want, raw)
		}
	}

	// The timeline says what is wrong, and changes nothing.
	tl := owner.want(200, "GET", v1+"/projects/"+pid+"/timeline", nil)
	ws, _ := tl["warnings"].([]any)
	if len(ws) != 1 {
		t.Fatalf("timeline = %v", tl)
	}
	if w := ws[0].(map[string]any); str(w, "code") != "starts_before_dependency_ends" || str(w, "itemId") != str(b, "id") {
		t.Fatalf("warning = %v", w)
	}
	again := owner.want(200, "GET", base+"/"+str(b, "id"), nil)
	if fmt.Sprint(again["version"]) != fmt.Sprint(b["version"]) {
		t.Errorf("reading the timeline changed the ticket: %v → %v", b["version"], again["version"])
	}

	// The board carries the workspace's labels, so a view needs one read.
	board := owner.want(200, "GET", v1+"/projects/"+pid+"/board", nil)
	if ls, _ := board["labels"].([]any); len(ls) != 1 {
		t.Fatalf("board labels = %v", board["labels"])
	}

	// Editing: clearing the dependencies and the plan, and the stale-version rule still applies.
	edited := owner.want(200, "PATCH", base+"/"+str(b, "id"), fmt.Sprintf(`{"dependencies":[],"plan":{},"version":%v}`, b["version"]))
	if fmt.Sprint(edited["dependencies"]) != "[]" || fmt.Sprint(edited["plan"]) != "map[]" {
		t.Fatalf("edited = %v", edited)
	}
	if code, _, _ := owner.do("PATCH", base+"/"+str(b, "id"), fmt.Sprintf(`{"title":"stale","version":%v}`, b["version"])); code != 409 {
		t.Errorf("stale edit: %d", code)
	}
}

func TestAProjectWithNoRepositoryRunsTheWholeBoardWithoutGit(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	pid := str(owner.want(201, "POST", v1+"/projects", `{"name":"Hiring"}`), "id")
	base := v1 + "/projects/" + pid + "/tickets"
	k := owner.want(201, "POST", base, `{"title":"Interview","status":"available","workMode":"human"}`)
	id := str(k, "id")
	owner.want(200, "POST", base+"/"+id+"/claim", nil)
	owner.want(200, "POST", base+"/"+id+"/submit", `{}`)
	done := owner.want(200, "POST", base+"/"+id+"/complete", nil)
	if str(done, "status") != "done" {
		t.Fatalf("done = %v", done)
	}
}

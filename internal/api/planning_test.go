package api

import (
	"fmt"
	"strings"
	"testing"
)

type labelJSON struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Version int64  `json:"version"`
	Tasks   int    `json:"tasks"`
}

func TestLabelsOverHTTP(t *testing.T) {
	ts := newTestServer(t, nil)
	var l labelJSON
	if code := do(t, "POST", ts.URL+"/api/labels", `{"name":"Q4 launch","color":"#3A7"}`, &l); code != 201 || l.Name != "Q4 launch" || l.Color != "#33aa77" || l.Version != 1 {
		t.Fatalf("create: %d %+v", code, l)
	}
	for body, want := range map[string]int{
		`{"name":"q4  LAUNCH","color":"#000000"}`:  409, // the same name
		`{"name":"","color":"#000000"}`:            400,
		`{"name":"x","color":"teal"}`:              400,
		`{"name":"x","color":"#000000","bogus":1}`: 400, // unknown fields are refused
	} {
		if code := do(t, "POST", ts.URL+"/api/labels", body, nil); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}

	var out struct {
		Labels []labelJSON `json:"labels"`
	}
	if code := do(t, "GET", ts.URL+"/api/labels", "", &out); code != 200 || len(out.Labels) != 1 {
		t.Fatalf("list: %d %+v", code, out)
	}

	var renamed labelJSON
	if code := do(t, "PATCH", ts.URL+"/api/labels/"+l.ID, fmt.Sprintf(`{"name":"Launch","color":"#ff0000","version":%d}`, l.Version), &renamed); code != 200 || renamed.Name != "Launch" || renamed.Version != 2 {
		t.Fatalf("patch: %d %+v", code, renamed)
	}
	if code := do(t, "PATCH", ts.URL+"/api/labels/"+l.ID, `{"name":"stale","version":1}`, nil); code != 409 {
		t.Errorf("stale patch: %d", code)
	}
	if code := do(t, "PATCH", ts.URL+"/api/labels/"+l.ID, `{"name":"no version"}`, nil); code != 400 {
		t.Errorf("patch without a version: %d", code)
	}
	if code := do(t, "PATCH", ts.URL+"/api/labels/lbl_none", `{"name":"x","version":1}`, nil); code != 404 {
		t.Errorf("patch of a missing label: %d", code)
	}
	if code := do(t, "DELETE", ts.URL+"/api/labels/"+l.ID, "", nil); code != 204 {
		t.Errorf("delete: %d", code)
	}
	if code := do(t, "DELETE", ts.URL+"/api/labels/"+l.ID, "", nil); code != 404 {
		t.Errorf("delete twice: %d", code)
	}
}

func TestTasksCarryLabelsModeAndPlanOverHTTP(t *testing.T) {
	ts := newTestServer(t, nil)
	var work struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Path string `json:"repoPath"`
	}
	if code := do(t, "POST", ts.URL+"/api/projects", `{"kind":"work","name":"Launch week"}`, &work); code != 201 || work.Kind != "work" || work.Path != "" {
		t.Fatalf("work project: %d %+v", code, work)
	}
	if code := do(t, "POST", ts.URL+"/api/projects", `{"kind":"work","name":"x","path":"/tmp"}`, nil); code != 400 {
		t.Errorf("a work project with a path: %d", code)
	}
	if code := do(t, "POST", ts.URL+"/api/projects", `{"kind":"folder","name":"x"}`, nil); code != 400 {
		t.Errorf("unknown kind: %d", code)
	}
	var one, two labelJSON
	do(t, "POST", ts.URL+"/api/labels", `{"name":"Press","color":"#aa0000"}`, &one)
	do(t, "POST", ts.URL+"/api/labels", `{"name":"Budget","color":"#00aa00"}`, &two)

	base := ts.URL + "/api/projects/" + work.ID + "/tasks"
	type taskJSON struct {
		ID       string   `json:"id"`
		Version  int64    `json:"version"`
		WorkMode string   `json:"workMode"`
		LabelIDs []string `json:"labelIds"`
		Plan     struct {
			Start     string `json:"start"`
			End       string `json:"end"`
			Milestone bool   `json:"milestone"`
		} `json:"plan"`
	}
	var a, b taskJSON
	if code := do(t, "POST", base, fmt.Sprintf(`{"title":"Write release","labelIds":[%q,%q],"plan":{"start":"2026-10-05","end":"2026-10-09"}}`, one.ID, two.ID), &a); code != 201 ||
		a.WorkMode != "human" || len(a.LabelIDs) != 2 || a.Plan.Start != "2026-10-05" {
		t.Fatalf("create: %d %+v", code, a)
	}
	if code := do(t, "POST", base, fmt.Sprintf(`{"title":"Launch","workMode":"hybrid","labelIds":[%q],"plan":{"start":"2026-10-12","milestone":true},"orchestration":{"dependencies":[%q]}}`, two.ID, a.ID), &b); code != 201 || b.WorkMode != "hybrid" || !b.Plan.Milestone {
		t.Fatalf("create 2: %d %+v", code, b)
	}
	for body, want := range map[string]int{
		`{"title":"x","workMode":"robot"}`:                               400,
		`{"title":"x","labelIds":["lbl_nope"]}`:                          400,
		`{"title":"x","plan":{"start":"2026-10-09","end":"2026-10-05"}}`: 400,
		`{"title":"x","workBranch":"feature/x"}`:                         400, // no repository, no branch
		`{"title":"x","orchestration":{"enabled":true}}`:                 400, // nothing for an agent to do here
	} {
		if code := do(t, "POST", base, body, nil); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}

	// Filtering: any label, all labels, work mode.
	count := func(query string) int {
		var out struct {
			Tasks []taskJSON `json:"tasks"`
		}
		if code := do(t, "GET", base+query, "", &out); code != 200 {
			t.Fatalf("%s: %d", query, code)
		}
		return len(out.Tasks)
	}
	for query, want := range map[string]int{
		"": 2, "?label=" + one.ID: 1, "?label=" + two.ID: 2, "?label=" + one.ID + "&label=" + two.ID: 2,
		"?label=" + one.ID + "&label=" + two.ID + "&match=all": 1, "?mode=human": 1, "?mode=hybrid": 1, "?mode=agent": 0,
	} {
		if got := count(query); got != want {
			t.Errorf("%s: %d tasks, want %d", query, got, want)
		}
	}
	for _, q := range []string{"?mode=robot", "?match=some"} {
		if code := do(t, "GET", base+q, "", nil); code != 400 {
			t.Errorf("%s: %d", q, code)
		}
	}

	// Edit the planning side of a task.
	var edited taskJSON
	body := fmt.Sprintf(`{"version":%d,"labelIds":[],"workMode":"agent","plan":{}}`, a.Version)
	if code := do(t, "PATCH", base+"/"+a.ID, body, &edited); code != 200 || len(edited.LabelIDs) != 0 || edited.WorkMode != "agent" || edited.Plan.Start != "" {
		t.Fatalf("patch: %d %+v", code, edited)
	}

	// The labels' usage shows in the list; the timeline reports the milestone's dependency as unplanned.
	var out struct {
		Labels []labelJSON `json:"labels"`
	}
	do(t, "GET", ts.URL+"/api/labels", "", &out)
	use := map[string]int{}
	for _, l := range out.Labels {
		use[l.ID] = l.Tasks
	}
	if use[one.ID] != 0 || use[two.ID] != 1 {
		t.Errorf("usage = %v", use)
	}
	var tl struct {
		Warnings []struct {
			Code   string `json:"code"`
			ItemID string `json:"itemId"`
		} `json:"warnings"`
	}
	if code := do(t, "GET", ts.URL+"/api/projects/"+work.ID+"/timeline", "", &tl); code != 200 || tl.Warnings == nil {
		t.Fatalf("timeline: %d %+v", code, tl)
	}
	if code := do(t, "GET", ts.URL+"/api/projects/prj_none/timeline", "", nil); code != 404 {
		t.Errorf("timeline of a missing project: %d", code)
	}

	// Nothing about a repository is offered for it.
	for _, path := range []string{"/refresh", "/git"} {
		method := "POST"
		if path == "/git" {
			method = "GET"
		}
		if code := do(t, method, ts.URL+"/api/projects/"+work.ID+path, "", nil); code == 200 {
			t.Errorf("%s %s answered 200 for a work project", method, path)
		}
	}
}

func TestLabelsAreNotReachableByAProgramsToken(t *testing.T) {
	// The route list in server.go is checked wholesale by the local-access test; this names the
	// planning routes so that a change to that list cannot quietly let them through.
	for _, r := range scopedRoutes {
		if strings.Contains(r.pattern, "/labels") || strings.Contains(r.pattern, "/timeline") {
			t.Errorf("%s %s is open to a local access token", r.method, r.pattern)
		}
		if r.method == "PATCH" {
			t.Errorf("a program's token may edit tasks through %s %s", r.method, r.pattern)
		}
	}
	for _, r := range scopedRoutes {
		if r.method == "POST" && r.pattern == "/api/projects/{}/tasks" {
			for _, f := range r.fields {
				if f == "workMode" || f == "labelIds" || f == "plan" || f == "orchestration" {
					t.Errorf("a program may set %q on a task it hands over", f)
				}
			}
		}
	}
}

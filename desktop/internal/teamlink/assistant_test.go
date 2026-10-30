package teamlink

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantBoardUsesFixedWorkspaceRoutesAndStripsCredentialsAndExecution(t *testing.T) {
	posts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("missing desktop authentication")
		}
		if !strings.HasPrefix(r.URL.Path, "/w/main/api/team/v1/projects") {
			t.Errorf("wrong slot: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			posts++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != "backlog" || body["title"] != "Extract CV" {
				t.Errorf("creation: %+v", body)
			}
			_, _ = w.Write([]byte(`{"id":"t1","projectId":"p1","title":"Extract CV","status":"review","version":1,"creatorId":"SECRET","branch":"PRIVATE"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"p1","name":"Portfolio","repository":"PRIVATE","token":"SECRET"}]`))
	}))
	defer ts.Close()
	l, err := New(ts.URL, writeKey(t, 0o600, key), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := l.AssistantBoard(bg, "team:main", "list_projects", BoardArgs{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "SECRET") {
		t.Fatalf("raw project data leaked: %s", raw)
	}
	out, err = l.AssistantBoard(bg, "team:main", "create_ticket", BoardArgs{ProjectID: "p1", Title: "Extract CV"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(out)
	if posts != 1 || !strings.Contains(string(raw), `"state":"review"`) || strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "PRIVATE") {
		t.Fatalf("ticket data: %s, posts %d", raw, posts)
	}
	for _, method := range []string{"start_run", "delete_ticket", "update_settings"} {
		if _, err := DecodeBoardArgs(method, json.RawMessage(`{}`)); err == nil {
			t.Errorf("dangerous method accepted: %s", method)
		}
	}
	for _, raw := range []string{`{"projectId":"../../other"}`, `{"projectId":"p1","url":"https://example.test"}`, `{"projectId":"p1"} {}`, `{"projectId":"p1"}]`} {
		if _, err := DecodeBoardArgs("get_project", json.RawMessage(raw)); err == nil {
			t.Errorf("invalid arguments accepted: %s", raw)
		}
	}
}

func TestAssistantBoardPreservesDefinitiveTeamResponseStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"You cannot create tickets in this project."}}`))
	}))
	defer ts.Close()
	l, err := New(ts.URL, writeKey(t, 0o600, key), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.AssistantBoard(bg, "team:main", "create_ticket", BoardArgs{ProjectID: "p1", Title: "Restricted"})
	var response *ResponseError
	if !errors.As(err, &response) || response.StatusCode != http.StatusForbidden || response.Message != "You cannot create tickets in this project." {
		t.Fatalf("response error: %#v", err)
	}
}

func TestWorkPlansUseFixedMetadataRoutesAndPreserveVersion(t *testing.T) {
	writes := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("missing authentication")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/w/main/api/team/v1/projects/p1/initiatives":
			_, _ = w.Write([]byte(`[{"initiative":{"id":"ini_1","title":"Delivery","objective":"Coordinate tasks","status":"active","projectIds":["p1"],"tickets":[{"projectId":"p1","ticketId":"t1"},{"projectId":"p1","ticketId":"t2"}],"version":3,"ownerId":"owner","dependencies":[],"memories":[],"questions":"Who reviews?"}}]`))
		case r.Method == "PUT" && r.URL.Path == "/w/main/api/team/v1/initiatives/ini_1":
			writes++
			var body initiativePlan
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Version != 3 || body.Tickets[0].TicketID != "t2" || body.OwnerID != "owner" || body.Questions != "Ready?" {
				t.Errorf("wrong plan mutation: %+v", body)
			}
			body.Version = 4
			_ = json.NewEncoder(w).Encode(body)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	l, err := New(ts.URL, writeKey(t, 0o600, key), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := l.AssistantBoard(bg, "team:main", "list_work_plans", BoardArgs{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	plans := out.([]workPlan)
	if len(plans) != 1 || plans[0].TaskIDs[1] != "t2" {
		t.Fatal(plans)
	}
	p := plans[0]
	p.TaskIDs = []string{"t2", "t1"}
	p.Questions = "Ready?"
	document, _ := json.Marshal(p)
	raw, _ := json.Marshal(BoardArgs{ProjectID: "p1", PlanJSON: string(document)})
	args, err := DecodeBoardArgs("save_work_plan", raw)
	if err != nil {
		t.Fatal(err)
	}
	result, err := l.AssistantBoard(bg, "team:main", "save_work_plan", args)
	if err != nil || result.(workPlan).Version != 4 || writes != 1 {
		t.Fatal(result, err, writes)
	}
	if _, err = DecodeBoardArgs("list_tickets", raw); err == nil {
		t.Fatal("plan data accepted by another operation")
	}
	p.ProjectID = "other"
	document, _ = json.Marshal(p)
	raw, _ = json.Marshal(BoardArgs{ProjectID: "p1", PlanJSON: string(document)})
	if _, err = DecodeBoardArgs("save_work_plan", raw); err == nil {
		t.Fatal("cross-project plan accepted")
	}
}

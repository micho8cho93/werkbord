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

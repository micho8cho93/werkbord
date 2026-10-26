package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"werkbord/internal/agent/fake"
	"werkbord/internal/domain"
)

// The console and floating card use these same project-scoped APIs. Merely inspecting
// sessions must preserve process ownership; answering must target the asking adapter.
func TestAttachedSessionsRouteFourRequestsWithoutStartingProcesses(t *testing.T) {
	rs := newRunsServer(t)
	runs := make([]domain.Run, 4)
	sessions := make([]*fake.Session, 4)
	for i := range runs {
		runs[i] = rs.start(t, rs.task(t, fmt.Sprintf("Session %d", i)))
		sessions[i] = rs.adapter.Last()
		sessions[i].Assistant(fmt.Sprintf("Activity %d", i))
		sessions[i].RequestApproval(fmt.Sprintf("permission-%d", i), fmt.Sprintf("Allow action %d?", i))
		rs.waitQuestions(t, i+1)
	}
	pending := rs.waitQuestions(t, 4)
	for i, q := range pending {
		// Repeated inspection and recovery reads attach to existing state only.
		for range 2 {
			rs.getRun(t, q.RunID)
			if code := do(t, "GET", rs.url+"/api/projects/"+q.ProjectID+"/runs/"+q.RunID+"/events", "", nil); code != 200 {
				t.Fatalf("events: %d", code)
			}
		}
		if q.RunID != runs[i].ID {
			t.Fatalf("pending order/context: %+v", pending)
		}
		// No model or read action has answered a permission on behalf of the person.
		if len(sessions[i].Responses()) != 0 {
			t.Fatal("approval was automatically answered")
		}
		wrong := rs.url + "/api/projects/prj_other/questions/" + q.ID + "/answer"
		if code := do(t, "POST", wrong, `{"answer":"Allow"}`, nil); code != http.StatusNotFound {
			t.Fatalf("cross-project answer: %d", code)
		}
		path := rs.url + "/api/projects/" + q.ProjectID + "/questions/" + q.ID + "/answer"
		if code := do(t, "POST", path, `{"answer":"Allow"}`, nil); code != 200 {
			t.Fatalf("answer: %d", code)
		}
		// Retrying after reconnect is idempotent, not a second permission grant.
		if code := do(t, "POST", path, `{"answer":"Allow"}`, nil); code != 200 {
			t.Fatalf("retry: %d", code)
		}
		got := sessions[i].Responses()
		if len(got) != 1 || got[0].Ref != fmt.Sprintf("permission-%d", i) || got[0].Answer != "Allow" {
			t.Fatalf("wrong recipient: %+v", got)
		}
		if len(rs.pendingQuestions(t)) != 3-i {
			t.Fatal("pending count did not advance")
		}
	}
	if len(rs.adapter.Sessions()) != 4 {
		t.Fatal("inspection started another process")
	}
	for i, run := range runs {
		path := rs.url + "/api/projects/" + run.ProjectID + "/runs/" + run.ID
		if code := do(t, "POST", path+"/input", `{"text":"Follow-up"}`, nil); code != 200 {
			t.Fatalf("input: %d", code)
		}
		if got := sessions[i].Sent(); len(got) != 1 || got[0] != "Follow-up" {
			t.Fatalf("wrong message recipient: %+v", got)
		}
		if code := do(t, "POST", path+"/stop", "", nil); code != 200 {
			t.Fatalf("stop: %d", code)
		}
		if code := do(t, "POST", path+"/input", `{"text":"Too late"}`, nil); code != 409 {
			t.Fatalf("ended input: %d", code)
		}
		if code := do(t, "GET", path+"/events", "", nil); code != 200 {
			t.Fatalf("completed history: %d", code)
		}
	}
}

func TestSessionActionsRequireExistingControllerAuthorization(t *testing.T) {
	previous := testAuthToken
	testAuthToken = "session-test-token"
	defer func() { testAuthToken = previous }()
	rs := newRunsServer(t, func(o *Options) { o.AuthRequired = true; o.Token = testAuthToken })
	run := rs.start(t, rs.task(t, "Authorised session"))
	session := rs.adapter.Last()
	session.RequestApproval("permission", "Allow command?")
	q := rs.waitQuestions(t, 1)[0]
	for _, path := range []string{
		"/runs/" + run.ID + "/input", "/runs/" + run.ID + "/stop", "/runs/" + run.ID + "/finish", "/questions/" + q.ID + "/answer",
	} {
		response, err := http.Post(rs.url+"/api/projects/"+run.ProjectID+path, "application/json", strings.NewReader(`{"answer":"Allow","text":"go"}`))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthorised %s: %d", path, response.StatusCode)
		}
	}
	if len(session.Sent()) != 0 || len(session.Responses()) != 0 || session.StopRequested() || session.CloseRequested() {
		t.Fatal("unauthorised action reached the runner")
	}
	if len(rs.pendingQuestions(t)) != 1 {
		t.Fatal("unauthorised approval changed lifecycle")
	}
}

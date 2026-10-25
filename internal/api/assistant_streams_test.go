package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"devboard/internal/assistant"
	"devboard/internal/assistant/provider/fake"
)

func TestAssistantMultiplexStreamsKeepCursorsAndSessionsIndependent(t *testing.T) {
	a := newAsst(t, true)
	first, second := a.newSession(t), a.newSession(t)
	// Produce separate histories before connecting, so replay and cursor handling are exercised.
	for _, id := range []string{first, second} {
		a.prov.Queue(fake.Reply("reply for " + id))
		if code, b := a.req(t, "POST", "/api/assistant/sessions/"+id+"/messages", `{"text":"hello"}`); code != 202 {
			t.Fatalf("%d %s", code, b)
		}
		a.stream(t, id, "", untilEnd)
	}
	view, err := a.eng.Session(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	// First has already seen everything; second has a cursor from before a controller restart.
	body := fmt.Sprintf(`{"sessions":[{"id":%q,"after":%d},{"id":%q,"after":999999},{"id":"ast_missing","after":0}]}`, first, view.LastEvent, second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", a.url+"/api/assistant/events", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+a.owner)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.Status)
	}
	scan := bufio.NewScanner(res.Body)
	snapshots := map[string]bool{}
	missing := false
	deltas := ""
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame struct {
			Type      string                 `json:"type"`
			SessionID string                 `json:"sessionId"`
			Text      string                 `json:"text"`
			Session   *assistant.SessionView `json:"session"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		switch frame.Type {
		case "session_snapshot":
			if frame.Session == nil || frame.Session.ID != frame.SessionID {
				t.Fatalf("bad snapshot: %+v", frame)
			}
			snapshots[frame.SessionID] = true
		case "session_missing":
			missing = frame.SessionID == "ast_missing"
		default:
			if frame.SessionID != second {
				t.Fatalf("replayed already-seen or unrequested session: %+v", frame)
			}
			if !snapshots[frame.SessionID] || !missing {
				t.Fatal("event arrived before reconciliation")
			}
			if frame.Type == "delta" {
				deltas += frame.Text
			}
			if frame.Type == "turn_completed" {
				if len(snapshots) != 2 || deltas != "reply for "+second {
					t.Fatalf("snapshots %v, reply %q", snapshots, deltas)
				}
				return
			}
		}
	}
	t.Fatalf("incomplete stream: %v", scan.Err())
}

func TestAssistantMultiplexRejectsInvalidSubscriptions(t *testing.T) {
	a := newAsst(t, true)
	id := a.newSession(t)
	for _, body := range []string{
		`{"sessions":[]}`,
		fmt.Sprintf(`{"sessions":[{"id":%q,"after":-1}]}`, id),
		fmt.Sprintf(`{"sessions":[{"id":%q,"after":0},{"id":%q,"after":0}]}`, id, id),
		`{"sessions":[],"command":"anything"}`,
		`{"sessions":[` + strings.Repeat(`{"id":"ast_x","after":0},`, 128) + `{"id":"ast_y","after":0}]}`,
	} {
		if code, b := a.req(t, "POST", "/api/assistant/events", body); code != 400 {
			t.Fatalf("%d %s", code, b)
		}
	}
}

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/assistant"
	"devboard/internal/assistant/provider/fake"
	"devboard/internal/phoneaccess"
)

func TestPhoneReceivesAssistantMultiplexCompletionAndRevocationClosesStream(t *testing.T) {
	phones, err := phoneaccess.Open(filepath.Join(t.TempDir(), "phones.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := newAsst(t, true, func(o *Options) { o.Phones = phones })
	code, _, err := phones.NewCode()
	if err != nil {
		t.Fatal(err)
	}
	entry, token, err := phones.Enroll(code, "stream regression")
	if err != nil {
		t.Fatal(err)
	}
	id := a.newSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body := fmt.Sprintf(`{"sessions":[{"id":%q,"after":0}]}`, id)
	req, _ := http.NewRequestWithContext(ctx, "POST", a.url+"/api/assistant/events", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.Status)
	}
	a.prov.Queue(fake.Reply("phone reply"))
	if status, raw := call(t, "POST", a.url+"/api/assistant/sessions/"+id+"/messages", token, `{"text":"hello"}`); status != 202 {
		t.Fatalf("send: %d %s", status, raw)
	}
	scanner := bufio.NewScanner(res.Body)
	completed := false
	var reply string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event assistant.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.SessionID != id {
			t.Fatalf("unexpected session: %s", event.SessionID)
		}
		if event.Type == "delta" {
			reply += event.Text
		}
		if event.Type == "turn_completed" {
			completed = true
			break
		}
	}
	if !completed || reply != "phone reply" {
		t.Fatalf("completed=%v reply=%q error=%v", completed, reply, scanner.Err())
	}
	if _, err := phones.Revoke(entry.ID); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, res.Body); closed <- err }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("stream did not close cleanly on revocation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revoked phone retained its stream")
	}
	if status, _ := call(t, "POST", a.url+"/api/assistant/events", token, body); status != http.StatusUnauthorized {
		t.Fatalf("revoked phone reconnected: %d", status)
	}
}

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

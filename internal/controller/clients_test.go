package controller

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"werkbord/internal/clientaccess"
	"werkbord/internal/device"
	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func TestDesktopPairingIsSeparateFromOwnerPhoneAndRunnerDoors(t *testing.T) {
	cfg := testConfig(t)
	cfg.RequireToken = true
	on := true
	cfg.Network.Enabled = &on
	node := newFakeNode()
	c := startWith(t, cfg, node)
	waitFor(t, "private listener", func() bool { return node.addr() != "" })
	local := "http://" + c.Addr()
	private := "http://" + node.addr()
	owner, _ := cfg.ResolveToken(false)
	call := func(method, base, path, token, body string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, res.StatusCode, want, raw)
		}
		return raw
	}
	var pair struct{ Code string }
	raw := call("POST", local, "/api/clients/codes", owner, "{}", 201)
	if err := json.Unmarshal(raw, &pair); err != nil {
		t.Fatal(err)
	}
	enrollment := `{"code":"` + pair.Code + `","name":"Laptop","deviceId":"0123456789abcdef0123456789abcdef"}`
	call("POST", local, "/api/client/v1/enroll", "", enrollment, 401)
	var grant struct {
		clientaccess.Entry
		Token string
	}
	raw = call("POST", private, "/api/client/v1/enroll", "", enrollment, 201)
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatal(err)
	}
	call("POST", private, "/api/client/v1/enroll", "", enrollment, 403)
	call("GET", private, "/api/projects", grant.Token, "", 200)
	call("GET", private, "/api/projects", owner, "", 401)
	call("GET", local, "/api/projects", grant.Token, "", 401)
	for _, path := range []string{"/api/clients", "/api/phones", "/api/assistant/sessions", "/api/connections", "/api/personal/v1/workspaces", "/api/folders"} {
		call("GET", private, path, grant.Token, "", 404)
	}
	call("POST", private, "/api/runners/pair", grant.Token, "{}", 404)
	call("POST", private, "/api/runner/sync", grant.Token, "{}", 404)
	// A stream is revoked immediately without waiting for a heartbeat or token rotation.
	req, _ := http.NewRequest("GET", private+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != 200 {
		t.Fatal(stream.Status)
	}
	reader := bufio.NewReader(stream.Body)
	if _, err = reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	// Inject private and reviewed events into the same durable log. Only reviewed
	// event kinds may reach this grant, including during replay.
	emitted := []domain.Event{}
	err = c.db.Update(req.Context(), func(tx store.Tx) error {
		for _, kind := range []domain.EventType{"personal.record.created", domain.EventTaskUpdated} {
			e, _ := domain.NewEvent(kind, map[string]string{"marker": string(kind)})
			if err := tx.Events().Append(req.Context(), &e); err != nil {
				return err
			}
			emitted = append(emitted, e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c.broker.Publish(emitted...)
	visible := make(chan string, 1)
	go func() {
		var seen strings.Builder
		for {
			line, e := reader.ReadString('\n')
			seen.WriteString(line)
			if e != nil || strings.Contains(line, "event: task.updated") {
				visible <- seen.String()
				return
			}
		}
	}()
	select {
	case seen := <-visible:
		if strings.Contains(seen, "personal.record.created") {
			t.Fatal("private event leaked before revocation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reviewed event missing")
	}
	call("DELETE", local, "/api/clients/"+grant.ID, owner, "", 204)
	done := make(chan []byte, 1)
	go func() { raw, _ := io.ReadAll(reader); done <- raw }()
	select {
	case raw := <-done:
		if strings.Contains(string(raw), "personal.record.created") {
			t.Fatal("private event leaked")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revocation did not close open stream")
	}
	call("GET", private, "/api/projects", grant.Token, "", 401)
	call("GET", local, "/api/projects", owner, "", 200)
}
func TestRemoteSelectionPreventsControllerOpeningDatabase(t *testing.T) {
	cfg := testConfig(t)
	if err := device.Save(cfg.DataDir, device.State{Mode: "remote", Controller: "http://home.test"}); err != nil {
		t.Fatal(err)
	}
	c := New(cfg, nil, "test")
	if err := c.Start(context.Background()); err == nil {
		t.Fatal("competing controller started")
	}
}

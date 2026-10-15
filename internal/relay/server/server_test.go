package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devboard/internal/relay"
	"devboard/internal/relay/conformance"
	"devboard/internal/relay/server"
	"github.com/coder/websocket"
)

func TestCompanyRelayUsesWorkerProtocol(t *testing.T) {
	h, err := server.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	defer h.Close()
	conformance.Run(t, s.URL)
}

func TestClaimSurvivesRestartAndOnlyHashIsStored(t *testing.T) {
	dir := t.TempDir()
	room := relay.NewRoom()
	h, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, _ := relay.WebSocketURL(s.URL, room.ID, "runner")
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{Subprotocols: []string{relay.Subprotocol, relay.AuthPrefix + room.JoinKey}})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(dir, room.ID))
	if err != nil || string(stored) != relay.JoinKeyHash(room.JoinKey) {
		t.Fatalf("claim is not a join-key hash: %v", err)
	}
	_ = c.CloseNow()
	h.Close()
	s.Close()
	h, err = server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s = httptest.NewServer(h)
	defer s.Close()
	defer h.Close()
	for _, tc := range []struct {
		key  string
		want int
	}{{room.JoinKey, 200}, {relay.NewRoom().JoinKey, 401}} {
		req, _ := http.NewRequest("GET", s.URL+"/v1/room/"+room.ID+"/status", nil)
		req.Header.Set("Authorization", "Bearer "+tc.key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("status %d, want %d", res.StatusCode, tc.want)
		}
	}
}

// Package relaytest is an in-process relay with the same behaviour as relay/worker, so the
// runner client and the conformance suite run in `go test` without Cloudflare or Node.
// internal/relay/conformance runs against both this and the real Worker.
package relaytest

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"devboard/internal/relay"
)

var (
	roomRE = regexp.MustCompile(`^/v1/room/([0-9a-f]{32})$`)
	keyRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

type room struct {
	mu     sync.Mutex
	auth   string
	runner *websocket.Conn
	phones map[relay.PeerID]*websocket.Conn
}

// Server is the fake relay.
type Server struct {
	*httptest.Server
	mu    sync.Mutex
	rooms map[string]*room
}

func New() *Server {
	s := &Server{rooms: map[string]*room{}}
	s.Server = httptest.NewServer(s)
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/health" {
		writeJSON(w, 200, map[string]any{"ok": true, "protocol": 1, "maxFrame": relay.MaxFrame})
		return
	}
	m := roomRE.FindStringSubmatch(r.URL.Path)
	if m == nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if r.Header.Get("Upgrade") != "websocket" {
		writeJSON(w, 426, map[string]string{"error": "websocket required"})
		return
	}
	role := r.URL.Query().Get("role")
	if role != "runner" && role != "phone" {
		writeJSON(w, 400, map[string]string{"error": "role must be runner or phone"})
		return
	}
	key := ""
	hasProto := false
	for _, p := range strings.Split(strings.ReplaceAll(r.Header.Get("Sec-WebSocket-Protocol"), " ", ""), ",") {
		if p == relay.Subprotocol {
			hasProto = true
		}
		if len(p) > len(relay.AuthPrefix) && p[:len(relay.AuthPrefix)] == relay.AuthPrefix {
			key = p[len(relay.AuthPrefix):]
		}
	}
	if !hasProto || !keyRE.MatchString(key) {
		writeJSON(w, 401, map[string]string{"error": "missing or malformed join key"})
		return
	}
	digest := relay.JoinKeyHash(key)

	s.mu.Lock()
	rm := s.rooms[m[1]]
	if rm == nil {
		rm = &room{phones: map[relay.PeerID]*websocket.Conn{}}
		s.rooms[m[1]] = rm
	}
	s.mu.Unlock()

	rm.mu.Lock()
	if role == "runner" {
		if rm.auth == "" {
			rm.auth = digest
		}
	}
	if rm.auth == "" || subtle.ConstantTimeCompare([]byte(rm.auth), []byte(digest)) != 1 {
		rm.mu.Unlock()
		writeJSON(w, 401, map[string]string{"error": "wrong join key"})
		return
	}
	if role == "phone" {
		if rm.runner == nil {
			rm.mu.Unlock()
			writeJSON(w, 503, map[string]string{"error": "runner offline"})
			return
		}
		if len(rm.phones) >= relay.MaxPhones {
			rm.mu.Unlock()
			writeJSON(w, 429, map[string]string{"error": "too many phones"})
			return
		}
	}
	rm.mu.Unlock()

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{relay.Subprotocol}})
	if err != nil {
		return
	}
	c.SetReadLimit(relay.MaxFrame)
	if role == "runner" {
		s.runner(rm, c)
	} else {
		s.phone(rm, c)
	}
}

func (s *Server) runner(rm *room, c *websocket.Conn) {
	rm.mu.Lock()
	old := rm.runner
	phones := rm.phones
	rm.runner, rm.phones = c, map[relay.PeerID]*websocket.Conn{}
	rm.mu.Unlock()
	if old != nil {
		_ = old.Close(relay.CloseReplaced, "replaced by a newer runner connection")
	}
	for _, p := range phones {
		_ = p.Close(relay.CloseRunnerGone, "runner reconnected")
	}
	ctx := context.Background()
	for {
		typ, b, err := c.Read(ctx)
		if err != nil {
			break
		}
		if typ == websocket.MessageText {
			if string(b) == relay.PingText {
				_ = c.Write(ctx, websocket.MessageText, []byte(relay.PongText))
			}
			continue
		}
		t, peer, payload, err := relay.ParseFrame(b)
		if err != nil {
			continue
		}
		rm.mu.Lock()
		p := rm.phones[peer]
		rm.mu.Unlock()
		if p == nil {
			continue
		}
		switch t {
		case relay.TData:
			_ = p.Write(ctx, websocket.MessageBinary, payload)
		case relay.TKick:
			_ = p.Close(relay.CloseKicked, "removed by the runner")
		}
	}
	rm.mu.Lock()
	if rm.runner == c {
		rm.runner = nil
		phones = rm.phones
		rm.phones = map[relay.PeerID]*websocket.Conn{}
	} else {
		phones = nil
	}
	rm.mu.Unlock()
	for _, p := range phones {
		_ = p.Close(relay.CloseRunnerGone, "runner went away")
	}
}

func (s *Server) phone(rm *room, c *websocket.Conn) {
	var peer relay.PeerID
	_, _ = rand.Read(peer[:])
	rm.mu.Lock()
	runner := rm.runner
	rm.phones[peer] = c
	rm.mu.Unlock()
	ctx := context.Background()
	if runner != nil {
		_ = runner.Write(ctx, websocket.MessageBinary, relay.Frame(relay.TEvent, peer, []byte{relay.EvConnected}))
	}
	defer func() {
		rm.mu.Lock()
		delete(rm.phones, peer)
		runner := rm.runner
		rm.mu.Unlock()
		if runner != nil {
			_ = runner.Write(ctx, websocket.MessageBinary, relay.Frame(relay.TEvent, peer, []byte{relay.EvDisconnected}))
		}
		_ = c.CloseNow()
	}()
	tokens, at := 200.0, time.Now()
	for {
		typ, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			if string(b) == relay.PingText {
				_ = c.Write(ctx, websocket.MessageText, []byte(relay.PongText))
			}
			continue
		}
		now := time.Now()
		tokens = min(200, tokens+now.Sub(at).Seconds()*50)
		at = now
		if tokens < 1 {
			_ = c.Close(relay.CloseRateLimited, "rate limit")
			return
		}
		tokens--
		rm.mu.Lock()
		runner := rm.runner
		rm.mu.Unlock()
		if runner == nil {
			_ = c.Close(relay.CloseRunnerAway, "runner offline")
			return
		}
		_ = runner.Write(ctx, websocket.MessageBinary, relay.Frame(relay.TData, peer, b))
	}
}

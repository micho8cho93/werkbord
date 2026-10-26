// Package server implements the opaque relay protocol used by the Worker.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"werkbord/internal/relay"
)

var (
	roomRE = regexp.MustCompile(`^/v1/room/([0-9a-f]{32})$`)
	keyRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

	statusRE = regexp.MustCompile(`^/v1/room/([0-9a-f]{32})/status$`)
	bearerRE = regexp.MustCompile(`^Bearer ([A-Za-z0-9_-]{43})$`)
)

type room struct {
	mu     sync.Mutex
	auth   string
	runner *websocket.Conn
	phones map[relay.PeerID]*websocket.Conn
}

// Server forwards ciphertext; it never receives application credentials or encryption keys.
type Server struct {
	mu       sync.Mutex
	rooms    map[string]*room
	claimDir string
}

func New() *Server { return &Server{rooms: map[string]*room{}} }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if m := statusRE.FindStringSubmatch(r.URL.Path); m != nil {
		s.status(w, r, m[1])
		return
	}
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
		if role != "runner" {
			s.mu.Unlock()
			writeJSON(w, 401, map[string]string{"error": "unknown room"})
			return
		}
		if len(s.rooms) >= 1024 {
			s.mu.Unlock()
			writeJSON(w, 429, map[string]string{"error": "room limit"})
			return
		}
		rm = &room{phones: map[relay.PeerID]*websocket.Conn{}}
		s.rooms[m[1]] = rm
	}
	s.mu.Unlock()

	rm.mu.Lock()
	if role == "runner" {
		if rm.auth == "" {
			if s.saveClaim(m[1], digest) != nil {
				rm.mu.Unlock()
				writeJSON(w, 503, map[string]string{"error": "claim unavailable"})
				return
			}
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
	// Keep admission, upgrade and registration under one lock. Dial can return as soon as Accept writes the handshake;
	// the next connection must see this one registered before checking runner availability or the phone limit.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{relay.Subprotocol}, InsecureSkipVerify: true})
	if err != nil {
		rm.mu.Unlock()
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
	// ServeHTTP holds rm.mu until this connection is registered.
	old := rm.runner
	phones := rm.phones
	rm.runner, rm.phones = c, map[relay.PeerID]*websocket.Conn{}
	rm.mu.Unlock()
	if old != nil {
		go func() { _ = old.Close(relay.CloseReplaced, "replaced by a newer runner connection") }()
	}
	for _, p := range phones {
		go func() { _ = p.Close(relay.CloseRunnerGone, "runner reconnected") }()
	}
	ctx := context.Background()
	defer c.CloseNow()
	tokens, at := 200.0, time.Now()
	for {
		typ, b, err := c.Read(ctx)
		if err != nil {
			break
		}
		if typ == websocket.MessageText {
			if string(b) == relay.PingText {
				_ = write(c, websocket.MessageText, []byte(relay.PongText))
			}
			continue
		}
		now := time.Now()
		tokens = min(200, tokens+now.Sub(at).Seconds()*50)
		at = now
		if tokens < 1 {
			_ = c.Close(relay.CloseRateLimited, "rate limit")
			break
		}
		tokens--
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
			_ = write(p, websocket.MessageBinary, payload)
		case relay.TKick:
			// A graceful close waits for the phone to answer; it must not hold up this runner's other traffic (the
			// real relay does not wait either).
			go func() { _ = p.Close(relay.CloseKicked, "removed by the runner") }()
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
		go func() { _ = p.Close(relay.CloseRunnerGone, "runner went away") }()
	}
}

func (s *Server) phone(rm *room, c *websocket.Conn) {
	// ServeHTTP holds rm.mu until this connection is registered.
	var peer relay.PeerID
	_, _ = rand.Read(peer[:])
	runner := rm.runner
	rm.phones[peer] = c
	rm.mu.Unlock()
	ctx := context.Background()
	defer c.CloseNow()
	if runner != nil {
		_ = write(runner, websocket.MessageBinary, relay.Frame(relay.TEvent, peer, []byte{relay.EvConnected}))
	}
	defer func() {
		rm.mu.Lock()
		delete(rm.phones, peer)
		runner := rm.runner
		rm.mu.Unlock()
		if runner != nil {
			_ = write(runner, websocket.MessageBinary, relay.Frame(relay.TEvent, peer, []byte{relay.EvDisconnected}))
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
				_ = write(c, websocket.MessageText, []byte(relay.PongText))
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
		_ = write(runner, websocket.MessageBinary, relay.Frame(relay.TData, peer, b))
	}
}

// status answers GET /v1/room/<id>/status: whether a runner is connected, to a caller with the room's key.
func (s *Server) status(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "GET only"})
		return
	}
	m := bearerRE.FindStringSubmatch(r.Header.Get("Authorization"))
	if m == nil {
		writeJSON(w, 401, map[string]string{"error": "missing or malformed join key"})
		return
	}
	s.mu.Lock()
	rm := s.rooms[id]
	s.mu.Unlock()
	if rm == nil {
		writeJSON(w, 401, map[string]string{"error": "wrong join key"})
		return
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.auth == "" || subtle.ConstantTimeCompare([]byte(rm.auth), []byte(relay.JoinKeyHash(m[1]))) != 1 {
		writeJSON(w, 401, map[string]string{"error": "wrong join key"})
		return
	}
	writeJSON(w, 200, map[string]any{"runner": rm.runner != nil, "phones": len(rm.phones)})
}

// Writes are bounded so an unresponsive peer cannot hold a room indefinitely.
func write(c *websocket.Conn, typ websocket.MessageType, b []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Write(ctx, typ, b)
}

// Close ends upgraded connections as well as their pending reads.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rm := range s.rooms {
		rm.mu.Lock()
		if rm.runner != nil {
			_ = rm.runner.CloseNow()
		}
		for _, p := range rm.phones {
			_ = p.CloseNow()
		}
		rm.mu.Unlock()
	}
}

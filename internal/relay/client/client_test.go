package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"devboard/internal/relay"
	"devboard/internal/relay/e2e"
	"devboard/internal/relay/relaytest"
)

type secrets struct {
	mu        sync.Mutex
	m         map[e2e.PSKID][]byte
	confirmed []e2e.PSKID
}

func (s *secrets) Lookup(id e2e.PSKID) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[id]
	return p, ok
}
func (s *secrets) Confirm(id e2e.PSKID) {
	s.mu.Lock()
	s.confirmed = append(s.confirmed, id)
	s.mu.Unlock()
}

type msg struct {
	peer relay.PeerID
	body string
}

type handler struct{ in, gone chan msg }

func (h handler) Message(p relay.PeerID, _ e2e.PSKID, b []byte) { h.in <- msg{p, string(b)} }
func (h handler) PeerGone(p relay.PeerID)                       { h.gone <- msg{peer: p} }

type rig struct {
	base    string
	room    relay.Room
	id      *e2e.Identity
	psk     []byte
	secrets *secrets
	h       handler
	c       *Client
}

func newRig(t *testing.T, base string) *rig {
	t.Helper()
	id, _ := e2e.NewIdentity()
	psk := e2e.NewSecret()
	r := &rig{base: base, room: relay.NewRoom(), id: id, psk: psk,
		secrets: &secrets{m: map[e2e.PSKID][]byte{e2e.IDOf(psk): psk}},
		h:       handler{make(chan msg, 16), make(chan msg, 16)}}
	r.c = New(Config{BaseURL: base, Room: r.room, Identity: id, Secrets: r.secrets, Handler: r.h,
		PingInterval: 200 * time.Millisecond, MinBackoff: 20 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, QuotaBackoff: 20 * time.Millisecond})
	return r
}

func (r *rig) start(t *testing.T) (done chan error, stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done = make(chan error, 1)
	go func() { done <- r.c.Run(ctx) }()
	t.Cleanup(cancel)
	waitFor(t, func() bool { return r.c.Status().Connected })
	return done, cancel
}

func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}

// phone is the other end: what the browser does.
type phone struct {
	c *websocket.Conn
	s *e2e.Session
}

func (r *rig) phone(t *testing.T) *phone {
	t.Helper()
	u, _ := relay.WebSocketURL(r.base, r.room.ID, "phone")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{Subprotocols: []string{relay.Subprotocol, relay.AuthPrefix + r.room.JoinKey}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	hello, in, _ := e2e.Initiate(r.id.Public(), r.psk)
	if err := c.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	_, accept, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s, err := in.Finish(accept)
	if err != nil {
		t.Fatal(err)
	}
	return &phone{c, s}
}

func (p *phone) send(t *testing.T, s string) {
	t.Helper()
	f, _ := p.s.Seal([]byte(s))
	if err := p.c.Write(context.Background(), websocket.MessageBinary, f); err != nil {
		t.Fatal(err)
	}
}

func (p *phone) recv(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b, err := p.c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := p.s.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(pt)
}

func runAgainst(t *testing.T, base string) {
	t.Run("encrypted round trip, confirm once, kick", func(t *testing.T) {
		r := newRig(t, base)
		r.start(t)
		p := r.phone(t)
		p.send(t, "hello")
		m := <-r.h.in
		if m.body != "hello" {
			t.Fatalf("got %q", m.body)
		}
		if err := r.c.Send(context.Background(), m.peer, []byte("welcome")); err != nil {
			t.Fatal(err)
		}
		if got := p.recv(t); got != "welcome" {
			t.Fatalf("got %q", got)
		}
		p.send(t, "again")
		<-r.h.in
		r.secrets.mu.Lock()
		n := len(r.secrets.confirmed)
		r.secrets.mu.Unlock()
		if n != 1 {
			t.Fatalf("secret confirmed %d times, want 1", n)
		}
		r.c.Kick(context.Background(), m.peer)
		<-r.h.gone
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, _, err := p.c.Read(ctx); websocket.CloseStatus(err) != relay.CloseKicked {
			t.Fatalf("phone not kicked: %v", err)
		}
	})

	t.Run("a phone with the wrong secret is dropped", func(t *testing.T) {
		r := newRig(t, base)
		r.start(t)
		u, _ := relay.WebSocketURL(base, r.room.ID, "phone")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{Subprotocols: []string{relay.Subprotocol, relay.AuthPrefix + r.room.JoinKey}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		hello, _, _ := e2e.Initiate(r.id.Public(), e2e.NewSecret())
		_ = c.Write(ctx, websocket.MessageBinary, hello)
		if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != relay.CloseKicked {
			t.Fatalf("expected kick, got %v", err)
		}
	})

	t.Run("a wrong join key stops the client for good", func(t *testing.T) {
		r := newRig(t, base)
		r.start(t) // claims the room
		bad := newRig(t, base)
		bad.c.cfg.Room.ID = r.room.ID
		done, _ := bad.start2(t)
		select {
		case err := <-done:
			if !errors.Is(err, ErrRejected) {
				t.Fatalf("got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("client kept retrying")
		}
	})
}

func (r *rig) start2(t *testing.T) (chan error, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.c.Run(ctx) }()
	t.Cleanup(cancel)
	return done, cancel
}

func TestAgainstInProcessRelay(t *testing.T) {
	s := relaytest.New()
	defer s.Close()
	runAgainst(t, s.URL)
}

func TestAgainstCloudflareWorker(t *testing.T) {
	base := os.Getenv("WERKBORD_RELAY_TEST_URL")
	if base == "" {
		t.Skip("set WERKBORD_RELAY_TEST_URL to run against a real Worker")
	}
	runAgainst(t, base)
}

func TestReconnectsAndReportsPeersGone(t *testing.T) {
	var mu sync.Mutex
	var inner *relaytest.Server
	inner = relaytest.New()
	defer func() { inner.Close() }()
	// A front door we can make fail: first 3 attempts get 503 (as when the free quota is spent).
	fails := 3
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		f := fails > 0
		if f {
			fails--
		}
		mu.Unlock()
		if f {
			http.Error(w, "quota", http.StatusServiceUnavailable)
			return
		}
		inner.Config.Handler.ServeHTTP(w, req)
	}))
	defer front.Close()
	r := newRig(t, front.URL)
	r.start(t)
	if mu.Lock(); fails != 0 {
		t.Fatalf("expected the client to retry through the 503s, %d left", fails)
	}
	mu.Unlock()
	st := r.c.Status()
	if !st.Connected {
		t.Fatalf("status %+v", st)
	}
	p := r.phone(t)
	p.send(t, "x")
	<-r.h.in
	// Drop the link from the relay side by replacing the runner: the old client sees the
	// close, reports the phone gone and reconnects on its own.
	other := newRig(t, front.URL)
	other.room, other.c.cfg.Room = r.room, r.room
	other.start(t)
	<-r.h.gone
}

// Package client is the runner's side of the relay: one outbound WebSocket to the relay,
// kept up with backoff, over which any number of phones are multiplexed. Each phone's traffic
// is end-to-end encrypted (internal/relay/e2e); the relay sees only ciphertext.
//
// It is built for the Cloudflare Workers Free plan: one connection, no traffic of its own
// beyond a text "ping" the relay answers without waking (free of charge), and a long, jittered
// backoff when the relay refuses or is out of its daily quota.
package client

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"werkbord/internal/relay"
	"werkbord/internal/relay/e2e"
)

// ErrRejected: the relay refused this runner's join key. Retrying cannot help.
var ErrRejected = errors.New("relay: join key rejected")

// Handler receives what phones send. Calls come from the read loop, in order; keep them short.
type Handler interface {
	// Message is a decrypted message from an authenticated phone.
	Message(peer relay.PeerID, secret e2e.PSKID, msg []byte)
	// PeerGone: that phone's session ended (it left, was kicked, or the relay link dropped).
	PeerGone(peer relay.PeerID)
}

type Config struct {
	BaseURL  string // https://relay.example.workers.dev
	Room     relay.Room
	Identity *e2e.Identity
	Secrets  e2e.PSKStore
	Handler  Handler

	PingInterval time.Duration // default 45s; the relay drops quiet sockets after about 100s
	MinBackoff   time.Duration // default 1s
	MaxBackoff   time.Duration // default 5m
	QuotaBackoff time.Duration // floor after a 429/5xx from the relay; default 30s
	HTTPClient   *http.Client
}

// Status is a snapshot for `werkbord doctor` and the UI.
type Status struct {
	Connected bool
	Peers     int
	LastError string
	RetryIn   time.Duration
}

type peerState struct {
	sess      *e2e.Session
	secret    e2e.PSKID
	confirmed bool
}

type Client struct {
	cfg Config

	wmu  sync.Mutex // serialises Seal+Write so counters stay in wire order
	mu   sync.Mutex
	conn *websocket.Conn
	peer map[relay.PeerID]*peerState
	st   Status
}

func New(cfg Config) *Client {
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 45 * time.Second
	}
	if cfg.MinBackoff == 0 {
		cfg.MinBackoff = time.Second
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 5 * time.Minute
	}
	if cfg.QuotaBackoff == 0 {
		cfg.QuotaBackoff = 30 * time.Second
	}
	return &Client{cfg: cfg, peer: map[relay.PeerID]*peerState{}}
}

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.st
	s.Peers = len(c.peer)
	return s
}

// Run keeps the connection up until ctx ends or the relay rejects the join key.
func (c *Client) Run(ctx context.Context) error {
	backoff := c.cfg.MinBackoff
	for {
		started := time.Now()
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var rej *rejected
		if errors.As(err, &rej) {
			if rej.status == http.StatusUnauthorized {
				c.setErr(ErrRejected.Error(), 0)
				return ErrRejected
			}
			if backoff < c.cfg.QuotaBackoff {
				backoff = c.cfg.QuotaBackoff // 429, 5xx: the relay is full, over quota or down
			}
		}
		if time.Since(started) > 2*c.cfg.PingInterval {
			backoff = c.cfg.MinBackoff // it was up for a while: start again from the bottom
		}
		wait := time.Duration(float64(backoff) * (0.8 + 0.4*rand.Float64()))
		c.setErr(err.Error(), wait)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		backoff = min(backoff*2, c.cfg.MaxBackoff)
	}
}

type rejected struct{ status int }

func (r *rejected) Error() string { return fmt.Sprintf("relay answered %d", r.status) }

func (c *Client) setErr(msg string, retry time.Duration) {
	c.mu.Lock()
	c.st.Connected, c.st.LastError, c.st.RetryIn = false, msg, retry
	c.mu.Unlock()
}

func (c *Client) session(ctx context.Context) error {
	u, err := relay.WebSocketURL(c.cfg.BaseURL, c.cfg.Room.ID, "runner")
	if err != nil {
		return err
	}
	conn, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{relay.Subprotocol, relay.AuthPrefix + c.cfg.Room.JoinKey},
		HTTPClient:   c.cfg.HTTPClient,
	})
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			return &rejected{resp.StatusCode}
		}
		return err
	}
	conn.SetReadLimit(relay.MaxFrame + 16)
	c.mu.Lock()
	c.conn = conn
	c.st = Status{Connected: true}
	c.mu.Unlock()

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	lastPong := make(chan struct{}, 1)
	go c.keepalive(sctx, conn, lastPong)

	err = c.readLoop(sctx, conn, lastPong)
	cancel()
	c.mu.Lock()
	gone := c.peer
	c.peer, c.conn = map[relay.PeerID]*peerState{}, nil
	c.mu.Unlock()
	for p := range gone {
		c.cfg.Handler.PeerGone(p)
	}
	_ = conn.CloseNow()
	return err
}

func (c *Client) keepalive(ctx context.Context, conn *websocket.Conn, pong <-chan struct{}) {
	t := time.NewTicker(c.cfg.PingInterval)
	defer t.Stop()
	missed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-pong:
			missed = 0
		case <-t.C:
			if missed >= 2 {
				_ = conn.Close(websocket.StatusGoingAway, "no pong")
				return
			}
			missed++
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = conn.Write(wctx, websocket.MessageText, []byte(relay.PingText))
			cancel()
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, pong chan<- struct{}) error {
	for {
		typ, b, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageText {
			if string(b) == relay.PongText {
				select {
				case pong <- struct{}{}:
				default:
				}
			}
			continue
		}
		t, peer, payload, err := relay.ParseFrame(b)
		if err != nil {
			continue
		}
		switch t {
		case relay.TEvent:
			if len(payload) == 1 && payload[0] == relay.EvDisconnected {
				c.mu.Lock()
				_, had := c.peer[peer]
				delete(c.peer, peer)
				c.mu.Unlock()
				if had {
					c.cfg.Handler.PeerGone(peer)
				}
			}
		case relay.TData:
			c.data(ctx, conn, peer, payload)
		}
	}
}

func (c *Client) data(ctx context.Context, conn *websocket.Conn, peer relay.PeerID, payload []byte) {
	if len(payload) == 0 {
		return
	}
	if payload[0] == e2e.TypeHello { // a new handshake, or a phone starting over
		accept, sess, secret, err := e2e.Respond(c.cfg.Identity, c.cfg.Secrets, payload)
		if err != nil {
			c.kick(ctx, conn, peer)
			return
		}
		c.mu.Lock()
		c.peer[peer] = &peerState{sess: sess, secret: secret}
		c.mu.Unlock()
		c.write(ctx, conn, relay.Frame(relay.TData, peer, accept))
		return
	}
	c.mu.Lock()
	ps := c.peer[peer]
	c.mu.Unlock()
	if ps == nil {
		c.kick(ctx, conn, peer)
		return
	}
	c.wmu.Lock()
	msg, err := ps.sess.Open(payload)
	c.wmu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.peer, peer)
		c.mu.Unlock()
		c.kick(ctx, conn, peer)
		c.cfg.Handler.PeerGone(peer)
		return
	}
	if !ps.confirmed {
		ps.confirmed = true
		c.cfg.Secrets.Confirm(ps.secret) // spends a single-use enrollment secret
	}
	c.cfg.Handler.Message(peer, ps.secret, msg)
}

func (c *Client) write(ctx context.Context, conn *websocket.Conn, frame []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return conn.Write(ctx, websocket.MessageBinary, frame)
}

func (c *Client) kick(ctx context.Context, conn *websocket.Conn, peer relay.PeerID) {
	_ = c.write(ctx, conn, relay.Frame(relay.TKick, peer, nil))
}

// Send encrypts msg for one phone and sends it.
func (c *Client) Send(ctx context.Context, peer relay.PeerID, msg []byte) error {
	c.mu.Lock()
	conn, ps := c.conn, c.peer[peer]
	c.mu.Unlock()
	if conn == nil || ps == nil {
		return errors.New("relay: no such phone")
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	sealed, err := ps.sess.Seal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, relay.Frame(relay.TData, peer, sealed))
}

// Kick removes one phone now (revocation).
func (c *Client) Kick(ctx context.Context, peer relay.PeerID) {
	c.mu.Lock()
	conn := c.conn
	_, had := c.peer[peer]
	delete(c.peer, peer)
	c.mu.Unlock()
	if conn != nil {
		c.kick(ctx, conn, peer)
	}
	if had {
		c.cfg.Handler.PeerGone(peer)
	}
}

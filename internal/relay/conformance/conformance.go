// Package conformance is the relay protocol's shared test suite (docs/RELAY.md). The same
// tests run against the in-process relay (internal/relay/relaytest) on every `go test`, and
// against the real Cloudflare Worker when WERKBORD_RELAY_TEST_URL points at one
// (`npm run dev` in relay/worker serves it on http://localhost:8787).
package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"devboard/internal/relay"
)

type env struct {
	t    *testing.T
	base string
	room relay.Room
}

func (e *env) dial(role string, key string) (*websocket.Conn, *http.Response, error) {
	u, err := relay.WebSocketURL(e.base, e.room.ID, role)
	if err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{Subprotocols: []string{relay.Subprotocol, relay.AuthPrefix + key}})
	if c != nil {
		c.SetReadLimit(relay.MaxFrame * 2)
	}
	return c, resp, err
}

func (e *env) must(role string) *websocket.Conn {
	e.t.Helper()
	c, _, err := e.dial(role, e.room.JoinKey)
	if err != nil {
		e.t.Fatalf("dial %s: %v", role, err)
	}
	e.t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func read(t *testing.T, c *websocket.Conn) (websocket.MessageType, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Read(ctx)
}

func readBin(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()
	typ, b, err := read(t, c)
	if err != nil || typ != websocket.MessageBinary {
		t.Fatalf("want binary frame, got %v %v", typ, err)
	}
	return b
}

func write(t *testing.T, c *websocket.Conn, typ websocket.MessageType, b []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Write(ctx, typ, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func closeCode(t *testing.T, c *websocket.Conn) int {
	t.Helper()
	_, _, err := read(t, c)
	if err == nil {
		t.Fatal("expected the connection to close")
	}
	return int(websocket.CloseStatus(err))
}

func status(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

// Run runs every test of the suite against the relay at base.
func Run(t *testing.T, base string) {
	fresh := func(t *testing.T) *env { return &env{t: t, base: base, room: relay.NewRoom()} }

	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(base + "/v1/health")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var h struct {
			OK       bool
			Protocol int
			MaxFrame int
		}
		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, &h); err != nil || !h.OK || h.Protocol != 1 || h.MaxFrame != relay.MaxFrame {
			t.Fatalf("health: %s", b)
		}
	})

	t.Run("a plain request to a room is refused", func(t *testing.T) {
		e := fresh(t)
		resp, err := http.Get(base + "/v1/room/" + e.room.ID + "?role=runner")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 426 {
			t.Fatalf("status %d, want 426", resp.StatusCode)
		}
	})

	t.Run("unknown paths and malformed rooms are 404", func(t *testing.T) {
		for _, p := range []string{"/", "/v1/room/xyz", "/v1/room/" + "0123456789ABCDEF0123456789ABCDEF"} {
			resp, err := http.Get(base + p)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 404 {
				t.Fatalf("%s: status %d", p, resp.StatusCode)
			}
		}
	})

	t.Run("a bad role or a missing key is refused before upgrade", func(t *testing.T) {
		e := fresh(t)
		if _, resp, err := e.dial("admin", e.room.JoinKey); err == nil || status(resp) != 400 {
			t.Fatalf("role: %v %d", err, status(resp))
		}
		if _, resp, err := e.dial("runner", "short"); err == nil || status(resp) != 401 {
			t.Fatalf("key: %v %d", err, status(resp))
		}
	})

	t.Run("the first runner claims the room and others need its key", func(t *testing.T) {
		e := fresh(t)
		e.must("runner")
		other := relay.NewRoom()
		if _, resp, err := e.dial("runner", other.JoinKey); err == nil || status(resp) != 401 {
			t.Fatalf("wrong key runner: %v %d", err, status(resp))
		}
		if _, resp, err := e.dial("phone", other.JoinKey); err == nil || status(resp) != 401 {
			t.Fatalf("wrong key phone: %v %d", err, status(resp))
		}
	})

	t.Run("a phone cannot claim a room, and cannot join one with no runner", func(t *testing.T) {
		e := fresh(t)
		if _, resp, err := e.dial("phone", e.room.JoinKey); err == nil || status(resp) != 401 {
			t.Fatalf("unclaimed: %v %d", err, status(resp))
		}
		r := e.must("runner")
		_ = r.Close(websocket.StatusNormalClosure, "")
		time.Sleep(300 * time.Millisecond)
		if _, resp, err := e.dial("phone", e.room.JoinKey); err == nil || status(resp) != 503 {
			t.Fatalf("offline: %v %d", err, status(resp))
		}
	})

	t.Run("bytes flow both ways and the runner is told who is who", func(t *testing.T) {
		e := fresh(t)
		runner := e.must("runner")
		phone := e.must("phone")

		f := readBin(t, runner)
		typ, peer, payload, err := relay.ParseFrame(f)
		if err != nil || typ != relay.TEvent || !bytes.Equal(payload, []byte{relay.EvConnected}) {
			t.Fatalf("connected event: %v %v %v", typ, payload, err)
		}
		write(t, phone, websocket.MessageBinary, []byte("up"))
		typ, p2, payload, _ := relay.ParseFrame(readBin(t, runner))
		if typ != relay.TData || p2 != peer || string(payload) != "up" {
			t.Fatalf("phone->runner: %v %v %q", typ, p2, payload)
		}
		write(t, runner, websocket.MessageBinary, relay.Frame(relay.TData, peer, []byte("down")))
		if got := readBin(t, phone); string(got) != "down" {
			t.Fatalf("runner->phone: %q", got)
		}
		_ = phone.Close(websocket.StatusNormalClosure, "")
		typ, p3, payload, _ := relay.ParseFrame(readBin(t, runner))
		if typ != relay.TEvent || p3 != peer || !bytes.Equal(payload, []byte{relay.EvDisconnected}) {
			t.Fatalf("disconnected event: %v %v", typ, payload)
		}
	})

	t.Run("two phones do not hear each other", func(t *testing.T) {
		e := fresh(t)
		runner := e.must("runner")
		a, b := e.must("phone"), e.must("phone")
		_, pa, _, _ := relay.ParseFrame(readBin(t, runner))
		_, pb, _, _ := relay.ParseFrame(readBin(t, runner))
		if pa == pb {
			t.Fatal("peers share an id")
		}
		write(t, runner, websocket.MessageBinary, relay.Frame(relay.TData, pb, []byte("for b")))
		if got := readBin(t, b); string(got) != "for b" {
			t.Fatalf("b: %q", got)
		}
		write(t, a, websocket.MessageBinary, []byte("from a"))
		_, who, payload, _ := relay.ParseFrame(readBin(t, runner))
		if who == pb || string(payload) != "from a" {
			t.Fatalf("attribution: %v %q", who, payload)
		}
	})

	t.Run("the runner can remove a phone", func(t *testing.T) {
		e := fresh(t)
		runner := e.must("runner")
		phone := e.must("phone")
		_, peer, _, _ := relay.ParseFrame(readBin(t, runner))
		write(t, runner, websocket.MessageBinary, relay.Frame(relay.TKick, peer, nil))
		if code := closeCode(t, phone); code != relay.CloseKicked {
			t.Fatalf("close code %d", code)
		}
	})

	t.Run("a newer runner connection replaces the old one and ends the phones", func(t *testing.T) {
		e := fresh(t)
		old := e.must("runner")
		phone := e.must("phone")
		readBin(t, old)
		e.must("runner")
		if code := closeCode(t, old); code != relay.CloseReplaced {
			t.Fatalf("old runner close code %d", code)
		}
		if code := closeCode(t, phone); code != relay.CloseRunnerGone {
			t.Fatalf("phone close code %d", code)
		}
	})

	t.Run("when the runner leaves, phones are closed", func(t *testing.T) {
		e := fresh(t)
		runner := e.must("runner")
		phone := e.must("phone")
		readBin(t, runner)
		_ = runner.Close(websocket.StatusNormalClosure, "")
		if code := closeCode(t, phone); code != relay.CloseRunnerGone {
			t.Fatalf("close code %d", code)
		}
	})

	t.Run("an oversize frame closes the sender", func(t *testing.T) {
		e := fresh(t)
		runner := e.must("runner")
		phone := e.must("phone")
		readBin(t, runner)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = phone.Write(ctx, websocket.MessageBinary, make([]byte, relay.MaxFrame+1))
		if code := closeCode(t, phone); code != int(websocket.StatusMessageTooBig) {
			t.Fatalf("close code %d", code)
		}
	})

	t.Run("ping is answered with pong", func(t *testing.T) {
		e := fresh(t)
		runner := e.must("runner")
		write(t, runner, websocket.MessageText, []byte(relay.PingText))
		typ, b, err := read(t, runner)
		if err != nil || typ != websocket.MessageText || string(b) != relay.PongText {
			t.Fatalf("pong: %v %q %v", typ, b, err)
		}
	})

	t.Run("at most eight phones", func(t *testing.T) {
		e := fresh(t)
		e.must("runner")
		for i := 0; i < relay.MaxPhones; i++ {
			e.must("phone")
		}
		if _, resp, err := e.dial("phone", e.room.JoinKey); err == nil || status(resp) != 429 {
			t.Fatalf("ninth phone: %v %d", err, status(resp))
		}
	})

	t.Run("status tells a browser whether the runner is there, with the key", func(t *testing.T) {
		e := fresh(t)
		get := func(key string) (int, string, string) {
			req, _ := http.NewRequest("GET", base+"/v1/room/"+e.room.ID+"/status", nil)
			if key != "" {
				req.Header.Set("Authorization", "Bearer "+key)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, string(b), resp.Header.Get("Access-Control-Allow-Origin")
		}
		if code, _, _ := get(e.room.JoinKey); code != 401 {
			t.Fatalf("a room no runner has claimed: %d, want 401", code)
		}
		runner := e.must("runner")
		code, body, cors := get(e.room.JoinKey)
		var st struct {
			Runner bool
			Phones int
		}
		if err := json.Unmarshal([]byte(body), &st); code != 200 || err != nil || !st.Runner || st.Phones != 0 {
			t.Fatalf("with a runner: %d %s", code, body)
		}
		if cors != "*" {
			t.Fatalf("a browser on another origin needs Access-Control-Allow-Origin, got %q", cors)
		}
		if code, _, _ := get(relay.NewRoom().JoinKey); code != 401 {
			t.Fatalf("wrong key: %d, want 401", code)
		}
		if code, _, _ := get(""); code != 401 {
			t.Fatalf("no key: %d, want 401", code)
		}
		_ = runner.Close(websocket.StatusNormalClosure, "bye")
		deadline := time.Now().Add(5 * time.Second)
		for {
			_, body, _ := get(e.room.JoinKey)
			if json.Unmarshal([]byte(body), &st) == nil && !st.Runner {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("status still says a runner is there: %s", body)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"devboard/internal/relay"
	"devboard/internal/relay/e2e"
	"devboard/internal/relay/relaytest"
	"devboard/internal/relay/tunnel"
)

// phoneLine is a phone as the connector is: a relay connection, an end-to-end session, and the tunnel over it.
type phoneLine struct {
	t    *testing.T
	c    *websocket.Conn
	s    *e2e.Session
	next uint32
}

// dialPhone connects with a pairing. It returns the close code if the runner puts it off instead.
func b64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func dialPhone(t *testing.T, p relay.Pairing) (*phoneLine, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u, _ := relay.WebSocketURL(p.Relay, p.Room, "phone")
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{Subprotocols: []string{relay.Subprotocol, relay.AuthPrefix + p.JoinKey}})
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(relay.MaxFrame * 2)
	t.Cleanup(func() { _ = c.CloseNow() })
	runnerKey, secret := b64(t, p.RunnerKey), b64(t, p.Secret)
	hello, in, _ := e2e.Initiate(runnerKey, secret)
	if err := c.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	_, accept, err := c.Read(ctx)
	if err != nil {
		return nil, int(websocket.CloseStatus(err))
	}
	s, err := in.Finish(accept)
	if err != nil {
		t.Fatal(err)
	}
	return &phoneLine{t: t, c: c, s: s}, 0
}

func (p *phoneLine) do(method, path, token, body string) (int, string) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p.next++
	head := map[string]any{"m": method, "p": path, "h": map[string]string{"content-type": "application/json"}}
	if token != "" {
		head["h"].(map[string]string)["authorization"] = "Bearer " + token
	}
	hb, _ := json.Marshal(head)
	send := func(typ byte, payload []byte) {
		sealed, err := p.s.Seal(tunnel.Pack(typ, p.next, payload))
		if err != nil {
			p.t.Fatal(err)
		}
		if err := p.c.Write(ctx, websocket.MessageBinary, sealed); err != nil {
			p.t.Fatal(err)
		}
	}
	send(tunnel.ReqHead, hb)
	if body != "" {
		send(tunnel.ReqBody, []byte(body))
	}
	send(tunnel.ReqEnd, nil)
	status, out := 0, bytes.Buffer{}
	for {
		_, frame, err := p.c.Read(ctx)
		if err != nil {
			p.t.Fatalf("reading the answer to %s %s: %v", method, path, err)
		}
		msg, err := p.s.Open(frame)
		if err != nil {
			p.t.Fatal(err)
		}
		typ, _, payload, _ := tunnel.Unpack(msg)
		switch typ {
		case tunnel.ResHead:
			var h tunnel.Head
			_ = json.Unmarshal(payload, &h)
			status = h.Status
		case tunnel.ResBody:
			out.Write(payload)
		case tunnel.ResEnd:
			return status, out.String()
		case tunnel.Err:
			p.t.Fatalf("tunnel error: %s", payload)
		}
	}
}

func TestAPhoneEnrollsThroughTheRelayAndComesBackUntilItIsRevoked(t *testing.T) {
	rl := relaytest.New()
	defer rl.Close()
	cfg := defaultConfig(t)
	cfg.Relay.URL, cfg.Relay.ConnectorURL = rl.URL, "https://connect.example.pages.dev"
	c := startWith(t, cfg, newFakeNode())
	base := "http://" + c.Addr()
	tok, _ := c.cfg.ResolveToken(false)
	waitFor(t, "the controller to reach the relay", func() bool {
		_, body := get(t, base+"/api/relay", tok)
		return strings.Contains(body, `"connected":true`)
	})

	// The QR code is a plain https link to the connector, with the pairing after the "#".
	code, body := post(t, base+"/api/phones/codes", tok)
	var made struct {
		Code, Link, QRSVG, Via string
	}
	_ = json.Unmarshal([]byte(body), &made)
	if code != 201 || made.Via != "relay" || made.QRSVG == "" {
		t.Fatalf("codes = %d %.300s", code, body)
	}
	const prefix = "https://connect.example.pages.dev/connect/#"
	if !strings.HasPrefix(made.Link, prefix) {
		t.Fatalf("link = %.120s", made.Link)
	}
	pairing, err := relay.DecodePairing(strings.TrimPrefix(made.Link, prefix))
	if err != nil || pairing.Code != made.Code {
		t.Fatalf("pairing: %v %+v", err, pairing)
	}
	if strings.Contains(body, tok) || strings.Contains(made.Link, tok) {
		t.Fatal("the controller's token is in the link")
	}

	// The phone enrolls with the code the pairing carries; the app does exactly this.
	line, closed := dialPhone(t, pairing)
	if line == nil {
		t.Fatalf("not let in: %d", closed)
	}
	if st, body := line.do("GET", "/api/projects", "", ""); st != 401 {
		t.Fatalf("a phone with no sign-in got %d %s", st, body)
	}
	st, body := line.do("POST", "/api/phones/enroll", "", `{"code":"`+pairing.Code+`","name":"Test phone"}`)
	var enrolled struct{ ID, Token string }
	_ = json.Unmarshal([]byte(body), &enrolled)
	if st != 201 || !strings.HasPrefix(enrolled.Token, "wbp_") {
		t.Fatalf("enroll = %d %.200s", st, body)
	}
	if n := c.relay.store.Phones(); n != 1 {
		t.Fatalf("after enrolling, %d secrets are bound to phones, want 1", n)
	}
	if st, _ := line.do("GET", "/api/projects", enrolled.Token, ""); st != 200 {
		t.Fatalf("an enrolled phone reading projects = %d", st)
	}
	// A phone is a remote control, not a second owner: the same reviewed list as on the private network.
	if st, _ := line.do("PUT", "/api/settings/execution", enrolled.Token, `{}`); st != 403 {
		t.Fatalf("a phone changing settings = %d, want 403", st)
	}
	if st, _ := line.do("POST", "/api/phones/codes", enrolled.Token, ""); st != 403 {
		t.Fatalf("a phone making codes = %d, want 403", st)
	}
	if st, _ := line.do("GET", "/api/relay", enrolled.Token, ""); st != 403 {
		t.Fatalf("a phone reading the relay's state = %d, want 403", st)
	}

	// Coming back later with the same secret needs no new code.
	again, closed := dialPhone(t, pairing)
	if again == nil {
		t.Fatalf("an enrolled phone could not come back: %d", closed)
	}
	if st, _ := again.do("GET", "/api/phones/self", enrolled.Token, ""); st != 200 {
		t.Fatalf("self = %d", st)
	}
	// The same code a second time is refused: it was spent.
	if st, _ := again.do("POST", "/api/phones/enroll", "", `{"code":"`+pairing.Code+`","name":"Again"}`); st != 403 {
		t.Fatalf("a spent code = %d, want 403", st)
	}

	// Revoking the phone puts it off the relay and its secret no longer opens a session.
	req, _ := http.NewRequest("DELETE", base+"/api/phones/"+enrolled.ID, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("revoke: %v %v", err, resp)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := again.c.Read(ctx); websocket.CloseStatus(err) != relay.CloseKicked {
		t.Fatalf("a revoked phone's connection ended with %v, want it kicked", err)
	}
	if line, closed := dialPhone(t, pairing); line != nil || closed != relay.CloseKicked {
		t.Fatalf("a revoked phone came back (%v, %d)", line, closed)
	}
}

func TestWithoutRelaySettingsThereIsNoRelayAndTheLinkIsThePrivateNetworks(t *testing.T) {
	c := startWith(t, defaultConfig(t), newFakeNode())
	base := "http://" + c.Addr()
	tok, _ := c.cfg.ResolveToken(false)
	if _, body := get(t, base+"/api/relay", tok); !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("relay status = %s", body)
	}
	code, body := post(t, base+"/api/phones/codes", tok)
	if code != 201 || strings.Contains(body, `"via":"relay"`) {
		t.Fatalf("codes = %d %.200s", code, body)
	}
}

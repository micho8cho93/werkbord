package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"werkbord/internal/relay"
	"werkbord/internal/relay/e2e"
	"werkbord/internal/relay/relaytest"
	"werkbord/internal/relay/tunnel"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/hostclient"
	"werkbord/internal/team/service"
)

type encryptedPhone struct {
	c  *websocket.Conn
	s  *e2e.Session
	id uint32
}

func (p *encryptedPhone) request(t *testing.T, method, path string) (int, string) {
	t.Helper()
	p.id++
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	head, _ := json.Marshal(tunnel.Head{Method: method, Path: path})
	for _, b := range [][]byte{tunnel.Pack(tunnel.ReqHead, p.id, head), tunnel.Pack(tunnel.ReqEnd, p.id, nil)} {
		sealed, err := p.s.Seal(b)
		if err != nil {
			t.Fatal(err)
		}
		if err = p.c.Write(ctx, websocket.MessageBinary, sealed); err != nil {
			t.Fatal(err)
		}
	}
	status := 0
	body := ""
	for {
		_, b, err := p.c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := p.s.Open(b)
		if err != nil {
			t.Fatal(err)
		}
		typ, _, payload, err := tunnel.Unpack(plain)
		if err != nil {
			t.Fatal(err)
		}
		switch typ {
		case tunnel.ResHead:
			var h tunnel.Head
			if err := json.Unmarshal(payload, &h); err != nil {
				t.Fatal(err)
			}
			status = h.Status
		case tunnel.ResBody:
			body += string(payload)
		case tunnel.ResEnd:
			return status, body
		case tunnel.Err:
			t.Fatalf("tunnel error: %s", payload)
		}
	}
}

func TestTeamPhoneGrantIsReadOnlyScopedAndRevokedOnAnOpenConnection(t *testing.T) {
	relayServer := relaytest.New()
	defer relayServer.Close()
	d := testDaemon(t)
	d.o.Config.PhoneRelayEndpoint = relayServer.URL
	d.o.Config.PhoneConnectorEndpoint = "https://connect.company.example"
	if err := d.openPhone(); err != nil {
		t.Fatal(err)
	}
	v, err := d.setupVault(d.workspaceConfig())
	if err != nil {
		t.Fatal(err)
	}
	occupyAsHost(t, v, "tws_phone", "10.233.0.0/16")
	if err = d.loadDevice(); err != nil {
		t.Fatal(err)
	}
	d.memberID = "tmb_me"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer member-device-credential" {
			t.Error("member credential missing upstream")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/team/v1/my-work" {
			_ = json.NewEncoder(w).Encode(service.MyWork{InProgress: []service.WorkItem{ticketItem("ttk_phone", "tpj_phone", "WB-42", "Review login fix", domain.TicketInProgress)}})
			return
		}
		w.WriteHeader(404)
	}))
	defer upstream.Close()
	d.host, err = hostclient.New(hostclient.Options{Bases: []string{upstream.URL}, Token: "member-device-credential", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	d.lastSync = time.Now()
	response := daemonCall(d, "POST", "/api/device/v1/phones", "")
	if response.Code != 201 {
		t.Fatalf("grant: %d %s", response.Code, response.Body)
	}
	var grant struct{ ID, URL string }
	if err = json.Unmarshal(response.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	pairing, err := relay.DecodePairing(strings.SplitN(grant.URL, "#", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	if pairing.Lane != "team" || strings.Contains(grant.URL, d.key()) {
		t.Fatal("wrong lane or local credential leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	d.phone.tunnel.Context = ctx
	go func() { done <- d.phone.client.Run(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for !d.phone.client.Status().Connected {
		if time.Now().After(deadline) {
			t.Fatal("runner not connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if output := os.Getenv("WERKBORD_PHONE_BROWSER_FIXTURE"); output != "" {
		root, _ := filepath.Abs("../../../relay/connector/dist")
		connector := httptest.NewServer(http.FileServer(http.Dir(root)))
		defer connector.Close()
		grantURL := connector.URL + "/connect/#" + pairing.Encode()
		raw, _ := json.Marshal(map[string]string{"url": grantURL, "origin": connector.URL})
		if err := os.WriteFile(output, raw, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			d.mu.Lock()
			d.lastSync = time.Now()
			d.mu.Unlock()
			if _, err := os.Stat(output + ".done"); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	d.lastSync = time.Now()
	dialCtx, end := context.WithTimeout(ctx, 5*time.Second)
	defer end()
	u, _ := relay.WebSocketURL(relayServer.URL, pairing.Room, "phone")
	c, _, err := websocket.Dial(dialCtx, u, &websocket.DialOptions{Subprotocols: []string{relay.Subprotocol, relay.AuthPrefix + pairing.JoinKey}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	key, _ := base64.RawURLEncoding.DecodeString(pairing.RunnerKey)
	secret, _ := base64.RawURLEncoding.DecodeString(pairing.Secret)
	hello, init, _ := e2e.Initiate(key, secret)
	if err = c.Write(dialCtx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	_, accept, err := c.Read(dialCtx)
	if err != nil {
		t.Fatal(err)
	}
	session, err := init.Finish(accept)
	if err != nil {
		t.Fatal(err)
	}
	p := encryptedPhone{c: c, s: session}
	// A live summary uses only the locally pinned member credential.
	if status, body := p.request(t, "GET", "/api/summary"); status != 200 || !strings.Contains(body, "Review login fix") || strings.Contains(body, "member-device-credential") || strings.Contains(body, d.key()) {
		t.Fatalf("summary: %d %s", status, body)
	}
	if got := d.connectionReport(true); !got.RunnerConnected || got.PhonesPaired != 1 || got.PhonesConnected != 1 {
		t.Fatalf("authenticated phone not reported: %+v", got)
	}
	d.lastSync = time.Time{}
	if status, body := p.request(t, "GET", "/api/summary"); status != 200 || strings.Contains(body, "Review login fix") {
		t.Fatal("offline summary leaked stale work")
	}
	for _, tc := range []struct{ method, path string }{{"POST", "/api/summary"}, {"GET", "/api/device/v1/state"}, {"GET", "/api/team/v1/my-work"}, {"POST", "/api/device/v1/execution/start"}, {"GET", "/api/summary/../device/v1/state"}, {"HEAD", "/"}} {
		if status, _ := p.request(t, tc.method, tc.path); status != 403 {
			t.Fatalf("allowed %s %s: %d", tc.method, tc.path, status)
		}
	}
	d.memberID = "tmb_someone_else"
	if got := d.connectionReport(false); got.PhonesPaired != 0 || got.PhonesConnected != 0 {
		t.Fatalf("connection report crossed member scope: %+v", got)
	}
	if status, _ := p.request(t, "GET", "/api/summary"); status != 401 {
		t.Fatal("grant crossed member boundary")
	}
	d.memberID = "tmb_me"
	if err = os.WriteFile(filepath.Join(d.o.Config.DataDir, "leaving"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if status, _ := p.request(t, "GET", "/api/summary"); status != 401 {
		t.Fatal("leaving member kept access")
	}
	_ = os.Remove(filepath.Join(d.o.Config.DataDir, "leaving"))
	if status, _ := p.request(t, "GET", "/"); status != 200 {
		t.Fatal("phone app unavailable")
	}
	// Revocation removes the secret and closes this already-authenticated connection.
	if r := daemonCall(d, "DELETE", "/api/device/v1/phones/"+grant.ID, ""); r.Code != 204 {
		t.Fatal(r.Code)
	}
	if got := d.connectionReport(true); got.PhonesPaired != 0 || got.PhonesConnected != 0 {
		t.Fatalf("revoked phone still reported: %+v", got)
	}
	if _, ok := d.phone.store.Lookup(e2e.IDOf(secret)); ok {
		t.Fatal("revoked secret survived")
	}
	_, _, err = c.Read(dialCtx)
	if websocket.CloseStatus(err) != relay.CloseKicked {
		t.Fatalf("revocation did not kick phone: %v", err)
	}
}

func TestNoCompanyRelayExplainsBlockedPhoneAccess(t *testing.T) {
	d := testDaemon(t)
	for _, method := range []string{"GET", "POST"} {
		res := daemonCall(d, method, "/api/device/v1/phones", "")
		if !strings.Contains(res.Body.String(), phoneBlocked) {
			t.Fatalf("%s", res.Body)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/device/v1/phones", nil)
	rec := httptest.NewRecorder()
	d.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("grant issued without local authentication")
	}
}

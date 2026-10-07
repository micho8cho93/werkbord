package hostclient

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/team/domain"
)

var bg = context.Background()

var overlay = netip.MustParsePrefix("10.128.0.0/16")

func TestTheClientDialsOnlyThisComputerAndTheWorkspacesNetwork(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:7430", "http://[::1]:7430", "http://10.128.0.1:7430", "http://10.128.255.9:7430"} {
		if _, err := CheckBase(ok, overlay); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "https://127.0.0.1:7430", "http://192.168.1.1:7430", "http://10.129.0.1:7430", "http://8.8.8.8", "http://example.com:7430",
		"http://localhost:7430", "http://user@127.0.0.1:7430", "http://127.0.0.1:7430/x", "http://100.64.0.1:7430"} {
		if _, err := CheckBase(bad, overlay); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	// With no network known, only this computer.
	if _, err := CheckBase("http://10.128.0.1:7430", netip.Prefix{}); err == nil {
		t.Error("an address in a network that is not known was accepted")
	}
	// And the dial itself checks, whatever got into the base.
	for _, addr := range []string{"192.168.1.5:80", "example.com:80", "8.8.8.8:53", "[2001:db8::1]:80"} {
		if checkDial(addr, overlay) == nil {
			t.Errorf("would dial %s", addr)
		}
	}
	if _, err := New(Options{Bases: []string{"http://127.0.0.1:1"}, Token: ""}); err == nil {
		t.Error("a client with no credential")
	}
}

func TestATestTransportCannotBypassTheDestinationCheck(t *testing.T) {
	var dialed atomic.Bool
	c, err := New(Options{Bases: []string{"http://127.0.0.1:7430"}, Token: "private", Network: overlay,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed.Store(true)
			return nil, net.ErrClosed
		}})
	if err != nil {
		t.Fatal(err)
	}
	// Check the transport itself even if an invalid address somehow gets past initial validation.
	c.bases = []string{"http://8.8.8.8:80"}
	if _, err := c.Me(bg); err == nil || dialed.Load() {
		t.Fatalf("an outside destination reached the substituted dialer: %v", err)
	}
}

type host struct {
	srv  *httptest.Server
	hits atomic.Int32
	auth atomic.Value
}

func newHost(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) *host {
	x := &host{}
	x.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x.hits.Add(1)
		x.auth.Store(r.Header.Get("Authorization"))
		h(w, r)
	}))
	t.Cleanup(x.srv.Close)
	return x
}

func TestTheCredentialIsSentAndErrorsAreSaidInTheWorkspacesWords(t *testing.T) {
	h := newHost(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case APIPrefix + "/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"member": map[string]any{"id": "tmb_1", "name": "Bo", "role": "member"}, "workspace": map[string]any{"id": "tws_1", "name": "Acme"}})
		default:
			w.WriteHeader(403)
			_, _ = io.WriteString(w, `{"error":{"code":"forbidden","message":"your role cannot do that"}}`)
		}
	})
	c, err := New(Options{Bases: []string{h.srv.URL}, Token: "wbt_device", Network: overlay})
	if err != nil {
		t.Fatal(err)
	}
	me, err := c.Me(bg)
	if err != nil || me.Member.Name != "Bo" || me.Workspace.Name != "Acme" || h.auth.Load() != "Bearer wbt_device" {
		t.Fatalf("%+v %v %v", me, err, h.auth.Load())
	}
	_, err = c.Devices(bg)
	if !IsStatus(err, 403) || !strings.Contains(err.Error(), "your role cannot do that") {
		t.Fatalf("err = %v", err)
	}
}

// With three hosts and one down, the device carries on with the others, and remembers which one answered.
func TestAHostThatIsDownIsPassedOver(t *testing.T) {
	good := newHost(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"member":{"id":"tmb_1"},"workspace":{}}`)
	})
	readOnly := newHost(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `{"error":{"code":"read_only","message":"no quorum"}}`)
	})
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	c, err := New(Options{Bases: []string{deadURL, readOnly.srv.URL, good.srv.URL}, Token: "t", Network: overlay})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Me(bg); err != nil {
		t.Fatal(err)
	}
	if got := c.Bases()[0]; got != good.srv.URL {
		t.Fatalf("the host that answered is not tried first next time: %v", c.Bases())
	}
	before := readOnly.hits.Load()
	if _, err := c.Me(bg); err != nil || readOnly.hits.Load() != before {
		t.Fatalf("a second request went to the failing host again: %v", err)
	}
	// Everything down is said as such.
	c2, _ := New(Options{Bases: []string{deadURL}, Token: "t", Network: overlay})
	if _, err := c2.Me(bg); err == nil || !strings.Contains(err.Error(), ErrUnreachable.Error()) {
		t.Fatalf("err = %v", err)
	}
	// An answer that is a refusal is final, not a reason to ask another host.
	deny := newHost(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	other := newHost(t, func(w http.ResponseWriter, r *http.Request) {})
	c3, _ := New(Options{Bases: []string{deny.srv.URL, other.srv.URL}, Token: "t", Network: overlay})
	if err := c3.Do(bg, "GET", "/me", nil, nil); !IsStatus(err, 401) || other.hits.Load() != 0 {
		t.Fatalf("a 401 was not final: %v (the other host was asked %d times)", err, other.hits.Load())
	}
}

func TestARedirectIsNotFollowedAndOnlyTheTeamAPIIsAddressed(t *testing.T) {
	var leaked atomic.Value
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(r.Header.Get("Authorization")) }))
	defer elsewhere.Close()
	h := newHost(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, elsewhere.URL, http.StatusFound) })
	c, _ := New(Options{Bases: []string{h.srv.URL}, Token: "secret", Network: overlay})
	err := c.Do(bg, "GET", "/me", nil, nil)
	if !IsStatus(err, 302) || leaked.Load() != nil {
		t.Fatalf("a redirect was followed or the credential was sent on: %v %v", err, leaked.Load())
	}
	for _, path := range []string{"/", "/api/other", "/api/team/v1", "/etc/passwd", "http://x/api/team/v1/me", "/api/team/v1/../x"} {
		if _, _, _, err := c.Raw(bg, "GET", path, "", nil); err == nil && !strings.HasPrefix(path, APIPrefix+"/") {
			t.Errorf("%q was requested", path)
		}
	}
}

func TestTheHeartbeatAndTheInboxCarryOnlyWhatTheyShould(t *testing.T) {
	var gotBody atomic.Value
	h := newHost(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody.Store(r.Method + " " + r.URL.RequestURI() + " " + string(b))
		switch {
		case strings.HasSuffix(r.URL.Path, "/device/messages"):
			_, _ = io.WriteString(w, `[{"id":"msg_1","state":"queued","action":"fetch_runner_status","envelope":{"x":1}}]`)
		default:
			w.WriteHeader(204)
		}
	})
	c, _ := New(Options{Bases: []string{h.srv.URL}, Token: "t", Network: overlay, Timeout: time.Second})
	if err := c.Heartbeat(bg, domain.DeviceProfile{Platform: "darwin", Form: domain.FormDesktop, Version: "2.7.0"}); err != nil {
		t.Fatal(err)
	}
	body := gotBody.Load().(string)
	for _, want := range []string{"POST /api/team/v1/device/heartbeat", `"platform":"darwin"`, `"form":"desktop"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%q is missing from %s", want, body)
		}
	}
	for _, leak := range []string{"hostname", "path", "token", "env"} {
		if strings.Contains(body, leak) {
			t.Errorf("the heartbeat carries %q: %s", leak, body)
		}
	}
	ms, err := c.Inbox(bg, 15*time.Second)
	if err != nil || len(ms) != 1 || ms[0].ID != "msg_1" || !strings.Contains(gotBody.Load().(string), "wait=15") {
		t.Fatalf("%+v %v %v", ms, err, gotBody.Load())
	}
	if err := c.Ack(bg, "msg_1", domain.MessageDone, map[string]any{"ok": true}); err != nil || !strings.Contains(gotBody.Load().(string), `"state":"done"`) {
		t.Fatalf("%v %v", err, gotBody.Load())
	}
}

func TestAHandoffOfAnUnknownKindIsRefused(t *testing.T) {
	h := newHost(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"schema":"other/v9","prompt":"do things"}`)
	})
	c, _ := New(Options{Bases: []string{h.srv.URL}, Token: "t", Network: overlay})
	if _, err := c.Handoff(bg, "tpj_1", "ttk_1"); err == nil || !strings.Contains(err.Error(), "does not understand") {
		t.Fatalf("err = %v", err)
	}
}

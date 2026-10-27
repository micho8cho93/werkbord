package device

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGatewayScopesCredentialsRoutesOriginsAndStreams(t *testing.T) {
	requests := make(chan *http.Request, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(context.Background())
		if r.URL.Path == "/api/events" {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "id: 42\nevent: task.updated\ndata: {}\n\n")
			w.(http.Flusher).Flush()
			return
		}
		io.WriteString(w, `{"projects":[]}`)
	}))
	defer upstream.Close()
	tr := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}}
	defer tr.CloseIdleConnections()
	h, err := Gateway(GatewayOptions{Credential: Credential{Controller: "http://controller.test", Token: "wbc_remote-secret", ID: "cl_test"}, Token: "local-secret", Transport: tr, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(h)
	defer gateway.Close()
	check := func(method, path, token, origin string, want int) {
		t.Helper()
		req, _ := http.NewRequest(method, gateway.URL+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := gateway.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d, want %d", method, path, res.StatusCode, want)
		}
	}
	check("GET", "/api/device", "local-secret", "", 200)
	check("GET", "/api/projects", "bad", "", 401)
	check("POST", "/api/projects/prj_test/tasks", "local-secret", "http://evil.test", 403)
	check("POST", "/api/runners/pair", "local-secret", "", 403)
	check("GET", "/api/connections", "local-secret", "", 403)
	check("GET", "/api/projects", "local-secret", "", 200)
	sent := <-requests
	if sent.Header.Get("Authorization") != "Bearer wbc_remote-secret" || sent.Host != "controller.test" {
		t.Fatal("incorrect upstream credential or host")
	}
	check("GET", "/api/events?access_token=local-secret&after=41", "", "", 200)
	sent = <-requests
	if sent.URL.Query().Get("access_token") != "" || sent.URL.Query().Get("after") != "41" {
		t.Fatal("SSE credential or cursor forwarding")
	}
	check("GET", "/api/client/v1/self", "local-secret", "", 200)
	<-requests
	select {
	case r := <-requests:
		t.Fatalf("forbidden route reached controller: %s", r.URL)
	default:
	}
	// A hostile Host cannot reach the gateway even with a local token.
	req, _ := http.NewRequest("GET", gateway.URL+"/api/device", nil)
	req.Host = "evil.test"
	req.Header.Set("Authorization", "Bearer local-secret")
	res, err := gateway.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("host rebinding accepted")
	}
	upstream.Close()
	check("GET", "/api/projects", "local-secret", "", 503)
}
func TestDeviceModeFailsClosedAndRetainsLocalData(t *testing.T) {
	dir := t.TempDir()
	if s, err := Load(dir); err != nil || s.Remote() {
		t.Fatal(s, err)
	}
	for _, url := range []string{"file:///tmp/x", "http://user@host", "http://127.0.0.1", "http://host/path", "http://host?q=1"} {
		if ValidateController(url) == nil {
			t.Fatalf("accepted %s", url)
		}
	}
	s := State{Mode: "switching", Controller: "https://home.test", Runner: true}
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got != s || !got.Remote() {
		t.Fatal(got, err)
	}
	id, err := DeviceID(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := DeviceID(dir)
	if err != nil || again != id || len(id) != 64 {
		t.Fatal("device identity changed")
	}
}

func TestGatewayRejectsRotatedLocalTokenWithoutChangingRemoteGrant(t *testing.T) {
	var local atomic.Value
	local.Store("before")
	var upstream atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstream.Add(1)
		if r.Header.Get("Authorization") != "Bearer remote-grant" {
			t.Fatal("local token sent upstream")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})
	h, err := Gateway(GatewayOptions{Credential: Credential{Controller: "http://home.test", Token: "remote-grant"}, TokenSource: func() string { return local.Load().(string) }, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token string
		want  int
	}{{"before", 200}, {"before", 401}, {"after", 200}} {
		if upstream.Load() == 1 {
			local.Store("after")
		}
		r := httptest.NewRequest("GET", "http://127.0.0.1/api/projects", nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("rotation: got %d want %d", w.Code, tc.want)
		}
	}
	if upstream.Load() != 2 {
		t.Fatal("revoked local request reached controller")
	}
	if _, err = Gateway(GatewayOptions{Credential: Credential{Controller: "http://home.test"}}); err == nil {
		t.Fatal("gateway allowed default network transport")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

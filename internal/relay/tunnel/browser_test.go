package tunnel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"werkbord/internal/relay/access"
	"werkbord/internal/relay/client"
	"werkbord/internal/relay/relaytest"
	"werkbord/internal/relay/tunnel"
)

// These tests run the connector's own code (relay/connector/src/link.js, the code the service worker runs) under Node
// against the real runner side in Go, over the in-process relay. They are the check that the two implementations of the
// tunnel and of the end-to-end handshake agree. They are skipped where Node is not installed.

func node(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	return p
}

type rig struct {
	relay     *relaytest.Server
	store     *access.Store
	client    *client.Client
	cancelled atomic.Int32
	stop      context.CancelFunc
	done      chan struct{}
}

func newRig(t *testing.T, h http.Handler) *rig {
	t.Helper()
	r := &rig{relay: relaytest.New(), done: make(chan struct{})}
	t.Cleanup(r.relay.Close)
	var err error
	if r.store, err = access.Open(filepath.Join(t.TempDir(), "relay.json")); err != nil {
		t.Fatal(err)
	}
	srv := &tunnel.Server{Handler: h}
	r.client = client.New(client.Config{
		BaseURL: r.relay.URL, Room: r.store.Room(), Identity: r.store.Identity(), Secrets: r.store, Handler: srv,
		PingInterval: time.Second, MinBackoff: 20 * time.Millisecond, MaxBackoff: 100 * time.Millisecond,
	})
	srv.Sender = r.client
	ctx, cancel := context.WithCancel(context.Background())
	srv.Context = ctx
	r.stop = cancel
	go func() { _ = r.client.Run(ctx); close(r.done) }()
	t.Cleanup(cancel)
	waitFor(t, func() bool { return r.client.Status().Connected })
	return r
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

func drive(t *testing.T, mode, pairing string) map[string]any {
	t.Helper()
	script, _ := filepath.Abs("../../../relay/connector/test/drive.mjs")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node(t), script, mode, pairing)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("node: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out); err != nil {
		t.Fatalf("node printed %q: %v", stdout.String(), err)
	}
	return out
}

func appHandler(r *rig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Set-Cookie", "no=1")
		fmt.Fprintf(w, "hello %s %s", req.Method, req.URL.RawQuery)
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		if req.ContentLength != int64(len(b)) {
			http.Error(w, "length", 500)
			return
		}
		w.Write(b)
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, req *http.Request) {
		b := make([]byte, 300*1024)
		for i := range b {
			b[i] = byte(i * 3)
		}
		w.Write(b)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "event %d\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(120 * time.Millisecond)
		}
	})
	mux.HandleFunc("/forbidden", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":"forbidden"}}`))
	})
	mux.HandleFunc("/hang", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-req.Context().Done():
			r.cancelled.Add(1)
		case <-time.After(20 * time.Second):
		}
	})
	mux.HandleFunc("/n/{i}", func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(time.Duration(rand.IntN(80)) * time.Millisecond)
		fmt.Fprint(w, req.PathValue("i"))
	})
	mux.HandleFunc("/enroll-check", func(w http.ResponseWriter, req *http.Request) {
		_, hasSecret := tunnel.SecretFrom(req.Context())
		fmt.Fprintf(w, "cookie=%q origin=%q host=%s remote=%s secret=%v accept=%q", req.Header.Get("Cookie"), req.Header.Get("Origin"), req.Host,
			strings.Split(req.RemoteAddr, ":")[0], hasSecret, req.Header.Get("Accept-Encoding"))
	})
	return mux
}

func TestBrowserEndToEnd(t *testing.T) {
	node(t)
	var rg *rig
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { appHandler(rg).ServeHTTP(w, req) })
	rg = newRig(t, h)
	p, err := rg.store.NewPairing(rg.relay.URL, "the-code")
	if err != nil {
		t.Fatal(err)
	}
	out := drive(t, "tunnel", p.Encode())

	if out["connected"] != true {
		t.Fatalf("did not connect: %v", out)
	}
	hello := out["hello"].(map[string]any)
	if hello["status"] != 200.0 || hello["body"] != "hello GET x=1" || hello["type"] != "text/plain" {
		t.Errorf("hello: %v", hello)
	}
	if out["echo"].(map[string]any)["same"] != true {
		t.Errorf("a 200 KiB request body did not arrive intact: %v", out["echo"])
	}
	if big := out["big"].(map[string]any); big["length"] != 300.0*1024 || big["ok"] != true {
		t.Errorf("a 300 KiB response did not arrive intact: %v", big)
	}
	stream := out["stream"].(map[string]any)
	if len(stream["chunks"].([]any)) != 3 || stream["spread"].(float64) < 150 {
		t.Errorf("a stream must arrive as it is written, not at its end: %v", stream)
	}
	if f := out["forbidden"].(map[string]any); f["status"] != 403.0 || !strings.Contains(f["body"].(string), "forbidden") {
		t.Errorf("an error answer must pass through: %v", f)
	}
	waitFor(t, func() bool { return rg.cancelled.Load() == 1 })
	many := out["many"].([]any)
	for i, v := range many {
		if v != fmt.Sprint(i) {
			t.Errorf("concurrent requests got mixed up: %v", many)
			break
		}
	}
	check := out["enrollCheck"].(string)
	for _, want := range []string{`cookie=""`, `origin=""`, "host=relay.invalid", "remote=relay", "secret=true", `accept=""`} {
		if !strings.Contains(check, want) {
			t.Errorf("a request reached the controller as %s; want %s in it", check, want)
		}
	}
}

func TestBrowserWhyItCouldNotConnect(t *testing.T) {
	node(t)
	rg := newRig(t, http.NotFoundHandler())
	good, _ := rg.store.NewPairing(rg.relay.URL, "")

	t.Run("a secret the computer does not know is a phone that was removed", func(t *testing.T) {
		stranger := newStore(t)
		p, _ := stranger.NewPairing(rg.relay.URL, "")
		p.Room, p.JoinKey, p.RunnerKey = good.Room, good.JoinKey, good.RunnerKey // right computer, wrong secret
		if out := drive(t, "connect", p.Encode()); out["reason"] != "removed" {
			t.Fatalf("%v", out)
		}
	})
	t.Run("a link made for another computer does not match this one", func(t *testing.T) {
		p := good
		other, _ := newStore(t).NewPairing(rg.relay.URL, "")
		p.RunnerKey = other.RunnerKey
		if out := drive(t, "connect", p.Encode()); out["reason"] != "invalid" {
			t.Fatalf("%v", out)
		}
	})
	t.Run("a relay that cannot be reached is temporarily unavailable", func(t *testing.T) {
		p := good
		p.Relay = "http://127.0.0.1:1"
		if out := drive(t, "connect", p.Encode()); out["reason"] != "unavailable" {
			t.Fatalf("%v", out)
		}
	})
	t.Run("a relay that is full is temporarily unavailable", func(t *testing.T) {
		full := httpServer(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "quota", http.StatusTooManyRequests) })
		p := good
		p.Relay = full
		if out := drive(t, "connect", p.Encode()); out["reason"] != "unavailable" {
			t.Fatalf("%v", out)
		}
	})
	t.Run("a computer that is asleep is offline", func(t *testing.T) {
		rg.stop()
		<-rg.done
		waitFor(t, func() bool { return !runnerThere(t, rg, good.Room, good.JoinKey) })
		p, _ := rg.store.NewPairing(rg.relay.URL, "")
		if out := drive(t, "connect", p.Encode()); out["reason"] != "offline" {
			t.Fatalf("%v", out)
		}
	})
}

func TestBrowserPairingText(t *testing.T) {
	node(t)
	cmd := exec.Command(node(t), "-e", `import("`+mustAbs(t, "../../../relay/connector/src/link.js")+`").then(m => { try { m.decodePairing("wbr1.nope"); } catch (e) { console.log(e.reason) } })`)
	b, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(b)) != "invalid" {
		t.Fatalf("%q %v", b, err)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal(err)
	}
	return a
}

func newStore(t *testing.T) *access.Store {
	t.Helper()
	s, err := access.Open(filepath.Join(t.TempDir(), "relay.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func httpServer(t *testing.T, f http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	return s.URL
}

func runnerThere(t *testing.T, rg *rig, room, key string) bool {
	t.Helper()
	req, _ := http.NewRequest("GET", rg.relay.URL+"/v1/room/"+room+"/status", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st struct{ Runner bool }
	_ = json.NewDecoder(resp.Body).Decode(&st)
	return st.Runner
}

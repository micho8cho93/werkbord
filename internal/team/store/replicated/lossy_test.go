package replicated

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type lossMode int32

const (
	passThrough lossMode = iota
	loseResponse
	loseRequest
)

// lossyProxy sits in front of a node's HTTP API and, when told to, loses what a write was told: either the
// answer (the node did the work) or the request (it did not). The connection is closed without a word, which is
// what a client sees when a network breaks at that moment.
type lossyProxy struct {
	addr    string
	mode    atomic.Int32
	dropped atomic.Int64
	ln      net.Listener
	target  string
}

func newLossyProxy(t testing.TB, target string) *lossyProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &lossyProxy{addr: ln.Addr().String(), ln: ln, target: target}
	srv := &http.Server{Handler: http.HandlerFunc(p.serve)}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return p
}

func (p *lossyProxy) serve(w http.ResponseWriter, r *http.Request) {
	write := r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/db/execute")
	mode := lossMode(p.mode.Load())
	if write && mode == loseRequest {
		p.dropped.Add(1)
		p.mode.Store(int32(passThrough)) // only the first
		hijackAndClose(w)
		return
	}
	body, _ := io.ReadAll(r.Body)
	req, _ := http.NewRequest(r.Method, "http://"+p.target+r.URL.RequestURI(), bytes.NewReader(body))
	req.Header = r.Header.Clone()
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	if write && mode == loseResponse {
		p.dropped.Add(1)
		p.mode.Store(int32(passThrough))
		_, _ = io.Copy(io.Discard, res.Body)
		hijackAndClose(w)
		return
	}
	for k, v := range res.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

func hijackAndClose(w http.ResponseWriter) {
	if h, ok := w.(http.Hijacker); ok {
		if conn, _, err := h.Hijack(); err == nil {
			_ = conn.Close()
		}
	}
}

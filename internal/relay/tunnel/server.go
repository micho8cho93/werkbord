package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"werkbord/internal/relay"
	"werkbord/internal/relay/e2e"
)

// Sender is how the server answers: relay/client.Client implements it.
type Sender interface {
	Send(ctx context.Context, peer relay.PeerID, msg []byte) error
}

type secretKey struct{}

// SecretFrom is the pre-shared secret the phone behind a request proved it holds. A wrapper uses it to tie a phone that
// has just enrolled to its secret (relay/access).
func SecretFrom(ctx context.Context) (e2e.PSKID, bool) {
	id, ok := ctx.Value(secretKey{}).(e2e.PSKID)
	return id, ok
}

// Server runs phones' requests against Handler. It implements relay/client.Handler.
type Server struct {
	Handler http.Handler
	Sender  Sender
	// Context scopes every request: when it ends so do they. Default: context.Background().
	Context context.Context
	Log     *slog.Logger

	mu    sync.Mutex
	peers map[relay.PeerID]*peer
}

type peer struct {
	secret   e2e.PSKID
	requests map[uint32]*call
}

// call is one request: collected in full, then handed to the handler.
type call struct {
	head   Head
	body   bytes.Buffer
	cancel context.CancelFunc
	ran    bool
}

func (s *Server) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.New(slog.DiscardHandler)
}

// Message handles one decrypted message from a phone. It returns quickly: the handler runs in its own goroutine.
func (s *Server) Message(p relay.PeerID, secret e2e.PSKID, msg []byte) {
	t, id, payload, err := Unpack(msg)
	if err != nil {
		return
	}
	s.mu.Lock()
	if s.peers == nil {
		s.peers = map[relay.PeerID]*peer{}
	}
	pe := s.peers[p]
	if pe == nil {
		pe = &peer{secret: secret, requests: map[uint32]*call{}}
		s.peers[p] = pe
	}
	c := pe.requests[id]

	switch t {
	case ReqHead:
		switch {
		case c != nil:
		case len(pe.requests) >= MaxOpenRequests:
			s.mu.Unlock()
			s.fail(p, id, failureBusy, "too many requests at once")
			return
		case len(payload) > MaxHeadBytes:
			s.mu.Unlock()
			s.fail(p, id, failureBadReq, "request head too large")
			return
		default:
			var h Head
			if json.Unmarshal(payload, &h) != nil || !validTarget(h.Path) || !validMethod(h.Method) {
				s.mu.Unlock()
				s.fail(p, id, failureBadReq, "malformed request")
				return
			}
			pe.requests[id] = &call{head: h}
		}
	case ReqBody:
		if c != nil && !c.ran {
			if c.body.Len()+len(payload) > MaxBodyBytes {
				delete(pe.requests, id)
				s.mu.Unlock()
				s.fail(p, id, failureTooLarge, "request body too large")
				return
			}
			c.body.Write(payload)
		}
	case ReqEnd:
		if c != nil && !c.ran {
			c.ran = true
			ctx, cancel := context.WithCancel(s.baseContext())
			c.cancel = cancel
			go s.serve(ctx, p, secret, id, c)
		}
	case Cancel:
		if c != nil {
			if c.cancel != nil {
				c.cancel()
			}
			delete(pe.requests, id)
		}
	}
	s.mu.Unlock()
}

func (s *Server) baseContext() context.Context {
	if s.Context != nil {
		return s.Context
	}
	return context.Background()
}

// PeerGone ends everything that phone had open.
func (s *Server) PeerGone(p relay.PeerID) {
	s.mu.Lock()
	pe := s.peers[p]
	delete(s.peers, p)
	s.mu.Unlock()
	if pe == nil {
		return
	}
	for _, c := range pe.requests {
		if c.cancel != nil {
			c.cancel()
		}
	}
}

// PeersOf lists the phones connected with a given secret: the ones to remove when that secret is revoked.
func (s *Server) PeersOf(secret e2e.PSKID) []relay.PeerID {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []relay.PeerID
	for id, pe := range s.peers {
		if pe.secret == secret {
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) done(p relay.PeerID, id uint32) {
	s.mu.Lock()
	if pe := s.peers[p]; pe != nil {
		delete(pe.requests, id)
	}
	s.mu.Unlock()
}

func (s *Server) send(ctx context.Context, p relay.PeerID, t byte, id uint32, payload []byte) error {
	return s.Sender.Send(ctx, p, Pack(t, id, payload))
}

func (s *Server) fail(p relay.PeerID, id uint32, code, message string) {
	b, _ := json.Marshal(Failure{Code: code, Message: message})
	_ = s.send(s.baseContext(), p, Err, id, b)
}

func (s *Server) serve(ctx context.Context, p relay.PeerID, secret e2e.PSKID, id uint32, c *call) {
	defer s.done(p, id)
	defer c.cancel()
	defer func() {
		if r := recover(); r != nil {
			s.log().Error("relay request panicked", "panic", fmt.Sprint(r))
			s.fail(p, id, failureInternal, "the controller failed on that request")
		}
	}()
	if s.Handler == nil {
		s.fail(p, id, failureNoHandler, "no handler")
		return
	}

	req, err := http.NewRequestWithContext(context.WithValue(ctx, secretKey{}, secret), c.head.Method, c.head.Path, io.NopCloser(bytes.NewReader(c.body.Bytes())))
	if err != nil {
		s.fail(p, id, failureBadReq, "malformed request")
		return
	}
	for name, v := range c.head.Header {
		if allowedRequestHeader(name) {
			req.Header.Set(name, v)
		}
	}
	req.ContentLength = int64(c.body.Len())
	// Not a socket address on purpose: nothing that asks "is this from this computer?" may say yes to a phone.
	req.RemoteAddr = "relay:" + p.String()
	req.Host = "relay.invalid"

	w := &responseWriter{srv: s, ctx: ctx, peer: p, id: id, header: http.Header{}}
	s.Handler.ServeHTTP(w, req)
	w.finish()
}

// ---- response ----

type responseWriter struct {
	srv    *Server
	ctx    context.Context
	peer   relay.PeerID
	id     uint32
	header http.Header
	sent   bool
	failed bool
}

func (w *responseWriter) Header() http.Header { return w.header }

func (w *responseWriter) WriteHeader(status int) {
	if w.sent {
		return
	}
	w.sent = true
	h := Head{Status: status, Header: map[string]string{}}
	for name, vals := range w.header {
		if allowedResponseHeader(name) {
			h.Header[strings.ToLower(name)] = strings.Join(vals, ", ")
		}
	}
	b, _ := json.Marshal(h)
	w.put(ResHead, b)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.sent {
		w.WriteHeader(http.StatusOK)
	}
	if w.failed {
		return 0, io.ErrClosedPipe
	}
	n := 0
	for len(b) > 0 {
		chunk := b
		if len(chunk) > ChunkBytes {
			chunk = chunk[:ChunkBytes]
		}
		if !w.put(ResBody, chunk) {
			return n, io.ErrClosedPipe
		}
		n += len(chunk)
		b = b[len(chunk):]
	}
	return n, nil
}

// Flush: every write is already on its way, so a stream (the event stream) reaches the phone as it is written.
func (w *responseWriter) Flush() {
	if !w.sent {
		w.WriteHeader(http.StatusOK)
	}
}

func (w *responseWriter) put(t byte, payload []byte) bool {
	if w.failed || w.ctx.Err() != nil {
		w.failed = true
		return false
	}
	if err := w.srv.send(w.ctx, w.peer, t, w.id, payload); err != nil {
		w.failed = true
		return false
	}
	return true
}

func (w *responseWriter) finish() {
	if w.ctx.Err() != nil {
		return // the phone cancelled, left, or was revoked: there is nobody to tell
	}
	if !w.sent {
		w.WriteHeader(http.StatusOK)
	}
	w.put(ResEnd, nil)
}

// ---- what may cross ----

func validMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// validTarget: a path on the controller, not an absolute URL or a host.
func validTarget(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.ContainsAny(p, "\r\n") && len(p) <= 8192
}

// allowedRequestHeader is a short list on purpose: a browser's Origin, Cookie, Host and forwarding headers mean nothing
// here, and must not be able to steer the controller's origin and host checks.
func allowedRequestHeader(name string) bool {
	switch strings.ToLower(name) {
	case "accept", "accept-language", "authorization", "content-type", "if-none-match", "if-modified-since", "range":
		return true
	}
	return false
}

func allowedResponseHeader(name string) bool {
	switch strings.ToLower(name) {
	case "content-length", "transfer-encoding", "connection", "keep-alive", "set-cookie", "set-cookie2", "content-encoding", "upgrade", "trailer", "te":
		return false
	}
	return true
}

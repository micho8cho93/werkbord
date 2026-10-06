package enrollment

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/httpkit"
)

// Authority is the workspace's side of enrollment: what decides whether a device
// that has proved it holds the invitation's credential and its own key becomes a
// member's device. The server below does the protocol (TLS, the channel-bound
// proof of possession, limits, rate); the Authority owns the workspace's records.
//
// An Authority must answer ErrRefused-shaped failures identically whether the
// invitation never existed, was used, was withdrawn, or has expired.
type Authority interface {
	// Enroll is called once the device's proof of possession has been verified. It
	// either enrolls the device, or records it as awaiting approval.
	Enroll(ctx context.Context, req AuthorityJoin) (Response, error)
	// Poll reports on a pending enrollment, after the device proved it is the one
	// that asked.
	Poll(ctx context.Context, req AuthorityPoll) (Response, error)
	// Identity is what GET /enroll/v1/hello says.
	Identity(ctx context.Context) (Hello, error)
}

// AuthorityJoin is a verified request to join.
type AuthorityJoin struct {
	JoinRequest
	// Key is the device's public key, parsed from the request.
	Key ed25519.PublicKey
	// RemoteAddr is the address the request came from (for the record, not for trust).
	RemoteAddr string
}

// AuthorityPoll is a verified request after a pending enrollment.
type AuthorityPoll struct {
	PollRequest
}

// KeyLookup finds the public key a pending enrollment was made with, so a poll can
// be checked against it.
type KeyLookup func(ctx context.Context, enrollmentID, deviceID string) (ed25519.PublicKey, error)

// ErrNotFound is what an Authority returns for an invitation or enrollment that is
// not there, not usable, or not the caller's: the server turns it, and every other
// refusal, into one answer.
var ErrNotFound = errors.New("enrollment: not found")

// ServerOptions configures a Server.
type ServerOptions struct {
	Authority Authority
	// WorkspaceID is the workspace this endpoint speaks for; a proof is bound to it.
	WorkspaceID string
	// Cert supplies the endpoint's certificate chain, fetched for each handshake so a
	// renewed certificate is picked up without a restart.
	Cert func() (ServerCert, error)
	// PollKey finds the key a pending enrollment is bound to.
	PollKey KeyLookup
	Log     *slog.Logger
	Now     func() time.Time
	// RatePerMinute bounds requests from one address (default 30).
	RatePerMinute int
}

// Server serves the enrollment protocol.
type Server struct {
	o   ServerOptions
	log *slog.Logger
	lim *limiter
}

// NewServer builds a Server.
func NewServer(o ServerOptions) *Server {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RatePerMinute <= 0 {
		o.RatePerMinute = 30
	}
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{o: o, log: log, lim: newLimiter(o.RatePerMinute, o.Now)}
}

const maxRequestBytes = 16 << 10

// Handler returns the protocol's handler. It refuses any request that did not arrive
// over TLS 1.3: it has no plaintext mode, in tests or otherwise.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathHello, s.hello)
	mux.HandleFunc("POST "+PathJoin, s.join)
	mux.HandleFunc("POST "+PathPoll, s.poll)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 {
			httpkit.WriteError(w, http.StatusForbidden, "tls_required", "TLS 1.3 is required")
			return
		}
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if !s.lim.allow(host) {
			w.Header().Set("Retry-After", "60")
			httpkit.WriteError(w, http.StatusTooManyRequests, "busy", "too many requests")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

// HTTPServer is the http.Server to run the protocol on: Serve it on Listener(ln).
// Its timeouts are short, and its bodies small: this is a door that strangers knock on.
func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    8 << 10,
		// Handshake failures are what a port scan looks like: debug, not an error.
		ErrorLog: slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}
}

// Listener wraps a plain listener in the endpoint's TLS 1.3.
func (s *Server) Listener(ln net.Listener) net.Listener {
	return tls.NewListener(ln, serverTLS(s.o.Cert))
}

// refuse is the one answer to everything that is not a valid enrollment.
func refuse(w http.ResponseWriter) {
	httpkit.WriteError(w, http.StatusForbidden, "refused", "the request was refused")
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	h, err := s.o.Authority.Identity(r.Context())
	if err != nil {
		httpkit.WriteError(w, http.StatusServiceUnavailable, "unavailable", "unavailable")
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, h)
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var req JoinRequest
	if err := httpkit.DecodeJSON(w, r, &req, maxRequestBytes); err != nil {
		refuse(w)
		return
	}
	key, ok := s.checkJoin(r, req)
	if !ok {
		refuse(w)
		return
	}
	resp, err := s.o.Authority.Enroll(r.Context(), AuthorityJoin{JoinRequest: req, Key: key, RemoteAddr: r.RemoteAddr})
	s.answer(w, r, resp, err)
}

func (s *Server) checkJoin(r *http.Request, req JoinRequest) (ed25519.PublicKey, bool) {
	if !ValidID(req.InviteID) || !deviceid.ValidID(req.DeviceID) || req.Credential == "" || len(req.Credential) > 128 ||
		len(req.NetworkPublicKey) == 0 || len(req.NetworkPublicKey) > 512 || len(req.SealingPublicKey) > 128 {
		return nil, false
	}
	kb, err := base64.RawURLEncoding.DecodeString(req.DevicePublicKey)
	proof, err2 := base64.RawURLEncoding.DecodeString(req.Proof)
	if err != nil || err2 != nil || len(kb) != ed25519.PublicKeySize {
		return nil, false
	}
	ex, err := exporter(*r.TLS)
	if err != nil {
		return nil, false
	}
	key := ed25519.PublicKey(kb)
	if !deviceid.Verify(key, JoinStatement(s.o.WorkspaceID, req, ex), proof) {
		return nil, false
	}
	return key, true
}

func (s *Server) poll(w http.ResponseWriter, r *http.Request) {
	var req PollRequest
	if err := httpkit.DecodeJSON(w, r, &req, maxRequestBytes); err != nil {
		refuse(w)
		return
	}
	proof, err := base64.RawURLEncoding.DecodeString(req.Proof)
	if err != nil || !deviceid.ValidID(req.DeviceID) || req.EnrollmentID == "" || len(req.EnrollmentID) > 64 {
		refuse(w)
		return
	}
	key, err := s.o.PollKey(r.Context(), req.EnrollmentID, req.DeviceID)
	ex, err2 := exporter(*r.TLS)
	if err != nil || err2 != nil || !deviceid.Verify(key, PollStatement(s.o.WorkspaceID, req, ex), proof) {
		refuse(w)
		return
	}
	resp, err := s.o.Authority.Poll(r.Context(), AuthorityPoll{PollRequest: req})
	s.answer(w, r, resp, err)
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request, resp Response, err error) {
	var rej *RejectedError
	switch {
	case errors.As(err, &rej):
		httpkit.WriteError(w, http.StatusConflict, "rejected", rej.Reason)
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrRefused):
		refuse(w)
	case err != nil:
		s.log.Error("enrollment failed", "err", err)
		httpkit.WriteError(w, http.StatusServiceUnavailable, "unavailable", "the workspace could not complete the request")
	case resp.State == StateDenied:
		httpkit.WriteJSON(w, http.StatusForbidden, Response{State: StateDenied})
	case resp.State == StatePending:
		httpkit.WriteJSON(w, http.StatusAccepted, resp)
	default:
		httpkit.WriteJSON(w, http.StatusOK, resp)
	}
}

// ---- a small per-address rate limit: a damper on guessing, not a defence by itself
// (the credential is 256 random bits) ----

type limiter struct {
	mu    sync.Mutex
	per   int
	now   func() time.Time
	seen  map[string][]time.Time
	calls int
}

func newLimiter(perMinute int, now func() time.Time) *limiter {
	return &limiter{per: perMinute, now: now, seen: map[string][]time.Time{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cut := now.Add(-time.Minute)
	keep := l.seen[key][:0]
	for _, t := range l.seen[key] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.per {
		l.seen[key] = keep
		return false
	}
	l.seen[key] = append(keep, now)
	if l.calls++; l.calls%256 == 0 { // forget addresses not seen for a minute
		for k, ts := range l.seen {
			if len(ts) == 0 || !ts[len(ts)-1].After(cut) {
				delete(l.seen, k)
			}
		}
	}
	return true
}

// ConstantTimeEqual compares two strings without leaking where they differ.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"devboard/internal/api"
	"devboard/internal/config"
	"devboard/internal/netprivate"
	"devboard/internal/phoneaccess"
	"devboard/internal/relay/access"
	relayclient "devboard/internal/relay/client"
	"devboard/internal/relay/tunnel"
)

// relayControl is the controller's connection to the relay: it keeps one outbound connection up, answers the requests
// phones make through it with the private handler, and hands out the pairings in QR codes. It exists only when
// relay.url and relay.connectorUrl are set.
type relayControl struct {
	cfg    config.RelayConfig
	store  *access.Store
	client *relayclient.Client
	srv    *tunnel.Server
	log    *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
	rejects bool
}

// newRelayControl returns nil, nil when the relay is not set up. h is what phones are served: the private handler.
func newRelayControl(cfg config.RelayConfig, dataDir string, phones *phoneaccess.Store, h http.Handler, log *slog.Logger) (*relayControl, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	store, err := access.Open(filepath.Join(dataDir, "relay.json"))
	if err != nil {
		return nil, err
	}
	store.Prune(func(id string) bool { _, ok := phones.Get(id); return ok })

	r := &relayControl{cfg: cfg, store: store, log: log}
	r.srv = &tunnel.Server{Handler: r.phoneHandler(h, phones), Log: log}
	r.client = relayclient.New(relayclient.Config{
		BaseURL: cfg.URL, Room: store.Room(), Identity: store.Identity(), Secrets: store, Handler: r.srv,
	})
	r.srv.Sender = r.client

	// A revoked phone loses its secret and is put off the relay now, not at its next request.
	phones.OnRevoke(func(id string) {
		for _, secret := range store.Unbind(id) {
			for _, peer := range r.srv.PeersOf(secret) {
				r.client.Kick(context.Background(), peer)
			}
		}
	})
	return r, nil
}

// phoneHandler is the private handler as a phone through the relay meets it. A phone that has just enrolled has its
// secret bound to it, which is what lets it reconnect without a second scan.
func (r *relayControl) phoneHandler(h http.Handler, phones *phoneaccess.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(netprivate.WithPrivateRequest(req.Context()))
		if req.Method != http.MethodPost || req.URL.Path != "/api/phones/enroll" {
			h.ServeHTTP(w, req)
			return
		}
		rec := &enrollRecorder{ResponseWriter: w}
		h.ServeHTTP(rec, req)
		if rec.status != http.StatusCreated {
			return
		}
		var e struct {
			ID string `json:"id"`
		}
		secret, ok := tunnel.SecretFrom(req.Context())
		if json.Unmarshal(rec.body.Bytes(), &e) != nil || e.ID == "" || !ok {
			return
		}
		if err := r.store.Bind(secret, e.ID); err != nil {
			// The phone is enrolled and signed in; it only cannot reconnect later. Say so, so it can be scanned again.
			r.log.Warn("could not keep a phone's relay secret: it will need a new code to reconnect", "err", err)
		}
	})
}

// enrollRecorder passes a response through and keeps the small body of an enrollment's.
type enrollRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (e *enrollRecorder) WriteHeader(s int) {
	if e.status == 0 {
		e.status = s
	}
	e.ResponseWriter.WriteHeader(s)
}

func (e *enrollRecorder) Write(b []byte) (int, error) {
	if e.status == 0 {
		e.status = http.StatusOK
	}
	if e.body.Len() < 4096 {
		e.body.Write(b)
	}
	return e.ResponseWriter.Write(b)
}

func (e *enrollRecorder) Flush() {
	if f, ok := e.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Start connects in the background.
func (r *relayControl) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done, r.started = cancel, make(chan struct{}), true
	r.srv.Context = ctx
	go func() {
		defer close(r.done)
		err := r.client.Run(ctx)
		if errors.Is(err, relayclient.ErrRejected) {
			r.mu.Lock()
			r.rejects = true
			r.mu.Unlock()
			r.log.Error("the relay refused this computer's key: it belongs to another runner. Remove relay.json from the data directory to start over, which signs every phone out")
		}
	}()
}

// Stop disconnects and ends every request phones have open.
func (r *relayControl) Stop(ctx context.Context) {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Status implements api.RelayController.
func (r *relayControl) Status() api.RelayStatus {
	st := r.client.Status()
	r.mu.Lock()
	rejected := r.rejects
	r.mu.Unlock()
	out := api.RelayStatus{Enabled: true, Connected: st.Connected, Phones: st.Peers, ConnectorURL: r.cfg.ConnectorURL, Error: st.LastError}
	if rejected {
		out.Error = "the relay refused this computer's key"
	}
	if st.RetryIn > 0 {
		out.RetryInSeconds = int(st.RetryIn / time.Second)
	}
	return out
}

// Link implements api.RelayController: the address a phone opens, with the pairing in its fragment, which a browser
// never sends to a server. /connect/ is the connector page that pairs (relay/connector); "/" is the app itself, which
// the phone's service worker answers from this computer.
func (r *relayControl) Link(code string) (string, error) {
	p, err := r.store.NewPairing(r.cfg.URL, code)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(r.cfg.ConnectorURL, "/") + "/connect/#" + p.Encode(), nil
}

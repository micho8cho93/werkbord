package netprivate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Options configures a Manager.
type Options struct {
	// Backend is the network node.
	Backend Backend
	// Handler serves the app and API to the tailnet. It must demand the access
	// token on every API request: reaching the tailnet is not authentication.
	Handler http.Handler
	Log     *slog.Logger
	// OnShutdown, if set, is called as the listeners begin to shut down. Long-lived
	// requests (an event stream) only end when something tells them to, and this is
	// how the owner does.
	OnShutdown func()

	// PollFast and PollSlow are how often the node is asked its state while it is
	// coming up and once it is connected. Defaults: 1 s and 15 s.
	PollFast, PollSlow time.Duration
}

// Manager brings the private network up in the background, keeps its status,
// and serves the controller on it once the node is connected.
type Manager struct {
	opt Options
	log *slog.Logger

	mu      sync.Mutex
	status  Status
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
	servers []*http.Server
}

// New returns a Manager that is off.
func New(opt Options) *Manager {
	if opt.PollFast <= 0 {
		opt.PollFast = time.Second
	}
	if opt.PollSlow <= 0 {
		opt.PollSlow = 15 * time.Second
	}
	log := opt.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{opt: opt, log: log, status: Status{State: StateOff}}
}

// Status returns the current status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	s.IPs = append([]string(nil), s.IPs...)
	s.Health = append([]string(nil), s.Health...)
	return s
}

// Enabled reports whether the network has been started.
func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *Manager) set(f func(*Status)) {
	m.mu.Lock()
	f(&m.status)
	m.mu.Unlock()
}

// Start begins bringing the network up and returns at once: the node may need
// the user to sign in, which is reported through Status, not waited for here.
// Starting a network that is already started does nothing.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.running, m.cancel, m.done = true, cancel, make(chan struct{})
	m.status = Status{State: StateStarting, Enabled: true}
	go m.run(ctx, m.done)
}

// Stop takes the network down: the listeners close and the node stops. A device
// that was signed in stays registered with the user's tailnet; it can be
// started again without signing in.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	cancel, done, servers := m.cancel, m.done, m.servers
	m.running, m.cancel, m.servers = false, nil, nil
	m.mu.Unlock()

	cancel()
	<-done
	var err error
	for _, s := range servers {
		if e := s.Shutdown(ctx); e != nil {
			err = errors.Join(err, e)
			_ = s.Close()
		}
	}
	err = errors.Join(err, m.opt.Backend.Close())
	m.set(func(s *Status) { *s = Status{State: StateOff} })
	return err
}

func (m *Manager) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	if err := m.opt.Backend.Start(ctx); err != nil {
		m.log.Warn("private network could not start", "err", err)
		m.set(func(s *Status) { s.State, s.Error = StateError, err.Error() })
		return
	}
	served := false
	for {
		st, err := m.opt.Backend.Status(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := m.opt.PollFast
		if err != nil {
			m.set(func(s *Status) { s.State, s.Error = StateError, err.Error() })
		} else {
			m.apply(st)
			if st.State == backendRunning {
				wait = m.opt.PollSlow
				if !served {
					if err := m.serve(ctx, st); err != nil {
						m.log.Warn("private network is up but the controller cannot listen on it", "err", err)
						m.set(func(s *Status) { s.State, s.Error = StateError, err.Error() })
						wait = m.opt.PollFast
					} else {
						served = true
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// apply folds a node snapshot into the status.
func (m *Manager) apply(st BackendStatus) {
	m.set(func(s *Status) {
		s.Error, s.AuthURL = "", ""
		s.Health = st.Health
		s.Tailnet = st.Tailnet
		s.IPs = s.IPs[:0]
		for _, ip := range st.IPs {
			s.IPs = append(s.IPs, ip.String())
		}
		s.Hostname = strings.TrimSuffix(st.DNSName, ".")
		switch st.State {
		case backendRunning:
			s.State = StateConnected
			s.HTTPS = st.HTTPS && s.Hostname != ""
			s.URL = m.url(s)
			s.HTTPSHint = ""
			if !s.HTTPS {
				s.HTTPSHint = "Turn on HTTPS certificates for your tailnet at https://login.tailscale.com/admin/dns to install Werkbord as an app and get notifications on your phone."
			}
		case backendNeedsLogin:
			s.State, s.AuthURL, s.URL = StateNeedsLogin, st.AuthURL, ""
		case backendNeedsMachineAuth:
			s.State, s.URL = StateNeedsApproval, ""
		default:
			s.State, s.URL = StateStarting, ""
		}
	})
}

// url is the address to open; the caller holds no lock on s, which is the status
// being built.
func (m *Manager) url(s *Status) string {
	host := s.Hostname
	if host == "" && len(s.IPs) > 0 {
		host = s.IPs[0]
	}
	if host == "" {
		return ""
	}
	scheme := "http"
	if s.HTTPS {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: host, Path: "/"}).String()
}

// serve starts the controller's listeners on the tailnet. With HTTPS available
// the app is served on 443, and port 80 sends a request for the node's name
// there; without it the app is served on 80. A request for the node's address
// (rather than its name) is served on 80 either way, since a certificate does not
// cover an address.
func (m *Manager) serve(ctx context.Context, st BackendStatus) error {
	name := strings.TrimSuffix(st.DNSName, ".")
	https := st.HTTPS && name != ""

	var servers []*http.Server
	var listeners []net.Listener
	fail := func(err error) error {
		for _, l := range listeners {
			_ = l.Close()
		}
		return err
	}
	handler80 := m.opt.Handler
	if https {
		handler80 = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.EqualFold(hostOnly(r.Host), name) {
				http.Redirect(w, r, "https://"+name+r.URL.RequestURI(), http.StatusPermanentRedirect)
				return
			}
			m.opt.Handler.ServeHTTP(w, r)
		})
	}
	l80, err := m.opt.Backend.Listen(":80", false)
	if err != nil {
		return fail(fmt.Errorf("listen on the private network: %w", err))
	}
	listeners = append(listeners, l80)
	servers = append(servers, m.newServer(handler80))
	pairs := []net.Listener{l80}
	if https {
		l443, err := m.opt.Backend.Listen(":443", true)
		if err != nil {
			return fail(fmt.Errorf("listen with HTTPS on the private network: %w", err))
		}
		listeners = append(listeners, l443)
		servers = append(servers, m.newServer(m.opt.Handler))
		pairs = append(pairs, l443)
	}

	m.mu.Lock()
	if ctx.Err() != nil || !m.running {
		m.mu.Unlock()
		return fail(ctx.Err())
	}
	m.servers = append(m.servers, servers...)
	m.mu.Unlock()
	for i, s := range servers {
		go func(s *http.Server, l net.Listener) {
			if err := s.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				m.log.Warn("private network listener stopped", "err", err)
			}
		}(s, pairs[i])
	}
	m.log.Info("controller reachable on the private network", "https", https)
	return nil
}

func (m *Manager) newServer(h http.Handler) *http.Server {
	s := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(m.log.Handler(), slog.LevelWarn),
		// No WriteTimeout: /api/events is a long-lived stream.
	}
	if m.opt.OnShutdown != nil {
		s.RegisterOnShutdown(m.opt.OnShutdown)
	}
	return s
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

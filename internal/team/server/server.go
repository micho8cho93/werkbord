// Package server runs Werkbord Team: it opens the database, builds the service and
// the HTTP API, and serves until the context ends.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"devboard/internal/team/api"
	"devboard/internal/team/config"
	"devboard/internal/team/console"
	"devboard/internal/team/service"
	"devboard/internal/team/store"
)

// Open opens Team's database and builds its service: wherever the workspace's data is (in one file, or in a cluster of
// Workspace Hosts, in which case this host's database node is started and waited for). Closing the store stops what
// was started.
func Open(ctx context.Context, cfg config.Config, log *slog.Logger) (store.Store, *service.Service, error) {
	w, err := OpenWorkspace(ctx, cfg, log)
	if err != nil {
		return nil, nil, err
	}
	return owned{Store: w.Storage.DB(), w: w}, w.Service, nil
}

// RunOptions are what the command that runs the server can give it that the server cannot make for itself.
type RunOptions struct {
	// NodeFallback tells this host's network node what it should be, and gives it a certificate, while the workspace's
	// database is not up: a host that has just been made a Workspace Host needs the network before it can join the
	// database, and learns what the network is from a Workspace Host's API, as a device that has only joined does.
	NodeFallback NodeSource
}

// Handler is Team's whole HTTP surface: the API and the console.
func Handler(db store.Store, svc *service.Service, log *slog.Logger, version string) http.Handler {
	return handler(db, svc, log, version, nil)
}

func handler(db store.Store, svc *service.Service, log *slog.Logger, version string, nodeStatus func() any) http.Handler {
	return api.New(api.Options{Service: svc, Ping: db.Ping, Log: log, Version: version, Console: console.Handler(), NodeStatus: nodeStatus, RequireDeviceProof: true}).Handler()
}

// Run serves Team on cfg.Addr until ctx is cancelled, then shuts down gracefully.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger, version string) error {
	return RunWith(ctx, cfg, log, version, RunOptions{})
}

// RunWith is Run with options.
func RunWith(ctx context.Context, cfg config.Config, log *slog.Logger, version string, opt RunOptions) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	// The workspace's storage starts first, and the server serves while a cluster's node is still coming up (it may
	// be waiting for the private network): until the data is there, requests are refused as read-only, and /health
	// says why.
	st, err := OpenStorage(ctx, cfg, log)
	if err != nil {
		return err
	}
	svc := service.New(st.DB())
	configureLicense(svc, cfg)
	st.Attach(svc, nil)
	st.Start(ctx)
	defer func() {
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		if err := st.Close(stop); err != nil {
			log.Warn("stopping the workspace's storage", "err", err)
		}
	}()
	db := st.DB()
	nw, err := openNetwork(cfg, log, svc)
	if err != nil {
		return err
	}
	var nodeStatus func() any
	if nw != nil {
		nodeStatus = nw.NodeStatus
		nw.fallback = opt.NodeFallback
		st.Attach(svc, nw.overlayAddr)
	}
	h := handler(db, svc, log, version, nodeStatus)

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	// Open change requests (the sync long poll) wait up to 20 seconds. Cancelling
	// their context when shutdown begins lets them finish at once instead of
	// holding the server up for the whole wait.
	base, stopWaiters := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWaiters()
	srv := &http.Server{
		Handler:           h,
		BaseContext:       func(net.Listener) context.Context { return base },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Info("Werkbord Team is running", "version", version, "addr", ln.Addr().String(), "data", cfg.DataDir)
	if !cfg.IsLoopback() {
		log.Warn("serving plain HTTP on a non-loopback address: tokens cross the network in the clear unless this is behind HTTPS (a reverse proxy) or a private network", "addr", cfg.Addr)
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	// The private network, if this host has one, runs for as long as the server does.
	netCtx, stopNet := context.WithCancel(ctx)
	netDone := make(chan struct{})
	if nw != nil {
		go func() { defer close(netDone); nw.run(netCtx, h) }()
	} else {
		close(netDone)
	}
	defer func() { stopNet(); <-netDone }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	stopWaiters()
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

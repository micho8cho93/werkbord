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

// Open opens Team's database and builds its service.
func Open(ctx context.Context, cfg config.Config, log *slog.Logger) (*store.DB, *service.Service, error) {
	db, err := store.Open(ctx, cfg.DBPath(), log)
	if err != nil {
		return nil, nil, err
	}
	return db, service.New(db), nil
}

// Handler is Team's whole HTTP surface: the API and the console.
func Handler(db *store.DB, svc *service.Service, log *slog.Logger, version string) http.Handler {
	return api.New(api.Options{Service: svc, Ping: db.Ping, Log: log, Version: version, Console: console.Handler()}).Handler()
}

// Run serves Team on cfg.Addr until ctx is cancelled, then shuts down gracefully.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger, version string) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	db, svc, err := Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer db.Close()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	srv := &http.Server{
		Handler:           Handler(db, svc, log, version),
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
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

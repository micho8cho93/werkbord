// Package controller wires the components together and owns their
// lifecycle: start in dependency order, stop in reverse.
package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"devboard/internal/agent"
	"devboard/internal/api"
	"devboard/internal/config"
	"devboard/internal/events"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
	"devboard/internal/webui"
)

// Controller is the long-running daemon.
type Controller struct {
	cfg     config.Config
	log     *slog.Logger
	version string

	release  func()
	db       *sqlite.DB
	broker   *events.Broker
	server   *http.Server
	listener net.Listener
	serveErr chan error
}

// New returns an unstarted controller.
func New(cfg config.Config, log *slog.Logger, version string) *Controller {
	return &Controller{cfg: cfg, log: log, version: version}
}

// Start brings the controller up: lock the data dir, open and migrate the
// database, reconcile state left by a previous process, then listen. On
// error, everything started so far is torn down.
func (c *Controller) Start(ctx context.Context) (err error) {
	if err := c.cfg.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	defer func() {
		if err != nil {
			c.teardown()
		}
	}()

	if err := os.MkdirAll(c.cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if c.release, err = acquireLock(c.cfg.LockPath()); err != nil {
		return err
	}

	if c.db, err = sqlite.Open(ctx, c.cfg.DBPath(), c.log); err != nil {
		return err
	}
	c.broker = events.NewBroker()

	deps := service.Deps{Store: c.db, Bus: c.broker, Log: c.log}
	projects := &service.Projects{Deps: deps, Git: &gitrepo.CLI{}}
	tasks := &service.Tasks{Deps: deps}
	runs := &service.Runs{Deps: deps}

	if _, err := runs.RecoverAfterRestart(ctx); err != nil {
		return fmt.Errorf("recover runs: %w", err)
	}

	token := ""
	if c.cfg.AuthRequired() {
		if token, err = c.cfg.ResolveToken(true); err != nil {
			return fmt.Errorf("api token: %w", err)
		}
	}

	srv := api.New(api.Options{
		Projects:     projects,
		Tasks:        tasks,
		Runs:         runs,
		Agents:       agent.NewRegistry(), // no adapters yet
		Store:        c.db,
		Events:       c.broker,
		Log:          c.log,
		Version:      c.version,
		Web:          webui.Handler(),
		AuthRequired: c.cfg.AuthRequired(),
		Token:        token,
		AllowedHosts: c.cfg.AllowedHosts,
	})

	c.server = &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(c.log.Handler(), slog.LevelWarn),
		// No WriteTimeout: /api/events is a long-lived stream.
	}
	// Shutdown waits for handlers to return, but an SSE handler only returns
	// when its subscription closes, so close the broker as shutdown begins.
	c.server.RegisterOnShutdown(c.broker.Close)

	if c.listener, err = net.Listen("tcp", c.cfg.Addr); err != nil {
		return fmt.Errorf("listen on %s: %w", c.cfg.Addr, err)
	}
	c.serveErr = make(chan error, 1)
	go func() { c.serveErr <- c.server.Serve(c.listener) }()

	c.log.Info("controller started",
		"addr", c.listener.Addr().String(), "data_dir", c.cfg.DataDir,
		"auth", c.cfg.AuthRequired(), "version", c.version)
	if c.cfg.AuthRequired() && c.cfg.Token == "" {
		c.log.Info("API token required; run `devboard token` to print it", "token_file", c.cfg.TokenPath())
	}
	if !c.cfg.AuthRequired() {
		c.log.Warn("API authentication is disabled (requireToken=false): any program on this computer can use the API and, once agents run, start processes as you")
	}
	return nil
}

// Addr returns the bound listen address (useful when Addr used port 0).
func (c *Controller) Addr() string {
	if c.listener == nil {
		return ""
	}
	return c.listener.Addr().String()
}

// Run starts the controller and blocks until ctx is cancelled or the server
// fails, then shuts down gracefully.
func (c *Controller) Run(ctx context.Context) error {
	if err := c.Start(ctx); err != nil {
		return err
	}
	var serveErr error
	select {
	case <-ctx.Done():
		c.log.Info("shutdown requested")
	case serveErr = <-c.serveErr:
		c.log.Error("http server stopped unexpectedly", "err", serveErr)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), c.cfg.ShutdownTimeout.Duration)
	defer cancel()
	return errors.Join(serveErr, c.Shutdown(shutdownCtx))
}

// Shutdown stops accepting requests, waits for in-flight ones (up to ctx's
// deadline), then closes the event broker and database and releases the
// data-dir lock.
func (c *Controller) Shutdown(ctx context.Context) error {
	var err error
	if c.server != nil {
		if e := c.server.Shutdown(ctx); e != nil {
			err = fmt.Errorf("http shutdown: %w", e)
			_ = c.server.Close()
		}
	}
	err = errors.Join(err, c.teardown())
	c.log.Info("controller stopped")
	return err
}

func (c *Controller) teardown() error {
	var err error
	if c.broker != nil {
		c.broker.Close()
	}
	if c.db != nil {
		err = c.db.Close()
		c.db = nil
	}
	if c.release != nil {
		c.release()
		c.release = nil
	}
	return err
}

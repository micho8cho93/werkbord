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
	"path/filepath"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/claude"
	"devboard/internal/agent/codex"
	"devboard/internal/api"
	"devboard/internal/config"
	"devboard/internal/events"
	"devboard/internal/github"
	"devboard/internal/gitrepo"
	"devboard/internal/runner"
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
	runner   *runner.Manager
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

	worktreeRoot, err := prepareWorktreeRoot(c.cfg.WorktreesPath())
	if err != nil {
		return err
	}
	git := &gitrepo.CLI{}
	deps := service.Deps{Store: c.db, Bus: c.broker, Log: c.log}
	projects := &service.Projects{Deps: deps, Git: git}
	tasks := &service.Tasks{Deps: deps}
	runs := &service.Runs{Deps: deps}
	worktrees := &service.Worktrees{Deps: deps, Root: worktreeRoot}
	gitControl := &service.GitControl{Deps: deps, Git: git, Worktrees: worktrees}
	if !c.cfg.GitHub.Disabled {
		gitControl.GitHub = &github.CLI{Binary: c.cfg.GitHub.Command}
	}
	agents, err := newAgents(c.cfg)
	if err != nil {
		return err
	}
	c.runner = runner.New(runner.Options{
		Runs: runs, Tasks: tasks, Projects: projects, Worktrees: worktrees, Git: git, Agents: agents,
		Log: c.log, WorktreeRoot: worktreeRoot,
	})
	for _, w := range c.cfg.RiskyAgentSettings() {
		c.log.Warn("agent setting lets it act without asking", "setting", w)
	}

	// Stop agent processes the previous controller left behind, and settle the
	// runs they belonged to, before anything can be asked of them.
	if err := c.runner.Recover(ctx); err != nil {
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
		Runner:       c.runner,
		Worktrees:    worktrees,
		Git:          gitControl,
		Agents:       agents,
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
	err = errors.Join(err, c.teardown(ctx))
	c.log.Info("controller stopped")
	return err
}

// teardown stops what Start started, in reverse order: agents first, while the
// database is still there to record how their runs ended, then the event
// broker and the database.
func (c *Controller) teardown(ctx ...context.Context) error {
	var err error
	if c.runner != nil {
		stop := context.Background()
		if len(ctx) > 0 {
			stop = ctx[0]
		}
		if e := c.runner.Shutdown(stop); e != nil {
			err = fmt.Errorf("stop agents: %w", e)
		}
		c.runner = nil
	}
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

// prepareWorktreeRoot creates the directory worktrees live in and returns its
// canonical path: worktree records are compared as text, so a symlink in the
// root (macOS's /var, for one) would make every path look different from what
// is on disk.
func prepareWorktreeRoot(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create worktree directory: %w", err)
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve worktree directory %s: %w", dir, err)
	}
	return root, nil
}

// newAgents registers the adapters. Both are always registered, even if the
// agent is not installed: the API then says why it cannot be used, which is
// more helpful than leaving it out.
func newAgents(cfg config.Config) (*agent.Registry, error) {
	r := agent.NewRegistry()
	cl, cx := cfg.Agents[config.AgentClaudeCode], cfg.Agents[config.AgentCodex]
	for _, a := range []agent.Adapter{
		claude.New(claude.Config{Command: cl.Command, Model: cl.Model, PermissionMode: cl.PermissionMode}),
		codex.New(codex.Config{Command: cx.Command, Model: cx.Model, ApprovalPolicy: cx.ApprovalPolicy, Sandbox: cx.Sandbox}),
	} {
		if err := r.Register(a); err != nil {
			return nil, err
		}
	}
	return r, nil
}

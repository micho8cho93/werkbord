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
	"sync"
	"time"

	"werkbord/internal/agent"
	"werkbord/internal/agent/acp"
	"werkbord/internal/agent/claude"
	"werkbord/internal/agent/codex"
	"werkbord/internal/agent/gemini"
	"werkbord/internal/agent/opencode"
	"werkbord/internal/agentcatalog"
	"werkbord/internal/agentkit"
	"werkbord/internal/api"
	"werkbord/internal/appops"
	"werkbord/internal/assistant"
	"werkbord/internal/config"
	"werkbord/internal/connections"
	"werkbord/internal/doctor"
	"werkbord/internal/domain"
	"werkbord/internal/events"
	"werkbord/internal/github"
	"werkbord/internal/gitrepo"
	"werkbord/internal/localaccess"
	"werkbord/internal/machine"
	"werkbord/internal/mcpaccess"
	"werkbord/internal/mcpgrant"
	"werkbord/internal/netprivate"
	"werkbord/internal/phoneaccess"
	"werkbord/internal/runner"
	"werkbord/internal/runnerwire"
	"werkbord/internal/service"
	"werkbord/internal/store"
	"werkbord/internal/store/sqlite"
	"werkbord/internal/terminal"
	"werkbord/internal/update"
	"werkbord/internal/webui"
)

// Controller is the long-running daemon.
type Controller struct {
	terminals     *terminal.Manager
	stopScheduler context.CancelFunc
	schedulerDone chan struct{}
	cfg           config.Config
	log           *slog.Logger
	version       string

	release     func()
	db          *sqlite.DB
	broker      *events.Broker
	runner      *runner.Manager
	assistant   *assistant.Engine
	appOps      *appops.Service
	connections *connections.Service
	freshness   *service.MemoryFreshness
	knowledge   *service.Knowledge
	health      *service.GitHealth
	stopHealth  context.CancelFunc
	server      *http.Server
	network     *networkControl
	relay       *relayControl
	// networkBackend replaces the embedded Tailscale node; tests use it so that no
	// real network is joined.
	networkBackend netprivate.Backend
	listener       net.Listener
	serveErr       chan error
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
			if c.relay != nil {
				c.relay.Stop(context.Background())
				c.relay = nil
			}
			if c.network != nil {
				_ = c.network.Stop(context.Background())
				c.network = nil
			}
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
	c.connections = connections.New(c.db, nil, appops.New(appops.Config{Store: c.db, Log: c.log}), c.log)

	worktreeRoot, err := prepareWorktreeRoot(c.cfg.WorktreesPath())
	if err != nil {
		return err
	}
	// Claude Code and Codex are each asked about themselves once, for both the coding-agent adapters and the assistant's
	// providers, so they share one prober.
	probe := agentkit.NewProber()
	agents, err := newAgents(c.cfg, probe)
	if err != nil {
		return err
	}
	providers, err := newAssistantProviders(c.cfg, probe)
	if err != nil {
		return err
	}
	git := &gitrepo.CLI{}
	deps := service.Deps{Store: c.db, Bus: c.broker, Log: c.log}
	settings := &service.Settings{Deps: deps, Catalog: agents, Version: c.version}
	projects := &service.Projects{Deps: deps, Git: git, Catalog: agents}
	tasks := &service.Tasks{Deps: deps, Catalog: agents}
	labels := &service.Labels{Deps: deps}
	runs := &service.Runs{Deps: deps}
	worktrees := &service.Worktrees{Deps: deps, Root: worktreeRoot}
	gitControl := &service.GitControl{Deps: deps, Git: git, Worktrees: worktrees}
	setup := &service.GitHubSetup{Deps: deps, Projects: projects}
	if !c.cfg.GitHub.Disabled {
		cli := &github.CLI{Binary: c.cfg.GitHub.Command}
		gitControl.GitHub = cli
		setup.CLI, setup.Login = cli, &github.LoginSession{CLI: cli}
	}
	// Repository health: recalculated when something that can change it happens, never on a timer.
	c.health = &service.GitHealth{Deps: deps, Control: gitControl}
	var healthCtx context.Context
	healthCtx, c.stopHealth = context.WithCancel(context.Background())
	c.health.Watch(healthCtx, c.broker)
	c.freshness = &service.MemoryFreshness{Deps: deps, Control: gitControl}
	c.freshness.Watch(healthCtx, c.broker)
	memory, err := service.NewMemory(deps, "individual", domain.MemoryLimits{})
	if err != nil {
		return err
	}
	c.knowledge = &service.Knowledge{Memory: memory, Freshness: c.freshness}
	c.knowledge.Watch(healthCtx)

	personal := &service.Personal{Deps: deps}
	scheduler := &service.Scheduler{Deps: deps, Git: git}
	handoffs := &service.Handoffs{Deps: deps, Git: git}
	runs.Handoffs = handoffs
	runnerRec, err := settings.RegisterRunner(ctx)
	if err != nil {
		return err
	}
	var capsMu sync.Mutex
	var cachedCaps domain.RunnerCapabilities
	var capsAt time.Time
	distributed := &service.Runners{Deps: deps, Runs: runs, LocalID: runnerRec.ID, LocalCapabilities: func(ctx context.Context) domain.RunnerCapabilities {
		capsMu.Lock()
		if time.Since(capsAt) > 10*time.Second {
			cachedCaps = machine.Capabilities(ctx, agents, c.cfg.DataDir)
			capsAt = time.Now()
		}
		caps := cachedCaps
		capsMu.Unlock()
		caps.Repositories = nil
		_ = c.db.View(ctx, func(tx store.Tx) error {
			ps, e := tx.Projects().List(ctx)
			for _, p := range ps {
				if p.HasRepository() {
					caps.Repositories = append(caps.Repositories, p.ID)
				}
			}
			return e
		})
		return caps
	}}
	catalog := service.ExecutionCatalog{Local: agents, Runners: distributed}
	settings.Catalog, projects.Catalog, tasks.Catalog = catalog, catalog, catalog
	scheduler.Runners = distributed
	selector := newModelSelector(c.cfg, agents, providers, settings, distributed)
	c.runner = runner.New(runner.Options{
		Selection: selector.Resolve,
		RefreshRemotes: func(ctx context.Context, id string) error {
			p, e := projects.Get(ctx, id)
			if e != nil {
				return e
			}
			if p.Repository == nil {
				return nil
			}
			for _, r := range p.Repository.Remotes {
				if !runnerwire.SafeRemote(r.URL) {
					continue
				}
				result, e := git.Fetch(ctx, p.RepoPath, r.Name)
				if e != nil {
					return e
				}
				if result.Outcome != domain.OutcomeDone {
					return fmt.Errorf("%w: cannot refresh remote %s: %s", domain.ErrConflict, r.Name, result.Message)
				}
			}
			return nil
		},
		Maintenance: personal.Maintain, Distributed: distributed, Scheduler: scheduler, Handoffs: handoffs, RepositoryLock: gitControl.LockExecution,
		Runs: runs, Tasks: tasks, Projects: projects, Settings: settings, Worktrees: worktrees, Git: git, Agents: agents,
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

	// This computer is a runner as soon as there is a controller on it.

	c.log.Info("runner registered", "runner", runnerRec.ID, "name", runnerRec.Name, "os", runnerRec.OS, "arch", runnerRec.Arch)

	// The token guards the API wherever it is reached from other than a loopback
	// that was left open on purpose, and the private network is such a place, so
	// there is always one.
	token, err := c.cfg.ResolveToken(true)
	if err != nil {
		return fmt.Errorf("api token: %w", err)
	}
	// Suggested next steps read the board, Git, GitHub and memory through the services the screens already use; they hold no
	// way to push, merge, branch or start anything.
	recommender := &service.Recommender{Deps: deps, Control: gitControl, Knowledge: c.knowledge, Runners: distributed}
	assistantSvc := assistantServices{personal: personal, recommender: recommender, knowledge: c.knowledge, projects: projects, tasks: tasks, labels: labels, runs: runs,
		control: &service.ControlCenter{Deps: deps, Scheduler: scheduler, Runners: distributed}, runner: c.runner, connections: c.connections}
	// Match the personal owner API: a tokenless controller cannot expose private
	// definitions through assistant tools as an alternate path.
	if !c.cfg.AuthRequired() {
		assistantSvc.personal = nil
	}
	c.appOps = newApplicationOperations(c.db, c.log, assistantSvc)
	mcpGrants, err := mcpgrant.Open(filepath.Join(c.cfg.DataDir, "mcp-clients.json"))
	if err != nil {
		return fmt.Errorf("MCP client grants: %w", err)
	}
	commands := map[string]string{}
	for _, id := range []string{"claude-code", "codex"} {
		commands[id] = c.cfg.Agents[id].Command
	}
	c.terminals, err = terminal.New(terminal.Options{Projects: projects, Settings: settings, Agents: agents, Commands: commands, Device: runnerRec.ID, StatePath: filepath.Join(c.cfg.DataDir, "terminals.json"), Log: c.log})
	if err != nil {
		return fmt.Errorf("terminals: %w", err)
	}
	apiOpts := api.Options{
		Terminals:    c.terminals,
		Personal:     personal,
		Memory:       c.knowledge,
		Recommender:  recommender,
		EmbedOrigins: c.cfg.EmbedOrigins,
		Distributed:  distributed,
		Scheduler:    scheduler, Handoffs: handoffs,
		Projects:      projects,
		Tasks:         tasks,
		Labels:        labels,
		Runs:          runs,
		Runner:        c.runner,
		Worktrees:     worktrees,
		Git:           gitControl,
		Health:        c.health,
		Agents:        agents,
		Settings:      settings,
		GitHub:        setup,
		Store:         c.db,
		Events:        c.broker,
		Log:           c.log,
		Version:       c.version,
		Connections:   c.connections,
		MCP:           mcpaccess.New(mcpGrants, c.appOps, projects, tasks),
		MCPManagement: true,
		Web:           webui.Handler(),
		AuthRequired:  c.cfg.AuthRequired(),
		Token:         token,
		TokenSource:   func() string { value, _ := c.cfg.ResolveToken(false); return value },
		AllowedHosts:  c.cfg.AllowedHosts,
	}
	if !c.cfg.AuthRequired() {
		apiOpts.Token = ""
	}
	// What is served on the private network never trusts the network: it demands
	// the token even when loopback has been opened without one.
	privateOpts := apiOpts
	privateOpts.Memory = nil
	// Terminal handlers still require an enrolled phone with a per-session grant.
	privateOpts.Terminals = c.terminals
	// Personal data is served here to an enrolled phone and to no one else, and only on the routes a phone has been
	// reviewed for (api/phone.go): the owner's token never opens it from the network (api.personalReady).
	privateOpts.PersonalPhonesOnly = true
	privateOpts.AuthRequired, privateOpts.Token = true, token
	privateOpts.Assistant = nil       // the assistant is not served on the private network
	privateOpts.Connections = nil     // upstream credentials and broker controls stay on this computer
	privateOpts.MCPManagement = false // the private network gets scoped MCP clients, never grant administration
	// The assistant borrows the sign-in of the coding agents already on this computer and reaches the board only through the
	// domain services, by way of application operations (internal/appops). It is served on the loopback listener only.
	if !c.cfg.Assistant.Disabled {
		assistantSvc.ops = c.appOps
		assistantSvc.selection = selector.Resolve
		if c.assistant, err = newAssistant(c.cfg, providers, c.db, c.log, assistantSvc); err != nil {
			return fmt.Errorf("assistant: %w", err)
		}
		if err := c.assistant.Recover(ctx); err != nil {
			return fmt.Errorf("recover the assistant: %w", err)
		}
		c.assistant.Start()
		apiOpts.Assistant = c.assistant
		// An enrolled phone may use it over the private network (and only an enrolled phone: see api.Options).
		privateOpts.Assistant, privateOpts.AssistantPhonesOnly = c.assistant, true
	}
	// Other programs on this computer may be given a narrow credential of their own (internal/localaccess). That is for
	// the loopback listener only: the private network, which anyone on it can reach, never takes one.
	access, err := localaccess.Open(filepath.Join(c.cfg.DataDir, "local-access.json"))
	if err != nil {
		return fmt.Errorf("local access: %w", err)
	}
	apiOpts.LocalAccess = access
	// Phones are enrolled with a credential of their own (internal/phoneaccess). It is read from the private network's
	// listener and from this computer, and from nowhere else.
	phones, err := phoneaccess.Open(filepath.Join(c.cfg.DataDir, "phones.json"))
	if err != nil {
		return fmt.Errorf("phone access: %w", err)
	}
	apiOpts.Phones, privateOpts.Phones = phones, phones
	privateHandler := &lateHandler{}
	c.network = newNetworkControl(c.cfg, settings, runnerDoor(privateHandler), c.networkBackend, c.log)
	apiOpts.Network, privateOpts.Network = c.network, c.network
	nw := c.network // not c.network: that is cleared when the controller shuts down
	env := doctor.Env{
		Config: c.cfg, Agents: agents, GitHub: setup, Settings: settings, Projects: projects, Git: git,
		Network: func() (netprivate.Status, bool) { return nw.Status(), true },
		Relay: func() (doctor.PhoneAccess, bool) {
			r := c.relay // cleared when the controller shuts down
			if r == nil {
				return doctor.PhoneAccess{}, true // no relay configured: phone access is off
			}
			st := r.Status()
			return doctor.PhoneAccess{Enabled: st.Enabled, Connected: st.Connected, Phones: st.Phones, Error: st.Error}, true
		},
	}
	apiOpts.Doctor = func(ctx context.Context) doctor.Report { return doctor.Run(ctx, c.version, env.Standard()...) }
	privateOpts.Doctor = apiOpts.Doctor
	// Whether a newer release exists. Unlike the rest of what the controller reaches out for, nobody asked
	// for this, so it is cached, never asked of a build from source, and can be turned off (noUpdateCheck).
	checker := &update.Checker{Current: c.version, Disabled: c.cfg.NoUpdateCheck}
	apiOpts.Update = checker.Status
	privateOpts.Update = apiOpts.Update
	privateHandler.set(api.New(privateOpts).Handler())
	if c.relay, err = newRelayControl(c.cfg.Relay, c.cfg.DataDir, phones, privateHandler, c.log); err != nil {
		return fmt.Errorf("relay: %w", err)
	}
	if c.relay != nil {
		apiOpts.Relay = c.relay
	}
	srv := api.New(apiOpts)

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
	if tcp, ok := c.listener.Addr().(*net.TCPAddr); ok {
		c.connections.SetOAuthRedirectURL(fmt.Sprintf("http://127.0.0.1:%d/api/connections/oauth/callback", tcp.Port))
	}
	c.serveErr = make(chan error, 1)
	go func() { c.serveErr <- c.server.Serve(c.listener) }()

	scheduleCtx, cancelSchedules := context.WithCancel(context.Background())
	c.stopScheduler = cancelSchedules
	c.schedulerDone = make(chan struct{})
	go func() { defer close(c.schedulerDone); c.runner.ScheduleLoop(scheduleCtx) }()
	c.log.Info("controller started",
		"addr", c.listener.Addr().String(), "data_dir", c.cfg.DataDir,
		"auth", c.cfg.AuthRequired(), "version", c.version)
	if err := c.network.StartIfWanted(ctx); err != nil {
		return fmt.Errorf("private network: %w", err)
	}
	if c.relay != nil {
		c.relay.Start()
	}
	if c.cfg.AuthRequired() && c.cfg.Token == "" {
		c.log.Info("API token required; run `werkbord token` to print it", "token_file", c.cfg.TokenPath())
	}
	if !c.cfg.AuthRequired() {
		c.log.Warn("API authentication is disabled (requireToken=false): any program on this computer can use the API and, once agents run, start processes as you")
	}
	return nil
}

// SetNetworkBackend replaces the embedded private network node before Start: for
// tests, which must not join a real network.
func (c *Controller) SetNetworkBackend(b netprivate.Backend) { c.networkBackend = b }

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
	if c.relay != nil {
		c.relay.Stop(ctx)
		c.relay = nil
	}
	if c.network != nil {
		err = c.network.Stop(ctx)
		c.network = nil
	}
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
	if c.terminals != nil {
		c.terminals.Close()
		c.terminals = nil
	}
	var err error
	// The assistant's turns run provider processes and record their end in the database: stop them while it is still there.
	if c.assistant != nil {
		stop := context.Background()
		if len(ctx) > 0 {
			stop = ctx[0]
		}
		if e := c.assistant.Shutdown(stop); e != nil {
			err = fmt.Errorf("stop the assistant: %w", e)
		}
		c.assistant = nil
	}
	if c.stopScheduler != nil {
		c.stopScheduler()
		<-c.schedulerDone
		c.stopScheduler = nil
	}
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
	// Health and memory workers read the database and observe events: stop both before either closes.
	if c.stopHealth != nil {
		c.stopHealth()
		c.stopHealth = nil
	}
	if c.health != nil {
		c.health.Wait()
		c.health = nil
	}
	if c.freshness != nil {
		c.freshness.Wait()
		c.freshness = nil
	}
	if c.knowledge != nil {
		c.knowledge.Wait()
		c.knowledge = nil
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

// NewAgents registers the adapters. Both are always registered, even if the
// agent is not installed: the API then says why it cannot be used, which is
// more helpful than leaving it out.
func NewAgents(cfg config.Config) (*agent.Registry, error) {
	return newAgents(cfg, agentkit.NewProber())
}

// newAgents is NewAgents with the prober the caller shares with the assistant's providers.
func newAgents(cfg config.Config, probe *agentkit.Prober) (*agent.Registry, error) {
	r := agent.NewRegistry()
	cl, cx := cfg.Agents[config.AgentClaudeCode], cfg.Agents[config.AgentCodex]
	gm, oc := cfg.Agents[config.AgentGeminiCLI], cfg.Agents[config.AgentOpenCode]
	for _, a := range []agent.Adapter{
		claude.New(claude.Config{Command: cl.Command, Model: cl.Model, PermissionMode: cl.PermissionMode, Models: choices(cl.Models), Reasoning: cl.Reasoning, Prober: probe}),
		codex.New(codex.Config{Command: cx.Command, Model: cx.Model, ApprovalPolicy: cx.ApprovalPolicy, Sandbox: cx.Sandbox, Models: choices(cx.Models), Reasoning: cx.Reasoning, Prober: probe}),
		gemini.New(gemini.Config{Command: gm.Command, Model: gm.Model, Models: choices(gm.Models), Reasoning: gm.Reasoning, Prober: probe}),
		opencode.New(opencode.Config{Command: oc.Command, Model: oc.Model, Models: choices(oc.Models), Reasoning: oc.Reasoning, Prober: probe}),
	} {
		if err := r.Register(a); err != nil {
			return nil, err
		}
	}
	for _, entry := range agentcatalog.Entries() {
		c := cfg.Agents[entry.ID]
		command := c.Command
		if command == "" {
			command = entry.Command
		}
		a := acp.New(acp.Config{ID: entry.ID, Name: entry.Name, Command: command, Args: entry.Args, DocsURL: entry.DocsURL, Guidance: entry.Setup, SessionOptions: true, DiscoverOptions: true, Model: c.Model, Models: choices(c.Models), Reasoning: c.Reasoning, Prober: probe})
		if err := r.Register(a); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// choices converts the models listed in config.json to the form pickers use.
func choices(in []config.ModelChoice) []domain.AgentOption {
	out := make([]domain.AgentOption, 0, len(in))
	for _, m := range in {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, domain.AgentOption{ID: m.ID, Name: name, Description: m.Description})
	}
	return out
}

// runnerDoor is all that is served on the private network (Tailscale): what a remote runner calls to join a controller
// and to sync with it, each of which proves itself with a key of its own. The app and the rest of the API are not served
// there, and phones do not use it: they come in through the relay.
func runnerDoor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/runner/join", "/api/runner/sync":
			next.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// lateHandler lets the private network be built before the handler it serves,
// which needs the network (to report its status). It is set once, before any
// request can reach it: the network starts after the controller is built.
type lateHandler struct{ h http.Handler }

func (l *lateHandler) set(h http.Handler) { l.h = h }

func (l *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { l.h.ServeHTTP(w, r) }

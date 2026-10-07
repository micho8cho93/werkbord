// Browser fixture only: disposable storage, a loopback listener and a scripted
// subprocess agent that edits only its assigned disposable worktree.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/fake"
	"devboard/internal/api"
	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/gitrepo"
	"devboard/internal/localaccess"
	"devboard/internal/netprivate"
	"devboard/internal/runner"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
	"devboard/internal/webui"
)

type network struct{}

func (network) Status() netprivate.Status {
	return netprivate.Status{State: netprivate.StateConnected, URL: "http://127.0.0.1:17421"}
}
func (network) Enable(context.Context) error  { return nil }
func (network) Disable(context.Context) error { return nil }
func (network) Choice(context.Context) string { return "on" }
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--agent-fixture" {
		if err := runFixtureAgent(); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	dir, err := os.MkdirTemp("", "werkbord-browser-fixture-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sqlite.Open(ctx, filepath.Join(dir, "fixture.db"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	broker := events.NewBroker()
	deps := service.Deps{Store: db, Bus: broker}
	g := &gitrepo.CLI{}
	agents := agent.NewRegistry()
	agents.Register(&fake.Adapter{Name: "codex", Info: domain.Agent{ID: "codex", Name: "Codex", Available: false}})
	if os.Getenv("WERKBORD_BROWSER_EXECUTION") == "1" {
		agents.Register(fixtureAdapter{})
	}
	settings := &service.Settings{Deps: deps, Catalog: agents}
	projects := &service.Projects{Deps: deps, Git: g, Catalog: agents}
	tasks := &service.Tasks{Deps: deps, Catalog: agents}
	runs := &service.Runs{Deps: deps}
	wt := &service.Worktrees{Deps: deps, Root: filepath.Join(dir, "worktrees")}
	hand := &service.Handoffs{Deps: deps, Git: g}
	runs.Handoffs = hand
	local, err := settings.RegisterRunner(ctx)
	if err != nil {
		log.Fatal(err)
	}
	distributed := &service.Runners{Deps: deps, Runs: runs, LocalID: local.ID, LocalCapabilities: func(context.Context) domain.RunnerCapabilities {
		return domain.RunnerCapabilities{CPU: 4, Agents: agents.Detect(ctx)}
	}}
	cat := service.ExecutionCatalog{Local: agents, Runners: distributed}
	settings.Catalog = cat
	projects.Catalog = cat
	tasks.Catalog = cat
	scheduler := &service.Scheduler{Deps: deps, Git: g, Runners: distributed}
	mgr := runner.New(runner.Options{Runs: runs, Tasks: tasks, Projects: projects, Settings: settings, Worktrees: wt, Git: g, Agents: agents, Scheduler: scheduler, Handoffs: hand, Distributed: distributed, WorktreeRoot: wt.Root})
	defer mgr.Shutdown(context.Background())
	gc := &service.GitControl{Deps: deps, Git: g, Worktrees: wt}
	access, err := localaccess.Open(filepath.Join(dir, "local-access.json"))
	if err != nil {
		log.Fatal(err)
	}
	handler := api.New(api.Options{Version: "browser-fixture", LocalAccess: access, Distributed: distributed, Scheduler: scheduler, Handoffs: hand, Projects: projects, Tasks: tasks, Runs: runs, Runner: mgr, Worktrees: wt, Git: gc, Agents: agents, Settings: settings, Network: network{}, Store: db, Events: broker, AuthRequired: true, Token: "disposable-browser-credential", Web: webui.Handler()}).Handler()
	addr := os.Getenv("WERKBORD_BROWSER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:17421"
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Print(err)
		}
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
	}

}

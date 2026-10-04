// Package api is the HTTP transport: a JSON API under /api, a Server-Sent
// Events stream at /api/events, and the embedded PWA at every other path.
// Handlers translate HTTP to service calls and contain no business rules.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"devboard/internal/agent"
	"devboard/internal/events"
	"devboard/internal/runner"
	"devboard/internal/service"
	"devboard/internal/store"
)

// Options configures a Server.
type Options struct {
	Projects  *service.Projects
	Tasks     *service.Tasks
	Runs      *service.Runs
	Control   *service.ControlCenter // the cross-project overview; built from Store if nil
	Runner    *runner.Manager        // starts and drives agent sessions
	Worktrees *service.Worktrees
	Git       *service.GitControl // the Git Control Center; nil disables its endpoints
	Health    *service.GitHealth  // repository health; nil disables its endpoints
	Agents    *agent.Registry
	Store     store.Store       // for health checks and event replay
	Events    events.Subscriber // live event source
	Log       *slog.Logger
	Version   string
	Web       http.Handler // serves the PWA; nil disables it

	// AuthRequired makes every /api request except /api/health present Token.
	AuthRequired bool
	Token        string
	// AllowedHosts are extra Host values accepted when auth is not required.
	AllowedHosts []string
}

// Server holds the HTTP handlers.
type Server struct {
	opt     Options
	log     *slog.Logger
	started time.Time
}

// New builds a Server.
func New(opt Options) *Server {
	if opt.Control == nil && opt.Store != nil {
		opt.Control = &service.ControlCenter{Deps: service.Deps{Store: opt.Store}}
	}
	log := opt.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{opt: opt, log: log, started: time.Now()}
}

// Handler returns the root handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Everything that belongs to a project is reached through that project: the
	// project in the path is checked against the thing asked for, and a mismatch
	// is a 404 exactly like a thing that does not exist, so no request scoped to
	// one project can read or act on another's. The few routes outside /projects
	// are global on purpose: the project list, the agents installed on this
	// computer, the event stream, and the Control Center.
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/control-center", s.handleControlCenter)
	mux.HandleFunc("GET /api/events", s.handleEvents) // ?project=<id> narrows it to one project

	mux.HandleFunc("GET /api/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/projects", s.handleRegisterProject)
	mux.HandleFunc("GET /api/projects/{pid}", s.handleGetProject)
	mux.HandleFunc("POST /api/projects/{pid}/refresh", s.handleRefreshProject)

	mux.HandleFunc("GET /api/projects/{pid}/tasks", s.handleListTasks)
	mux.HandleFunc("POST /api/projects/{pid}/tasks", s.handleCreateTask)
	mux.HandleFunc("PATCH /api/projects/{pid}/tasks/{id}", s.handleUpdateTask)
	mux.HandleFunc("GET /api/projects/{pid}/tasks/{id}/runs", s.handleListTaskRuns)
	mux.HandleFunc("POST /api/projects/{pid}/tasks/{id}/runs", s.handleStartRun)

	mux.HandleFunc("GET /api/projects/{pid}/runs", s.handleListProjectRuns) // the latest run of each task: the board
	mux.HandleFunc("GET /api/projects/{pid}/activity", s.handleProjectActivity)
	mux.HandleFunc("GET /api/projects/{pid}/runs/{id}", s.handleGetRun)
	mux.HandleFunc("GET /api/projects/{pid}/runs/{id}/events", s.handleRunEvents)
	mux.HandleFunc("POST /api/projects/{pid}/runs/{id}/input", s.handleRunInput)
	mux.HandleFunc("POST /api/projects/{pid}/runs/{id}/finish", s.handleFinishRun)
	mux.HandleFunc("POST /api/projects/{pid}/runs/{id}/stop", s.handleStopRun)

	mux.HandleFunc("GET /api/projects/{pid}/questions", s.handleListQuestions)
	mux.HandleFunc("GET /api/projects/{pid}/questions/{id}", s.handleGetQuestion)
	mux.HandleFunc("POST /api/projects/{pid}/questions/{id}/answer", s.handleAnswerQuestion)

	mux.HandleFunc("GET /api/projects/{pid}/worktrees/{id}", s.handleGetWorktree)

	// The Git Control Center. Reads never change anything; the POSTs are the explicit
	// actions, each of which checks again before it acts (see service.GitControl).
	mux.HandleFunc("GET /api/projects/{pid}/git", s.handleGitOverview)
	mux.HandleFunc("GET /api/projects/{pid}/git/pull-requests", s.handleGitPullRequests)
	mux.HandleFunc("GET /api/projects/{pid}/git/commits", s.handleGitCommits)
	mux.HandleFunc("GET /api/projects/{pid}/git/compare", s.handleGitCompare)
	mux.HandleFunc("GET /api/projects/{pid}/git/diff", s.handleGitDiff)
	mux.HandleFunc("GET /api/projects/{pid}/git/changes", s.handleGitChanges)
	mux.HandleFunc("GET /api/projects/{pid}/git/changes/diff", s.handleGitChangeDiff)
	mux.HandleFunc("POST /api/projects/{pid}/git/fetch", s.handleGitFetch)
	mux.HandleFunc("POST /api/projects/{pid}/git/push", s.handleGitPush)
	mux.HandleFunc("POST /api/projects/{pid}/git/merge/plan", s.handleGitMergePlan)
	mux.HandleFunc("POST /api/projects/{pid}/git/merge", s.handleGitMerge)
	mux.HandleFunc("POST /api/projects/{pid}/git/branches/delete-plan", s.handleGitDeletePlan)
	mux.HandleFunc("POST /api/projects/{pid}/git/branches/delete", s.handleGitDelete)
	mux.HandleFunc("POST /api/projects/{pid}/git/worktrees/clean-plan", s.handleGitCleanPlan)
	mux.HandleFunc("POST /api/projects/{pid}/git/worktrees/clean", s.handleGitClean)
	mux.HandleFunc("POST /api/projects/{pid}/git/pull-requests", s.handleGitCreatePR)

	// Repository health: what Git state needs attention. Reading is a database read; the
	// refresh looks at Git (never the network) and changes nothing in the repository.
	mux.HandleFunc("GET /api/projects/{pid}/git/health", s.handleRepoHealth)
	mux.HandleFunc("POST /api/projects/{pid}/git/health/refresh", s.handleRepoHealthRefresh)
	mux.HandleFunc("POST /api/projects/{pid}/git/health/findings/{id}/dismiss", s.handleRepoHealthDismiss)
	mux.HandleFunc("POST /api/projects/{pid}/git/health/findings/{id}/reopen", s.handleRepoHealthReopen)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	if s.opt.Web != nil {
		mux.Handle("/", s.opt.Web)
	}

	var h http.Handler = mux
	h = s.authenticate(h)
	h = s.checkOrigin(h)
	h = s.checkHost(h)
	h = securityHeaders(h)
	h = s.logRequests(h)
	h = s.recoverPanics(h)
	return h
}

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
	Runner    *runner.Manager // starts and drives agent sessions
	Worktrees *service.Worktrees
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
	log := opt.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{opt: opt, log: log, started: time.Now()}
}

// Handler returns the root handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/projects", s.handleRegisterProject)
	mux.HandleFunc("GET /api/projects/{id}", s.handleGetProject)
	mux.HandleFunc("POST /api/projects/{id}/refresh", s.handleRefreshProject)
	mux.HandleFunc("GET /api/projects/{id}/tasks", s.handleListTasks)
	mux.HandleFunc("POST /api/projects/{id}/tasks", s.handleCreateTask)
	mux.HandleFunc("PATCH /api/tasks/{id}", s.handleUpdateTask)
	mux.HandleFunc("GET /api/tasks/{id}/runs", s.handleListTaskRuns)
	mux.HandleFunc("POST /api/tasks/{id}/runs", s.handleStartRun)
	mux.HandleFunc("GET /api/projects/{id}/runs", s.handleListProjectRuns)
	mux.HandleFunc("GET /api/runs", s.handleListRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	mux.HandleFunc("GET /api/runs/{id}/events", s.handleRunEvents)
	mux.HandleFunc("POST /api/runs/{id}/input", s.handleRunInput)
	mux.HandleFunc("POST /api/runs/{id}/finish", s.handleFinishRun)
	mux.HandleFunc("POST /api/runs/{id}/stop", s.handleStopRun)
	mux.HandleFunc("GET /api/worktrees/{id}", s.handleGetWorktree)
	mux.HandleFunc("GET /api/questions", s.handleListQuestions)
	mux.HandleFunc("POST /api/questions/{id}/answer", s.handleAnswerQuestion)
	mux.HandleFunc("GET /api/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/events", s.handleEvents)
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

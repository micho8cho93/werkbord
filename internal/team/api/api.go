// Package api is Werkbord Team's HTTP transport: a JSON API under /api/team/v1 and
// the Team console at every other path. Handlers translate HTTP to service calls
// and hold no rules of their own.
//
// Every /api/team/v1 route except /health needs the member's token, as
// "Authorization: Bearer <token>", always: unlike the individual product's
// controller, a Team server is not a loopback-only program, so there is no
// loopback exemption. The token says which member, and so which workspace, a
// request is for; nothing in a URL selects a workspace.
//
// There is deliberately no route here that starts a process, reads a file, or
// reaches a member's computer (docs/PRODUCTS.md).
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"devboard/internal/httpkit"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
)

const maxBodyBytes = 64 << 10

// Options configures a Server.
type Options struct {
	Service *service.Service
	// Ping checks the database for /health.
	Ping    func(context.Context) error
	Log     *slog.Logger
	Version string
	// Console serves the Team console; nil disables it.
	Console http.Handler
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

type ctxKey struct{}

// Handler returns the root handler with its middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/team/v1/health", s.handleHealth)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/team/v1/me", s.handleMe)
	api.HandleFunc("GET /api/team/v1/roles", s.handleRoles)
	api.HandleFunc("GET /api/team/v1/workspace", s.handleWorkspace)
	api.HandleFunc("PATCH /api/team/v1/workspace", s.handleRenameWorkspace)
	api.HandleFunc("GET /api/team/v1/members", s.handleListMembers)
	api.HandleFunc("POST /api/team/v1/members", s.handleAddMember)
	api.HandleFunc("DELETE /api/team/v1/members/{id}", s.handleRemoveMember)
	api.HandleFunc("POST /api/team/v1/members/{id}/token", s.handleReissueToken)
	api.HandleFunc("GET /api/team/v1/projects", s.handleListProjects)
	api.HandleFunc("POST /api/team/v1/projects", s.handleCreateProject)
	api.HandleFunc("GET /api/team/v1/projects/{id}", s.handleGetProject)
	api.HandleFunc("PATCH /api/team/v1/projects/{id}", s.handleUpdateProject)
	api.HandleFunc("GET /api/team/v1/projects/{id}/members", s.handleListProjectMembers)
	api.HandleFunc("PUT /api/team/v1/projects/{id}/members/{memberId}", s.handleAddProjectMember)
	api.HandleFunc("DELETE /api/team/v1/projects/{id}/members/{memberId}", s.handleRemoveProjectMember)
	// the views that span projects, and the sync that keeps them current
	api.HandleFunc("GET /api/team/v1/overview", s.handleOverview)
	api.HandleFunc("GET /api/team/v1/my-work", s.handleMyWork)
	api.HandleFunc("GET /api/team/v1/reviews", s.handleReviews)
	api.HandleFunc("GET /api/team/v1/sync", s.handleWorkspaceSync)
	// the board and its tickets
	api.HandleFunc("GET /api/team/v1/projects/{id}/board", s.handleBoard)
	api.HandleFunc("GET /api/team/v1/projects/{id}/people", s.handleProjectPeople)
	api.HandleFunc("GET /api/team/v1/projects/{id}/sync", s.handleSync)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets", s.handleCreateTicket)
	api.HandleFunc("GET /api/team/v1/projects/{id}/tickets/{tid}", s.handleGetTicket)
	api.HandleFunc("PATCH /api/team/v1/projects/{id}/tickets/{tid}", s.handleUpdateTicket)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/move", s.handleMoveTicket)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/claim", s.handleClaimTicket)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/release", s.handleReleaseTicket)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/assign", s.handleAssignTicket)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/submit", s.handleSubmitTicket)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/request-changes", s.handleRequestChanges)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/complete", s.handleCompleteTicket)
	api.HandleFunc("PUT /api/team/v1/projects/{id}/tickets/{tid}/git", s.handleReportGit)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/handoff", s.handleHandoff)
	// repository awareness, activity and invites
	api.HandleFunc("GET /api/team/v1/projects/{id}/repository", s.handleRepositoryState)
	api.HandleFunc("POST /api/team/v1/projects/{id}/repository/branches", s.handleReportBranches)
	api.HandleFunc("GET /api/team/v1/projects/{id}/activity", s.handleActivity)
	api.HandleFunc("GET /api/team/v1/projects/{id}/invites", s.handleListInvites)
	api.HandleFunc("POST /api/team/v1/projects/{id}/invites", s.handleCreateInvite)
	api.HandleFunc("DELETE /api/team/v1/projects/{id}/invites/{inviteId}", s.handleRevokeInvite)
	api.HandleFunc("POST /api/team/v1/invites/join", s.handleJoinInvite)
	api.HandleFunc("/api/team/v1/", func(w http.ResponseWriter, r *http.Request) {
		httpkit.WriteError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	// Redeeming an invite is the one thing a person without an account can do: the
	// invite code is their credential.
	mux.HandleFunc("POST /api/team/v1/invites/redeem", s.handleRedeemInvite)
	mux.Handle("/api/team/v1/", s.authenticate(api))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpkit.WriteError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	if s.opt.Console != nil {
		mux.Handle("/", s.opt.Console)
	}

	var h http.Handler = mux
	h = checkOrigin(h)
	h = httpkit.SecurityHeaders(httpkit.DefaultCSP, h)
	h = httpkit.LogRequests(s.log, h)
	h = httpkit.RecoverPanics(s.log, h)
	return h
}

// authenticate resolves the bearer token to a member before any /api/team/v1 handler runs.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		actor, err := s.opt.Service.Authenticate(r.Context(), strings.TrimSpace(token))
		if err != nil {
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, actor)))
	})
}

func actorOf(r *http.Request) service.Actor {
	a, _ := r.Context().Value(ctxKey{}).(service.Actor)
	return a
}

// checkOrigin rejects cross-site state-changing requests from a browser.
func checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					httpkit.WriteError(w, http.StatusForbidden, "forbidden_origin", "cross-origin request rejected")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// fail maps service errors to HTTP responses. Unexpected errors are logged and
// reported generically so internals do not leak to clients.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		w.Header().Set("WWW-Authenticate", `Bearer realm="werkbord-team"`)
		httpkit.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid token")
	case errors.Is(err, domain.ErrForbidden):
		httpkit.WriteError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, domain.ErrNotFound):
		httpkit.WriteError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domain.ErrConflict):
		httpkit.WriteError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, domain.ErrInvalid):
		httpkit.WriteError(w, http.StatusBadRequest, "invalid", err.Error())
	case errors.Is(err, domain.ErrBusy):
		w.Header().Set("Retry-After", "5")
		httpkit.WriteError(w, http.StatusTooManyRequests, "busy", err.Error())
	case errors.Is(err, context.Canceled):
		// The client went away; nothing useful to send.
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		httpkit.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpkit.DecodeJSON(w, r, dst, maxBodyBytes); err != nil {
		s.fail(w, r, fmt.Errorf("%w: %v", domain.ErrInvalid, err))
		return false
	}
	return true
}

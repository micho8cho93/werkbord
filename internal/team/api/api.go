// Package api is Werkbord Team's HTTP transport: a JSON API under /api/team/v1 and
// the Team console at every other path. Handlers translate HTTP to service calls
// and hold no rules of their own.
//
// Protected routes require a bearer credential. Production remote calls also
// require a device-signed request proof; member credentials authorize only local
// administration. Authentication and each service transaction consult current
// membership. The credential determines the workspace, never a URL selector.
//
// There is deliberately no route here that starts a process, reads a file, or
// reaches a member's computer (docs/PRODUCTS.md).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/httpkit"
	"devboard/internal/team/authproof"
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
	// NodeStatus describes this host's own network node, for the network's health
	// report; nil when this host runs none.
	NodeStatus func() any
	// RequireDeviceProof is mandatory in production wiring. Member bearer credentials are local administration only.
	RequireDeviceProof bool
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
	api.HandleFunc("PUT /api/team/v1/license", s.handleInstallLicense)
	api.HandleFunc("GET /api/team/v1/me", s.handleMe)
	api.HandleFunc("GET /api/team/v1/roles", s.handleRoles)
	api.HandleFunc("GET /api/team/v1/workspace", s.handleWorkspace)
	api.HandleFunc("PATCH /api/team/v1/workspace", s.handleRenameWorkspace)
	api.HandleFunc("GET /api/team/v1/members", s.handleListMembers)
	api.HandleFunc("POST /api/team/v1/members", s.handleAddMember)
	api.HandleFunc("DELETE /api/team/v1/members/{id}", s.handleRemoveMember)
	api.HandleFunc("POST /api/team/v1/members/{id}/token", s.handleReissueToken)
	api.HandleFunc("PUT /api/team/v1/members/{id}/role", s.handleSetMemberRole)
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
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/archive-done", s.handleArchiveDone)
	api.HandleFunc("POST /api/team/v1/projects/{id}/tickets/{tid}/archive", s.handleArchiveTicket)
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
	// devices and the private network they join (coordination metadata only: addresses,
	// roles, reachability and invitations; nothing here reaches a device)
	api.HandleFunc("GET /api/team/v1/devices", s.handleListDevices)
	api.HandleFunc("POST /api/team/v1/devices/{id}/revoke", s.handleRevokeDevice)
	api.HandleFunc("PUT /api/team/v1/devices/{id}/network", s.handleSetDeviceNetwork)
	api.HandleFunc("GET /api/team/v1/devices/{id}/network", s.handleDeviceNetwork)
	api.HandleFunc("PUT /api/team/v1/devices/{id}/capabilities", s.handleSetDeviceCapabilities)
	api.HandleFunc("PUT /api/team/v1/devices/{id}/name", s.handleRenameDevice)
	api.HandleFunc("POST /api/team/v1/devices/{id}/provision", s.handleProvisionDevice)
	api.HandleFunc("DELETE /api/team/v1/devices/{id}/replica", s.handleRemoveHost)
	// where the workspace's data is kept, and backing it up
	api.HandleFunc("GET /api/team/v1/resilience", s.handleResilience)
	api.HandleFunc("POST /api/team/v1/device/heartbeat", s.handleHeartbeat)
	// signed requests between a person's own devices: stored and handed over, never made or run here
	api.HandleFunc("POST /api/team/v1/messages", s.handleSendMessage)
	api.HandleFunc("GET /api/team/v1/messages", s.handleListMessages)
	api.HandleFunc("GET /api/team/v1/messages/{id}", s.handleGetMessage)
	api.HandleFunc("GET /api/team/v1/device/messages", s.handleInbox)
	api.HandleFunc("POST /api/team/v1/device/messages/{id}/ack", s.handleAckMessage)
	api.HandleFunc("GET /api/team/v1/storage", s.handleStorage)
	api.HandleFunc("POST /api/team/v1/storage/backup", s.handleStorageBackup)
	api.HandleFunc("GET /api/team/v1/network", s.handleNetworkHealth)
	api.HandleFunc("PUT /api/team/v1/network/approval", s.handleSetApproval)
	api.HandleFunc("GET /api/team/v1/network/config", s.handleNetworkConfig)
	api.HandleFunc("POST /api/team/v1/network/certificate", s.handleRenewCertificate)
	api.HandleFunc("POST /api/team/v1/network/checks", s.handleNetworkCheck)
	api.HandleFunc("GET /api/team/v1/network/provision", s.handleCollectProvision)
	api.HandleFunc("POST /api/team/v1/network/provision/ack", s.handleAckProvision)
	api.HandleFunc("GET /api/team/v1/enrollment-invitations", s.handleListEnrollInvitations)
	api.HandleFunc("POST /api/team/v1/enrollment-invitations", s.handleCreateEnrollInvitation)
	api.HandleFunc("DELETE /api/team/v1/enrollment-invitations/{id}", s.handleWithdrawEnrollInvitation)
	api.HandleFunc("GET /api/team/v1/enrollments", s.handleListEnrollments)
	api.HandleFunc("POST /api/team/v1/enrollments/{id}/approve", s.handleApproveEnrollment)
	api.HandleFunc("POST /api/team/v1/enrollments/{id}/deny", s.handleDenyEnrollment)
	api.HandleFunc("/api/team/v1/", func(w http.ResponseWriter, r *http.Request) {
		httpkit.WriteError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	// Redeeming an invite is the one thing a person without an account can do: the
	// invite code is their credential.
	mux.HandleFunc("POST /api/team/v1/invites/redeem", func(w http.ResponseWriter, r *http.Request) {
		if s.opt.RequireDeviceProof && !loopbackRequest(r) {
			s.fail(w, r, domain.ErrUnauthenticated)
			return
		}
		s.handleRedeemInvite(w, r)
	})
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
		token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !bearer || len(r.Header.Values("Authorization")) != 1 {
			s.fail(w, r, domain.ErrUnauthenticated)
			return
		}
		actor, err := s.opt.Service.Authenticate(r.Context(), strings.TrimSpace(token))
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if s.opt.RequireDeviceProof && (actor.Device != nil || !loopbackRequest(r)) {
			if actor.Device == nil {
				s.fail(w, r, domain.ErrUnauthenticated)
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
			if err != nil {
				s.fail(w, r, domain.ErrInvalid)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			key, err := deviceid.ParsePublicKey(actor.Device.PublicKey)
			if err != nil {
				s.fail(w, r, domain.ErrUnauthenticated)
				return
			}
			proof, err := authproof.Verify(r, body, key, actor.Workspace.ID, actor.Member.ID, actor.Device.ID, time.Now())
			if err != nil {
				s.fail(w, r, domain.ErrUnauthenticated)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
				if err := s.opt.Service.ConsumeAPINonce(r.Context(), actor, proof.Nonce, time.UnixMilli(proof.Expires).Add(authproof.Skew)); err != nil {
					s.fail(w, r, err)
					return
				}
			}
		}
		ctx := service.AuthenticatedContext(r.Context(), actor)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxKey{}, actor)))
	})
}

func loopbackRequest(r *http.Request) bool {
	addr, err := netip.ParseAddrPort(r.RemoteAddr)
	// A proxy cannot turn remote member credentials into local administration.
	return err == nil && addr.Addr().IsLoopback() && r.Header.Get("Forwarded") == "" && r.Header.Get("X-Forwarded-For") == "" && r.Header.Get("X-Real-IP") == ""
}

func (s *Server) handleInstallLicense(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Document json.RawMessage `json:"document"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	c, err := s.opt.Service.InstallLicense(r.Context(), actorOf(r), in.Document)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, c)
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
	case errors.Is(err, domain.ErrReadOnly):
		// No quorum: writes and remote authorization stop. Cached local diagnostics may still read.
		w.Header().Set("Retry-After", "10")
		httpkit.WriteError(w, http.StatusServiceUnavailable, "read_only", err.Error())
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

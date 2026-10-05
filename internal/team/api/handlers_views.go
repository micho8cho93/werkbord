package api

import (
	"net/http"
	"strconv"
	"time"
)

// The views that span projects: the workspace overview, My Work and Reviews, and
// the sync that keeps them current. As everywhere, a handler decodes, calls one
// service method and encodes.

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	v, err := s.opt.Service.Overview(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, v, err)
}

func (s *Server) handleMyWork(w http.ResponseWriter, r *http.Request) {
	v, err := s.opt.Service.MyWork(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, v, err)
}

func (s *Server) handleReviews(w http.ResponseWriter, r *http.Request) {
	v, err := s.opt.Service.Reviews(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, v, err)
}

// handleWorkspaceSync holds the request open until anything the member can see
// changes (or about 20 seconds pass). since is the revision the client has, after
// the newest event id it has seen (leave it out to start: no events, just the
// cursor). A client that lost its connection asks again with the same two numbers
// and learns what it missed; see service.WaitForWorkspaceChange.
func (s *Server) handleWorkspaceSync(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	after := int64(-1)
	if v, err := strconv.ParseInt(q.Get("after"), 10, 64); err == nil && v >= 0 {
		after = v
	}
	wait := time.Duration(0)
	if v, err := strconv.Atoi(q.Get("wait")); err == nil && v > 0 {
		wait = time.Duration(v) * time.Second
	}
	res, err := s.opt.Service.WaitForWorkspaceChange(r.Context(), actorOf(r), since, after, wait)
	s.respond(w, r, http.StatusOK, res, err)
}

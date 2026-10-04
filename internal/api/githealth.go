package api

import (
	"net/http"
)

// Repository health. Like the rest of the Git endpoints these are thin: they call
// service.GitHealth and encode. Nothing here changes the repository; the one write
// is a person saying they know about a finding.

func (s *Server) health(w http.ResponseWriter) bool {
	if s.opt.Health == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "repository health is not enabled")
		return false
	}
	return true
}

// handleRepoHealth answers with what was last worked out, recalculating only if the
// project was never checked, something that can change the answer happened, or the
// last calculation is a few minutes old.
func (s *Server) handleRepoHealth(w http.ResponseWriter, r *http.Request) {
	if !s.health(w) {
		return
	}
	rep, err := s.opt.Health.Report(r.Context(), r.PathValue("pid"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleRepoHealthRefresh recalculates now.
func (s *Server) handleRepoHealthRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.health(w) {
		return
	}
	rep, err := s.opt.Health.Refresh(r.Context(), r.PathValue("pid"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleRepoHealthDismiss(w http.ResponseWriter, r *http.Request) {
	if !s.health(w) {
		return
	}
	rep, err := s.opt.Health.Dismiss(r.Context(), r.PathValue("pid"), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleRepoHealthReopen(w http.ResponseWriter, r *http.Request) {
	if !s.health(w) {
		return
	}
	rep, err := s.opt.Health.Reopen(r.Context(), r.PathValue("pid"), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

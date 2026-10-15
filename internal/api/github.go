package api

import (
	"net/http"

	"devboard/internal/service"
)

func (s *Server) githubReady(w http.ResponseWriter) bool {
	if s.opt.GitHub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the GitHub connection is not available")
		return false
	}
	return true
}

// handleGitHubStatus reports whether the user's GitHub account is connected.
func (s *Server) handleGitHubStatus(w http.ResponseWriter, r *http.Request) {
	if !s.githubReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.opt.GitHub.Status(r.Context()))
}

// handleGitHubLogin starts signing in. The response carries the one-time code
// and the address to enter it at; the app then polls /api/github.
func (s *Server) handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	if !s.githubReady(w) {
		return
	}
	l, err := s.opt.GitHub.StartLogin(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// handleGitHubScopes asks for the permissions the guided setup needs on the
// account already signed in; the app polls /api/github like a sign-in.
func (s *Server) handleGitHubScopes(w http.ResponseWriter, r *http.Request) {
	if !s.githubReady(w) {
		return
	}
	l, err := s.opt.GitHub.StartScopeRefresh(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleGitHubLoginCancel(w http.ResponseWriter, r *http.Request) {
	if !s.githubReady(w) {
		return
	}
	s.opt.GitHub.CancelLogin()
	writeJSON(w, http.StatusOK, s.opt.GitHub.Status(r.Context()))
}

// handleGitHubRepos lists the repositories the user can reach, with which are on
// this computer and which are GitHub only.
func (s *Server) handleGitHubRepos(w http.ResponseWriter, r *http.Request) {
	if !s.githubReady(w) {
		return
	}
	list, err := s.opt.GitHub.Repositories(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleGitHubAddRepo makes a repository a project: the clone the user chose on
// this computer, or a new clone of it.
func (s *Server) handleGitHubAddRepo(w http.ResponseWriter, r *http.Request) {
	if !s.githubReady(w) {
		return
	}
	var req struct {
		FullName string `json:"fullName"`
		Path     string `json:"path"` // a local clone; empty means clone it
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.opt.GitHub.Add(r.Context(), req.FullName, req.Path)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// handleGitHubCreateRepo creates the repository the person confirmed, turns on
// Pages, and makes it a project on this computer.
func (s *Server) handleGitHubCreateRepo(w http.ResponseWriter, r *http.Request) {
	if !s.githubReady(w) {
		return
	}
	var req service.CreateRepoRequest
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.opt.GitHub.CreateRepository(r.Context(), req)
	if err != nil {
		if res != nil { // partly done: say what exists
			writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{"code": "conflict", "message": err.Error()}, "result": res})
			return
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// handleGitHubPages reports whether a repository's site is up.
func (s *Server) handleGitHubPages(w http.ResponseWriter, r *http.Request) {
	if !s.githubReady(w) {
		return
	}
	st, err := s.opt.GitHub.Pages(r.Context(), r.URL.Query().Get("repo"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

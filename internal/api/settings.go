package api

import (
	"net/http"

	"devboard/internal/domain"
)

// settingsReady answers 503 and returns false when settings are not enabled.
func (s *Server) settingsReady(w http.ResponseWriter) bool {
	if s.opt.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "settings are not enabled")
		return false
	}
	return true
}

// handleAgentOptions lists the models and reasoning levels an agent offers,
// always beginning with "Agent default". It may ask the agent, so it is slower
// than /api/agents; the answer is remembered for a few minutes.
func (s *Server) handleAgentOptions(w http.ResponseWriter, r *http.Request) {
	if s.opt.Agents == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agents are not enabled")
		return
	}
	opts, ok := s.opt.Agents.Options(r.Context(), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such agent")
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

// handleGetSettings returns the global defaults: only what has been set. What a
// field means when unset is the built-in default, which the app words itself.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) {
		return
	}
	exec, err := s.opt.Settings.Execution(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"execution": exec})
}

func (s *Server) handleSetExecution(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) {
		return
	}
	var cfg domain.ExecutionConfig
	if err := decode(w, r, &cfg); err != nil {
		s.fail(w, r, err)
		return
	}
	cfg, err := s.opt.Settings.SetExecution(r.Context(), cfg)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"execution": cfg})
}

func (s *Server) handleSetProjectExecution(w http.ResponseWriter, r *http.Request) {
	var cfg domain.ExecutionConfig
	if err := decode(w, r, &cfg); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.opt.Projects.SetExecution(r.Context(), r.PathValue("pid"), cfg)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleListRunners(w http.ResponseWriter, r *http.Request) {
	if s.opt.Distributed != nil {
		rs, e := s.opt.Distributed.List(r.Context())
		if e != nil {
			s.fail(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"runners": rs})
		return
	}
	if !s.settingsReady(w) {
		return
	}
	rs, err := s.opt.Settings.Runners(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runners": rs})
}

func (s *Server) handleGetOnboarding(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) {
		return
	}
	ob, err := s.opt.Settings.Onboarding(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ob)
}

func (s *Server) handleCompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) {
		return
	}
	var req struct {
		Skipped []string `json:"skipped"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ob, err := s.opt.Settings.CompleteOnboarding(r.Context(), req.Skipped)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ob)
}

func (s *Server) handleResetOnboarding(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) {
		return
	}
	if err := s.opt.Settings.ResetOnboarding(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, domain.Onboarding{})
}

// handleDoctor runs the health checks inside the controller, so they see what the
// controller sees (its PATH, its agents, its network), which is what matters. The
// report never contains a secret.
func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if s.opt.Doctor == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the health check is not available")
		return
	}
	writeJSON(w, http.StatusOK, s.opt.Doctor(r.Context()))
}

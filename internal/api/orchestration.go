package api

import (
	"net/http"

	"werkbord/internal/domain"
	"werkbord/internal/runner"
	"werkbord/internal/service"
)

func (s *Server) handleSchedule(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	plan, e := s.opt.Scheduler.Plan(r.Context(), r.PathValue("pid"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"decisions": plan})
}
func (s *Server) handleOrchestrationSettings(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	settings, e := s.opt.Scheduler.Settings(r.Context(), r.PathValue("pid"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, settings)
}
func (s *Server) handleSetOrchestrationSettings(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	var in service.OrchestrationSettings
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	if e := s.opt.Scheduler.SetSettings(r.Context(), r.PathValue("pid"), in); e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, in)
}
func (s *Server) handoffReady(w http.ResponseWriter) bool {
	if s.opt.Handoffs == nil {
		writeError(w, 503, "unavailable", "handoffs are not enabled")
		return false
	}
	return true
}
func (s *Server) handleGenerateHandoff(w http.ResponseWriter, r *http.Request) {
	if !s.handoffReady(w) {
		return
	}
	if _, e := s.opt.Runs.GetIn(r.Context(), r.PathValue("pid"), r.PathValue("id")); e != nil {
		s.fail(w, r, e)
		return
	}
	run, e := s.opt.Handoffs.Generate(r.Context(), r.PathValue("id"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, run)
}
func (s *Server) handleSaveHandoff(w http.ResponseWriter, r *http.Request) {
	if !s.handoffReady(w) {
		return
	}
	var in struct {
		Version int64          `json:"version"`
		Handoff domain.Handoff `json:"handoff"`
	}
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	if _, e := s.opt.Runs.GetIn(r.Context(), r.PathValue("pid"), r.PathValue("id")); e != nil {
		s.fail(w, r, e)
		return
	}
	run, e := s.opt.Handoffs.Save(r.Context(), r.PathValue("id"), in.Version, in.Handoff)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, run)
}
func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) || !s.handoffReady(w) {
		return
	}
	var in struct {
		Mode            domain.SelectionMode    `json:"mode"`
		RunnerID        string                  `json:"runnerId"`
		AgentID         string                  `json:"agentId"`
		Model           string                  `json:"model"`
		Reasoning       string                  `json:"reasoning"`
		Purpose         string                  `json:"purpose"`
		Instructions    string                  `json:"instructions"`
		SelectedContext string                  `json:"selectedContext"`
		Policy          *domain.ExecutionPolicy `json:"policy"`
	}
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	parent, e := s.opt.Runs.GetIn(r.Context(), r.PathValue("pid"), r.PathValue("id"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	run, e := s.opt.Runner.Start(r.Context(), runner.StartInput{Mode: in.Mode, RunnerID: in.RunnerID, TaskID: parent.TaskID, AgentID: in.AgentID, Model: in.Model, Reasoning: in.Reasoning, ParentRunID: parent.ID, Purpose: in.Purpose, Instructions: in.Instructions, SelectedContext: in.SelectedContext, Policy: in.Policy})
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 201, run)
}

// fallbackView is a project's automatic-fallback choices with what it has done. Nothing in it names a prompt or a path.
type fallbackView struct {
	domain.FallbackSettings
	Ledger []domain.FallbackEntry `json:"ledger"`
}

func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	cfg, e := s.opt.Scheduler.FallbackSettings(r.Context(), r.PathValue("pid"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	l, e := s.opt.Scheduler.FallbackLedger(r.Context(), r.PathValue("pid"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, fallbackView{cfg, l.Entries})
}
func (s *Server) handleSetFallback(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	var in domain.FallbackSettings
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	out, e := s.opt.Scheduler.SetFallbackSettings(r.Context(), r.PathValue("pid"), in)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	l, _ := s.opt.Scheduler.FallbackLedger(r.Context(), r.PathValue("pid"))
	writeJSON(w, 200, fallbackView{out, l.Entries})
}

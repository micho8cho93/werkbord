package api

import (
	"fmt"
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
		// StopFirst ends a session that is still open (blocked, or waiting for a message) before continuing from it. It is
		// the person's own approval in the Continue dialog, so one action stands for "stop it and carry on elsewhere".
		StopFirst bool `json:"stopFirst"`
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
	if in.StopFirst && !parent.State.Terminal() {
		if parent.Remote || (parent.State != domain.RunBlocked && parent.State != domain.RunWaitingForUser) {
			s.fail(w, r, fmt.Errorf("%w: only a local session that is blocked or waiting for you can be stopped and continued in one step", domain.ErrConflict))
			return
		}
		if _, e := s.opt.Runner.Stop(r.Context(), parent.ID); e != nil {
			s.fail(w, r, e)
			return
		}
		// Best effort: the stop already recorded a handoff, and this adds the controller's reading of the worktree.
		_, _ = s.opt.Handoffs.Generate(r.Context(), parent.ID)
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

// usageView is the person's usage settings and the meter they are set against. It is the person's own, on this computer: it is not
// part of anything a team or a phone is shown.
type usageView struct {
	Settings domain.UsageSettings `json:"settings"`
	Meter    domain.UsageMeter    `json:"meter"`
}

func (s *Server) writeUsage(w http.ResponseWriter, r *http.Request, cfg domain.UsageSettings) {
	m, e := s.opt.Scheduler.UsageMeter(r.Context())
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, usageView{cfg, m})
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	cfg, e := s.opt.Scheduler.UsageSettings(r.Context())
	if e != nil {
		s.fail(w, r, e)
		return
	}
	s.writeUsage(w, r, cfg)
}

func (s *Server) handleSetUsage(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	var in domain.UsageSettings
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	cfg, e := s.opt.Scheduler.SetUsageSettings(r.Context(), in)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	s.writeUsage(w, r, cfg)
}

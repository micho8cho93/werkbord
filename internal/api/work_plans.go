package api

import (
	"net/http"
	"werkbord/internal/domain"
	"werkbord/internal/service"
)

func (s *Server) handleWorkPlans(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Tasks.WorkPlans(r.Context(), r.PathValue("pid"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleSaveWorkPlan(w http.ResponseWriter, r *http.Request) {
	var in domain.WorkPlan
	if err := decode(w, r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	out, err := s.opt.Tasks.SaveWorkPlan(r.Context(), r.PathValue("pid"), r.PathValue("plan"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handlePlanRunPreview(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	out, err := s.opt.Scheduler.PlanRunPreview(r.Context(), r.PathValue("pid"), r.PathValue("plan"), r.URL.Query().Get("mode"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleRunPlan(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	var in service.PlanRunInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	out, err := s.opt.Scheduler.RunPlan(r.Context(), r.PathValue("pid"), r.PathValue("plan"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleHoldPlan(w http.ResponseWriter, r *http.Request) {
	if s.opt.Scheduler == nil {
		writeError(w, 503, "unavailable", "scheduler is not enabled")
		return
	}
	var in struct {
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	out, err := s.opt.Scheduler.HoldPlan(r.Context(), r.PathValue("pid"), r.PathValue("plan"), in.Version)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

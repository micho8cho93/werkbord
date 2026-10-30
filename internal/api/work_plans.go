package api

import (
	"net/http"
	"werkbord/internal/domain"
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

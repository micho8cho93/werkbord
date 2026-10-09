package api

import (
	"devboard/internal/team/service"
	"net/http"
)

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.Schedules(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, 200, out, err)
}
func (s *Server) handleSetSchedule(w http.ResponseWriter, r *http.Request) {
	var in service.ScheduleInput
	if !s.decode(w, r, &in) {
		return
	}
	out, err := s.opt.Service.SetSchedule(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), in)
	s.respond(w, r, 200, out, err)
}
func (s *Server) handleDispatchSchedule(w http.ResponseWriter, r *http.Request) {
	var in service.ScheduleDispatch
	if !s.decode(w, r, &in) {
		return
	}
	out, err := s.opt.Service.DispatchSchedule(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), in)
	s.respond(w, r, 200, out, err)
}
func (s *Server) handleCancelSchedule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64 `json:"version"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	err := s.opt.Service.CancelSchedule(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), in.Version)
	s.respond(w, r, 204, nil, err)
}

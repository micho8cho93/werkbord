package api

import (
	"devboard/internal/team/domain"
	"net/http"
)

func (s *Server) handleReportProgress(w http.ResponseWriter, r *http.Request) {
	var in domain.Progress
	if !s.decode(w, r, &in) {
		return
	}
	out, err := s.opt.Service.ReportProgress(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), in)
	s.respond(w, r, 200, out, err)
}
func (s *Server) handleTicketProgress(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.TicketProgress(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"))
	s.respond(w, r, 200, out, err)
}

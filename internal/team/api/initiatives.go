package api

import (
	"net/http"
	"werkbord/internal/team/domain"
)

func (s *Server) handleInitiatives(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.Initiatives(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("iid"))
	s.respond(w, r, 200, out, err)
}
func (s *Server) handleInitiativeSave(w http.ResponseWriter, r *http.Request) {
	var in domain.Initiative
	if !s.decode(w, r, &in) {
		return
	}
	out, err := s.opt.Service.SaveInitiative(r.Context(), actorOf(r), r.PathValue("iid"), in)
	s.respond(w, r, 200, out, err)
}

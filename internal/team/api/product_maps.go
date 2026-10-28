package api

import (
	"net/http"
	"werkbord/internal/team/service"
)

func (s *Server) handleProductMaps(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.ProductMaps(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, out, err)
}
func (s *Server) handleProposeProductMap(w http.ResponseWriter, r *http.Request) {
	var in service.ProductMapProposal
	if !s.decode(w, r, &in) {
		return
	}
	out, err := s.opt.Service.ProposeProductMap(r.Context(), actorOf(r), r.PathValue("id"), in)
	s.respond(w, r, http.StatusCreated, out, err)
}
func (s *Server) handleReviewProductMap(w http.ResponseWriter, r *http.Request) {
	var in service.ProductMapDecision
	if !s.decode(w, r, &in) {
		return
	}
	out, err := s.opt.Service.ReviewProductMap(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("mid"), in)
	s.respond(w, r, http.StatusOK, out, err)
}

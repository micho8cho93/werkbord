package api

import (
	"net/http"
	"strconv"
	"werkbord/internal/team/domain"
)

func (s *Server) handleRelationships(w http.ResponseWriter, r *http.Request) {
	depth := 1
	var err error
	if raw := r.URL.Query().Get("depth"); raw != "" {
		depth, err = strconv.Atoi(raw)
		if err != nil {
			s.fail(w, r, domain.ErrInvalid)
			return
		}
	}
	out, err := s.opt.Service.Relationships(r.Context(), actorOf(r), r.PathValue("id"), depth)
	s.respond(w, r, 200, out, err)
}

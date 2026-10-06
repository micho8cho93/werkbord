package api

import "net/http"

func (s *Server) handleArchiveTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version  int64 `json:"version"`
		Archived bool  `json:"archived"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	k, err := s.opt.Service.ArchiveTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), in.Version, in.Archived)
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleArchiveDone(w http.ResponseWriter, r *http.Request) {
	n, err := s.opt.Service.ArchiveDone(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, map[string]int{"archived": n}, err)
}

package api

import "net/http"

func (s *Server) handleArchiveDone(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.opt.Tasks.ArchiveDone(r.Context(), r.PathValue("pid"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

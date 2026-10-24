package api

import (
	"devboard/internal/service"
	"net/http"
)

func (s *Server) handlePersonalAction(w http.ResponseWriter, r *http.Request) {
	if !s.personalReady(w, r) {
		return
	}
	if len(r.URL.RawQuery) != 0 {
		writeError(w, 400, "invalid", "actions take no query parameters")
		return
	}
	var in service.PersonalActionRequest
	if !personalDecode(w, r, &in) {
		return
	}
	out, err := s.opt.Personal.ExecuteAction(r.Context(), r.PathValue("id"), r.PathValue("view"), r.PathValue("action"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handlePersonalBinding(w http.ResponseWriter, r *http.Request) {
	if !s.personalReady(w, r) {
		return
	}
	if len(r.URL.RawQuery) != 0 {
		writeError(w, 400, "invalid", "bindings take no query parameters")
		return
	}
	out, err := s.opt.Personal.ResolveBinding(r.Context(), r.PathValue("id"), r.PathValue("binding"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

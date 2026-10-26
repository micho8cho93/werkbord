package api

import (
	"fmt"
	"net/http"
	"strconv"

	"werkbord/internal/httpkit"
	"werkbord/internal/team/domain"
)

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n == 0 {
			s.fail(w, r, fmt.Errorf("%w: invalid analytics range", domain.ErrInvalid))
			return
		}
		days = n
	}
	project := r.PathValue("id")
	if project == "" {
		project = r.URL.Query().Get("project")
	} else if p := r.URL.Query().Get("project"); p != "" && p != project {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	out, e := s.opt.Service.CoordinationAnalytics(r.Context(), actorOf(r), project, days)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpkit.WriteJSON(w, http.StatusOK, out)
}

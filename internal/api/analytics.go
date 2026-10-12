package api

import (
	"fmt"
	"net/http"
	"strconv"

	"devboard/internal/domain"
	"devboard/internal/service"
)

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		var e error
		days, e = strconv.Atoi(raw)
		if e != nil || days == 0 {
			s.fail(w, r, fmt.Errorf("%w: invalid analytics range", domain.ErrInvalid))
			return
		}
	}
	project := r.PathValue("pid")
	if project == "" {
		project = r.URL.Query().Get("project")
	} else if requested := r.URL.Query().Get("project"); requested != "" && requested != project {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	if s.opt.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "analytics is unavailable")
		return
	}
	out, e := (&service.Analytics{Deps: service.Deps{Store: s.opt.Store}}).Report(r.Context(), service.AnalyticsFilter{ProjectID: project, Provider: r.URL.Query().Get("provider"), Days: days})
	if e != nil {
		s.fail(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

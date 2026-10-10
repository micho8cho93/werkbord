package api

import (
	"fmt"
	"net/http"

	"devboard/internal/domain"
	"devboard/internal/service"
)

var errVersionRequired = fmt.Errorf("%w: version is required", domain.ErrInvalid)

func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request) {
	ls, err := s.opt.Labels.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": ls})
}

func (s *Server) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	l, err := s.opt.Labels.Create(r.Context(), service.NewLabel{Name: req.Name, Color: req.Color, Description: req.Description})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleUpdateLabel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        *string `json:"name"`
		Color       *string `json:"color"`
		Description *string `json:"description"`
		Version     *int64  `json:"version"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Version == nil {
		s.fail(w, r, errVersionRequired)
		return
	}
	l, err := s.opt.Labels.Update(r.Context(), r.PathValue("id"), service.LabelPatch{Name: req.Name, Color: req.Color, Description: req.Description, Version: *req.Version})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteLabel(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Labels.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTimeline reports what is wrong with a project's dependencies and planned dates. It only
// reads: it never moves a date or a task.
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	t, err := s.opt.Tasks.Timeline(r.Context(), r.PathValue("pid"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

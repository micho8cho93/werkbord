package api

import (
	"fmt"
	"net/http"

	"devboard/internal/httpkit"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
)

func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request) {
	ls, err := s.opt.Service.ListLabels(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, map[string]any{"labels": ls})
}

func (s *Server) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	l, err := s.opt.Service.CreateLabel(r.Context(), actorOf(r), service.LabelInput{Name: in.Name, Color: in.Color, Description: in.Description})
	s.respond(w, r, http.StatusCreated, l, err)
}

func (s *Server) handleUpdateLabel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        *string `json:"name"`
		Color       *string `json:"color"`
		Description *string `json:"description"`
		Version     *int64  `json:"version"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if in.Version == nil {
		s.fail(w, r, fmt.Errorf("%w: version is required", domain.ErrInvalid))
		return
	}
	l, err := s.opt.Service.UpdateLabel(r.Context(), actorOf(r), r.PathValue("id"), service.LabelPatch{Name: in.Name, Color: in.Color, Description: in.Description, Version: *in.Version})
	s.respond(w, r, http.StatusOK, l, err)
}

func (s *Server) handleDeleteLabel(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Service.DeleteLabel(r.Context(), actorOf(r), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	t, err := s.opt.Service.Timeline(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, t, err)
}

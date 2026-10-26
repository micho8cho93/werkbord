package api

import (
	"net/http"
	"strconv"
	"werkbord/internal/domain"
	"werkbord/internal/integration"
)

func (s *Server) handleRelationships(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	depth := 1
	var err error
	if raw := r.URL.Query().Get("depth"); raw != "" {
		depth, err = strconv.Atoi(raw)
		if err != nil {
			s.fail(w, r, domain.ErrInvalid)
			return
		}
	}
	out, err := s.opt.Memory.Relationships(r.Context(), r.PathValue("pid"), depth)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleRelationshipSave(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	var in struct {
		Relationship integration.RepositoryRelationship `json:"relationship"`
		Accept       bool                               `json:"accept"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Memory.SaveRelationship(r.Context(), r.PathValue("pid"), in.Relationship, in.Accept)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 201, out)
}
func (s *Server) handleRelationshipDiscover(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	var in struct {
		Source   integration.MemoryTarget `json:"source"`
		Manifest string                   `json:"manifest"`
		Content  string                   `json:"content"`
		Revision string                   `json:"revision"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Memory.DiscoverRelationships(r.Context(), r.PathValue("pid"), in.Source, in.Manifest, []byte(in.Content), in.Revision)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 201, out)
}

func (s *Server) handleRelationshipAccept(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	var in struct {
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Memory.AcceptRelationship(r.Context(), r.PathValue("pid"), r.PathValue("id"), in.Version)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

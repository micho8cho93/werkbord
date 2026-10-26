package api

import (
	"net/http"
	"werkbord/internal/domain"
	"werkbord/internal/integration"
)

func (s *Server) handlePublicationGrants(w http.ResponseWriter, r *http.Request) {
	if !s.memoryShareReady(w, r) {
		return
	}
	if r.Method == "PUT" {
		var in domain.PublicationGrant
		if err := decode(w, r, &in); err != nil {
			s.fail(w, r, err)
			return
		}
		if err := s.opt.Memory.Memory.SetPublicationGrant(r.Context(), in); err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, 200, nil)
		return
	}
	out, err := s.opt.Memory.Memory.PublicationGrants(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handlePublications(w http.ResponseWriter, r *http.Request) {
	if !s.memoryShareReady(w, r) {
		return
	}
	out, err := s.opt.Memory.Memory.ReconcilePublication(r.Context(), r.URL.Query().Get("workspaceId"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handlePublicationReview(w http.ResponseWriter, r *http.Request) {
	if !s.memoryShareReady(w, r) {
		return
	}
	var in struct {
		WorkspaceID string `json:"workspaceId"`
		ProjectID   string `json:"projectId"`
		MemoryID    string `json:"memoryId"`
		Version     int64  `json:"version"`
		Summary     string `json:"summary"`
		Revoke      bool   `json:"revoke"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	var err error
	if in.Revoke {
		err = s.opt.Memory.Memory.RevokePublication(r.Context(), in.WorkspaceID, in.ProjectID, in.MemoryID)
	} else {
		err = s.opt.Memory.Memory.ReviewPublication(r.Context(), in.WorkspaceID, in.ProjectID, in.MemoryID, in.Version, in.Summary)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, nil)
}
func (s *Server) handlePublicationEvents(w http.ResponseWriter, r *http.Request) {
	if !s.memoryShareReady(w, r) {
		return
	}
	workspace := r.URL.Query().Get("workspaceId")
	if r.Method == "POST" {
		var in integration.MemoryReceipt
		if err := decode(w, r, &in); err != nil {
			s.fail(w, r, err)
			return
		}
		if err := s.opt.Memory.Memory.AckPublication(r.Context(), workspace, in); err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, 200, nil)
		return
	}
	out, err := s.opt.Memory.Memory.PublicationEvents(r.Context(), workspace)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

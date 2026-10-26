package api

import (
	"net/http"
	"time"
	"werkbord/internal/integration"
)

func (s *Server) memoryShareReady(w http.ResponseWriter, r *http.Request) bool {
	if !fromThisComputer(r) || !s.opt.AuthRequired {
		writeError(w, 403, "forbidden", "memory sharing requires the authenticated loopback controller")
		return false
	}
	return s.memoryReady(w)
}
func (s *Server) handleMemoryShareApprove(w http.ResponseWriter, r *http.Request) {
	if !s.memoryShareReady(w, r) {
		return
	}
	var in struct {
		Proposal  integration.MemoryProposal `json:"proposal"`
		Reviewed  bool                       `json:"reviewed"`
		ExpiresAt time.Time                  `json:"expiresAt"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Memory.Memory.ApproveMemoryShare(r.Context(), in.Proposal, in.Reviewed, in.ExpiresAt)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 201, out)
}
func (s *Server) handleMemoryShares(w http.ResponseWriter, r *http.Request) {
	if !s.memoryShareReady(w, r) {
		return
	}
	for k, values := range r.URL.Query() {
		if k != "workspaceId" && k != "after" || len(values) != 1 {
			writeError(w, 400, "invalid", "unsupported memory query")
			return
		}
	}
	out, err := s.opt.Memory.Memory.MemoryShares(r.Context(), r.URL.Query().Get("workspaceId"), r.URL.Query().Get("after"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleMemoryShareRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.memoryShareReady(w, r) {
		return
	}
	if err := s.opt.Memory.Memory.RevokeMemoryShare(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(204)
}

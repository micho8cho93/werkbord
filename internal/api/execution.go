package api

import (
	"devboard/internal/integration"
	"net/http"
	"time"
)

func (s *Server) handleExecutionPreview(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) {
		return
	}
	var in integration.ExecutionRequest
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	policy, err := s.opt.Runner.ExecutionPolicy(in.AgentID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Tasks.PreviewExecution(r.Context(), in, policy)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleExecutionApprove(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) {
		return
	}
	var in struct {
		Preview   integration.ExecutionPreview `json:"preview"`
		ExpiresAt time.Time                    `json:"expiresAt"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	policy, err := s.opt.Runner.ExecutionPolicy(in.Preview.Request.AgentID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Tasks.ApproveExecution(r.Context(), in.Preview, in.ExpiresAt, policy)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleExecutionApproval(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Tasks.ExecutionApproval(r.Context(), integration.ExecutionDispatch{ExecutionID: r.PathValue("id"), Fence: r.URL.Query().Get("fence")})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleExecutionDispatch(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) {
		return
	}
	var in integration.ExecutionDispatch
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Runner.ScheduleAuthorized(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// No prompt, worktree path, raw logs or handoff text in a dispatch acknowledgment.
	writeJSON(w, 200, map[string]string{"runId": out.ID, "state": string(out.State)})
}
func (s *Server) handleExecutionRevoke(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Tasks.RevokeExecution(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(204)
}

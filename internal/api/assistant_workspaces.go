package api

import (
	"encoding/json"
	"net/http"

	"devboard/internal/appops"
)

func (s *Server) handleAssistantWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	var in struct {
		Workspaces []appops.Workspace `json:"workspaces"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.opt.Assistant.ConnectWorkspaces(r.Context(), r.PathValue("id"), in.Workspaces); err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
}

func (s *Server) handleAssistantWorkspacePending(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"pending": s.opt.Assistant.WorkspaceRequestPending(r.PathValue("id"), r.PathValue("request"))})
}

func (s *Server) handleAssistantWorkspaceAnswer(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	var in struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.opt.Assistant.AnswerWorkspace(r.PathValue("id"), r.PathValue("request"), in.Data, in.Error); err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

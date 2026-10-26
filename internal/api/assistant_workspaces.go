package api

import (
	"encoding/json"
	"net/http"

	"werkbord/internal/appops"
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
	request, pending := s.opt.Assistant.WorkspaceRequestStatus(r.PathValue("id"), r.PathValue("request"))
	writeJSON(w, http.StatusOK, map[string]any{"pending": pending, "request": request})
}

func (s *Server) handleAssistantWorkspaceAnswer(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	var in struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
		Code  string          `json:"code"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.opt.Assistant.AnswerWorkspace(r.PathValue("id"), r.PathValue("request"), in.Data, in.Error, in.Code); err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

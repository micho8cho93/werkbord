package api

import (
	"net/http"

	"devboard/internal/httpkit"
	"devboard/internal/mcpcontract"
	"devboard/internal/team/service"
)

func (s *Server) handleMCPOperations(w http.ResponseWriter, r *http.Request) {
	operations := append([]string(nil), service.MCPOperations...)
	httpkit.WriteJSON(w, http.StatusOK, map[string]any{"operations": operations})
}

func (s *Server) handleMCPGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := s.opt.Service.ListMCPGrants(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, map[string]any{"grants": grants}, err)
}

func (s *Server) handleMCPGrantCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string   `json:"name"`
		Projects   []string `json:"projects"`
		Operations []string `json:"operations"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	grant, err := s.opt.Service.CreateMCPGrant(r.Context(), actorOf(r), mcpcontract.Principal{Name: in.Name, Projects: in.Projects, Operations: in.Operations})
	s.respond(w, r, http.StatusCreated, grant, err)
}

func (s *Server) handleMCPGrantRevoke(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Service.RevokeMCPGrant(r.Context(), actorOf(r), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

package api

import (
	"net/http"

	"werkbord/internal/httpkit"
	"werkbord/internal/mcpcontract"
	"werkbord/internal/mcpplan"
	"werkbord/internal/team/service"
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

func (s *Server) handleMCPTaskPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.opt.Service.ListMCPTaskPlans(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, map[string]any{"proposals": plans}, err)
}

func (s *Server) handleMCPTaskPlanStatus(w http.ResponseWriter, r *http.Request) {
	proposal, err := s.opt.Service.MCPTaskPlanStatus(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, proposal, err)
}

func (s *Server) handleMCPTaskPlanEdit(w http.ResponseWriter, r *http.Request) {
	var plan mcpplan.Plan
	if !s.decode(w, r, &plan) {
		return
	}
	proposal, err := s.opt.Service.ReplaceMCPTaskPlan(r.Context(), actorOf(r), r.PathValue("id"), plan)
	s.respond(w, r, http.StatusOK, proposal, err)
}

func (s *Server) handleMCPTaskPlanApprove(w http.ResponseWriter, r *http.Request) {
	proposal, err := s.opt.Service.ResolveMCPTaskPlan(r.Context(), actorOf(r), r.PathValue("id"), true)
	s.respond(w, r, http.StatusOK, proposal, err)
}

func (s *Server) handleMCPTaskPlanDecline(w http.ResponseWriter, r *http.Request) {
	proposal, err := s.opt.Service.ResolveMCPTaskPlan(r.Context(), actorOf(r), r.PathValue("id"), false)
	s.respond(w, r, http.StatusOK, proposal, err)
}

package api

import (
	"errors"
	"net/http"

	"devboard/internal/appops"
	"devboard/internal/domain"
	"devboard/internal/mcpcontract"
	"devboard/internal/mcpplan"
)

func (s *Server) mcpReady(w http.ResponseWriter) bool {
	if s.opt.MCP == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "MCP is disabled on this controller")
		return false
	}
	return true
}

func (s *Server) handleMCPOperations(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operations": s.opt.MCP.Operations()})
}

func (s *Server) handleMCPGrants(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": s.opt.MCP.List()})
}

func (s *Server) handleMCPGrantCreate(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	var in struct {
		Name       string   `json:"name"`
		Projects   []string `json:"projects"`
		Operations []string `json:"operations"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	created, err := s.opt.MCP.Create(r.Context(), mcpcontract.Principal{Name: in.Name, Projects: in.Projects, Operations: in.Operations})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleMCPGrantRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	if !s.opt.MCP.Revoke(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "not_found", "no active MCP client grant has that id")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMCPProposals(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	proposals, err := s.opt.MCP.Pending(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": proposals})
}

func (s *Server) handleMCPProposalResolve(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	var in struct {
		Approve bool `json:"approve"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	result, err := s.opt.MCP.Resolve(r.Context(), r.PathValue("grant"), r.PathValue("action"), in.Approve)
	if err != nil {
		var operationErr *appops.Error
		if errors.As(err, &operationErr) {
			status := http.StatusConflict
			if operationErr.Code == appops.CodeNotFound {
				status = http.StatusNotFound
			}
			writeError(w, status, operationErr.Code, operationErr.Message)
			return
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMCPProposalEdit(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	var plan mcpplan.Plan
	if err := decode(w, r, &plan); err != nil {
		s.fail(w, r, err)
		return
	}
	proposal, err := s.opt.MCP.ReplaceTaskPlan(r.Context(), r.PathValue("grant"), r.PathValue("action"), plan)
	if err != nil {
		var operationErr *appops.Error
		if errors.As(err, &operationErr) {
			status := http.StatusConflict
			if operationErr.Code == appops.CodeNotFound {
				status = http.StatusNotFound
			} else if operationErr.Code == appops.CodeInvalidArguments {
				status = http.StatusBadRequest
			}
			writeError(w, status, operationErr.Code, operationErr.Message)
			return
		}
		status := http.StatusConflict
		if errors.Is(err, domain.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, domain.ErrInvalid) {
			status = http.StatusBadRequest
		}
		writeError(w, status, "proposal_edit_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, proposal)
}

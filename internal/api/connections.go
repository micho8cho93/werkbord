package api

import (
	"errors"
	"net/http"

	"werkbord/internal/connections"
	"werkbord/internal/domain"
)

func (s *Server) connectionsReady(w http.ResponseWriter) bool {
	if s.opt.Connections == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "MCP connections are not enabled on this listener")
		return false
	}
	return true
}

func (s *Server) handleConnectionsList(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	items, err := s.opt.Connections.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": items})
}

func (s *Server) handleConnectionsOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	if !fromThisComputer(r) {
		http.Error(w, "OAuth callbacks are accepted from this computer only.", http.StatusForbidden)
		return
	}
	if !s.opt.Connections.HandleOAuthCallback(r.URL.Query()) {
		http.Error(w, "This sign-in has expired or was already used. Return to Werkbord and try again.", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("Sign-in completed. Return to Werkbord."))
}

func (s *Server) handleConnectionsSave(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	var in connections.SaveInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if id := r.PathValue("id"); id != "" {
		in.ID = id
	}
	c, err := s.opt.Connections.Save(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_connection", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": c})
}

func (s *Server) handleConnectionsDelete(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	if err := s.opt.Connections.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.connectionFailure(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConnectionsTest(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	c, err := s.opt.Connections.Test(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"connection": c, "error": safeAPIError(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": c})
}

func (s *Server) handleConnectionsReauthorize(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	c, err := s.opt.Connections.Reauthorize(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"connection": c, "error": safeAPIError(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": c})
}

func (s *Server) handleConnectionsEnabled(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.opt.Connections.SetEnabled(r.Context(), r.PathValue("id"), in.Enabled)
	if err != nil {
		s.connectionFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": c})
}

func (s *Server) handleConnectionsContext(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.opt.Connections.SetContext(r.Context(), r.PathValue("id"), r.PathValue("kind"), in.Enabled)
	if err != nil {
		s.connectionFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": c})
}

func (s *Server) handleConnectionsPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	var in struct {
		Context string `json:"context"`
		Policy  string `json:"policy"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.opt.Connections.SetToolPolicy(r.Context(), r.PathValue("id"), r.PathValue("tool"), in.Context, in.Policy)
	if err != nil {
		s.connectionFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": c})
}

func (s *Server) handleConnectionsCall(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	var in struct {
		Context   string         `json:"context"`
		Arguments map[string]any `json:"arguments"`
		Confirmed bool           `json:"confirmed"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	data, err := s.opt.Connections.CallTool(r.Context(), r.PathValue("id"), r.PathValue("tool"), in.Context, in.Arguments, in.Confirmed)
	if err != nil {
		s.connectionFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": data})
}

func (s *Server) handleConnectionsReadResource(w http.ResponseWriter, r *http.Request) {
	if !s.connectionsReady(w) {
		return
	}
	var in struct {
		Context string `json:"context"`
		URI     string `json:"uri"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	data, err := s.opt.Connections.ReadResource(r.Context(), r.PathValue("id"), in.Context, in.URI)
	if err != nil {
		s.connectionFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": data})
}

func (s *Server) connectionFailure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "MCP connection was not found")
	case errors.Is(err, connections.ErrConfirmationRequired):
		writeError(w, http.StatusConflict, "confirmation_required", err.Error())
	case errors.Is(err, connections.ErrResultTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "result_too_large", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "mcp_connection_error", safeAPIError(err))
	}
}

func safeAPIError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

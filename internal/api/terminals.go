package api

import (
	"net/http"
	"strconv"

	"devboard/internal/domain"
)

// Terminals are served only by the local owner's controller. Program, phone,
// runner and Team grants cannot reach this surface through their route allow-lists.
func (s *Server) terminalReady(w http.ResponseWriter, r *http.Request) bool {
	if !s.opt.AuthRequired || s.opt.Terminals == nil || !s.opt.Terminals.Enabled() || !fromThisComputer(r) {
		writeError(w, 403, "forbidden", "native terminals require the execution device's authenticated local owner controller")
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}
func (s *Server) terminalRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/terminals", s.handleTerminalList)
	m.HandleFunc("POST /api/projects/{pid}/terminals", s.handleTerminalCreate)
	m.HandleFunc("GET /api/projects/{pid}/terminals/{id}/output", s.handleTerminalOutput)
	m.HandleFunc("POST /api/projects/{pid}/terminals/{id}/input", s.handleTerminalInput)
	m.HandleFunc("POST /api/projects/{pid}/terminals/{id}/resize", s.handleTerminalResize)
	m.HandleFunc("POST /api/projects/{pid}/terminals/{id}/stop", s.handleTerminalStop)
}
func (s *Server) handleTerminalList(w http.ResponseWriter, r *http.Request) {
	if !s.terminalReady(w, r) {
		return
	}
	writeJSON(w, 200, s.opt.Terminals.List())
}
func (s *Server) handleTerminalCreate(w http.ResponseWriter, r *http.Request) {
	if !s.terminalReady(w, r) {
		return
	}
	var in struct {
		AgentID string `json:"agentId"`
		Device  string `json:"device"`
		Cols    int    `json:"cols"`
		Rows    int    `json:"rows"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Terminals.Create(r.Context(), r.PathValue("pid"), in.AgentID, in.Device, in.Cols, in.Rows)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 201, out)
}
func (s *Server) handleTerminalOutput(w http.ResponseWriter, r *http.Request) {
	if !s.terminalReady(w, r) {
		return
	}
	after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	if err != nil {
		s.fail(w, r, domain.ErrInvalid)
		return
	}
	out, err := s.opt.Terminals.Output(r.Context(), r.PathValue("pid"), r.PathValue("id"), after)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleTerminalInput(w http.ResponseWriter, r *http.Request) {
	if !s.terminalReady(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var in struct {
		Data string `json:"data"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.opt.Terminals.Input(r.PathValue("pid"), r.PathValue("id"), []byte(in.Data)); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"accepted": true})
}
func (s *Server) handleTerminalResize(w http.ResponseWriter, r *http.Request) {
	if !s.terminalReady(w, r) {
		return
	}
	var in struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.opt.Terminals.Resize(r.PathValue("pid"), r.PathValue("id"), in.Cols, in.Rows); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"resized": true})
}
func (s *Server) handleTerminalStop(w http.ResponseWriter, r *http.Request) {
	if !s.terminalReady(w, r) {
		return
	}
	if err := s.opt.Terminals.Stop(r.PathValue("pid"), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"stopped": true})
}

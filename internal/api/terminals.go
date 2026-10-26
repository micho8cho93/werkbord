package api

import (
	"net/http"
	"strconv"

	"werkbord/internal/domain"
	"werkbord/internal/netprivate"
	"werkbord/internal/phoneaccess"
	"werkbord/internal/terminal"
)

// Native terminals remain local-owner only unless that owner grants one phone one session.
func (s *Server) terminalReady(w http.ResponseWriter, r *http.Request) bool {
	entry, phone := r.Context().Value(phoneKey{}).(phoneaccess.Entry)
	if !s.opt.AuthRequired || s.opt.Terminals == nil || !s.opt.Terminals.Enabled() || (!phone && (netprivate.IsPrivateRequest(r.Context()) || !fromThisComputer(r))) {
		writeError(w, 403, "forbidden", "native terminals require an authorised execution device and session grant")
		return false
	}
	if r.Context().Err() != nil {
		writeError(w, 403, "forbidden", "connection permission ended")
		return false
	}
	if phone {
		valid := false
		if s.opt.Phones != nil {
			for _, p := range s.opt.Phones.List() {
				if p.ID == entry.ID {
					valid = true
				}
			}
		}
		if !valid {
			writeError(w, 403, "forbidden", forbiddenForPhone)
			return false
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	if id := r.PathValue("id"); phone && id != "" {
		entry, _ := r.Context().Value(phoneKey{}).(phoneaccess.Entry)
		if err := s.opt.Terminals.Access(r.Context(), r.PathValue("pid"), id, entry.ID); err != nil {
			s.fail(w, r, err)
			return false
		}
	}
	return true
}
func (s *Server) handleTerminalGrant(w http.ResponseWriter, r *http.Request) {
	if _, phone := r.Context().Value(phoneKey{}).(phoneaccess.Entry); phone {
		writeError(w, 403, "forbidden", forbiddenForPhone)
		return
	}
	if !s.terminalReady(w, r) {
		return
	}
	var in struct {
		PhoneID string `json:"phoneId"`
		Enabled bool   `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	found := false
	if s.opt.Phones != nil {
		for _, p := range s.opt.Phones.List() {
			if p.ID == in.PhoneID {
				found = true
			}
		}
	}
	if !found {
		s.fail(w, r, domain.ErrForbidden)
		return
	}
	if in.Enabled {
		if err := s.opt.Terminals.Access(r.Context(), r.PathValue("pid"), r.PathValue("id"), ""); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if err := s.opt.Terminals.Grant(r.PathValue("pid"), r.PathValue("id"), in.PhoneID, in.Enabled); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}
func (s *Server) terminalRoutes(m *http.ServeMux) {
	m.HandleFunc("PUT /api/projects/{pid}/terminals/{id}/phone", s.handleTerminalGrant)
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
	entry, _ := r.Context().Value(phoneKey{}).(phoneaccess.Entry)
	out := []terminal.Metadata{}
	for _, session := range s.opt.Terminals.List() {
		if entry.ID == "" || s.opt.Terminals.Access(r.Context(), session.ProjectID, session.ID, entry.ID) == nil {
			if entry.ID != "" {
				session.Phones = nil
			}
			out = append(out, session)
		}
	}
	writeJSON(w, 200, out)
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
	var out terminal.Output
	switch r.URL.Query().Get("wait") {
	case "":
		out, err = s.opt.Terminals.Output(r.Context(), r.PathValue("pid"), r.PathValue("id"), after)
	case "0":
		out, err = s.opt.Terminals.OutputNow(r.Context(), r.PathValue("pid"), r.PathValue("id"), after)
	default:
		err = domain.ErrInvalid
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !s.terminalReady(w, r) {
		return
	}
	if _, phone := r.Context().Value(phoneKey{}).(phoneaccess.Entry); phone {
		out.Metadata.Phones = nil
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleTerminalInput(w http.ResponseWriter, r *http.Request) {
	if !s.terminalReady(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var in struct {
		Data     string `json:"data"`
		Client   string `json:"client"`
		Sequence uint64 `json:"sequence"`
		Rendered uint64 `json:"rendered"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if !s.terminalReady(w, r) {
		return
	}
	var err error
	_, phone := r.Context().Value(phoneKey{}).(phoneaccess.Entry)
	if phone || in.Client != "" {
		err = s.opt.Terminals.InputOnce(r.PathValue("pid"), r.PathValue("id"), in.Client, in.Sequence, []byte(in.Data), in.Rendered)
	} else {
		err = s.opt.Terminals.Input(r.PathValue("pid"), r.PathValue("id"), []byte(in.Data))
	}
	if err != nil {
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
	if !s.terminalReady(w, r) {
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

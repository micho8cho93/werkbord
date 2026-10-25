package api

import (
	"devboard/internal/agentcatalog"
	"devboard/internal/domain"
	"net/http"
)

func (s *Server) handleSelectionSettings(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) {
		return
	}
	if r.Method == http.MethodPut {
		var in domain.SelectionSettings
		if err := decode(w, r, &in); err != nil {
			s.fail(w, r, err)
			return
		}
		out, err := s.opt.Settings.SetSelection(r.Context(), in)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	out, err := s.opt.Settings.Selection(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleAgentCatalog(w http.ResponseWriter, r *http.Request) {
	entries := map[string]agentcatalog.Entry{}
	for _, e := range agentcatalog.Entries() {
		entries[e.ID] = e
	}
	type item struct {
		domain.Agent
		Command     string               `json:"command"`
		Setup       string               `json:"setup"`
		Adapter     bool                 `json:"adapter"`
		Assistant   bool                 `json:"assistant"`
		Coding      bool                 `json:"coding"`
		Interactive interactiveCLIStatus `json:"interactive"`
	}
	out := []item{}
	if s.opt.Agents != nil {
		for _, a := range s.opt.Agents.Detect(r.Context()) {
			e := entries[a.ID]
			salie := a.ID == "claude-code" || a.ID == "codex" || a.ID == "gemini-cli" || a.ID == "opencode"
			out = append(out, item{Agent: a, Command: e.Command, Setup: e.Setup, Adapter: e.Adapter, Assistant: salie, Coding: true, Interactive: nativeInteractiveCLIStatus(a.SignIn)})
		}
	}
	writeJSON(w, 200, map[string]any{"agents": out})
}

// No native terminal launch transport exists in Phase 1. Fail closed independently of
// installed ACP runtimes, assistant providers, automatic selection or billing declarations.
type interactiveCLIStatus struct {
	Supported      bool   `json:"supported"`
	Authorized     bool   `json:"authorized"`
	Device         string `json:"device"`
	Authentication string `json:"authentication"`
}

func nativeInteractiveCLIStatus(signIn domain.SignIn) interactiveCLIStatus {
	status := interactiveCLIStatus{Device: "local", Authentication: "unknown"}
	if signIn == domain.SignedIn {
		status.Authentication = "verified"
	}
	if signIn == domain.SignedOut {
		status.Authentication = "signed_out"
	}
	return status
}

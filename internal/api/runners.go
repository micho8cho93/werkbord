package api

import (
	"encoding/json"
	"io"
	"net/http"

	"devboard/internal/domain"
	"devboard/internal/netprivate"
	"devboard/internal/runnerwire"
)

func (s *Server) handlePairRunner(w http.ResponseWriter, r *http.Request) {
	if s.opt.Distributed == nil || s.opt.Network == nil {
		writeError(w, 503, "unavailable", "runner pairing needs private network access")
		return
	}
	status := s.opt.Network.Status()
	if status.State != netprivate.StateConnected || status.URL == "" {
		writeError(w, 409, "network_offline", "Connect the private network in Settings before pairing a runner")
		return
	}
	var req struct {
		Projects   []string `json:"projects"`
		AllowClone bool     `json:"allowClone"`
	}
	if e := decode(w, r, &req); e != nil {
		s.fail(w, r, e)
		return
	}
	p, e := s.opt.Distributed.Pair(r.Context(), status.URL, req.Projects, req.AllowClone)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 201, p)
}
func (s *Server) handleManageRunner(w http.ResponseWriter, r *http.Request) {
	if s.opt.Distributed == nil {
		writeError(w, 503, "unavailable", "runner management unavailable")
		return
	}
	var req domain.Runner
	if r.Method != "DELETE" {
		if e := decode(w, r, &req); e != nil {
			s.fail(w, r, e)
			return
		}
	}
	out, e := s.opt.Distributed.Manage(r.Context(), r.PathValue("id"), req, r.Method == "DELETE")
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}

// This is the entire runner-facing surface. No runner key authenticates to the
// owner API, even with auth disabled on local loopback.
func (s *Server) runnerProtocol(w http.ResponseWriter, r *http.Request) {
	if s.opt.Distributed == nil || r.Method != "POST" {
		writeError(w, 404, "not_found", "not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	body, e := io.ReadAll(r.Body)
	if e != nil {
		writeError(w, 400, "invalid", "runner message too large")
		return
	}
	switch r.URL.Path {
	case "/api/runner/join":
		var in runnerwire.Join
		if json.Unmarshal(body, &in) != nil || !runnerwire.Verify(in.PublicKey, r.Header.Get("X-Runner-Signature"), body) {
			writeError(w, 401, "unauthorized", "invalid runner identity")
			return
		}
		out, e := s.opt.Distributed.Join(r.Context(), in)
		if e != nil {
			s.fail(w, r, e)
			return
		}
		writeJSON(w, 201, out)
	case "/api/runner/sync":
		out, e := s.opt.Distributed.Sync(r.Context(), body, r.Header.Get("X-Runner-Signature"))
		if e != nil {
			s.fail(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	}
}
func (s *Server) handleRoutingRules(w http.ResponseWriter, r *http.Request) {
	if s.opt.Distributed == nil {
		writeError(w, 503, "unavailable", "routing unavailable")
		return
	}
	if r.Method == "PUT" {
		var req struct {
			Rules []domain.RoutingRule `json:"rules"`
		}
		if e := decode(w, r, &req); e != nil {
			s.fail(w, r, e)
			return
		}
		if e := s.opt.Distributed.SetRules(r.Context(), req.Rules); e != nil {
			s.fail(w, r, e)
			return
		}
	}
	rules, e := s.opt.Distributed.Rules(r.Context())
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"rules": rules})
}
func (s *Server) handleRunUsage(w http.ResponseWriter, r *http.Request) {
	run, err := s.opt.Runs.GetIn(r.Context(), r.PathValue("pid"), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var usage domain.Usage
	if e := decode(w, r, &usage); e != nil {
		s.fail(w, r, e)
		return
	}
	out, e := s.opt.Runs.SetUsage(r.Context(), run.ID, usage)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleRunAssessment(w http.ResponseWriter, r *http.Request) {
	run, e := s.opt.Runs.GetIn(r.Context(), r.PathValue("pid"), r.PathValue("id"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	var in struct {
		Acceptance string `json:"acceptance"`
	}
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	out, e := s.opt.Runs.SetAcceptance(r.Context(), run.ID, in.Acceptance)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}

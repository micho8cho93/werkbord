package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"werkbord/internal/clientaccess"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/phoneaccess"
)

func (s *Server) deviceOperationRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/device/operations", s.handleDeviceOperation)
	m.HandleFunc("GET /api/device/operations/{id}", s.handleDeviceOperation)
	m.HandleFunc("GET /api/device/terminals", s.handleDeviceTerminals)
	m.HandleFunc("PUT /api/projects/{pid}/terminals/{id}/clients/{client}", s.handleClientTerminalGrant)
}
func (s *Server) deviceActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if entry, ok := r.Context().Value(clientKey{}).(clientaccess.Entry); ok && s.opt.ClientListener {
		return entry.ID, true
	}
	if !s.clientManagementReady(w, r) {
		return "", false
	}
	return "owner", true
}
func (s *Server) handleDeviceOperation(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.deviceActor(w, r)
	if !ok {
		return
	}
	if s.opt.DeviceOperations == nil {
		writeError(w, 503, "unavailable", "device operations unavailable")
		return
	}
	if r.Method == "GET" {
		out, err := s.opt.DeviceOperations.Get(r.Context(), actor, r.PathValue("id"))
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	var in devicewire.Request
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	// Terminal requests use the session-grant API, not an alternate generic door.
	if !strings.HasPrefix(in.Kind, "git.") {
		s.fail(w, r, domain.ErrForbidden)
		return
	}
	out, err := s.opt.DeviceOperations.Submit(r.Context(), actor, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 202, out)
}
func (s *Server) allDeviceTerminals(ctx context.Context) ([]devicewire.Terminal, error) {
	out := []devicewire.Terminal{}
	if s.opt.Terminals != nil {
		out = append(out, s.opt.Terminals.List()...)
	}
	if s.opt.DeviceOperations != nil {
		remote, err := s.opt.DeviceOperations.RemoteTerminals(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, remote...)
	}
	return out, nil
}
func (s *Server) handleDeviceTerminals(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.deviceActor(w, r)
	if !ok {
		return
	}
	if s.opt.DeviceOperations == nil {
		s.fail(w, r, domain.ErrForbidden)
		return
	}
	terminals, err := s.allDeviceTerminals(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := []devicewire.Terminal{}
	for _, meta := range terminals {
		if s.opt.DeviceOperations.TerminalAllowed(r.Context(), actor, meta.Device, meta.ProjectID, meta.ID) {
			if actor != "owner" {
				meta.Phones = nil
			}
			out = append(out, meta)
		}
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleClientTerminalGrant(w http.ResponseWriter, r *http.Request) {
	if !s.clientManagementReady(w, r) {
		return
	}
	if s.opt.DeviceOperations == nil || s.opt.Clients == nil {
		s.fail(w, r, domain.ErrForbidden)
		return
	}
	found := false
	for _, entry := range s.opt.Clients.List() {
		if entry.ID == r.PathValue("client") {
			found = true
		}
	}
	if !found {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	var in struct {
		RunnerID string `json:"runnerId"`
		Enabled  bool   `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	sessions, err := s.allDeviceTerminals(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	found = false
	for _, meta := range sessions {
		if meta.ID == r.PathValue("id") && meta.Device == in.RunnerID && meta.ProjectID == r.PathValue("pid") {
			found = true
		}
	}
	if !found {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	if err = s.opt.DeviceOperations.GrantTerminal(r.Context(), in.RunnerID, r.PathValue("pid"), r.PathValue("id"), r.PathValue("client"), in.Enabled); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}

// deviceTerminal adapts existing terminal endpoints. Phone grants retain their
// original local-only path and semantics; desktop grants never inherit them.
func (s *Server) deviceTerminal(w http.ResponseWriter, r *http.Request) bool {
	if s.opt.DeviceOperations == nil {
		return false
	}
	if _, phone := r.Context().Value(phoneKey{}).(phoneaccess.Entry); phone {
		return false
	}
	if r.Method == "GET" && r.URL.Path == "/api/terminals" {
		s.handleDeviceTerminals(w, r)
		return true
	}
	actor, ok := s.deviceActor(w, r)
	if !ok {
		return true
	}
	request := devicewire.Request{ProjectID: r.PathValue("pid"), SessionID: r.PathValue("id"), RequestID: domain.NewID("request")}
	if r.Method == "POST" && request.SessionID == "" {
		var in struct {
			AgentID   string `json:"agentId"`
			Device    string `json:"device"`
			Cols      int    `json:"cols"`
			Rows      int    `json:"rows"`
			RequestID string `json:"requestId"`
		}
		if err := decode(w, r, &in); err != nil {
			s.fail(w, r, err)
			return true
		}
		request.Kind = "terminal.create"
		request.AgentID = in.AgentID
		request.RunnerID = in.Device
		request.Cols = in.Cols
		request.Rows = in.Rows
		if in.RequestID != "" {
			request.RequestID = in.RequestID
		}
	} else {
		sessions, err := s.allDeviceTerminals(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return true
		}
		found := false
		for _, meta := range sessions {
			if meta.ID == request.SessionID && meta.ProjectID == request.ProjectID {
				request.RunnerID = meta.Device
				found = true
				break
			}
		}
		if !found {
			s.fail(w, r, domain.ErrNotFound)
			return true
		}
		// Preserve the owner's existing immediate/long-poll local terminal interface.
		if actor == "owner" && s.opt.Terminals != nil && request.RunnerID == s.opt.Terminals.Device() {
			return false
		}
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/output"):
			after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
			if err != nil {
				s.fail(w, r, domain.ErrInvalid)
				return true
			}
			if wait := r.URL.Query().Get("wait"); wait != "" && wait != "0" {
				s.fail(w, r, domain.ErrInvalid)
				return true
			}
			request.Kind = "terminal.output"
			request.After = after
		case strings.HasSuffix(r.URL.Path, "/input"):
			var in struct {
				Data     string `json:"data"`
				Client   string `json:"client"`
				Sequence uint64 `json:"sequence"`
				Rendered uint64 `json:"rendered"`
			}
			if err := decode(w, r, &in); err != nil {
				s.fail(w, r, err)
				return true
			}
			request.Kind = "terminal.input"
			request.Data = in.Data
			request.Client = in.Client
			request.Sequence = in.Sequence
			request.Rendered = in.Rendered
			// Input retries receive the same durable operation identity. Reusing a
			// sequence with different bytes is a conflicting request, never new input.
			sum := sha256.Sum256([]byte(request.SessionID + ":" + in.Client + ":" + strconv.FormatUint(in.Sequence, 10)))
			request.RequestID = hex.EncodeToString(sum[:])
		case strings.HasSuffix(r.URL.Path, "/resize"):
			var in struct {
				Cols int `json:"cols"`
				Rows int `json:"rows"`
			}
			if err := decode(w, r, &in); err != nil {
				s.fail(w, r, err)
				return true
			}
			request.Kind = "terminal.resize"
			request.Cols = in.Cols
			request.Rows = in.Rows
		case strings.HasSuffix(r.URL.Path, "/stop"):
			request.Kind = "terminal.stop"
		default:
			s.fail(w, r, domain.ErrForbidden)
			return true
		}
	}
	queued, err := s.opt.DeviceOperations.Submit(r.Context(), actor, request)
	if err != nil {
		s.fail(w, r, err)
		return true
	}
	settled, err := s.opt.DeviceOperations.Wait(r.Context(), actor, queued.Operation.ID)
	if err != nil {
		s.fail(w, r, err)
		return true
	}
	result := settled.Result
	if result.State != "done" {
		writeJSON(w, 409, map[string]any{"error": map[string]string{"code": "device_operation_" + result.State, "message": "Check the operation outcome before retrying: " + result.Error}, "operationId": result.ID})
		return true
	}
	switch request.Kind {
	case "terminal.create":
		writeJSON(w, 201, result.Terminal)
	case "terminal.output":
		writeJSON(w, 200, result.Output)
	default:
		writeJSON(w, 200, map[string]bool{"accepted": true})
	}
	return true
}

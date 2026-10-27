package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"werkbord/internal/clientaccess"
	"werkbord/internal/clientpolicy"
	"werkbord/internal/netprivate"
)

type clientKey struct{}

func clientRequest(r *http.Request) bool {
	_, ok := r.Context().Value(clientKey{}).(clientaccess.Entry)
	return ok
}
func (s *Server) clientManagementReady(w http.ResponseWriter, r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	want := s.currentToken()
	if s.opt.ClientListener || !s.opt.AuthRequired || !fromThisComputer(r) || netprivate.IsPrivateRequest(r.Context()) || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		writeError(w, 403, "forbidden", "device administration requires the local controller owner")
		return false
	}
	return true
}
func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	if !s.clientManagementReady(w, r) {
		return
	}
	if s.opt.Clients == nil {
		writeError(w, 503, "unavailable", "desktop pairing unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case "GET":
		writeJSON(w, 200, map[string]any{"clients": s.opt.Clients.List()})
	case "DELETE":
		if err := s.opt.Clients.Revoke(r.PathValue("id")); err != nil {
			s.fail(w, r, err)
			return
		}
		w.WriteHeader(204)
	case "POST":
		if s.opt.Network == nil || s.opt.Network.Status().State != netprivate.StateConnected {
			writeError(w, 409, "network_offline", "connect this controller to Tailscale first")
			return
		}
		code, expires, err := s.opt.Clients.NewCode()
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, 201, map[string]any{"code": code, "expiresAt": expires, "controller": s.opt.Network.Status().URL})
	}
}
func (s *Server) handleClientEnroll(w http.ResponseWriter, r *http.Request) {
	if !s.opt.ClientListener || s.opt.Clients == nil {
		writeError(w, 403, "forbidden", "desktop enrollment requires the private client listener")
		return
	}
	if ok, wait := s.enrollLimit.allow(); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, 429, "rate_limited", "wait before trying another code")
		return
	}
	var in struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		DeviceID string `json:"deviceId"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	e, token, err := s.opt.Clients.Enroll(in.Code, in.Name, in.DeviceID)
	if err != nil {
		writeError(w, 403, "forbidden", "desktop pairing refused; check the name and create a fresh code on the controller")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, struct {
		clientaccess.Entry
		Token string `json:"token"`
	}{e, token})
}
func (s *Server) clientAuthenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/client/v1/enroll" {
			s.handleClientEnroll(w, r)
			return
		}
		if !clientpolicy.Allowed(r) || s.opt.Clients == nil {
			writeError(w, 404, "not_found", "this desktop capability does not permit that operation")
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		e, ok := s.opt.Clients.Authenticate(token)
		if !ok {
			writeError(w, 401, "unauthorized", "desktop credential missing or revoked")
			return
		}
		ctx, cancel := context.WithCancel(context.WithValue(r.Context(), clientKey{}, e))
		defer cancel()
		revoked, stop := s.opt.Clients.Watch(e.ID)
		defer stop()
		go func() {
			select {
			case <-revoked:
				cancel()
			case <-ctx.Done():
			}
		}()
		r = r.WithContext(ctx)
		w.Header().Set("Cache-Control", "no-store")
		select {
		case <-ctx.Done():
			writeError(w, 401, "unauthorized", "desktop access revoked")
			return
		default:
		}
		switch r.URL.Path {
		case "/api/client/v1/self":
			writeJSON(w, 200, e)
		case "/api/client/v1/capabilities":
			writeJSON(w, 200, map[string]any{"version": 1, "workspaceControl": true, "gitWrites": false, "terminals": false, "assistant": false, "personal": false, "deviceAdministration": false, "routes": clientpolicy.Routes})
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func (s *Server) handleDeviceSwitch(w http.ResponseWriter, r *http.Request) {
	if !s.clientManagementReady(w, r) {
		return
	}
	if s.opt.PrepareDeviceSwitch == nil || s.opt.ResumeDeviceSwitch == nil {
		writeError(w, 503, "unavailable", "device switching unavailable")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/resume") {
		s.opt.ResumeDeviceSwitch()
	} else if err := s.opt.PrepareDeviceSwitch(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ready": true})
}

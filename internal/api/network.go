package api

import (
	"context"
	"net/http"

	"devboard/internal/netprivate"
)

// NetworkController is what the API needs from the private network.
type NetworkController interface {
	// Status is the network's current state.
	Status() netprivate.Status
	// Enable starts the network, remembering that the user wants it, and returns
	// at once: signing in and connecting happen in the background and show in Status.
	Enable(ctx context.Context) error
	// Disable stops it and remembers that.
	Disable(ctx context.Context) error
	// Choice says who decided whether it is on: "on" or "off" (the user, in the
	// app), "unset" (nobody yet), or "pinned_on" / "pinned_off" (config.json).
	Choice(ctx context.Context) string
}

// networkBody is the status with the choice, so a client can tell "off because
// nobody asked" from "off because the user turned it off".
type networkBody struct {
	netprivate.Status
	Choice string `json:"choice"`
}

func (s *Server) networkStatus(ctx context.Context) networkBody {
	return networkBody{Status: s.opt.Network.Status(), Choice: s.opt.Network.Choice(ctx)}
}

func (s *Server) networkReady(w http.ResponseWriter) bool {
	if s.opt.Network == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the private network is not available")
		return false
	}
	return true
}

// handleNetworkStatus reports the private network. It never contains the access token.
func (s *Server) handleNetworkStatus(w http.ResponseWriter, r *http.Request) {
	if !s.networkReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.networkStatus(r.Context()))
}

func (s *Server) handleNetworkEnable(w http.ResponseWriter, r *http.Request) {
	if !s.networkReady(w) {
		return
	}
	if err := s.opt.Network.Enable(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.networkStatus(r.Context()))
}

func (s *Server) handleNetworkDisable(w http.ResponseWriter, r *http.Request) {
	if !s.networkReady(w) {
		return
	}
	if err := s.opt.Network.Disable(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.networkStatus(r.Context()))
}

// handlePhoneLink returns where a phone reaches the controller: its private address. It carries no credential. A phone
// is let in with an enrollment code, made by POST /api/phones/codes (phone.go), never with the controller's own token.
func (s *Server) handlePhoneLink(w http.ResponseWriter, r *http.Request) {
	if !s.networkReady(w) {
		return
	}
	st := s.opt.Network.Status()
	if st.State != netprivate.StateConnected || st.URL == "" {
		writeError(w, http.StatusConflict, "conflict", "the private network is not connected yet")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": st.URL, "https": st.HTTPS})
}

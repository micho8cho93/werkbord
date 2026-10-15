package api

import "net/http"

// RelayController is what the API needs from the relay connection (internal/controller/relay.go).
type RelayController interface {
	Status() RelayStatus
	// Link makes the address a phone opens to pair: the connector page, with a pairing for code in its fragment.
	Link(code string) (string, error)
}

// RelayStatus is the relay as the app shows it. It holds no key and no secret.
type RelayStatus struct {
	Enabled        bool   `json:"enabled"`
	Connected      bool   `json:"connected"`
	Phones         int    `json:"phones"`
	ConnectorURL   string `json:"connectorUrl,omitempty"`
	Error          string `json:"error,omitempty"`
	RetryInSeconds int    `json:"retryInSeconds,omitempty"`
}

// handleRelayStatus reports the relay. It is the owner's: a phone's screens have no use for it.
func (s *Server) handleRelayStatus(w http.ResponseWriter, r *http.Request) {
	if s.opt.Relay == nil {
		writeJSON(w, http.StatusOK, RelayStatus{})
		return
	}
	writeJSON(w, http.StatusOK, s.opt.Relay.Status())
}

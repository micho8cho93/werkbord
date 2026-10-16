package api

import (
	"net/http"

	"devboard/internal/phoneaccess"
	"devboard/internal/service"
)

func deckOwner(r *http.Request) string {
	if phone, ok := r.Context().Value(phoneKey{}).(phoneaccess.Entry); ok {
		return "phone:" + phone.ID
	}
	return "owner"
}
func (s *Server) handleDeck(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) {
		return
	}
	owner := deckOwner(r)
	if r.Method == http.MethodPut {
		// Decode into a slice so JSON arrays longer/shorter than nine are rejected,
		// rather than silently truncated by encoding/json's fixed-array decoding.
		var req struct {
			Version int64               `json:"version"`
			Slots   []*service.DeckSlot `json:"slots"`
		}
		if err := decode(w, r, &req); err != nil {
			s.fail(w, r, err)
			return
		}
		if len(req.Slots) != 9 {
			writeError(w, 400, "invalid", "a deck has exactly nine positions, including empty slots")
			return
		}
		in := service.DeckLayout{Version: req.Version}
		copy(in.Slots[:], req.Slots)
		out, err := s.opt.Settings.SetDeck(r.Context(), owner, in)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	out, err := s.opt.Settings.Deck(r.Context(), owner)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleDeckHistory(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) {
		return
	}
	out, err := s.opt.Settings.DeckHistory(r.Context(), deckOwner(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleDeckWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.settingsReady(w) || !s.runnerReady(w) {
		return
	}
	var req struct {
		SlotID    string `json:"slotId"`
		RequestID string `json:"requestId"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	task, run, err := s.opt.Runner.StartDeckWorkflow(r.Context(), s.opt.Settings, deckOwner(r), req.SlotID, req.RequestID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"task": task, "run": run})
}

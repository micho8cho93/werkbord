package api

import (
	"net/http"

	"werkbord/internal/domain"
	"werkbord/internal/phoneaccess"
	"werkbord/internal/service"
)

// Suggested next steps for a project. A suggestion is advice with its evidence: it carries ways to open screens that already
// exist and ask the person themselves, and nothing here starts, merges, pushes or approves anything.
//
// On a connection that does not serve memory (the private network, used by an enrolled phone), and for a phone wherever it
// connects from, the answer is the same with the words of saved memory taken out, and feedback is not recorded in memory: the
// phone keeps its own dismissals on the device.

type recommendationsResponse struct {
	domain.RecommendationSet
	// FeedbackStoredIn says where a verdict on a suggestion is kept: "memory" (this person's own memory on this computer)
	// or "device" (only on the screen that gave it).
	FeedbackStoredIn string `json:"feedbackStoredIn"`
}

func (s *Server) recommendReady(w http.ResponseWriter) bool {
	if s.opt.Recommender == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "suggestions are not enabled")
		return false
	}
	return true
}

func (s *Server) handleRecommendations(w http.ResponseWriter, r *http.Request) {
	if !s.recommendReady(w) {
		return
	}
	kind := "owner"
	if _, ok := r.Context().Value(phoneKey{}).(phoneaccess.Entry); ok {
		kind = "phone"
	}
	// A connection that does not serve memory, and a phone wherever it connects from, get the answer without the words of saved
	// memory and keep their verdicts on the device: the phone is the owner's remote control, not a place memory is read or written.
	withheld := s.opt.Memory == nil || kind == "phone"
	set, err := s.opt.Recommender.Recommend(r.Context(), service.RecommendInput{
		ProjectID: r.PathValue("pid"), Focus: r.URL.Query().Get("focus"), PersonalMemory: true, Redact: withheld,
		// The person at the controller, or their phone: everything the application lets them read.
		Viewer: domain.RecViewer{Kind: kind, Tasks: true, Runs: true, Repo: true, Memory: true},
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := recommendationsResponse{RecommendationSet: *set, FeedbackStoredIn: "memory"}
	if withheld {
		out.FeedbackStoredIn = "device"
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleRecommendationFeedback(w http.ResponseWriter, r *http.Request) {
	if !s.recommendReady(w) || !s.memoryReady(w) {
		return
	}
	var in service.FeedbackInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	in.ProjectID = r.PathValue("pid")
	out, err := s.opt.Recommender.Feedback(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"feedback": out})
}

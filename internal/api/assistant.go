package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"devboard/internal/appops"
	"devboard/internal/assistant"
)

// The assistant API. It belongs to the person at this computer and nobody else: it takes the controller's own token
// and nothing narrower (a local access token is refused here like on every route outside its list, and there is a test
// that says so), and it is not served on the private network. It starts a conversation with a coding agent the person
// has signed in on this computer and lets that conversation look at the board and propose changes. A proposed change is
// carried out only by POST .../actions/{id} with {"decision":"approve"}, which is this API's way of the person saying yes.

func (s *Server) assistantReady(w http.ResponseWriter) bool {
	if s.opt.Assistant == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the assistant is not available here")
		return false
	}
	return true
}

// assistantFail maps an operation's or the engine's error to a response.
func (s *Server) assistantFail(w http.ResponseWriter, r *http.Request, err error) {
	var op *appops.Error
	if errors.As(err, &op) {
		status := http.StatusInternalServerError
		switch op.Code {
		case appops.CodeNotFound:
			status = http.StatusNotFound
		case appops.CodeConflict, appops.CodeNotPending, appops.CodeTooManyPending:
			status = http.StatusConflict
		case appops.CodeExpired:
			status = http.StatusGone
		case appops.CodeInvalidArguments, appops.CodeUnknownOperation:
			status = http.StatusBadRequest
		case appops.CodePermissionDenied, appops.CodeRefused, appops.CodeApprovalIsPersons:
			status = http.StatusForbidden
		case appops.CodeTooLarge:
			status = http.StatusRequestEntityTooLarge
		case appops.CodeAuditUnavailable, appops.CodeUnavailable:
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, op.Code, op.Message)
		return
	}
	s.fail(w, r, err)
}

func (s *Server) handleAssistantProviders(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": s.opt.Assistant.Providers(r.Context())})
}

func (s *Server) handleAssistantSessions(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	list, err := s.opt.Assistant.Sessions(r.Context())
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (s *Server) handleAssistantCreateSession(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	var req struct {
		Provider  string `json:"provider"`
		Model     string `json:"model"`
		Reasoning string `json:"reasoning"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	v, err := s.opt.Assistant.CreateSession(r.Context(), assistant.CreateRequest{Provider: req.Provider, Model: req.Model, Reasoning: req.Reasoning})
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleAssistantGetSession(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	v, err := s.opt.Assistant.Session(r.Context(), r.PathValue("id"))
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleAssistantConfigure(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	var req struct {
		Provider  *string `json:"provider"`
		Model     *string `json:"model"`
		Reasoning *string `json:"reasoning"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	cur, err := s.opt.Assistant.Session(r.Context(), r.PathValue("id"))
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	providerID, model, reasoning := cur.Provider, cur.Model, cur.Reasoning
	if req.Provider != nil && *req.Provider != cur.Provider {
		// A model and a level belong to a provider: the old ones do not carry over to another.
		providerID, model, reasoning = *req.Provider, "", ""
	}
	if req.Model != nil {
		model = *req.Model
	}
	if req.Reasoning != nil {
		reasoning = *req.Reasoning
	}
	v, err := s.opt.Assistant.Configure(r.Context(), cur.ID, providerID, model, reasoning)
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleAssistantDeleteSession(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	if err := s.opt.Assistant.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
		s.assistantFail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAssistantSend(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	turn, err := s.opt.Assistant.Send(r.Context(), r.PathValue("id"), req.Text)
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	// The reply is read from the event stream; the turn does not depend on this request staying open.
	writeJSON(w, http.StatusAccepted, map[string]any{"turnId": turn})
}

func (s *Server) handleAssistantCancel(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	ok, err := s.opt.Assistant.Cancel(r.PathValue("id"))
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": ok})
}

func (s *Server) handleAssistantResolve(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	var req struct {
		Decision string `json:"decision"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Decision != "approve" && req.Decision != "decline" {
		writeError(w, http.StatusBadRequest, "invalid", `decision must be "approve" or "decline"`)
		return
	}
	res, err := s.opt.Assistant.Resolve(r.Context(), r.PathValue("id"), r.PathValue("aid"), req.Decision == "approve")
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAssistantAudit(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	q := r.URL.Query()
	before, limit := int64(0), 100
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid", "before must be a number")
			return
		}
		before = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "invalid", "limit must be 1 to 500")
			return
		}
		limit = n
	}
	entries, err := s.opt.Assistant.Audit(r.Context(), q.Get("session"), before, limit)
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) handleAssistantAuditVerify(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	rep, err := s.opt.Assistant.VerifyAudit(r.Context())
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleAssistantEvents streams a conversation's events as Server-Sent Events. A client resumes with Last-Event-ID (or
// ?after=): the turn it was watching went on without it, and it is sent what it missed. Unlike /api/events, the
// credential is only taken from the Authorization header (never from the URL), so the stream is read with fetch.
func (s *Server) handleAssistantEvents(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	ctx := r.Context()
	after, _, err := resumePoint(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	credential := s.currentToken()
	credentialValid := func() bool { return !s.opt.AuthRequired || credential == s.currentToken() && credential != "" }
	ch, err := s.opt.Assistant.Subscribe(ctx, r.PathValue("id"), after)
	if err != nil {
		s.assistantFail(w, r, err)
		return
	}
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	_ = rc.Flush()
	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return // dropped for falling behind: the client reconnects from the last event it saw
			}
			if !credentialValid() {
				return
			}
			b, err := json.Marshal(e)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, b); err != nil || rc.Flush() != nil {
				return
			}
		case <-heartbeat.C:
			if !credentialValid() {
				return
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

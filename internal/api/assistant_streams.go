package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"devboard/internal/assistant"
	"devboard/internal/domain"
)

// One HTTP stream carries independent, session-scoped subscriptions. This avoids the
// browser's HTTP/1 connection limit without storing transcripts or sharing principals.
func (s *Server) handleAssistantStreams(w http.ResponseWriter, r *http.Request) {
	if !s.assistantReady(w) {
		return
	}
	var in struct {
		Sessions []struct {
			ID    string `json:"id"`
			After int64  `json:"after"`
		} `json:"sessions"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(in.Sessions) == 0 || len(in.Sessions) > 128 {
		writeError(w, 400, "invalid", "choose between 1 and 128 conversations")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	type subscription struct {
		view   *assistant.SessionView
		id     string
		events <-chan assistant.Event
	}
	subs := []subscription{}
	seen := map[string]bool{}
	for _, cursor := range in.Sessions {
		if cursor.After < 0 || seen[cursor.ID] {
			writeError(w, 400, "invalid", "invalid or duplicate conversation cursor")
			return
		}
		seen[cursor.ID] = true
		view, err := s.opt.Assistant.Session(ctx, cursor.ID)
		if errors.Is(err, domain.ErrNotFound) {
			subs = append(subs, subscription{id: cursor.ID})
			continue
		}
		if err != nil {
			s.assistantFail(w, r, err)
			return
		}
		after := cursor.After
		if after > view.LastEvent {
			after = 0
		}
		events, err := s.opt.Assistant.Subscribe(ctx, cursor.ID, after)
		if err != nil {
			s.assistantFail(w, r, err)
			return
		}
		subs = append(subs, subscription{view: view, id: cursor.ID, events: events})
	}
	credential := s.currentToken()
	valid := func() bool { return !s.opt.AuthRequired || credential != "" && credential == s.currentToken() }
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(value any) bool {
		if !valid() {
			return false
		}
		b, err := json.Marshal(value)
		if err != nil {
			return false
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", b)
		return err == nil && rc.Flush() == nil
	}
	// Reconcile each cursor and pending confirmation before replaying that session's events.
	for _, sub := range subs {
		kind := "session_snapshot"
		if sub.view == nil {
			kind = "session_missing"
		}
		if !write(struct {
			Type      string                 `json:"type"`
			SessionID string                 `json:"sessionId"`
			Session   *assistant.SessionView `json:"session,omitempty"`
		}{kind, sub.id, sub.view}) {
			return
		}
	}
	type delivery struct {
		event assistant.Event
		ok    bool
	}
	joined := make(chan delivery, 256)
	for _, sub := range subs {
		if sub.events == nil {
			continue
		}
		go func(events <-chan assistant.Event) {
			for {
				select {
				case <-ctx.Done():
					return
				case e, ok := <-events:
					select {
					case joined <- delivery{e, ok}:
					case <-ctx.Done():
						return
					}
					if !ok {
						return
					}
				}
			}
		}(sub.events)
	}
	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case delivery := <-joined:
			if !delivery.ok || !write(delivery.event) {
				return
			}
		case <-heartbeat.C:
			if !valid() {
				return
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

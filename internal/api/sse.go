package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

const (
	sseHeartbeat  = 25 * time.Second
	sseReplayPage = 500
	sseBuffer     = 256
)

// handleEvents streams the event log as Server-Sent Events.
//
// A client resumes by sending Last-Event-ID (EventSource does this
// automatically on reconnect) or ?after=<seq>. Without either, the stream
// starts at the current end of the log.
//
// The event log, not the in-memory broker, is the source of truth: live
// events are only used as a signal. Whenever a live event is not the next
// expected Seq (the broker does not guarantee order across concurrent
// writers), the gap is filled from the database. A subscriber that falls too
// far behind is disconnected and catches up by reconnecting.
//
// With ?project=<id> the stream carries only that project's events: the filter
// is applied here, before anything is written, so a client that asks for one
// project is never sent another's. The sequence numbers keep their gaps, and a
// resume point (an id the client saw) is still valid.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rc := http.NewResponseController(w)

	project := r.URL.Query().Get("project")
	if project != "" {
		if _, err := s.opt.Projects.Get(ctx, project); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	write := func(e domain.Event) error {
		if project != "" && e.ProjectID != project {
			return nil
		}
		return writeEvent(w, e)
	}

	after, explicit, err := resumePoint(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}

	// Subscribe before reading the log so nothing committed in between is missed.
	sub := s.opt.Events.Subscribe(sseBuffer)
	defer sub.Close()

	if !explicit {
		if err := s.opt.Store.View(ctx, func(tx store.Tx) error {
			after, err = tx.Events().LatestSeq(ctx)
			return err
		}); err != nil {
			s.fail(w, r, err)
			return
		}
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}

	catchUp := func() error {
		for {
			var page []domain.Event
			if err := s.opt.Store.View(ctx, func(tx store.Tx) error {
				var err error
				page, err = tx.Events().ListAfter(ctx, after, sseReplayPage)
				return err
			}); err != nil {
				return err
			}
			for _, e := range page {
				if err := write(e); err != nil {
					return err
				}
				after = e.Seq
			}
			if len(page) < sseReplayPage {
				return rc.Flush()
			}
		}
	}

	if err := catchUp(); err != nil {
		return
	}

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-sub.C:
			if !ok {
				return // dropped for falling behind, or controller shutting down
			}
			switch {
			case e.Seq <= after:
				continue
			case e.Seq == after+1:
				if write(e) != nil || rc.Flush() != nil {
					return
				}
				after = e.Seq
			default:
				if catchUp() != nil {
					return
				}
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

func resumePoint(r *http.Request) (after int64, explicit bool, err error) {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("after")
	}
	if v == "" {
		return 0, false, nil
	}
	after, err = strconv.ParseInt(v, 10, 64)
	if err != nil || after < 0 {
		return 0, false, fmt.Errorf("invalid resume point %q", v)
	}
	return after, true, nil
}

func writeEvent(w http.ResponseWriter, e domain.Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, b)
	return err
}

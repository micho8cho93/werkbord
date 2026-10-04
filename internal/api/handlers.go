package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"devboard/internal/domain"
	"devboard/internal/service"
)

const maxBodyBytes = 1 << 20

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var b errorBody
	b.Error.Code, b.Error.Message = code, msg
	writeJSON(w, status, b)
}

// fail maps domain errors to HTTP responses. Unexpected errors are logged and
// reported generically so internals do not leak to clients.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domain.ErrDuplicate):
		writeError(w, http.StatusConflict, "duplicate", err.Error())
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrTransition):
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
	case errors.Is(err, context.Canceled):
		// Client went away; nothing useful to send.
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: request body: %v", domain.ErrInvalid, err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("%w: request body must be a single JSON object", domain.ErrInvalid)
	}
	return nil
}

type healthResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	Database      string `json:"database"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{Status: "ok", Version: s.opt.Version, UptimeSeconds: int64(time.Since(s.started).Seconds()), Database: "ok"}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.opt.Store.Ping(ctx); err != nil {
		s.log.Warn("health check: database unavailable", "err", err)
		resp.Status, resp.Database = "degraded", "unavailable"
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.opt.Projects.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

func (s *Server) handleRegisterProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.opt.Projects.Register(r.Context(), req.Path, req.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.opt.Projects.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleRefreshProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.opt.Projects.Refresh(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	ts, err := s.opt.Tasks.List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": ts})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.opt.Tasks.Create(r.Context(), r.PathValue("id"), req.Title, req.Description)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       *string  `json:"title"`
		Description *string  `json:"description"`
		State       *string  `json:"state"`
		Position    *float64 `json:"position"`
		Version     *int64   `json:"version"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Version == nil {
		s.fail(w, r, fmt.Errorf("%w: version is required", domain.ErrInvalid))
		return
	}
	patch := service.TaskPatch{Title: req.Title, Description: req.Description, Position: req.Position, Version: *req.Version}
	if req.State != nil {
		st, err := domain.ParseTaskState(*req.State)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		patch.State = &st
	}
	t, err := s.opt.Tasks.Update(r.Context(), r.PathValue("id"), patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.opt.Runs.ListActive(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleListQuestions(w http.ResponseWriter, r *http.Request) {
	qs, err := s.opt.Runs.ListPendingQuestions(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": qs})
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents := []domain.Agent{}
	if s.opt.Agents != nil {
		agents = s.opt.Agents.Detect(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

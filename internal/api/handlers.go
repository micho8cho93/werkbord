package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"devboard/internal/domain"
	"devboard/internal/runner"
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
	case errors.Is(err, domain.ErrAgent):
		// The message says why the agent could not start, which is what the user needs.
		writeError(w, http.StatusBadGateway, "agent_failed", err.Error())
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

func (s *Server) handleListTaskRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.opt.Runs.ListByTask(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleListProjectRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.opt.Runs.ListLatestByProject(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// handleGetWorktree says where a run's work is: its branch and directory.
func (s *Server) handleGetWorktree(w http.ResponseWriter, r *http.Request) {
	if s.opt.Worktrees == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "worktrees are not enabled")
		return
	}
	wt, err := s.opt.Worktrees.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, wt)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.opt.Runs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// handleStartRun starts an agent on a task. The response comes once the agent
// is running or has failed to start; the session itself does not depend on this
// request from then on.
func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) {
		return
	}
	var req struct {
		AgentID      string `json:"agentId"`
		Instructions string `json:"instructions"`
		Resume       bool   `json:"resume"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	run, err := s.opt.Runner.Start(r.Context(), runner.StartInput{
		TaskID: r.PathValue("id"), AgentID: req.AgentID, Instructions: req.Instructions, Resume: req.Resume,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

// handleRunEvents returns a run's activity: the newest `limit` events (default
// 200) before `before`, oldest first, and whether older ones exist.
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r, "limit", 200, 1, 1000)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := intParam(r, "before", 0, 0, 1<<62)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	evs, err := s.opt.Runs.Events(r.Context(), r.PathValue("id"), int64(before), limit+1)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	more := len(evs) > limit
	if more {
		evs = evs[len(evs)-limit:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs, "hasMore": more})
}

func intParam(r *http.Request, name string, def, min, max int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%w: %s must be a number from %d to %d", domain.ErrInvalid, name, min, max)
	}
	return n, nil
}

func (s *Server) handleRunInput(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	id := r.PathValue("id")
	if err := s.opt.Runner.Send(r.Context(), id, req.Text); err != nil {
		s.fail(w, r, err)
		return
	}
	s.respondWithRun(w, r, id)
}

func (s *Server) handleFinishRun(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) {
		return
	}
	run, err := s.opt.Runner.Finish(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) {
		return
	}
	run, err := s.opt.Runner.Stop(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	if !s.runnerReady(w) {
		return
	}
	var req struct {
		Answer string `json:"answer"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	id := r.PathValue("id")
	if err := s.opt.Runner.Answer(r.Context(), id, req.Answer); err != nil {
		s.fail(w, r, err)
		return
	}
	q, err := s.opt.Runs.GetQuestion(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// runnerReady reports whether agent execution is wired in, and answers the
// request if it is not.
func (s *Server) runnerReady(w http.ResponseWriter) bool {
	if s.opt.Runner == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "agent execution is not enabled")
		return false
	}
	return true
}

func (s *Server) respondWithRun(w http.ResponseWriter, r *http.Request, id string) {
	run, err := s.opt.Runs.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
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

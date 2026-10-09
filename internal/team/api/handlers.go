package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"devboard/internal/envelope"
	"devboard/internal/httpkit"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"status": "ok", "product": "werkbord-team", "version": s.opt.Version, "uptimeSeconds": int64(time.Since(s.started).Seconds()), "database": "ok"}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.opt.Ping != nil {
		if err := s.opt.Ping(ctx); err != nil {
			s.log.Warn("health check: database unavailable", "err", err)
			resp["status"], resp["database"] = "degraded", "unavailable"
			resp["storage"] = s.opt.Service.StorageBriefStatus(ctx)
			httpkit.WriteJSON(w, http.StatusServiceUnavailable, resp)
			return
		}
	}
	// A workspace that has lost its quorum still answers, and says it is read-only: it can be read and cannot be written.
	brief := s.opt.Service.StorageBriefStatus(ctx)
	resp["storage"] = brief
	if brief.ReadOnly {
		resp["status"] = "read_only"
	}
	httpkit.WriteJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	httpkit.WriteJSON(w, http.StatusOK, s.opt.Service.Me(actorOf(r)))
}

type roleInfo struct {
	Role        domain.Role         `json:"role"`
	Permissions []domain.Permission `json:"permissions"`
}

func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	out := []roleInfo{}
	for _, role := range domain.Roles() {
		out = append(out, roleInfo{Role: role, Permissions: role.Permissions()})
	}
	httpkit.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	httpkit.WriteJSON(w, http.StatusOK, actorOf(r).Workspace)
}

func (s *Server) handleRenameWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	ws, err := s.opt.Service.RenameWorkspace(r.Context(), actorOf(r), in.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, ws)
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	ms, err := s.opt.Service.ListMembers(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, ms)
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	role := domain.Role(in.Role)
	mt, err := s.opt.Service.AddMember(r.Context(), actorOf(r), in.Name, in.Email, role)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusCreated, mt)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Service.RemoveMember(r.Context(), actorOf(r), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReissueToken(w http.ResponseWriter, r *http.Request) {
	mt, err := s.opt.Service.ReissueToken(r.Context(), actorOf(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, mt)
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	m, err := s.opt.Service.SetMemberRole(r.Context(), actorOf(r), r.PathValue("id"), domain.Role(in.Role))
	s.respond(w, r, http.StatusOK, m, err)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.opt.Service.ListProjects(r.Context(), actorOf(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, ps)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Repository  string `json:"repository"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.opt.Service.CreateProject(r.Context(), actorOf(r), service.ProjectInput{Name: in.Name, Description: in.Description, Repository: in.Repository})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.opt.Service.GetProject(r.Context(), actorOf(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Repository  *string `json:"repository"`
		Archived    *bool   `json:"archived"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.opt.Service.UpdateProject(r.Context(), actorOf(r), r.PathValue("id"),
		service.ProjectPatch{Name: in.Name, Description: in.Description, Repository: in.Repository, Archived: in.Archived})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, p)
}

func (s *Server) handleListProjectMembers(w http.ResponseWriter, r *http.Request) {
	ms, err := s.opt.Service.ListProjectMembers(r.Context(), actorOf(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, ms)
}

func (s *Server) handleAddProjectMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
	}
	if r.ContentLength != 0 && !s.decode(w, r, &in) {
		return
	}
	if err := s.opt.Service.AddProjectMember(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("memberId"), domain.ProjectRole(in.Role)); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Service.RemoveProjectMember(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("memberId")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request) {
	st, err := s.opt.Service.StorageStatus(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, st, err)
}

func (s *Server) handleStorageBackup(w http.ResponseWriter, r *http.Request) {
	b, err := s.opt.Service.StorageBackup(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, b, err)
}

func (s *Server) handleRemoveHost(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.RemoveWorkspaceHost(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, out, err)
}

func (s *Server) handleResilience(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.Resilience(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, out, err)
}

// handleHeartbeat is a device saying that it is here and what kind of machine it is, with its own credential.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Platform    string `json:"platform"`
		Form        string `json:"form"`
		Sleeps      bool   `json:"sleeps"`
		SleepEvents int    `json:"sleepEvents"`
		Version     string `json:"version"`
		// HostConflict: the device already hosts another workspace.
		HostConflict bool `json:"hostConflict"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	err := s.opt.Service.Heartbeat(r.Context(), actorOf(r), domain.DeviceProfile{Platform: in.Platform, Form: domain.DeviceForm(in.Form), Sleeps: in.Sleeps, SleepEvents: in.SleepEvents, Version: in.Version, HostConflict: in.HostConflict})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSendMessage stores a request that a device signed, for another device of the same person. The server checks it and
// hands it over; it never makes, changes or signs one.
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var env envelope.Envelope
	if !s.decode(w, r, &env) {
		return
	}
	m, err := s.opt.Service.SendMessage(r.Context(), actorOf(r), env)
	s.respond(w, r, http.StatusCreated, m, err)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	ms, err := s.opt.Service.Messages(r.Context(), actorOf(r))
	s.respond(w, r, http.StatusOK, ms, err)
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	m, err := s.opt.Service.Message(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, m, err)
}

// handleInbox hands a device the signed requests waiting for it, holding the request open a little when there are none.
func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	wait := time.Duration(0)
	if v := r.URL.Query().Get("wait"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			wait = time.Duration(n) * time.Second
		}
	}
	ms, err := s.opt.Service.Inbox(r.Context(), actorOf(r), wait)
	s.respond(w, r, http.StatusOK, ms, err)
}

func (s *Server) handleAckMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		State  string          `json:"state"`
		Result json.RawMessage `json:"result"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	m, err := s.opt.Service.AckMessage(r.Context(), actorOf(r), r.PathValue("id"), domain.MessageState(in.State), in.Result)
	s.respond(w, r, http.StatusOK, m, err)
}

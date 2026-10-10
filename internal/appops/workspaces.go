package appops

// Connected boards are reached through a transport supplied by the person's desktop window. This package knows only
// project and ticket metadata; no address, credential, product API or execution capability crosses this contract.
import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"devboard/internal/domain"
	"devboard/internal/planning"
	"devboard/internal/service"
)

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WorkspaceCall is bounded, semantic transport, never an arbitrary HTTP request. Only reads and confirmed creation
// use it. The desktop enforces its own allow-list and the destination's service enforces current membership.
type WorkspaceCall func(context.Context, Workspace, string, any) (json.RawMessage, error)

type workspaceBinding struct {
	workspaces []Workspace
	call       WorkspaceCall
}
type workspaceBindings struct {
	sync.RWMutex
	sessions map[string]workspaceBinding
}

func (s *Service) BindWorkspaces(session string, workspaces []Workspace, call WorkspaceCall) {
	s.workspaces.Lock()
	defer s.workspaces.Unlock()
	if s.workspaces.sessions == nil {
		s.workspaces.sessions = map[string]workspaceBinding{}
	}
	if call == nil {
		delete(s.workspaces.sessions, session)
		return
	}
	s.workspaces.sessions[session] = workspaceBinding{append([]Workspace(nil), workspaces...), call}
}

func (s *Service) backend(p Principal) Backend {
	s.workspaces.RLock()
	b, ok := s.workspaces.sessions[p.SessionID]
	s.workspaces.RUnlock()
	if !ok {
		return s.b
	}
	return &workspaceBackend{Backend: s.b, binding: b}
}

type workspaceBackend struct {
	Backend
	binding workspaceBinding
}

func workspaceProjectID(w, p string) string { return "workspace:" + w + "/" + p }

func (b *workspaceBackend) locate(id string) (Workspace, string, bool) {
	for _, w := range b.binding.workspaces {
		if p, ok := strings.CutPrefix(id, "workspace:"+w.ID+"/"); ok && p != "" && !strings.Contains(p, "/") {
			return w, p, true
		}
	}
	return Workspace{}, "", false
}

func (b *workspaceBackend) read(ctx context.Context, w Workspace, method string, args, out any) error {
	raw, err := b.binding.call(ctx, w, method, args)
	if err != nil {
		return err
	}
	if len(raw) > maxResultBytes {
		return errorf(CodeTooLarge, "the connected board returned too much information")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errorf(CodeFailed, "the connected board returned unreadable information")
	}
	return nil
}

func (b *workspaceBackend) Projects(ctx context.Context) ([]service.ProjectDetail, error) {
	local, err := b.Backend.Projects(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range b.binding.workspaces {
		var projects []ProjectView
		if err := b.read(ctx, w, "list_projects", map[string]string{}, &projects); err != nil {
			return nil, err
		}
		for _, p := range projects {
			if p.ID == "" || strings.Contains(p.ID, "/") {
				return nil, errorf(CodeFailed, "the connected board returned an invalid project")
			}
			local = append(local, service.ProjectDetail{Project: domain.Project{ID: workspaceProjectID(w.ID, p.ID), Name: p.Name + " (" + w.Name + ")", Kind: domain.ProjectWork}})
		}
	}
	return local, nil
}

func (b *workspaceBackend) Project(ctx context.Context, id string) (*service.ProjectDetail, error) {
	w, pid, ok := b.locate(id)
	if !ok {
		return b.Backend.Project(ctx, id)
	}
	var p ProjectView
	if err := b.read(ctx, w, "get_project", map[string]string{"projectId": pid}, &p); err != nil {
		return nil, err
	}
	if p.ID != pid {
		return nil, errorf(CodeNotFound, "the connected project was not found")
	}
	return &service.ProjectDetail{Project: domain.Project{ID: id, Name: p.Name + " (" + w.Name + ")", Kind: domain.ProjectWork}}, nil
}

func connectedTicket(w Workspace, t TicketView) domain.Task {
	return domain.Task{ID: workspaceProjectID(w.ID, t.ID), ProjectID: workspaceProjectID(w.ID, t.ProjectID), Title: t.Title,
		Description: t.Description, State: domain.TaskState(t.State), WorkMode: planning.ExecutionMode(t.WorkMode),
		Plan: domain.Plan{Start: t.PlannedStart, End: t.PlannedEnd, Milestone: t.Milestone}, Version: t.Version,
		SourceRef: w.Name, UpdatedAt: t.UpdatedAt}
}

func (b *workspaceBackend) Tickets(ctx context.Context, id string) ([]domain.Task, error) {
	w, pid, ok := b.locate(id)
	if !ok {
		return b.Backend.Tickets(ctx, id)
	}
	var views []TicketView
	if err := b.read(ctx, w, "list_tickets", map[string]string{"projectId": pid}, &views); err != nil {
		return nil, err
	}
	tickets := []domain.Task{}
	for _, t := range views {
		if t.ProjectID != pid || t.ID == "" || strings.Contains(t.ID, "/") {
			return nil, errorf(CodeFailed, "the connected board returned an invalid ticket")
		}
		tickets = append(tickets, connectedTicket(w, t))
	}
	return tickets, nil
}

func (b *workspaceBackend) Ticket(ctx context.Context, pid, tid string) (*domain.Task, error) {
	if _, _, ok := b.locate(pid); ok {
		return nil, errorf(CodeRefused, "connected tickets can be listed and created; open their workspace to edit them")
	}
	return b.Backend.Ticket(ctx, pid, tid)
}

func (b *workspaceBackend) CreateTicket(ctx context.Context, in service.NewTask) (*domain.Task, error) {
	w, pid, ok := b.locate(in.ProjectID)
	if !ok {
		return b.Backend.CreateTicket(ctx, in)
	}
	if len(in.LabelIDs) > 0 {
		return nil, errorf(CodeRefused, "choose labels in the connected workspace after creating the ticket")
	}
	var ticket TicketView
	args := map[string]any{"projectId": pid, "title": in.Title, "description": in.Description,
		"workMode": in.WorkMode, "plannedStart": in.Plan.Start, "plannedEnd": in.Plan.End, "milestone": in.Plan.Milestone}
	if err := b.read(ctx, w, "create_ticket", args, &ticket); err != nil {
		return nil, err
	}
	if ticket.ProjectID != pid || ticket.ID == "" {
		return nil, errorf(CodeFailed, "the connected board returned an invalid new ticket; check its board before retrying")
	}
	t := connectedTicket(w, ticket)
	return &t, nil
}

func (b *workspaceBackend) Overview(ctx context.Context) (*service.Overview, error) {
	local, err := b.Backend.Overview(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range b.binding.workspaces {
		var projects []service.ProjectActivity
		if err := b.read(ctx, w, "get_overview", map[string]string{}, &projects); err != nil {
			return nil, err
		}
		for _, p := range projects {
			p.ProjectID = workspaceProjectID(w.ID, p.ProjectID)
			p.Name += " (" + w.Name + ")"
			local.Projects = append(local.Projects, p)
		}
	}
	return local, nil
}

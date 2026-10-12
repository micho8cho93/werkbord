// Package mcp serves grant-scoped Team board metadata. It delegates every
// operation to Team services, which recheck member and project permissions.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"devboard/internal/mcpcontract"
	"devboard/internal/planning"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
)

type Server struct{ service *service.Service }

func New(svc *service.Service) *Server { return &Server{service: svc} }

type actorKey struct{}

func (s *Server) Handler() http.Handler {
	return mcpcontract.StreamableHandlerWithContext(func(r *http.Request) (mcpcontract.Principal, context.Context, bool) {
		if len(r.Header.Values("Authorization")) != 1 {
			return mcpcontract.Principal{}, nil, false
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			return mcpcontract.Principal{}, nil, false
		}
		actor, principal, err := s.service.AuthenticateMCPGrant(r.Context(), strings.TrimSpace(token))
		if err != nil {
			return mcpcontract.Principal{}, nil, false
		}
		ctx := service.AuthenticatedContext(r.Context(), actor)
		ctx = context.WithValue(ctx, actorKey{}, actor)
		return principal, ctx, true
	}, s.operationsFor, s.call)
}

func (s *Server) operationsFor(principal mcpcontract.Principal) []mcpcontract.Operation {
	definitions := map[string]mcpcontract.Operation{
		"list_projects": {Name: "list_projects", Title: "List projects", Description: "List the projects this client can see.", Kind: "read", InputSchema: raw(`{"type":"object","additionalProperties":false}`)},
		"list_tickets":  {Name: "list_tickets", Title: "List tasks", Description: "List active tasks on one project board.", Kind: "read", InputSchema: raw(`{"type":"object","properties":{"projectId":{"type":"string","maxLength":128}},"required":["projectId"],"additionalProperties":false}`)},
		"get_ticket":    {Name: "get_ticket", Title: "Get a task", Description: "Read one task, including its plan and dependencies.", Kind: "read", InputSchema: raw(`{"type":"object","properties":{"projectId":{"type":"string","maxLength":128},"ticketId":{"type":"string","maxLength":128}},"required":["projectId","ticketId"],"additionalProperties":false}`)},
		"get_schedule":  {Name: "get_schedule", Title: "Read the timeline", Description: "Read planned tasks and conflicts in a project's timeline.", Kind: "read", InputSchema: raw(`{"type":"object","properties":{"projectId":{"type":"string","maxLength":128}},"required":["projectId"],"additionalProperties":false}`)},
		"create_ticket": {Name: "create_ticket", Title: "Create a task", Description: "Create a task on this project. This does not start work on it.", Kind: "mutation", InputSchema: raw(`{"type":"object","properties":{"projectId":{"type":"string","maxLength":128},"title":{"type":"string","maxLength":200},"description":{"type":"string","maxLength":20000},"requirements":{"type":"string","maxLength":20000},"workMode":{"type":"string","enum":["human","agent","hybrid"]},"labelIds":{"type":"array","items":{"type":"string"},"maxItems":20},"plan":{"type":"object","properties":{"start":{"type":"string","format":"date"},"end":{"type":"string","format":"date"},"milestone":{"type":"boolean"}},"additionalProperties":false},"dependencies":{"type":"array","items":{"type":"string"},"maxItems":100}},"required":["projectId","title"],"additionalProperties":false}`)},
		"update_ticket": {Name: "update_ticket", Title: "Update a task", Description: "Edit task details, planned dates or dependencies using the version last read. This does not start work.", Kind: "mutation", InputSchema: raw(`{"type":"object","properties":{"projectId":{"type":"string","maxLength":128},"ticketId":{"type":"string","maxLength":128},"version":{"type":"integer","minimum":1},"title":{"type":"string","maxLength":200},"description":{"type":"string","maxLength":20000},"requirements":{"type":"string","maxLength":20000},"workMode":{"type":"string","enum":["human","agent","hybrid"]},"labelIds":{"type":"array","items":{"type":"string"},"maxItems":20},"plan":{"type":"object","properties":{"start":{"type":"string","format":"date"},"end":{"type":"string","format":"date"},"milestone":{"type":"boolean"}},"additionalProperties":false},"dependencies":{"type":"array","items":{"type":"string"},"maxItems":100}},"required":["projectId","ticketId","version"],"additionalProperties":false}`)},
	}
	out := make([]mcpcontract.Operation, 0, len(service.MCPOperations))
	for _, name := range service.MCPOperations {
		if principal.Allows(name) {
			out = append(out, definitions[name])
		}
	}
	return out
}

func (s *Server) call(ctx context.Context, principal mcpcontract.Principal, name string, raw json.RawMessage) (any, error) {
	if !principal.Allows(name) {
		return nil, domain.ErrForbidden
	}
	actor, ok := ctx.Value(actorKey{}).(service.Actor)
	if !ok {
		return nil, domain.ErrUnauthenticated
	}
	switch name {
	case "list_projects":
		projects, err := s.service.ListProjects(ctx, actor)
		if err != nil {
			return nil, err
		}
		out := make([]domain.Project, 0, len(projects))
		for _, project := range projects {
			if principal.CanSee(project.ID) {
				out = append(out, project)
			}
		}
		return map[string]any{"projects": out}, nil
	case "list_tickets", "get_schedule":
		var args struct {
			ProjectID string `json:"projectId"`
		}
		if err := decode(raw, &args); err != nil || !principal.CanSee(args.ProjectID) {
			return nil, fmt.Errorf("%w: project was not found in this client's grant", domain.ErrNotFound)
		}
		if name == "get_schedule" {
			board, err := s.service.Board(ctx, actor, args.ProjectID)
			if err != nil {
				return nil, err
			}
			timeline, err := s.service.Timeline(ctx, actor, args.ProjectID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"tickets": board.Tickets, "warnings": timeline.Warnings}, nil
		}
		board, err := s.service.Board(ctx, actor, args.ProjectID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tickets": board.Tickets}, nil
	case "get_ticket":
		var args struct {
			ProjectID string `json:"projectId"`
			TicketID  string `json:"ticketId"`
		}
		if err := decode(raw, &args); err != nil || !principal.CanSee(args.ProjectID) {
			return nil, fmt.Errorf("%w: project was not found in this client's grant", domain.ErrNotFound)
		}
		return s.service.GetTicket(ctx, actor, args.ProjectID, args.TicketID)
	case "create_ticket":
		var args struct {
			ProjectID    string                 `json:"projectId"`
			Title        string                 `json:"title"`
			Description  string                 `json:"description"`
			Requirements string                 `json:"requirements"`
			WorkMode     planning.ExecutionMode `json:"workMode"`
			LabelIDs     []string               `json:"labelIds"`
			Plan         planning.Range         `json:"plan"`
			Dependencies []string               `json:"dependencies"`
		}
		if err := decode(raw, &args); err != nil || !principal.CanSee(args.ProjectID) {
			return nil, fmt.Errorf("%w: project was not found in this client's grant", domain.ErrNotFound)
		}
		return s.service.CreateTicket(ctx, actor, args.ProjectID, service.TicketInput{Title: args.Title, Description: args.Description,
			Requirements: args.Requirements, WorkMode: args.WorkMode, LabelIDs: args.LabelIDs, Plan: args.Plan, Dependencies: args.Dependencies})
	case "update_ticket":
		var args struct {
			ProjectID    string                  `json:"projectId"`
			TicketID     string                  `json:"ticketId"`
			Version      *int64                  `json:"version"`
			Title        *string                 `json:"title"`
			Description  *string                 `json:"description"`
			Requirements *string                 `json:"requirements"`
			WorkMode     *planning.ExecutionMode `json:"workMode"`
			LabelIDs     *[]string               `json:"labelIds"`
			Plan         *planning.Range         `json:"plan"`
			Dependencies *[]string               `json:"dependencies"`
		}
		if err := decode(raw, &args); err != nil || args.Version == nil || !principal.CanSee(args.ProjectID) {
			return nil, fmt.Errorf("%w: project, ticket and version are required within this client's grant", domain.ErrInvalid)
		}
		return s.service.UpdateTicket(ctx, actor, args.ProjectID, args.TicketID, service.TicketPatch{Version: args.Version,
			Title: args.Title, Description: args.Description, Requirements: args.Requirements, WorkMode: args.WorkMode,
			LabelIDs: args.LabelIDs, Plan: args.Plan, Dependencies: args.Dependencies})
	default:
		return nil, domain.ErrNotFound
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func decode(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("arguments are required")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing argument data")
	}
	return nil
}

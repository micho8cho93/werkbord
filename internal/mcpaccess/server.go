// Package mcpaccess exposes Individual's board operations to external MCP
// clients. Credentials and scopes belong to this controller; board rules stay
// in appops and the existing domain services.
package mcpaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"devboard/internal/appops"
	"devboard/internal/domain"
	"devboard/internal/mcpcontract"
	"devboard/internal/mcpgrant"
	"devboard/internal/service"
)

type Server struct {
	grants   *mcpgrant.Store
	ops      *appops.Service
	projects *service.Projects
}

func New(grants *mcpgrant.Store, ops *appops.Service, projects *service.Projects) *Server {
	return &Server{grants: grants, ops: ops, projects: projects}
}

func (s *Server) Handler() http.Handler {
	return mcpcontract.StreamableHandler(s.grants.Authenticate, s.listOperations, s.call)
}

func (s *Server) List() []mcpgrant.Grant { return s.grants.List() }

func (s *Server) Operations() []mcpcontract.Operation { return s.allOperations() }

func (s *Server) Create(ctx context.Context, principal mcpcontract.Principal) (mcpgrant.Created, error) {
	if s.grants == nil || s.ops == nil || s.projects == nil {
		return mcpgrant.Created{}, errors.New("MCP access is unavailable")
	}
	if err := principal.Validate(); err != nil {
		return mcpgrant.Created{}, err
	}
	known := map[string]bool{}
	for _, op := range s.allOperations() {
		known[op.Name] = true
	}
	for _, name := range principal.Operations {
		if !known[name] {
			return mcpgrant.Created{}, errors.New("grant includes an operation that is not available")
		}
	}
	for _, projectID := range principal.Projects {
		if _, err := s.projects.Get(ctx, projectID); err != nil {
			return mcpgrant.Created{}, domain.ErrNotFound
		}
	}
	created, err := s.grants.Create(principal)
	if err != nil {
		return mcpgrant.Created{}, err
	}
	if err := s.ops.EnsureMCPPrincipal(ctx, s.appPrincipal(created.Grant.Principal)); err != nil {
		s.grants.Revoke(created.Grant.ID)
		return mcpgrant.Created{}, err
	}
	return created, nil
}

func (s *Server) Revoke(id string) bool { return s.grants.Revoke(id) }

func (s *Server) Resolve(ctx context.Context, grantID, actionID string, approve bool) (*appops.Result, error) {
	grant, ok := s.grant(grantID)
	if !ok {
		return nil, domain.ErrNotFound
	}
	p := s.appPrincipal(grant.Principal)
	return s.ops.Resolve(ctx, appops.Decision{SessionID: p.SessionID, ActionID: actionID, Approve: approve, By: "owner", Executor: p})
}

type PendingProposal struct {
	GrantID  string              `json:"grantId"`
	Client   string              `json:"client"`
	Proposal appops.ActionStatus `json:"proposal"`
}

func (s *Server) Pending(ctx context.Context) ([]PendingProposal, error) {
	var out []PendingProposal
	for _, grant := range s.grants.List() {
		statuses, err := s.ops.ActionStatuses(ctx, s.appPrincipal(grant.Principal))
		if err != nil {
			return nil, err
		}
		for _, status := range statuses {
			if status.State == domain.ActionPending {
				out = append(out, PendingProposal{GrantID: grant.ID, Client: grant.Name, Proposal: status})
			}
		}
	}
	return out, nil
}

func (s *Server) grant(id string) (mcpgrant.Grant, bool) {
	for _, grant := range s.grants.List() {
		if grant.ID == id {
			return grant, true
		}
	}
	return mcpgrant.Grant{}, false
}

func (s *Server) listOperations(principal mcpcontract.Principal) []mcpcontract.Operation {
	all := s.allOperations()
	out := make([]mcpcontract.Operation, 0, len(principal.Operations))
	for _, op := range all {
		if principal.Allows(op.Name) {
			out = append(out, op)
		}
	}
	return out
}

func (s *Server) allOperations() []mcpcontract.Operation {
	p := appops.Principal{ID: "mcp:catalog", Actor: "mcp:catalog", Via: "mcp", SessionID: "mcp_catalog", Grants: appops.AllPermissions, Projects: []string{}}
	specs := s.ops.Catalog(p)
	out := make([]mcpcontract.Operation, 0, len(specs)+2)
	for _, spec := range specs {
		schema, err := json.Marshal(spec.Input)
		if err != nil {
			continue
		}
		out = append(out, mcpcontract.Operation{Name: spec.Name, Title: spec.Title, Description: spec.Description,
			Kind: string(spec.Kind), InputSchema: schema})
	}
	out = append(out,
		mcpcontract.Operation{Name: "list_pending_proposals", Title: "Pending proposals", Kind: "read",
			Description: "List this client's changes waiting for approval in Werkbord.", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`)},
		mcpcontract.Operation{Name: "get_proposal_status", Title: "Proposal status", Kind: "read",
			Description: "Check the state of one proposal made by this client.", InputSchema: json.RawMessage(`{"type":"object","properties":{"proposalId":{"type":"string","maxLength":80}},"required":["proposalId"],"additionalProperties":false}`)},
	)
	return out
}

func (s *Server) call(ctx context.Context, grant mcpcontract.Principal, name string, raw json.RawMessage) (any, error) {
	if !grant.Allows(name) {
		return nil, errors.New("operation is not included in this MCP client's grant")
	}
	p := s.appPrincipal(grant)
	switch name {
	case "list_pending_proposals":
		var args struct{}
		if err := decodeObject(raw, &args); err != nil {
			return nil, err
		}
		statuses, err := s.ops.ActionStatuses(ctx, p)
		if err != nil {
			return nil, err
		}
		pending := make([]appops.ActionStatus, 0, len(statuses))
		for _, status := range statuses {
			if status.State == domain.ActionPending {
				pending = append(pending, status)
			}
		}
		return pending, nil
	case "get_proposal_status":
		var args struct {
			ProposalID string `json:"proposalId"`
		}
		if err := decodeObject(raw, &args); err != nil || args.ProposalID == "" {
			return nil, errors.New("proposalId is required")
		}
		return s.ops.ActionStatus(ctx, p, args.ProposalID)
	default:
		result, err := s.ops.Call(ctx, p, name, raw)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
}

func decodeObject(raw json.RawMessage, dst any) error {
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

func (s *Server) appPrincipal(grant mcpcontract.Principal) appops.Principal {
	permissions := map[appops.Permission]bool{}
	for _, spec := range s.ops.Catalog(appops.Principal{ID: "mcp:catalog", Actor: "mcp:catalog", Via: "mcp", SessionID: "mcp_catalog", Grants: appops.AllPermissions, Projects: []string{}}) {
		if grant.Allows(spec.Name) {
			permissions[spec.Permission] = true
		}
	}
	grants := make([]appops.Permission, 0, len(permissions))
	for permission := range permissions {
		grants = append(grants, permission)
	}
	return appops.Principal{ID: "mcp:" + grant.ID, Actor: "mcp:" + grant.Name, Via: "mcp", SessionID: grant.ID,
		Grants: grants, Projects: append([]string{}, grant.Projects...)}
}

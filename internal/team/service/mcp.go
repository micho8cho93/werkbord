package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"devboard/internal/mcpcontract"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

const MCPTokenPrefix = "wbtm_"

// MCPOperations are the Team metadata operations a member may delegate to a
// separately revocable MCP client. Every call still passes through this service.
var MCPOperations = []string{
	"list_projects", "list_tickets", "get_ticket", "get_schedule", "create_ticket", "update_ticket",
}

type CreatedMCPGrant struct {
	Grant domain.MCPGrant `json:"grant"`
	Token string          `json:"token"`
}

// CreateMCPGrant issues a scoped token to the current member. The token itself
// is returned once; the database retains only its SHA-256 digest.
func (s *Service) CreateMCPGrant(ctx context.Context, a Actor, scope mcpcontract.Principal) (CreatedMCPGrant, error) {
	scope.ID = ""
	if err := scope.Validate(); err != nil {
		return CreatedMCPGrant{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	allowed := map[string]bool{}
	for _, op := range MCPOperations {
		allowed[op] = true
	}
	for _, op := range scope.Operations {
		if !allowed[op] {
			return CreatedMCPGrant{}, fmt.Errorf("%w: operation %q is not available for Team MCP clients", domain.ErrInvalid, op)
		}
	}
	visible, err := s.ListProjects(ctx, a)
	if err != nil {
		return CreatedMCPGrant{}, err
	}
	visibleIDs := make(map[string]bool, len(visible))
	for _, project := range visible {
		visibleIDs[project.ID] = true
	}
	for _, projectID := range scope.Projects {
		if !visibleIDs[projectID] {
			return CreatedMCPGrant{}, fmt.Errorf("%w: project %s was not found", domain.ErrNotFound, projectID)
		}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return CreatedMCPGrant{}, err
	}
	token := MCPTokenPrefix + hex.EncodeToString(raw)
	grant := domain.MCPGrant{ID: domain.NewID(domain.PrefixMCPGrant), WorkspaceID: a.Workspace.ID, MemberID: a.Member.ID,
		Name: scope.Name, Projects: scope.Projects, Operations: scope.Operations, TokenHash: domain.HashToken(token), CreatedAt: s.stamp()}
	if err := s.db.Update(ctx, func(tx store.Tx) error { return tx.InsertMCPGrant(ctx, grant) }); err != nil {
		return CreatedMCPGrant{}, err
	}
	s.changed(a.Workspace.ID, nil)
	return CreatedMCPGrant{Grant: grant, Token: token}, nil
}

// ListMCPGrants returns the current member's own grants, including revoked
// metadata so an already-pending human decision remains attributable.
func (s *Service) ListMCPGrants(ctx context.Context, a Actor) ([]domain.MCPGrant, error) {
	if err := a.require(domain.PermWorkspaceView, "view MCP client grants"); err != nil {
		return nil, err
	}
	var out []domain.MCPGrant
	err := s.db.View(ctx, func(tx store.Tx) (err error) {
		out, err = tx.MCPGrants(ctx, a.Workspace.ID, a.Member.ID)
		return err
	})
	return out, err
}

// RevokeMCPGrant revokes one of the current member's MCP credentials.
func (s *Service) RevokeMCPGrant(ctx context.Context, a Actor, id string) error {
	var revoked bool
	err := s.db.Update(ctx, func(tx store.Tx) (err error) {
		revoked, err = tx.RevokeMCPGrant(ctx, a.Workspace.ID, a.Member.ID, id, s.stamp())
		return err
	})
	if err != nil {
		return err
	}
	if !revoked {
		return domain.ErrNotFound
	}
	s.changed(a.Workspace.ID, nil)
	return nil
}

// AuthenticateMCPGrant resolves a bearer to its member and current scope.
// Membership, project membership and role are read afresh for every request.
func (s *Service) AuthenticateMCPGrant(ctx context.Context, token string) (Actor, mcpcontract.Principal, error) {
	if len(token) > 128 || !strings.HasPrefix(token, MCPTokenPrefix) {
		return Actor{}, mcpcontract.Principal{}, domain.ErrUnauthenticated
	}
	hash := domain.HashToken(token)
	var actor Actor
	var principal mcpcontract.Principal
	err := s.base.View(ctx, func(tx store.Tx) error {
		grant, err := tx.MCPGrantByTokenHash(ctx, hash)
		if errors.Is(err, domain.ErrNotFound) || err == nil && grant.RevokedAt != nil {
			return domain.ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		member, err := tx.Member(ctx, grant.WorkspaceID, grant.MemberID)
		if err != nil {
			return domain.ErrUnauthenticated
		}
		workspace, err := tx.Workspace(ctx, grant.WorkspaceID)
		if err != nil {
			return domain.ErrUnauthenticated
		}
		actor = Actor{Member: member, Workspace: workspace, credentialHash: hash, MCPGrantID: grant.ID}
		principal = mcpcontract.Principal{ID: grant.ID, Name: grant.Name, Projects: append([]string(nil), grant.Projects...), Operations: append([]string(nil), grant.Operations...)}
		return nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		err = domain.ErrUnauthenticated
	}
	return actor, principal, err
}

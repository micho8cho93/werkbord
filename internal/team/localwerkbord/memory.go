package localwerkbord

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"
	"werkbord/internal/integration"
)

// ApprovedMemory reads only the local owner's explicit sharing projections. The
// grant is issued by Individual's owner API and passed here; this path never
// reads or exchanges the controller token. The standard loopback/redirect guards
// remain mandatory, and response fields and versions are strictly decoded.
func (c *Client) ApprovedMemory(ctx context.Context, workspace, after string) (integration.MemoryShares, error) {
	var out integration.MemoryShares
	if !integration.Identifier(workspace) || after != "" && !integration.Identifier(after) {
		return out, errors.New("invalid memory cursor or workspace")
	}
	var entry struct {
		Scope       string `json:"scope"`
		WorkspaceID string `json:"memoryWorkspaceId"`
	}
	if err := c.do(ctx, "GET", "/api/local-access/self", nil, &entry); err != nil {
		return out, err
	}
	if entry.Scope != "memory-v1" || entry.WorkspaceID != workspace {
		return out, errors.New("memory sharing requires a separate workspace-bound memory-v1 grant")
	}
	var raw json.RawMessage
	if err := c.do(ctx, "GET", "/api/memory/v1/shares?workspaceId="+url.QueryEscape(workspace)+"&after="+url.QueryEscape(after), nil, &raw); err != nil {
		return out, err
	}
	if err := integration.DecodeMemory(raw, &out); err != nil {
		return out, err
	}
	for _, s := range out.Shares {
		if s.Proposal.Scope.WorkspaceID != workspace || !s.ExpiresAt.After(time.Now()) {
			return out, errors.New("memory share expired or belongs to another workspace")
		}
	}
	return out, nil
}

// MemoryEvents and AckMemory expose only this workspace's durable projection journal.
func (c *Client) MemoryEvents(ctx context.Context, w string) (integration.MemoryEventPage, error) {
	var out integration.MemoryEventPage
	if !integration.Identifier(w) {
		return out, errors.New("invalid workspace")
	}
	var entry struct {
		Scope     string `json:"scope"`
		Workspace string `json:"memoryWorkspaceId"`
	}
	if err := c.do(ctx, "GET", "/api/local-access/self", nil, &entry); err != nil {
		return out, err
	}
	if entry.Scope != "memory-v1" || entry.Workspace != w {
		return out, errors.New("workspace-bound memory grant required")
	}
	var raw json.RawMessage
	err := c.do(ctx, "GET", "/api/memory/v1/publication/events?workspaceId="+url.QueryEscape(w), nil, &raw)
	if err != nil {
		return out, err
	}
	if err = integration.DecodeMemory(raw, &out); err != nil {
		return out, err
	}
	if out.Schema != integration.MemorySchema || len(out.Events)+len(out.Tracking) > integration.MaxMemoryBatch {
		return out, errors.New("invalid event page")
	}
	for _, e := range append(out.Events, out.Tracking...) {
		if e.Validate() != nil || e.Proposal.Scope.WorkspaceID != w {
			return out, errors.New("invalid memory event")
		}
	}
	return out, err
}
func (c *Client) AckMemory(ctx context.Context, w string, a integration.MemoryReceipt) error {
	return c.do(ctx, "POST", "/api/memory/v1/publication/events?workspaceId="+url.QueryEscape(w), a, nil)
}

package localwerkbord

import (
	"context"
	"devboard/internal/integration"
	"encoding/json"
	"errors"
	"net/url"
	"time"
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

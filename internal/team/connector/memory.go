package connector

import (
	"context"
	"devboard/internal/integration"
	"encoding/json"
	"errors"
	"time"
)

type MemoryLocal interface {
	ApprovedMemory(context.Context, string, string) (integration.MemoryShares, error)
}

// ShareMemory performs one bounded, explicitly requested synchronization page.
// It is independent of Tick and the integration-v1 grant. Only proposals are
// sent, with the device-signed Host client; nothing approves or publishes them.
// Retrying a lost response uses Team's durable per-author ClientID. No local
// deletion/expiry is interpreted as a request to delete shared Team knowledge.
func ShareMemory(ctx context.Context, host Workspace, local MemoryLocal, workspace, after string) (string, bool, error) {
	if !integration.Identifier(workspace) || after != "" && !integration.Identifier(after) {
		return "", false, errors.New("invalid memory scope or cursor")
	}
	me, err := host.Me(ctx)
	if err != nil {
		return "", false, err
	}
	if me.Workspace.ID != workspace {
		return "", false, errors.New("memory workspace identity mismatch")
	}
	page, err := local.ApprovedMemory(ctx, workspace, after)
	if err != nil {
		return "", false, err
	}
	// Validate test/adapted clients too, before crossing the network.
	raw, err := json.Marshal(page)
	if err != nil {
		return "", false, err
	}
	var valid integration.MemoryShares
	if err := integration.DecodeMemory(raw, &valid); err != nil {
		return "", false, err
	}
	if valid.More && valid.Next != valid.Shares[len(valid.Shares)-1].ID || !valid.More && valid.Next != "" {
		return "", false, errors.New("invalid memory sharing cursor")
	}
	next := after
	for _, share := range valid.Shares {
		if share.ID <= next || share.Proposal.Scope.WorkspaceID != workspace || !share.ExpiresAt.After(time.Now()) {
			return next, false, errors.New("invalid or expired memory sharing record")
		}
		var ack json.RawMessage
		if err := host.Do(ctx, "POST", "/memory/proposals", share.Proposal, &ack); err != nil {
			return next, false, err
		}
		next = share.ID
	}
	return next, valid.More, nil
}

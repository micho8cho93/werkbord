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

type MemoryJournal interface {
	MemoryEvents(context.Context, string) (integration.MemoryEventPage, error)
	AckMemory(context.Context, string, integration.MemoryReceipt) error
}

// SyncMemory retries the oldest unacknowledged events. Individual retains its
// cursor until both the signed Host receipt and the local ack have committed.
func SyncMemory(ctx context.Context, host Workspace, local MemoryJournal, w, member, device string) error {
	me, err := host.Me(ctx)
	if err != nil {
		return err
	}
	if me.Workspace.ID != w || me.Member.ID != member {
		return errors.New("memory connector identity mismatch")
	}
	page, err := local.MemoryEvents(ctx, w)
	if err != nil {
		return err
	}
	if page.Schema != integration.MemorySchema || len(page.Events)+len(page.Tracking) > integration.MaxMemoryBatch {
		return errors.New("invalid memory journal page")
	}
	var last int64
	for i, e := range append(page.Events, page.Tracking...) {
		if e.Validate() != nil || e.Proposal.Scope.WorkspaceID != w || i < len(page.Events) && last != 0 && e.Sequence != last+1 {
			return errors.New("invalid memory journal order")
		}
		last = e.Sequence
		var ack integration.MemoryReceipt
		var raw json.RawMessage
		if err = host.Do(ctx, "POST", "/memory/delivery", e, &raw); err != nil {
			return err
		}
		if err = integration.DecodeMemory(raw, &ack); err != nil {
			return err
		}
		if ack.EventID != e.ID || ack.Sequence != e.Sequence || ack.Record.AuthorID != member || ack.Record.AuthorDeviceID != device || ack.Record.Validate() != nil {
			return errors.New("invalid memory delivery receipt")
		}
		if err = local.AckMemory(ctx, w, ack); err != nil {
			return err
		}
	}
	return nil
}

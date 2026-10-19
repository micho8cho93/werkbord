package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"devboard/internal/integration"
	"devboard/internal/team/domain"
)

func TestSharedMemoryQueriesCommitAtomicallyAndKeepExactScope(t *testing.T) {
	c := newConv(t)
	ctx := context.Background()
	scope := integration.SharedMemoryScope{WorkspaceID: c.ws, Kind: "project", ProjectID: c.project}
	proposal := integration.MemoryProposal{
		Schema: integration.MemorySchema,
		Scope:  scope,
		Content: integration.SharedMemoryContent{
			Kind: "convention", Summary: "Use explicit transactions", Classification: "team", Verification: "verified",
			Source: integration.MemorySource{Kind: "human", ID: "review_one"},
		},
		ClientID: "store_client_one",
	}
	principal := MemoryPrincipal{WorkspaceID: c.ws, MemberID: c.bo}
	before := c.revision()
	rolledBack := errors.New("roll back shared memory")
	if err := c.db.Update(ctx, func(tx Tx) error {
		if _, err := tx.ProposeMemory(ctx, principal, proposal, domain.NewID("mem"), t0); err != nil {
			return err
		}
		return rolledBack
	}); !errors.Is(err, rolledBack) {
		t.Fatalf("rollback returned %v", err)
	}
	if got := c.revision(); got != before {
		t.Fatalf("rolled back memory changed workspace revision: %d -> %d", before, got)
	}
	var record integration.SharedMemoryRecord
	c.update(func(tx Tx) error {
		var err error
		record, err = tx.ProposeMemory(ctx, principal, proposal, domain.NewID("mem"), t0)
		return err
	})
	if got := c.revision(); got != before+1 {
		t.Fatalf("committed memory did not bump workspace revision: %d -> %d", before, got)
	}
	c.view(func(tx Tx) error {
		page, err := tx.ReadMemories(ctx, principal, scope, "", false)
		if err != nil || len(page.Records) != 1 || page.Records[0].ID != record.ID {
			t.Fatalf("exact-scope read: %+v %v", page, err)
		}
		wrong := scope
		wrong.ProjectID = "prj_hidden"
		if _, err := tx.ReadMemories(ctx, principal, wrong, "", false); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("inaccessible project was not hidden: %v", err)
		}
		return nil
	})
	denied := integration.MemoryCommand{Schema: integration.MemorySchema, Scope: scope, Version: record.Version, Action: "delete"}
	if err := c.db.Update(ctx, func(tx Tx) error {
		_, err := tx.ChangeMemory(ctx, principal, record.ID, denied, t0)
		return err
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("contributor deleted shared memory: %v", err)
	}
	owner := MemoryPrincipal{WorkspaceID: c.ws, MemberID: c.ada}
	approved := integration.MemoryCommand{Schema: integration.MemorySchema, Scope: scope, Version: record.Version, Action: "approve"}
	c.update(func(tx Tx) error {
		var err error
		record, err = tx.ChangeMemory(ctx, owner, record.ID, approved, t0.Add(time.Second))
		return err
	})
	if record.State != "approved" || record.Approval == nil || record.Approval.MemberID != c.ada || record.Version != 2 {
		t.Fatalf("approval not committed with its memory revision: %+v", record)
	}
	stale := approved
	if err := c.db.Update(ctx, func(tx Tx) error {
		_, err := tx.ChangeMemory(ctx, owner, record.ID, stale, t0.Add(2*time.Second))
		return err
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale memory version was accepted: %v", err)
	}
}

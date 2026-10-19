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

func TestSharedMemoryQuotaCannotPreventWithdrawal(t *testing.T) {
	c := newConv(t)
	ctx := context.Background()
	scope := integration.SharedMemoryScope{WorkspaceID: c.ws, Kind: "project", ProjectID: c.project}
	p := integration.MemoryProposal{Schema: integration.MemorySchema, Scope: scope, ClientID: "quota_memory", Content: integration.SharedMemoryContent{Kind: "observation", Summary: "Withdrawable knowledge", Classification: "team", Verification: "verified", Source: integration.MemorySource{Kind: "human", ID: "review_one"}}}
	author := MemoryPrincipal{WorkspaceID: c.ws, MemberID: c.bo}
	owner := MemoryPrincipal{WorkspaceID: c.ws, MemberID: c.ada}
	var r integration.SharedMemoryRecord
	c.update(func(tx Tx) error {
		var err error
		r, err = tx.ProposeMemory(ctx, author, p, "memory_quota", t0)
		return err
	})
	c.update(func(tx Tx) error {
		_, err := tx.(*sqlTx).q.ExecContext(ctx, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000) INSERT INTO shared_memory_revisions(workspace_id,memory_id,version,document) SELECT workspace_id,id,100+x,document FROM shared_memories,n WHERE workspace_id=? AND id=?`, c.ws, r.ID)
		return err
	})
	if err := c.db.Update(ctx, func(tx Tx) error {
		_, e := tx.ChangeMemory(ctx, owner, r.ID, integration.MemoryCommand{Schema: p.Schema, Scope: scope, Version: r.Version, Action: "approve"}, t0.Add(time.Second))
		return e
	}); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("quota not active: %v", err)
	}
	c.update(func(tx Tx) error {
		out, err := tx.(*sqlTx).withdrawProjection(ctx, author, r, t0.Add(time.Second))
		if err == nil && (out.State != "deleted" || out.Content.Summary != "") {
			t.Fatalf("withdrawal retained content: %+v", out)
		}
		return err
	})
}

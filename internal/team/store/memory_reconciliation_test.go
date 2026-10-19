package store

import (
	"context"
	"devboard/internal/integration"
	"fmt"
	"testing"
	"time"
)

func TestMemoryReconciliationBoundedAndCompactsHistory(t *testing.T) {
	c := newConv(t)
	ctx := context.Background()
	a := MemoryPrincipal{WorkspaceID: c.ws, MemberID: c.bo}
	scope := integration.SharedMemoryScope{WorkspaceID: c.ws, Kind: "project", ProjectID: c.project}
	for i := 0; i < 51; i++ {
		c.update(func(tx Tx) error {
			_, err := tx.ProposeMemory(ctx, a, integration.MemoryProposal{Schema: integration.MemorySchema, Scope: scope, ClientID: fmt.Sprintf("batch_%d", i), Content: integration.SharedMemoryContent{Kind: "observation", Summary: "Bounded knowledge", Classification: "team", Verification: "verified", Source: integration.MemorySource{Kind: "human", ID: "source"}}}, fmt.Sprintf("mem_%02d", i), t0)
			return err
		})
	}
	c.update(func(tx Tx) error { return tx.ReconcileMemoryBatch(ctx, t0) })
	c.view(func(tx Tx) error {
		var n int
		err := tx.(*sqlTx).q.QueryRowContext(ctx, `SELECT count(*) FROM memory_reconciliation WHERE workspace_id=?`, c.ws).Scan(&n)
		if err != nil {
			return err
		}
		if n != 50 {
			t.Fatalf("tick processed %d records, want 50", n)
		}
		return nil
	})
	c.update(func(tx Tx) error { return tx.ReconcileMemoryBatch(ctx, t0.Add(time.Second)) })
	c.view(func(tx Tx) error {
		var n int
		err := tx.(*sqlTx).q.QueryRowContext(ctx, `SELECT count(*) FROM memory_reconciliation WHERE workspace_id=?`, c.ws).Scan(&n)
		if err == nil && n != 51 {
			t.Fatalf("next tick starved record: %d", n)
		}
		return err
	})
	// Populate more than 20 historical revisions; compaction keeps the current
	// version and never alters its contents or its durable client key.
	c.update(func(tx Tx) error {
		q := tx.(*sqlTx).q
		_, err := q.ExecContext(ctx, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<30) INSERT INTO shared_memory_revisions SELECT workspace_id,id,x+1,document FROM shared_memories,n WHERE workspace_id=? AND id='mem_00'`, c.ws)
		return err
	})
	for i := 0; i < 2; i++ {
		c.update(func(tx Tx) error { return tx.ReconcileMemoryBatch(ctx, t0.Add(time.Duration(i+2)*time.Second)) })
	}
	c.view(func(tx Tx) error {
		var n int
		err := tx.(*sqlTx).q.QueryRowContext(ctx, `SELECT count(*) FROM shared_memory_revisions WHERE workspace_id=? AND memory_id='mem_00'`, c.ws).Scan(&n)
		if err == nil && n > MemoryRevisionLimit+1 {
			t.Fatalf("unbounded revisions: %d", n)
		}
		return err
	})
	for i := 0; i < 2; i++ {
		c.update(func(tx Tx) error {
			return tx.ReconcileMemoryBatch(ctx, t0.Add(MemoryAuditRetention+time.Duration(i+1)*time.Hour))
		})
	}
	c.view(func(tx Tx) error {
		var n int
		err := tx.(*sqlTx).q.QueryRowContext(ctx, `SELECT count(*) FROM shared_memory_revisions WHERE workspace_id=? AND memory_id='mem_00'`, c.ws).Scan(&n)
		if err == nil && n != 1 {
			t.Fatalf("expired audit retained %d payloads", n)
		}
		return err
	})
}

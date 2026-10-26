package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func TestKnowledgeEvidenceRestartDeleteAndCapacity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "knowledge.db")
	db, e := Open(ctx, path, nil)
	if e != nil {
		t.Fatal(e)
	}
	m := memoryRecord(t, "Source evidence")
	m.Pinned = true
	evidence := domain.MemoryEvidence{OwnerID: m.Scope.OwnerID, Key: strings.Repeat("a", 64), MemoryID: m.ID, Source: m.Source, Excerpt: m.Summary, ObservedAt: time.Now()}
	mustUpdate(t, db, func(tx store.Tx) error {
		if e := tx.Memory().Create(ctx, m); e != nil {
			return e
		}
		return tx.Memory().AddEvidence(ctx, evidence)
	})
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e = Open(ctx, path, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = db.View(ctx, func(tx store.Tx) error {
		got, e := tx.Memory().Get(ctx, m.Scope, m.ID)
		if e != nil || !got.Pinned {
			t.Fatal(got, e)
		}
		sources, e := tx.Memory().Evidence(ctx, m.Scope, m.ID, 64)
		if e != nil || len(sources) != 1 || sources[0].Excerpt != m.Summary {
			t.Fatal(sources, e)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Delete(ctx, m.Scope, m.ID, m.Version) })
	if e = db.View(ctx, func(tx store.Tx) error {
		seen, e := tx.Memory().EvidenceSeen(ctx, m.Scope.OwnerID, evidence.Key)
		if e != nil || !seen {
			t.Fatal(seen, e)
		}
		size, e := tx.Memory().EvidenceUsage(ctx, m.Scope.OwnerID)
		if e != nil || size != 130 {
			t.Fatalf("expected content-free 128+{} byte tombstone, got %d %v", size, e)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	// Global byte cap is enforced in SQLite, independently of service limits.
	if _, e = db.writer.ExecContext(ctx, `UPDATE memory_evidence_usage SET bytes=67108864`); e != nil {
		t.Fatal(e)
	}
	m = memoryRecord(t, "Rejected capacity")
	evidence.MemoryID = m.ID
	evidence.Key = strings.Repeat("b", 64)
	e = db.Update(ctx, func(tx store.Tx) error {
		if e := tx.Memory().Create(ctx, m); e != nil {
			return e
		}
		return tx.Memory().AddEvidence(ctx, evidence)
	})
	if !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	if e = db.View(ctx, func(tx store.Tx) error {
		_, e := tx.Memory().Get(ctx, m.Scope, m.ID)
		if !errors.Is(e, domain.ErrNotFound) {
			t.Fatal("capacity left partial record", e)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

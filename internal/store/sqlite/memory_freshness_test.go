package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func TestMemoryFreshnessCheckpointAtomicCASRestartAndCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := memoryRecord(t, "checkpoint rollback")
	m.State = domain.MemoryCurrent
	state := &domain.MemoryFreshness{Scope: m.Scope, EventSeq: 4, Revision: domain.MemoryRepositoryRevision{Head: "source"}, After: &domain.MemoryCursor{CreatedAt: m.CreatedAt, ID: m.ID}, CheckedAt: time.Now()}
	mustUpdate(t, db, func(tx store.Tx) error {
		if err := tx.Memory().Create(ctx, m); err != nil {
			return err
		}
		return tx.Memory().SaveFreshness(ctx, state)
	})
	stale := *state
	if err := db.Update(ctx, func(tx store.Tx) error { state.EventSeq = 9; return tx.Memory().SaveFreshness(ctx, state) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error {
		// This invalidation must roll back if another worker already moved cursor.
		m.State = domain.MemoryNeedsVerification
		if err := tx.Memory().Update(ctx, m); err != nil {
			return err
		}
		return tx.Memory().SaveFreshness(ctx, &stale)
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.View(ctx, func(tx store.Tx) error {
		got, err := tx.Memory().Freshness(ctx, m.Scope)
		if err != nil || got.EventSeq != 9 || got.After == nil || got.After.ID != m.ID {
			t.Fatal(got, err)
		}
		record, err := tx.Memory().Get(ctx, m.Scope, m.ID)
		if err != nil || record.State != domain.MemoryCurrent || record.Version != 1 {
			t.Fatal(record, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The last partition deletion also removes its private checkpoint.
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Delete(ctx, m.Scope, m.ID, 1) })
	if err := db.View(ctx, func(tx store.Tx) error { _, err := tx.Memory().Freshness(ctx, m.Scope); return err }); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestMemoryFreshnessScopeTraversalBoundedAndIsolated(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	for _, owner := range []string{"a", "b", "c"} {
		m := memoryRecord(t, owner)
		m.Scope.OwnerID = owner
		mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, m) })
	}
	mustUpdate(t, db, func(tx store.Tx) error {
		page, err := tx.Memory().FreshnessScopes(ctx, nil, 2)
		if err != nil || len(page) != 2 || page[0].OwnerID != "a" {
			t.Fatal(page, err)
		}
		next, err := tx.Memory().FreshnessScopes(ctx, &page[1], 2)
		if err != nil || len(next) != 1 || next[0].OwnerID != "c" {
			t.Fatal(next, err)
		}
		if _, err := tx.Memory().FreshnessScopes(ctx, nil, 1000); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
		return nil
	})
}
func TestMemoryFreshnessUpgradeFromPhase1A(t *testing.T) {
	path := oldDatabase(t, 17)
	db, err := Open(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustUpdate(t, db, func(tx store.Tx) error {
		m := memoryRecord(t, "additive freshness migration")
		if err := tx.Memory().Create(context.Background(), m); err != nil {
			return err
		}
		return tx.Memory().SaveFreshness(context.Background(), &domain.MemoryFreshness{Scope: m.Scope, CheckedAt: time.Now()})
	})
}

func TestMemoryFreshnessInvalidationStillWorksAtContentCapacity(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	m := memoryRecord(t, "full content capacity")
	m.State = domain.MemoryCurrent
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, m) })
	if _, err := db.writer.ExecContext(ctx, `UPDATE memory_usage SET bytes=268435456 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Invalidate(ctx, m.Scope, m.ID, m.Version, time.Now()) })
	if err := db.View(ctx, func(tx store.Tx) error {
		got, err := tx.Memory().Get(ctx, m.Scope, m.ID)
		if err != nil || got.State != domain.MemoryNeedsVerification || got.Version != 2 {
			t.Fatal(got, err)
		}
		usage, err := tx.Memory().Usage(ctx, "")
		if err != nil || usage.Bytes != domain.MaxMemoryBytes {
			t.Fatal(usage, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

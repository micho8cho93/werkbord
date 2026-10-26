package sqlite

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

// TestAuditMemoryScale reports storage growth and latency as the memory table
// grows. It always runs a small size; AUDIT_MEMORIES=50000 measures a large one.
func TestAuditMemoryScale(t *testing.T) {
	n := 2000
	if v, err := strconv.Atoi(os.Getenv("AUDIT_MEMORIES")); err == nil && v > 0 {
		n = v
	}
	db, path := openTemp(t)
	ctx := context.Background()
	project := seedProject(t, db)
	scope := domain.MemoryScope{OwnerID: "individual", ProjectID: project.ID}
	words := []string{"cache", "sqlite", "queue", "deploy", "review", "retry", "schema", "token", "runner", "branch", "merge", "build"}
	start := time.Now()
	const batch = 500
	for done := 0; done < n; done += batch {
		mustUpdate(t, db, func(tx store.Tx) error {
			for i := done; i < done+batch && i < n; i++ {
				c, err := (domain.MemoryContent{Type: "decision", Authority: domain.MemoryObservation, State: domain.MemoryNeedsVerification,
					Source:  domain.MemoryProvenance{Type: domain.MemorySourceHandoff, Reference: fmt.Sprintf("handoff:run_%d:decision:v1", i), RunID: fmt.Sprintf("run_%d", i), HandoffID: fmt.Sprintf("run_%d", i)},
					Summary: fmt.Sprintf("Decision %d: use %s with %s for %s", i, words[i%12], words[(i/12)%12], words[(i/144)%12])}).Normalized()
				if err != nil {
					return err
				}
				at := now()
				m := &domain.Memory{ID: domain.NewID("mem"), Scope: scope, MemoryContent: c, Fingerprint: domain.MemoryFingerprint(c), SchemaVersion: 1, Version: 1, CreatedAt: at, UpdatedAt: at}
				if err := tx.Memory().Create(ctx, m); err != nil {
					return err
				}
			}
			return nil
		})
	}
	insert := time.Since(start)
	if _, err := db.pool.Writer.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	timeIt := func(label string, q domain.MemoryQuery, retrieval bool) {
		var worst, total time.Duration
		const reps = 20
		for i := 0; i < reps; i++ {
			s := time.Now()
			if err := db.View(ctx, func(tx store.Tx) error {
				_, e := tx.Memory().KnowledgeList(ctx, scope, q, retrieval)
				return e
			}); err != nil {
				t.Fatal(err)
			}
			d := time.Since(s)
			total += d
			worst = max(worst, d)
		}
		t.Logf("%-34s mean %8.2fms worst %8.2fms", label, float64(total)/reps/1e6, float64(worst)/1e6)
	}
	t.Logf("records=%d insert=%v (%.0f/s) db=%.1f MiB (%.0f B/record)", n, insert.Round(time.Millisecond), float64(n)/insert.Seconds(), float64(info.Size())/(1<<20), float64(info.Size())/float64(n))
	timeIt("list newest 100", domain.MemoryQuery{Limit: 100}, false)
	timeIt("retrieval candidates (no text)", domain.MemoryQuery{Limit: 100, IncludeCandidates: true, PrioritizeAuthority: true}, true)
	timeIt("retrieval FTS 'sqlite cache'", domain.MemoryQuery{Text: "sqlite cache", Limit: 100, IncludeCandidates: true, PrioritizeAuthority: true}, true)
	timeIt("retrieval FTS rare term", domain.MemoryQuery{Text: "Decision 77", Limit: 100, IncludeCandidates: true}, true)
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	t.Logf("Go heap in use after the queries (whole test process, retained pages included): %.1f MiB", float64(ms.HeapInuse)/(1<<20))
	s := time.Now()
	if _, err := db.pool.Writer.ExecContext(ctx, `INSERT INTO memories_fts(memories_fts) VALUES('optimize')`); err != nil {
		t.Fatal(err)
	}
	t.Logf("FTS optimize %v", time.Since(s).Round(time.Millisecond))
	s = time.Now()
	var pruned int
	mustUpdate(t, db, func(tx store.Tx) error {
		var e error
		pruned, e = tx.Memory().Prune(ctx, "individual", time.Now().Add(time.Hour), 100)
		return e
	})
	t.Logf("prune scan (0 eligible expected, got %d) %v", pruned, time.Since(s).Round(time.Millisecond))
}

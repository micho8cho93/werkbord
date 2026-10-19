package service

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"devboard/internal/integration"
)

// TestAuditTeamMemoryScale reports Team memory latency as a workspace accumulates
// records from several members. AUDIT_TEAM_MEMORIES raises the size.
func TestAuditTeamMemoryScale(t *testing.T) {
	n := 1000
	if v, err := strconv.Atoi(os.Getenv("AUDIT_TEAM_MEMORIES")); err == nil && v > 0 {
		n = v
	} else if os.Getenv("WERKBORD_TEST_STORE") == "rqlite" {
		// The replicated-cluster run shares one database between tests, and the worker tick checks only the 50
		// least recently checked memories of all of it: a thousand more would slow other tests' reconciliation.
		t.Skip("measurement; set AUDIT_TEAM_MEMORIES to run it on the replicated store")
	}
	tm := newTeam(t)
	authors := []Actor{tm.bo, tm.cy}
	start := time.Now()
	var published int
	for i := 0; i < n; i++ {
		in := memoryProposal(authors[i%2], tm.pid())
		in.ClientID = fmt.Sprintf("scale_%d", i)
		in.Content.Summary = fmt.Sprintf("Decision %d: use %s for module %d", i, []string{"queues", "sqlite", "retries", "caching"}[i%4], i%50)
		r, err := tm.svc.ProposeMemory(bg, authors[i%2], in)
		if err != nil {
			t.Fatal(err)
		}
		if i%5 == 0 {
			r, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, memoryCommand(r, "approve"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tm.svc.ChangeMemory(bg, tm.di, r.ID, memoryCommand(r, "publish")); err == nil {
				published++
			}
		}
	}
	insert := time.Since(start)
	timeIt := func(label string, f func() error) {
		var worst, total time.Duration
		const reps = 10
		for i := 0; i < reps; i++ {
			s := time.Now()
			if err := f(); err != nil {
				t.Fatal(label, err)
			}
			d := time.Since(s)
			total += d
			worst = max(worst, d)
		}
		t.Logf("%-38s mean %8.2fms worst %8.2fms", label, float64(total)/reps/1e6, float64(worst)/1e6)
	}
	t.Logf("records=%d (%d published) from 2 authors + 1 reviewer: %v (%.0f proposals/s incl. approvals)", n, published, insert.Round(time.Millisecond), float64(n)/insert.Seconds())
	scope := integration.SharedMemoryScope{WorkspaceID: tm.owner.Workspace.ID, Kind: "project", ProjectID: tm.pid()}
	timeIt("project search, no query (first page)", func() error { _, e := tm.svc.SearchMemories(bg, tm.di, tm.pid(), "", "", ""); return e })
	timeIt("project search, 'sqlite module 7'", func() error { _, e := tm.svc.SearchMemories(bg, tm.di, tm.pid(), "sqlite", "", ""); return e })
	timeIt("project search, no match (full scan)", func() error {
		_, e := tm.svc.SearchMemories(bg, tm.di, tm.pid(), "nothing-matches-this", "", "")
		return e
	})
	timeIt("exact-scope read (first page)", func() error { _, e := tm.svc.ReadMemories(bg, tm.di, scope, "", false); return e })
	timeIt("exact-scope read, published only", func() error { _, e := tm.svc.ReadMemories(bg, tm.di, scope, "", true); return e })
	timeIt("worker tick (ReconcileMemory, 50 max)", func() error { return tm.svc.ReconcileMemory(bg) })
}

package devicestate

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncJournalBoundedAtomicRetriesAndWorkspaceIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sync.db")
	j, err := OpenSyncJournal(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { j.Close() }()
	a := Association{WorkspaceID: "w_one", TeamProjectID: "p", TicketID: "t", MemberID: "m", DeviceID: "d", TaskID: "local", LocalProjectID: "project", Assignment: 1, ClaimAt: time.Now()}
	b := a
	b.WorkspaceID = "w_two"
	for _, v := range []Association{a, b} {
		if err := j.Save(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	observations := make([]domain.Progress, MaxPendingProgress)
	for i := range observations {
		observations[i].Execution = integration.Execution{State: "running"}
	}
	if err := j.Enqueue(ctx, &a, 10, observations); err != nil {
		t.Fatal(err)
	}
	if err := j.Enqueue(ctx, &a, 20, observations[:1]); !errors.Is(err, ErrQueueFull) {
		t.Fatal(err)
	}
	saved, _ := j.Associations(ctx, a.WorkspaceID)
	if saved[0].Cursor != 10 || saved[0].NextSequence != MaxPendingProgress {
		t.Fatal("full queue advanced checkpoint")
	}
	if err := j.Enqueue(ctx, &b, 50, observations[:1]); err != nil {
		t.Fatal("one team's full queue stopped another", err)
	}
	now := time.Now()
	pending, _ := j.Pending(ctx, a.Key(), now)
	if len(pending) != 32 {
		t.Fatalf("unbounded delivery %d", len(pending))
	}
	if err := j.Retry(ctx, pending[0], now); err != nil {
		t.Fatal(err)
	}
	pending, _ = j.Pending(ctx, a.Key(), now)
	if len(pending) != 0 {
		t.Fatal("retry did not block later sequences")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenSyncJournal(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	pending, _ = j.Pending(ctx, a.Key(), now.Add(time.Minute))
	if len(pending) != 32 || pending[0].Attempts != 1 || pending[0].Progress.Sequence != 1 {
		t.Fatal("retry did not survive restart")
	}
	if err := j.Ack(ctx, a.Key(), 30); err != nil {
		t.Fatal(err)
	}
	pending, _ = j.Pending(ctx, a.Key(), now.Add(time.Minute))
	if len(pending) == 0 || pending[0].Progress.Sequence != 31 {
		t.Fatal("acknowledgment did not trim the queue")
	}
	if err := j.Suspend(ctx, &a, "reassigned"); err != nil {
		t.Fatal(err)
	}
	pending, _ = j.Pending(ctx, a.Key(), now.Add(time.Minute))
	if len(pending) != 0 {
		t.Fatal("suspended work retained reports")
	}
	pending, _ = j.Pending(ctx, b.Key(), now.Add(time.Minute))
	if len(pending) != 1 {
		t.Fatal("suspension leaked across teams")
	}
}

package sqlite

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/store"
	"testing"
	"time"
)

func TestRetentionKeepsActiveEvidenceAndSetsReplayFloor(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	p := seedProject(t, db)
	at := now()
	task := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: p.ID, Title: "Work", State: domain.TaskBacklog, CreatedAt: at, UpdatedAt: at}
	terminal := &domain.Run{ID: domain.NewID(domain.PrefixRun), ProjectID: p.ID, TaskID: task.ID, AgentID: "fake", State: domain.RunCompleted, CreatedAt: at, UpdatedAt: at, EndedAt: &at}
	active := *terminal
	active.ID = domain.NewID(domain.PrefixRun)
	active.State = domain.RunRunning
	active.EndedAt = nil
	var removed domain.Event
	err := db.Update(ctx, func(tx store.Tx) error {
		if err := tx.Tasks().Create(ctx, task); err != nil {
			return err
		}
		if err := tx.Runs().Create(ctx, terminal); err != nil {
			return err
		}
		if err := tx.Runs().Create(ctx, &active); err != nil {
			return err
		}
		for _, run := range []string{terminal.ID, active.ID} {
			for _, kind := range []domain.EventType{domain.EventAgentOutput, domain.EventAgentStarted} {
				ev := domain.Event{Type: kind, ProjectID: p.ID, RunID: run, CreatedAt: at.Add(-31 * 24 * time.Hour)}
				if err := tx.Events().Append(ctx, &ev); err != nil {
					return err
				}
				if run == terminal.ID && kind == domain.EventAgentOutput {
					removed = ev
				}
			}
		}
		n, err := tx.Events().Prune(ctx, at)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("removed %d, expected only old terminal transcript", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.View(ctx, func(tx store.Tx) error {
		floor, err := tx.Events().ReplayFloor(ctx)
		if err != nil {
			return err
		}
		if floor != removed.Seq {
			t.Fatal(floor)
		}
		events, err := tx.Events().ListAfterProject(ctx, p.ID, 0, 10)
		if err != nil {
			return err
		}
		if len(events) != 3 {
			t.Fatalf("events=%v", events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

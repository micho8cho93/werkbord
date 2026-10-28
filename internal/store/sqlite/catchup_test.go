package sqlite

import (
	"context"
	"testing"
	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func TestRecentChangesOnlyRecordsTransitionsIntoDone(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	err := db.Update(ctx, func(tx store.Tx) error {
		for _, state := range []string{"todo", "doing", "done", "done", "review", "done"} {
			e, err := domain.NewEvent(domain.EventTaskUpdated, map[string]string{"state": state, "title": "Feature"})
			if err != nil {
				return err
			}
			e.TaskID = "task_1"
			e.ProjectID = "project_1"
			if err = tx.Events().Append(ctx, &e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.View(ctx, func(tx store.Tx) error {
		events, err := tx.Events().RecentChanges(ctx)
		if err != nil {
			return err
		}
		if len(events) != 2 {
			t.Fatalf("got %d completions, want two state transitions", len(events))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

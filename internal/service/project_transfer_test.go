package service

import (
	"context"
	"errors"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
)

func TestProjectMoveAtomicRetryAndArchiveReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := integration.ProjectTransfer{Source: "team:a/p1/personal", Name: "Imported", Repository: "https://github.com/acme/repo", Tickets: []integration.TransferTicket{{Title: "First", Description: "Reviewed text"}, {Title: "Second"}}}
	id, err := f.projects.ImportTransfer(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.projects.ImportTransfer(ctx, in)
	if err != nil || again != id {
		t.Fatalf("retry: %s %v", again, err)
	}
	p, err := f.projects.Get(ctx, id)
	if err != nil || p.HasRepository() || p.RepoPath != "" {
		t.Fatalf("import executed or attached a checkout: %+v %v", p, err)
	}
	snapshot, err := f.projects.ExportTransfer(ctx, id)
	if err != nil || !snapshot.SameText(in) {
		t.Fatalf("export: %+v %v", snapshot, err)
	}
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		tasks, err := tx.Tasks().ListByProject(ctx, id)
		if err != nil {
			return err
		}
		if len(tasks) != 2 {
			t.Fatalf("duplicated import: %d tasks", len(tasks))
		}
		tasks[0].Title = "Edited after review"
		return tx.Tasks().Update(ctx, &tasks[0])
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.projects.ArchiveTransfer(ctx, id, snapshot); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale review archived: %v", err)
	}
	if _, err := f.projects.ImportTransfer(ctx, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("edited destination reused: %v", err)
	}
	snapshot, err = f.projects.ExportTransfer(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.projects.ArchiveTransfer(ctx, id, snapshot); err != nil {
		t.Fatal(err)
	}
	p, err = f.projects.Get(ctx, id)
	if err != nil || p.ArchivedAt == nil {
		t.Fatalf("source not recoverably archived: %+v %v", p, err)
	}
	bad := in
	bad.Source = "different"
	bad.Tickets = []integration.TransferTicket{{Title: "Good"}, {Title: ""}}
	if _, err := f.projects.ImportTransfer(ctx, bad); err == nil {
		t.Fatal("invalid batch accepted")
	}
	ps, err := f.projects.List(ctx)
	if err != nil || len(ps) != 0 {
		t.Fatalf("partial import leaked: %v %v", ps, err)
	}
}

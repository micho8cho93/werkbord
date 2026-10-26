package service

import (
	"errors"
	"testing"

	"werkbord/internal/integration"
	"werkbord/internal/team/domain"
)

func TestProjectMoveAtomicRetryPermissionAndArchiveReview(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Studio", "Owner")
	in := integration.ProjectTransfer{Source: "personal/p1/team:main", Name: "Moved", Repository: "https://github.com/acme/repo", Tickets: []integration.TransferTicket{{Title: "First", Description: "Reviewed text"}, {Title: "Second"}}}
	id, err := w.svc.ImportTransfer(bg, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.svc.ImportTransfer(bg, owner, in)
	if err != nil || again != id {
		t.Fatalf("retry: %s %v", again, err)
	}
	board, err := w.svc.Board(bg, owner, id)
	if err != nil || len(board.Tickets) != 2 {
		t.Fatalf("partial or duplicated import: %+v %v", board, err)
	}
	for _, k := range board.Tickets {
		if k.Status != domain.TicketAvailable || k.AssigneeID != "" || k.Mode() != "human" {
			t.Fatalf("execution/assignment leaked: %+v", k)
		}
	}
	snapshot, err := w.svc.ExportTransfer(bg, owner, id)
	if err != nil || !snapshot.SameText(in) {
		t.Fatalf("export %+v %v", snapshot, err)
	}
	member, _ := w.member(owner, "Other")
	if _, err := w.svc.ExportTransfer(bg, member, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("private project leaked: %v", err)
	}
	changed := "Changed after review"
	if _, err := w.svc.UpdateProject(bg, owner, id, ProjectPatch{Name: &changed}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.ArchiveTransfer(bg, owner, id, snapshot); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale review archived: %v", err)
	}
	if _, err := w.svc.ImportTransfer(bg, owner, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("edited destination reused: %v", err)
	}
	snapshot, err = w.svc.ExportTransfer(bg, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.ArchiveTransfer(bg, owner, id, snapshot); err != nil {
		t.Fatal(err)
	}
	p, err := w.svc.GetProject(bg, owner, id)
	if err != nil || !p.Archived {
		t.Fatalf("source not archived: %+v %v", p, err)
	}
	bad := in
	bad.Source = "different"
	bad.Name = "Bad"
	bad.Tickets = []integration.TransferTicket{{Title: "Good"}, {Title: ""}}
	if _, err := w.svc.ImportTransfer(bg, owner, bad); err == nil {
		t.Fatal("invalid batch accepted")
	}
	ps, err := w.svc.ListProjects(bg, owner)
	if err != nil || len(ps) != 1 {
		t.Fatalf("partial import leaked: %+v %v", ps, err)
	}
}

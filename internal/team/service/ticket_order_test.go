package service

import (
	"math"
	"testing"

	"werkbord/internal/team/domain"
)

func (tm *team) queueOrder() []string {
	tm.t.Helper()
	b, err := tm.svc.Board(bg, tm.owner, tm.pid())
	if err != nil {
		tm.t.Fatal(err)
	}
	out := make([]string, 0, len(b.Tickets))
	for _, k := range b.Tickets {
		out = append(out, k.Title)
	}
	return out
}

func sameOrder(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestTicketsQueueInTheOrderTheyWereCreatedUntilSomeoneMovesThem(t *testing.T) {
	tm := newTeam(t)
	a, b, c := tm.ticket(tm.owner, "First", domain.TicketAvailable), tm.ticket(tm.owner, "Second", domain.TicketAvailable), tm.ticket(tm.owner, "Third", domain.TicketAvailable)
	if !(a.Position < b.Position && b.Position < c.Position) {
		t.Fatalf("new tickets are not appended: %v %v %v", a.Position, b.Position, c.Position)
	}
	sameOrder(t, tm.queueOrder(), "First", "Second", "Third")

	moved, err := tm.svc.ReorderTicket(bg, tm.owner, tm.pid(), c.ID, a.Position-1)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Position != a.Position-1 {
		t.Fatalf("position = %v", moved.Position)
	}
	sameOrder(t, tm.queueOrder(), "Third", "First", "Second")
}

func TestReorderingNeverMakesAnOpenEditStale(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Stay editable", domain.TicketAvailable)
	other := tm.ticket(tm.owner, "Other", domain.TicketAvailable)
	if _, err := tm.svc.ReorderTicket(bg, tm.owner, tm.pid(), k.ID, other.Position+1); err != nil {
		t.Fatal(err)
	}
	after, err := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != k.Version || !after.UpdatedAt.Equal(k.UpdatedAt) {
		t.Fatalf("a reorder changed version %d -> %d or updatedAt", k.Version, after.Version)
	}
	title := "Edited after the reorder"
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{Title: &title, Version: &k.Version}); err != nil {
		t.Fatalf("an edit based on the earlier version must still apply: %v", err)
	}
}

func TestWhoMayReorderTheQueue(t *testing.T) {
	tm := newTeam(t)
	open := tm.ticket(tm.owner, "Nobody holds this", domain.TicketAvailable)
	held := tm.ticket(tm.owner, "Bo holds this", domain.TicketAvailable)
	held, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), held.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The shared order of unclaimed work is for people who may edit tickets.
	_, err = tm.svc.ReorderTicket(bg, tm.bo, tm.pid(), open.ID, 0)
	wantErr(t, err, domain.ErrForbidden)
	if _, err := tm.svc.ReorderTicket(bg, tm.owner, tm.pid(), open.ID, 0); err != nil {
		t.Fatal(err)
	}

	// A ticket someone holds is ordered by its owner, and by editors; not by another contributor.
	if _, err := tm.svc.ReorderTicket(bg, tm.bo, tm.pid(), held.ID, 50); err != nil {
		t.Fatalf("the owner of a claimed ticket may order it: %v", err)
	}
	_, err = tm.svc.ReorderTicket(bg, tm.cy, tm.pid(), held.ID, 60)
	wantErr(t, err, domain.ErrForbidden)
	if _, err := tm.svc.ReorderTicket(bg, tm.owner, tm.pid(), held.ID, 70); err != nil {
		t.Fatalf("an editor may order work someone holds: %v", err)
	}
	got, err := tm.svc.GetTicket(bg, tm.owner, tm.pid(), held.ID)
	if err != nil || got.Position != 70 || got.AssigneeID != tm.bo.Member.ID {
		t.Fatalf("held = %+v, %v", got, err)
	}
}

func TestFinishedAndInvalidMovesAreRefused(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Done and dusted", domain.TicketAvailable)
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 2e12, -2e12} {
		_, err := tm.svc.ReorderTicket(bg, tm.owner, tm.pid(), k.ID, bad)
		wantErr(t, err, domain.ErrInvalid)
	}
	archived, err := tm.svc.ArchiveTicket(bg, tm.owner, tm.pid(), k.ID, k.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ReorderTicket(bg, tm.owner, tm.pid(), archived.ID, 5)
	wantErr(t, err, domain.ErrConflict)
	_, err = tm.svc.ReorderTicket(bg, tm.owner, tm.pid(), "no-such-ticket", 5)
	wantErr(t, err, domain.ErrNotFound)
}

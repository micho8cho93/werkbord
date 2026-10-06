package service

import (
	"testing"

	"devboard/internal/team/domain"
)

func TestTicketArchivePermissionsHistoryAndRestore(t *testing.T) {
	tm := newTeam(t)
	k := tm.claimed(tm.bo, "Retain the work")
	commits := []domain.Commit{{SHA: shaOne, Subject: "Archived implementation"}}
	k, err := tm.svc.ReportGit(bg, tm.bo, tm.pid(), k.ID, GitReport{Commits: &commits, PullRequest: &domain.PullRequest{URL: prURL, Number: 7, State: domain.PROpen, Mergeable: domain.MergeConflicting}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ArchiveTicket(bg, tm.cy, tm.pid(), k.ID, k.Version, true)
	wantErr(t, err, domain.ErrForbidden)
	closed, err := tm.svc.ArchiveTicket(bg, tm.bo, tm.pid(), k.ID, k.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if closed.ArchivedAt == nil || closed.Status != k.Status || closed.AssigneeID != k.AssigneeID || closed.Description != k.Description {
		t.Fatalf("closed = %+v", closed)
	}
	b, err := tm.svc.Board(bg, tm.owner, tm.pid())
	if err != nil || len(b.Tickets) != 0 || len(b.Archived) != 1 {
		t.Fatalf("board = %+v, %v", b, err)
	}
	got, err := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if err != nil || len(got.Commits) != 1 || got.Commits[0].SHA != shaOne || got.PullRequest == nil || got.PullRequest.URL != prURL {
		t.Fatalf("archived reports = %+v, %v", got, err)
	}
	repo, err := tm.svc.RepositoryState(bg, tm.owner, tm.pid())
	if err != nil || len(repo.Attention) != 0 {
		t.Fatalf("closed work still requests attention: %+v, %v", repo, err)
	}
	for _, b := range repo.Branches {
		if b.TicketID == k.ID && b.Active {
			t.Fatal("closed branch counted as active")
		}
	}
	_, err = tm.svc.ClaimTicket(bg, tm.cy, tm.pid(), k.ID)
	wantErr(t, err, domain.ErrConflict)
	_, err = tm.svc.ReleaseTicket(bg, tm.bo, tm.pid(), k.ID)
	wantErr(t, err, domain.ErrConflict)
	_, err = tm.svc.ArchiveTicket(bg, tm.bo, tm.pid(), k.ID, k.Version, false)
	wantErr(t, err, domain.ErrConflict)
	restored, err := tm.svc.ArchiveTicket(bg, tm.bo, tm.pid(), k.ID, closed.Version, false)
	if err != nil || restored.ArchivedAt != nil || restored.Status != k.Status {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
	kinds := tm.activityKinds()
	if kinds[len(kinds)-2] != domain.ActTicketArchived || kinds[len(kinds)-1] != domain.ActTicketRestored {
		t.Fatalf("activity = %v", kinds)
	}
}

func TestTeamClearDonePreservesDetailsAndDoesNotHideOpenWork(t *testing.T) {
	tm := newTeam(t)
	k := tm.claimed(tm.bo, "Finished")
	k, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{})
	if err != nil {
		t.Fatal(err)
	}
	k, err = tm.svc.CompleteTicket(bg, tm.owner, tm.pid(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	open := tm.ticket(tm.owner, "Still available", domain.TicketAvailable)
	_, err = tm.svc.ArchiveDone(bg, tm.bo, tm.pid())
	wantErr(t, err, domain.ErrForbidden)
	n, err := tm.svc.ArchiveDone(bg, tm.owner, tm.pid())
	if err != nil || n != 1 {
		t.Fatalf("clear = %d, %v", n, err)
	}
	b, err := tm.svc.Board(bg, tm.owner, tm.pid())
	if err != nil || len(b.Tickets) != 1 || b.Tickets[0].ID != open.ID || len(b.Archived) != 1 {
		t.Fatalf("board = %+v, %v", b, err)
	}
	got, err := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if err != nil || got.ArchivedAt == nil || got.Description != k.Description || got.CompletedAt == nil {
		t.Fatalf("history = %+v, %v", got, err)
	}
	n, err = tm.svc.ArchiveDone(bg, tm.owner, tm.pid())
	if err != nil || n != 0 {
		t.Fatalf("repeat = %d, %v", n, err)
	}
}

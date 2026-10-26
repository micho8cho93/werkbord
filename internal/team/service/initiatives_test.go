package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"werkbord/internal/team/domain"
)

func TestInitiativeCoordinatesExistingWorkAndRevokesWholeScope(t *testing.T) {
	tm := newTeam(t)
	other := tm.project2()
	if e := tm.svc.AddProjectMember(bg, tm.owner, other.ID, tm.bo.Member.ID, domain.ProjectContributor); e != nil {
		t.Fatal(e)
	}
	front := tm.working(tm.bo, "Frontend auth")
	back, e := tm.svc.CreateTicket(bg, tm.owner, other.ID, TicketInput{Title: "Backend auth", Status: domain.TicketAvailable})
	if e != nil {
		t.Fatal(e)
	}
	back, e = tm.svc.ClaimTicket(bg, tm.bo, other.ID, back.ID)
	if e != nil {
		t.Fatal(e)
	}
	ref1 := domain.InitiativeTicket{ProjectID: tm.pid(), TicketID: front.ID}
	ref2 := domain.InitiativeTicket{ProjectID: other.ID, TicketID: back.ID}
	i, e := tm.svc.SaveInitiative(bg, tm.bo, "", domain.Initiative{Title: "Authentication Upgrade", Objective: "Coordinate compatible auth APIs", ProjectIDs: []string{tm.pid(), other.ID}, Tickets: []domain.InitiativeTicket{ref1, ref2}, Dependencies: []domain.InitiativeDependency{{Before: ref2, After: ref1}}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := tm.svc.Initiatives(bg, tm.bo, tm.pid(), i.ID)
	if e != nil || len(got) != 1 || len(got[0].Blockers) != 1 || got[0].Done != 0 {
		t.Fatal(got, e)
	}
	if got[0].Tickets[0].AssigneeID != tm.bo.Member.ID || got[0].Tickets[0].Version != front.Version {
		t.Fatal("initiative rewrote ticket")
	}
	// A member of just the frontend sees no title, ID, counts, owner or dependency.
	hidden, e := tm.svc.Initiatives(bg, tm.cy, "", "")
	if e != nil || len(hidden) != 0 {
		t.Fatal("partial access leaked initiative", hidden, e)
	}
	_, e = tm.svc.Initiatives(bg, tm.cy, "", i.ID)
	wantErr(t, e, domain.ErrNotFound)
	// Updating coordination is guarded by the saved version and does not move work.
	stale := i
	i.Status = "paused"
	i, e = tm.svc.SaveInitiative(bg, tm.bo, i.ID, i)
	if e != nil {
		t.Fatal(e)
	}
	_, e = tm.svc.SaveInitiative(bg, tm.bo, i.ID, stale)
	wantErr(t, e, domain.ErrConflict)
	cycle := i
	cycle.Dependencies = append(append([]domain.InitiativeDependency{}, i.Dependencies...), domain.InitiativeDependency{Before: ref1, After: ref2})
	_, e = tm.svc.SaveInitiative(bg, tm.bo, i.ID, cycle)
	wantErr(t, e, domain.ErrInvalid)
	// Git observations refresh the projection without touching the Initiative version.
	_, e = tm.svc.ReportGit(bg, tm.bo, tm.pid(), front.ID, GitReport{PullRequest: &domain.PullRequest{URL: prURL, State: domain.PROpen, Mergeable: domain.MergeConflicting}})
	if e != nil {
		t.Fatal(e)
	}
	got, e = tm.svc.Initiatives(bg, tm.bo, "", i.ID)
	if e != nil || got[0].OpenPRs != 1 || len(got[0].Activity) == 0 || got[0].Initiative.Version != i.Version {
		t.Fatal(got, e)
	}
	// Completed is a coordinator's status, never a substitute for verified ticket progress.
	i.Status = "completed"
	if _, e = tm.svc.SaveInitiative(bg, tm.bo, i.ID, i); e != nil {
		t.Fatal(e)
	}
	got, e = tm.svc.Initiatives(bg, tm.bo, "", i.ID)
	if e != nil || got[0].Done != 0 {
		t.Fatal(got, e)
	}
	if e = tm.svc.RemoveProjectMember(bg, tm.owner, other.ID, tm.bo.Member.ID); e != nil {
		t.Fatal(e)
	}
	hidden, e = tm.svc.Initiatives(bg, tm.bo, "", "")
	if e != nil || len(hidden) != 0 {
		t.Fatal("revocation leaked initiative", hidden, e)
	}
	_, e = tm.svc.SaveInitiative(bg, tm.bo, i.ID, i)
	wantErr(t, e, domain.ErrNotFound)
}

func TestInitiativeMemoryClassificationAndCredentialRevocation(t *testing.T) {
	tm, now := memoryGitProject(t)
	proposal := memoryProposal(tm.bo, tm.pid())
	proposal.Content.Classification = "restricted"
	m, e := tm.svc.ProposeMemory(bg, tm.bo, proposal)
	if e != nil {
		t.Fatal(e)
	}
	m, e = tm.svc.ChangeMemory(bg, tm.owner, m.ID, memoryCommand(m, "approve"))
	if e != nil {
		t.Fatal(e)
	}
	m, e = tm.svc.ChangeMemory(bg, tm.owner, m.ID, memoryCommand(m, "publish"))
	if e != nil {
		t.Fatal(e)
	}
	i, e := tm.svc.SaveInitiative(bg, tm.owner, "", domain.Initiative{Title: "Auth", Objective: "Upgrade authentication", ProjectIDs: []string{tm.pid()}, Memories: []domain.InitiativeMemory{{ProjectID: tm.pid(), MemoryID: m.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	views, e := tm.svc.Initiatives(bg, tm.bo, "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(views)
	if strings.Contains(string(raw), m.ID) || strings.Contains(string(raw), m.Content.Summary) {
		t.Fatalf("restricted memory leaked: %s", raw)
	}
	views, e = tm.svc.Initiatives(bg, tm.owner, "", i.ID)
	if e != nil || len(views[0].Memories) != 1 {
		t.Fatal(views, e)
	}
	*now = now.Add(48 * time.Hour)
	forged := tm.bo
	forged.MCPGrantID = "grant"
	_, e = tm.svc.Initiatives(bg, forged, "", "")
	wantErr(t, e, domain.ErrForbidden)
	if _, e = tm.svc.ReissueToken(bg, tm.owner, tm.bo.Member.ID); e != nil {
		t.Fatal(e)
	}
	_, e = tm.svc.Initiatives(bg, tm.bo, "", "")
	wantErr(t, e, domain.ErrUnauthenticated)
}

func TestInitiativeRevalidatesStalePRsAndReportedOverlappingBranches(t *testing.T) {
	tm := newTeam(t)
	now := time.Now().UTC()
	tm.svc.SetClock(func() time.Time { return now })
	first := tm.working(tm.bo, "Auth upgrade")
	second := tm.working(tm.cy, "Other auth work")
	for _, work := range []struct {
		a Actor
		k domain.Ticket
	}{{tm.bo, first}, {tm.cy, second}} {
		_, e := tm.svc.ReportGit(bg, work.a, tm.pid(), work.k.ID, GitReport{Branch: work.k.Branch, State: &BranchState{Files: []string{"auth/session.go"}}})
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e := tm.svc.ReportGit(bg, tm.bo, tm.pid(), first.ID, GitReport{State: &BranchState{Files: []string{"auth/session.go"}}, PullRequest: &domain.PullRequest{URL: prURL, State: domain.PROpen, Mergeable: domain.MergeConflicting}})
	if e != nil {
		t.Fatal(e)
	}
	i, e := tm.svc.SaveInitiative(bg, tm.owner, "", domain.Initiative{Title: "Auth", Objective: "Coordinate auth", ProjectIDs: []string{tm.pid()}, Tickets: []domain.InitiativeTicket{{ProjectID: tm.pid(), TicketID: first.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	views, e := tm.svc.Initiatives(bg, tm.bo, "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(views[0].Blockers)
	if !strings.Contains(string(raw), "reported file overlap") || !strings.Contains(string(raw), "reported PR conflict") {
		t.Fatal(string(raw))
	}
	now = now.Add(25 * time.Hour)
	views, e = tm.svc.Initiatives(bg, tm.bo, "", i.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = json.Marshal(views[0].Blockers)
	if !strings.Contains(string(raw), "PR report needs revalidation") || views[0].OpenPRs != 1 {
		t.Fatal(views)
	}
}

func TestInitiativeRespectsExistingProjectDependencies(t *testing.T) {
	tm := newTeam(t)
	prerequisite := tm.ticket(tm.owner, "Required API", domain.TicketAvailable)
	dependent, e := tm.svc.CreateTicket(bg, tm.owner, tm.pid(), TicketInput{Title: "API consumer", Status: domain.TicketAvailable, Dependencies: []string{prerequisite.ID}})
	if e != nil {
		t.Fatal(e)
	}
	i, e := tm.svc.SaveInitiative(bg, tm.bo, "", domain.Initiative{Title: "API upgrade", Objective: "Coordinate APIs", ProjectIDs: []string{tm.pid()}, Tickets: []domain.InitiativeTicket{{ProjectID: tm.pid(), TicketID: dependent.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	views, e := tm.svc.Initiatives(bg, tm.cy, "", i.ID)
	if e != nil || len(views) != 1 || len(views[0].Blockers) != 1 {
		t.Fatal(views, e)
	}
	for _, next := range views[0].NextActions {
		if strings.HasPrefix(next, dependent.Key+": claim") {
			t.Fatal("suggested blocked claim", next)
		}
	}
	_, e = tm.svc.SaveInitiative(bg, tm.cy, i.ID, i)
	wantErr(t, e, domain.ErrForbidden)
	other, _ := tm.workspace("Other workspace", "Other owner")
	_, e = tm.svc.Initiatives(bg, other, "", i.ID)
	wantErr(t, e, domain.ErrNotFound)
}

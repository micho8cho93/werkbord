package service

import (
	"fmt"
	"strings"
	"testing"

	"devboard/internal/team/domain"
)

func TestSearchStaysWithinCurrentProjectMembershipAndWorkspace(t *testing.T) {
	tm := newTeam(t)
	public := tm.ticket(tm.owner, "Shared needle", domain.TicketAvailable)
	private := tm.project2Named("Private needle")
	if _, err := tm.svc.CreateTicket(bg, tm.owner, private.ID, TicketInput{Title: "Hidden needle"}); err != nil {
		t.Fatal(err)
	}
	foreign, _ := tm.workspace("Other company", "Other owner")
	other := tm.world.project(foreign, "Foreign needle")
	if _, err := tm.svc.CreateTicket(bg, foreign, other.ID, TicketInput{Title: "Foreign needle"}); err != nil {
		t.Fatal(err)
	}
	find := func(a Actor) SearchResult {
		t.Helper()
		r, err := tm.svc.Search(bg, a, "needle")
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := find(tm.bo); len(r.Projects) != 0 || len(r.Tickets) != 1 || r.Tickets[0].ID != public.ID {
		t.Fatalf("member sees private or foreign work: %+v", r)
	}
	if r := find(tm.owner); len(r.Projects) != 1 || r.Projects[0].ID != private.ID || len(r.Tickets) != 2 {
		t.Fatalf("owner sees foreign work or cannot find own workspace: %+v", r)
	}
	if err := tm.svc.AddProjectMember(bg, tm.owner, private.ID, tm.bo.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	if r := find(tm.bo); len(r.Projects) != 1 || len(r.Tickets) != 2 {
		t.Fatalf("new membership missing from search: %+v", r)
	}
	if err := tm.svc.RemoveProjectMember(bg, tm.owner, private.ID, tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	if r := find(tm.bo); len(r.Projects) != 0 || len(r.Tickets) != 1 {
		t.Fatalf("removed membership still searchable: %+v", r)
	}
}

func TestSearchFindsTicketContextKeysAndArchiveWithLiteralQueries(t *testing.T) {
	tm := newTeam(t)
	k, err := tm.svc.CreateTicket(bg, tm.owner, tm.pid(), TicketInput{Title: "Improve the checkout", Description: "Handle declined cards", Requirements: "Use the billing webhook", Status: domain.TicketAvailable})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"IMPROVE checkout", "declined", "billing webhook", k.Key, "shop checkout"} {
		r, err := tm.svc.Search(bg, tm.bo, query)
		if err != nil || len(r.Tickets) != 1 || r.Tickets[0].ID != k.ID || r.Tickets[0].Project.Name != "Shop" {
			t.Fatalf("%q: %+v %v", query, r, err)
		}
	}
	archived, err := tm.svc.ArchiveTicket(bg, tm.owner, tm.pid(), k.ID, k.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Archived: &yes}); err != nil {
		t.Fatal(err)
	}
	r, err := tm.svc.Search(bg, tm.bo, archived.Key)
	if err != nil || len(r.Tickets) != 1 || !r.Tickets[0].Archived || !r.Tickets[0].Project.Archived {
		t.Fatalf("archived ticket or project lost: %+v %v", r, err)
	}
	no := false
	if _, err := tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Archived: &no}); err != nil {
		t.Fatal(err)
	}
	literal := tm.ticket(tm.owner, "Handle 100%_coverage", domain.TicketAvailable)
	for _, query := range []string{"%_", "100%_coverage"} {
		r, err := tm.svc.Search(bg, tm.bo, query)
		if err != nil || len(r.Tickets) != 1 || r.Tickets[0].ID != literal.ID {
			t.Fatalf("wildcards treated as query syntax: %+v %v", r, err)
		}
	}
	for _, query := range []string{"' OR 1=1 --", "no such thing", "   "} {
		r, err := tm.svc.Search(bg, tm.bo, query)
		if err != nil || len(r.Tickets) != 0 || len(r.Projects) != 0 || r.Tickets == nil || r.Projects == nil {
			t.Fatalf("%q: %+v %v", query, r, err)
		}
	}
	_, err = tm.svc.Search(bg, tm.bo, strings.Repeat("é", MaxSearchQuery+1))
	wantErr(t, err, domain.ErrInvalid)
}

func TestSearchLimitsResultsAndRanksAnExactTicketKeyFirst(t *testing.T) {
	tm := newTeam(t)
	first := tm.ticket(tm.owner, "Bulk ticket 0", domain.TicketAvailable)
	for i := 1; i < searchLimit+5; i++ {
		tm.ticket(tm.owner, fmt.Sprintf("Bulk ticket %d", i), domain.TicketAvailable)
	}
	r, err := tm.svc.Search(bg, tm.bo, "bulk")
	if err != nil || len(r.Tickets) != searchLimit || !r.MoreTickets {
		t.Fatalf("unbounded or unmarked ticket results: %+v %v", r, err)
	}
	r, err = tm.svc.Search(bg, tm.bo, first.Key)
	if err != nil || len(r.Tickets) == 0 || r.Tickets[0].ID != first.ID {
		t.Fatalf("exact key buried behind partial matches: %+v %v", r, err)
	}
	for i := 0; i < searchLimit+1; i++ {
		tm.project2Named(fmt.Sprintf("Bulk project %d", i))
	}
	r, err = tm.svc.Search(bg, tm.owner, "bulk project")
	if err != nil || len(r.Projects) != searchLimit || !r.MoreProjects {
		t.Fatalf("unbounded or unmarked project results: %+v %v", r, err)
	}
	exact := tm.project2Named("Bulk")
	yes := true
	if _, err := tm.svc.UpdateProject(bg, tm.owner, exact.ID, ProjectPatch{Archived: &yes}); err != nil {
		t.Fatal(err)
	}
	r, err = tm.svc.Search(bg, tm.owner, "bulk")
	if err != nil || len(r.Projects) == 0 || r.Projects[0].ID != exact.ID {
		t.Fatalf("exact archived project hidden by partial matches: %+v %v", r, err)
	}
}

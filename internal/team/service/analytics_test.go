package service

import (
	"errors"
	"testing"
	"time"

	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
)

func TestCoordinationAnalyticsAggregation(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	data := store.AnalyticsData{Tickets: []domain.Ticket{{ID: "a", ProjectID: "p", CreatedAt: start, LabelIDs: []string{"bug", "feature"}}, {ID: "b", ProjectID: "p", CreatedAt: start.Add(-24 * time.Hour)}}, Completions: []domain.Activity{{TicketID: "a", ProjectID: "p", CreatedAt: start.Add(time.Hour)}, {TicketID: "a", ProjectID: "p", CreatedAt: start.Add(2 * time.Hour)}, {TicketID: "b", ProjectID: "p", CreatedAt: start.Add(time.Hour)}}}
	out := aggregateCoordination(data, []domain.Label{{ID: "bug", Name: "Bug"}, {ID: "feature", Name: "Feature"}}, start, end)
	if out.Created != 1 || out.Completed != 2 || out.HumanCompletionDecisions != 3 || out.DurationSamples != 2 || *out.AverageTicketSeconds != 13*3600 || *out.MedianTicketSeconds != 13*3600 {
		t.Fatalf("aggregation %+v", out)
	}
	if len(out.Categories) != 3 || out.Categories[0].Created != 1 || out.Categories[1].Completed != 1 {
		t.Fatalf("labels %+v", out.Categories)
	}
	empty := aggregateCoordination(store.AnalyticsData{}, nil, start, end)
	if empty.AverageTicketSeconds != nil || empty.MedianTicketSeconds != nil || empty.Created != 0 || len(empty.Days) != 8 {
		t.Fatalf("missing %+v", empty)
	}
	boundary := aggregateCoordination(store.AnalyticsData{
		Tickets:     []domain.Ticket{{ID: "at-end", ProjectID: "p", CreatedAt: end}},
		Completions: []domain.Activity{{TicketID: "at-end", ProjectID: "p", CreatedAt: end}},
	}, nil, start, end)
	if boundary.Created != 1 || boundary.Completed != 1 || boundary.Days[len(boundary.Days)-1].Completed != 1 {
		t.Fatalf("snapshot millisecond excluded: %+v", boundary)
	}
}

func TestCoordinationAnalyticsPermissionsAndReadOnly(t *testing.T) {
	tm := newTeam(t)
	visible := tm.ticket(tm.owner, "private title", domain.TicketAvailable)
	secret := tm.world.project(tm.owner, "Secret")
	_, err := tm.svc.CreateTicket(bg, tm.owner, secret.ID, TicketInput{Title: "secret task"})
	if err != nil {
		t.Fatal(err)
	}
	otherOwner, _ := tm.world.workspace("Other", "Other owner")
	other := tm.world.project(otherOwner, "Other project")
	_, err = tm.svc.CreateTicket(bg, otherOwner, other.ID, TicketInput{Title: "other workspace"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := tm.svc.ListActivity(bg, tm.owner, tm.pid(), 0, 200)
	all, err := tm.svc.CoordinationAnalytics(bg, tm.bo, "", 7)
	if err != nil || all.Created != 1 || len(all.Projects) != 1 || all.Projects[0].ID != tm.pid() {
		t.Fatalf("visible only: %+v %v", all, err)
	}
	second := tm.world.project(tm.owner, "Second visible")
	if err = tm.svc.AddProjectMember(bg, tm.owner, second.ID, tm.bo.Member.ID, domain.ProjectReviewer); err != nil {
		t.Fatal(err)
	}
	filtered, err := tm.svc.CoordinationAnalytics(bg, tm.bo, tm.pid(), 7)
	if err != nil || len(filtered.Projects) != 2 {
		t.Fatalf("project filter should retain all visible project choices: %+v %v", filtered.Projects, err)
	}
	if _, err = tm.svc.CoordinationAnalytics(bg, tm.bo, secret.ID, 7); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("hidden project: %v", err)
	}
	if _, err = tm.svc.CoordinationAnalytics(bg, tm.owner, other.ID, 7); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross workspace: %v", err)
	}
	after, _ := tm.svc.ListActivity(bg, tm.owner, tm.pid(), 0, 200)
	if len(before) != len(after) {
		t.Fatal("analytics appended activity")
	}
	var projection store.AnalyticsData
	err = tm.world.db.View(bg, func(tx store.Tx) error {
		var e error
		projection, e = tx.AnalyticsData(bg, tm.owner.Workspace.ID, []string{tm.pid()}, time.Now().Add(-24*time.Hour), time.Now().Add(time.Hour), 1)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Tickets) != 1 || projection.Tickets[0].ID != visible.ID || projection.Tickets[0].Title != "" || projection.Tickets[0].AssigneeID != "" {
		t.Fatalf("projection %+v", projection)
	}
	if err = tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	if err = tm.svc.RemoveProjectMember(bg, tm.owner, second.ID, tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	removed, err := tm.svc.CoordinationAnalytics(bg, tm.bo, "", 7)
	if err != nil || removed.Created != 0 || len(removed.Projects) != 0 {
		t.Fatalf("removed member %+v %v", removed, err)
	}
}

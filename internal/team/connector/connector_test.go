package connector

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/service"
	"path/filepath"
	"testing"
	"time"
)

type localStub struct {
	projects []localwerkbord.Project
	feed     integration.Feed
	snapshot integration.Snapshot
}

func (l *localStub) IntegrationProjects(context.Context) ([]localwerkbord.Project, error) {
	return l.projects, nil
}
func (l *localStub) Import(context.Context, integration.Import) (integration.Imported, error) {
	panic("unexpected import")
}
func (l *localStub) Events(context.Context, string, string, int64) (integration.Feed, error) {
	return l.feed, nil
}
func (l *localStub) Snapshot(context.Context, string, string) (integration.Snapshot, error) {
	return l.snapshot, nil
}
func TestSelectionRequiresCanonicalIdentityAndExplicitAmbiguity(t *testing.T) {
	ctx := context.Background()
	l := &localStub{projects: []localwerkbord.Project{{ID: "p_one", Remotes: []string{"git@github.com:acme/repo.git"}}, {ID: "p_two", Remotes: []string{"https://github.com/acme/repo"}}, {ID: "p_wrong", Remotes: []string{"https://github.com/other/repo"}}}}
	c := Connector{Local: l, Projects: map[string]string{"team": ""}}
	p := service.ProjectRef{ID: "team", Repository: "https://github.com/acme/repo"}
	if _, err := c.choose(ctx, p); err == nil {
		t.Fatal("ambiguous match silently selected")
	}
	c.Projects["team"] = "p_two"
	id, err := c.choose(ctx, p)
	if err != nil || id != "p_two" {
		t.Fatal(id, err)
	}
	c.Projects["team"] = "p_wrong"
	if _, err := c.choose(ctx, p); err == nil {
		t.Fatal("explicit choice bypassed repository check")
	}
}
func TestObservationRejectsDisorderedEventsAndRecoversRetention(t *testing.T) {
	ctx := context.Background()
	j, err := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	a := devicestate.Association{WorkspaceID: "w", TeamProjectID: "p", TicketID: "t", MemberID: "m", DeviceID: "d", TaskID: "local", LocalProjectID: "project", Cursor: 5}
	if err := j.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	running := integration.Execution{State: "running"}
	done := integration.Execution{State: "completed", Outcome: "completed"}
	l := &localStub{feed: integration.Feed{Schema: integration.Schema, Cursor: 10, Events: []integration.Event{{Seq: 9, Execution: &running}, {Seq: 8, Execution: &done}}}, snapshot: integration.Snapshot{Schema: integration.Schema, Cursor: 10, Execution: done}}
	c := Connector{Local: l, Journal: j}
	if err := c.observe(ctx, &a); err == nil {
		t.Fatal("disordered feed accepted")
	}
	if a.Cursor != 5 || a.NextSequence != 0 {
		t.Fatal("invalid feed advanced journal")
	}
	l.feed = integration.Feed{Schema: integration.Schema, Cursor: 10, Reset: true}
	if err := c.observe(ctx, &a); err != nil {
		t.Fatal(err)
	}
	p, _ := j.Pending(ctx, a.Key(), time.Now())
	if len(p) != 1 || p[0].Progress.Execution.State != "completed" || a.Cursor != 10 {
		t.Fatal("retention gap failed to reconcile snapshot")
	}
	// A newer snapshot must not jump past lifecycle events still being paged.
	l.feed = integration.Feed{Schema: integration.Schema, Cursor: 11, Events: []integration.Event{{Seq: 11, Execution: &running}}}
	l.snapshot.Cursor = 12
	if err := c.observe(ctx, &a); err != nil {
		t.Fatal(err)
	}
	p, _ = j.Pending(ctx, a.Key(), time.Now())
	if len(p) != 2 || p[1].Progress.Execution.State != "running" || a.Cursor != 11 {
		t.Fatal("snapshot skipped durable events")
	}
}

func TestObservationSnapshotSurvivesUnrelatedControllerWrites(t *testing.T) {
	ctx := context.Background()
	j, err := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	a := devicestate.Association{WorkspaceID: "w", TeamProjectID: "p", TicketID: "t", MemberID: "m", DeviceID: "d", TaskID: "local", LocalProjectID: "project", Cursor: 5}
	if err := j.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	l := &localStub{snapshot: integration.Snapshot{Schema: integration.Schema, Cursor: 10, Execution: integration.Execution{State: "running", RunnerOnline: true, RunnerAvailable: true}}, feed: integration.Feed{Schema: integration.Schema, Cursor: 11}}
	c := Connector{Local: l, Journal: j}
	if err := c.observe(ctx, &a); err != nil {
		t.Fatal(err)
	}
	pending, _ := j.Pending(ctx, a.Key(), time.Now())
	if len(pending) != 1 || !pending[0].Progress.Execution.RunnerOnline {
		t.Fatal("unrelated writes starved current availability")
	}
	// Newer lifecycle metadata supersedes an older snapshot, even while Git lookup runs.
	done := integration.Execution{State: "completed", Outcome: "completed"}
	l.snapshot.Cursor = 12
	l.feed = integration.Feed{Schema: integration.Schema, Cursor: 13, Events: []integration.Event{{Seq: 13, Execution: &done}}}
	if err := c.observe(ctx, &a); err != nil {
		t.Fatal(err)
	}
	pending, _ = j.Pending(ctx, a.Key(), time.Now())
	if len(pending) != 2 || pending[1].Progress.Execution.State != "completed" {
		t.Fatal("older snapshot regressed completed run")
	}
}

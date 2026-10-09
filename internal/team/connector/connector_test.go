package connector

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
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

type workspaceStub struct{ work service.MyWork }

func (w *workspaceStub) Me(context.Context) (hostclient.Me, error) {
	var m hostclient.Me
	m.Member.ID, m.Workspace.ID, m.Workspace.Name = "m", "w", "Northstar"
	return m, nil
}
func (w *workspaceStub) Do(_ context.Context, method, path string, _ any, out any) error {
	if method == "GET" && path == "/my-work" {
		*(out.(*service.MyWork)) = w.work
	}
	return nil
}
func (w *workspaceStub) Handoff(_ context.Context, p, t string) (hostclient.Handoff, error) {
	var h hostclient.Handoff
	h.Ticket.ID, h.Ticket.Key, h.Ticket.Title, h.Project.ID, h.For.ID = t, "WB-1", "Improve sign-in", p, "m"
	h.Git.Repository, h.Git.Branch, h.Prompt = "https://github.com/acme/"+p, "wb-1-sign-in", "Do it"
	return h, nil
}

type importingLocal struct {
	localStub
	imported []integration.Import
	waiting  []integration.WaitingSet
}

func (l *importingLocal) Import(_ context.Context, in integration.Import) (integration.Imported, error) {
	l.imported = append(l.imported, in)
	return integration.Imported{Schema: integration.Schema, ProjectID: in.ProjectID, TaskID: "task_1"}, nil
}
func (l *importingLocal) ReportWaiting(_ context.Context, set integration.WaitingSet) error {
	l.waiting = append(l.waiting, set)
	return set.Valid()
}

func TestTickImportsMatchedTicketsAndReportsTheOnesWaitingForARepository(t *testing.T) {
	ctx := context.Background()
	j, err := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	claimed := time.Now()
	held := func(project, repo, ticket, key string) service.WorkItem {
		return service.WorkItem{Project: service.ProjectRef{ID: project, Repository: repo}, Ticket: domain.Ticket{ID: ticket, ProjectID: project, Key: key, Title: "Title " + key, AssigneeID: "m", Status: domain.TicketInProgress, ClaimedAt: &claimed, Assignment: 1}}
	}
	ws := &workspaceStub{work: service.MyWork{InProgress: []service.WorkItem{
		held("shop", "https://github.com/acme/shop", "t1", "WB-1"),
		held("billing", "https://github.com/acme/billing", "t2", "BI-2"),
		held("off", "https://github.com/acme/off", "t3", "OF-3"),
	}}}
	l := &importingLocal{localStub: localStub{projects: []localwerkbord.Project{{ID: "local_shop", Remotes: []string{"git@github.com:acme/shop.git"}}}, snapshot: integration.Snapshot{Schema: integration.Schema, Execution: integration.Execution{State: "not_started"}}, feed: integration.Feed{Schema: integration.Schema}}}
	c := Connector{WorkspaceID: "w", MemberID: "m", DeviceID: "d", Host: ws, Local: l, Journal: j, Projects: map[string]string{"shop": "", "billing": ""}}
	_ = c.Tick(ctx)
	if len(l.imported) != 1 || l.imported[0].ProjectID != "local_shop" || l.imported[0].SourceRef != Source("w", "shop", "t1", "m") {
		t.Fatalf("imports: %+v", l.imported)
	}
	if st := c.Status["shop:t1"]; st.TaskID != "task_1" || st.LocalProjectID != "local_shop" || st.Waiting {
		t.Fatalf("imported status: %+v", st)
	}
	if st := c.Status["billing:t2"]; !st.Waiting || st.TaskID != "" {
		t.Fatalf("waiting status: %+v", st)
	}
	if _, ok := c.Status["off:t3"]; ok {
		t.Fatal("a project that is not synchronized has a status")
	}
	if len(l.waiting) != 1 || l.waiting[0].Source != integration.SourcePrefix+"w:" || len(l.waiting[0].Items) != 1 {
		t.Fatalf("waiting: %+v", l.waiting)
	}
	if it := l.waiting[0].Items[0]; it.Repository != "https://github.com/acme/billing" || it.Title != "BI-2: Title BI-2" || it.From != "Northstar" {
		t.Fatalf("waiting item: %+v", it)
	}
	// Once the repository is added locally, the next tick imports it and reports nothing waiting.
	l.projects = append(l.projects, localwerkbord.Project{ID: "local_billing", Remotes: []string{"https://github.com/acme/billing.git"}})
	_ = c.Tick(ctx)
	if len(l.imported) != 2 || l.imported[1].ProjectID != "local_billing" || len(l.waiting) != 2 || len(l.waiting[1].Items) != 0 {
		t.Fatalf("after linking: %+v %+v", l.imported, l.waiting)
	}
	if st := c.Status["billing:t2"]; st.Waiting || st.TaskID == "" {
		t.Fatalf("linked status: %+v", st)
	}
}

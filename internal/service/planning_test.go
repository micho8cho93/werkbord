package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/planning"
)

func labelsOf(f *fixture) *Labels { return &Labels{Deps: f.deps} }

func mustLabel(t *testing.T, l *Labels, name, color string) *domain.Label {
	t.Helper()
	out, err := l.Create(context.Background(), NewLabel{Name: name, Color: color})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLabelsAreDefinedByThePersonAndValidated(t *testing.T) {
	f := newFixture(t)
	l, ctx := labelsOf(f), context.Background()
	sub := f.bus.Subscribe(20)

	design, err := l.Create(ctx, NewLabel{Name: "  Design   system ", Color: "#ABC", Description: " tokens and components "})
	if err != nil {
		t.Fatal(err)
	}
	if design.Name != "Design system" || design.Color != "#aabbcc" || design.Description != "tokens and components" || design.Version != 1 || !strings.HasPrefix(design.ID, "lbl_") {
		t.Fatalf("label = %+v", design)
	}
	if ev := <-sub.C; ev.Type != domain.EventLabelCreated || ev.ProjectID != "" {
		t.Fatalf("event = %+v (labels belong to no project)", ev)
	}

	// Nothing is predefined: any words and any colour are accepted, and the only fixed rule is uniqueness.
	for _, name := range []string{"Marketing", "Q4 launch", "Waiting on legal", "日本語", "🚀 ship it"} {
		mustLabel(t, l, name, "#112233")
	}
	for _, in := range []NewLabel{
		{Name: "design SYSTEM", Color: "#000000"}, // same name, other case and spacing
		{Name: "", Color: "#000000"}, {Name: "x", Color: "blue"}, {Name: "x", Color: "#000000;background:url(x)"},
		{Name: strings.Repeat("x", 41), Color: "#000000"}, {Name: "x", Color: "#000000", Description: strings.Repeat("d", 201)},
	} {
		if _, err := l.Create(ctx, in); err == nil {
			t.Errorf("%+v was accepted", in)
		} else if !errors.Is(err, domain.ErrInvalid) && !errors.Is(err, domain.ErrDuplicate) {
			t.Errorf("%+v: %v", in, err)
		}
	}

	list, err := l.List(ctx)
	if err != nil || len(list) != 6 || list[0].Name != "Design system" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestLabelUpdateRenamesEverywhereAndGuardsVersions(t *testing.T) {
	f := newFixture(t)
	l, ctx := labelsOf(f), context.Background()
	a, b := mustLabel(t, l, "Alpha", "#111111"), mustLabel(t, l, "Bravo", "#222222")

	name, color := "Alpha 2", "#ff8800"
	got, err := l.Update(ctx, a.ID, LabelPatch{Name: &name, Color: &color, Version: a.Version})
	if err != nil || got.Name != "Alpha 2" || got.Color != "#ff8800" || got.Version != a.Version+1 {
		t.Fatalf("update = %+v, %v", got, err)
	}
	if _, err := l.Update(ctx, a.ID, LabelPatch{Name: &name, Version: a.Version}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("stale version: %v", err)
	}
	taken := "bravo"
	if _, err := l.Update(ctx, a.ID, LabelPatch{Name: &taken, Version: got.Version}); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("renaming onto another label: %v", err)
	}
	bad := "red"
	if _, err := l.Update(ctx, b.ID, LabelPatch{Color: &bad, Version: b.Version}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("bad colour: %v", err)
	}
	if _, err := l.Update(ctx, "lbl_missing", LabelPatch{Version: 1}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestOneLabelServesEveryProject(t *testing.T) {
	f := newFixture(t)
	l, ctx := labelsOf(f), context.Background()
	web, _ := f.projects.Register(ctx, "/repos/web", "Web")
	ops, _ := f.projects.CreateWork(ctx, "Operations")
	urgent := mustLabel(t, l, "Customer-facing", "#cc0000")
	docs := mustLabel(t, l, "Docs", "#0000cc")

	a, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: web.ID, Title: "Fix login", LabelIDs: []string{urgent.ID, docs.ID}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: ops.ID, Title: "Book venue", LabelIDs: []string{urgent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.LabelIDs) != 2 || len(b.LabelIDs) != 1 {
		t.Fatalf("a=%v b=%v", a.LabelIDs, b.LabelIDs)
	}
	use, _ := l.List(ctx)
	counts := map[string]int{}
	for _, u := range use {
		counts[u.ID] = u.Tasks
	}
	if counts[urgent.ID] != 2 || counts[docs.ID] != 1 {
		t.Fatalf("usage = %v", counts)
	}

	// Replacing the set is a patch, guarded by the task's version like any other.
	none := []string{}
	patched, err := f.tasks.Update(ctx, a.ID, TaskPatch{LabelIDs: &none, Version: a.Version})
	if err != nil || len(patched.LabelIDs) != 0 || patched.Version != a.Version+1 {
		t.Fatalf("patched = %+v, %v", patched, err)
	}
	if _, err := f.tasks.Update(ctx, a.ID, TaskPatch{LabelIDs: &[]string{docs.ID}, Version: a.Version}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("stale label change: %v", err)
	}
	if _, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: web.ID, Title: "x", LabelIDs: []string{"lbl_nope"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("unknown label: %v", err)
	}
	many := make([]string, planning.MaxLabelsPerTask+1)
	for i := range many {
		many[i] = "lbl_" + string(rune('a'+i))
	}
	if _, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: web.ID, Title: "x", LabelIDs: many}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("too many labels: %v", err)
	}
}

func TestDeletingALabelOnlyTakesItOffTheTasks(t *testing.T) {
	f := newFixture(t)
	l, ctx := labelsOf(f), context.Background()
	p, _ := f.projects.Register(ctx, "/repos/web", "Web")
	keep, drop := mustLabel(t, l, "Keep", "#111111"), mustLabel(t, l, "Drop", "#222222")
	task, _ := f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: "t", LabelIDs: []string{drop.ID, keep.ID}})
	doing := domain.TaskDoing
	task, _ = f.tasks.Update(ctx, task.ID, TaskPatch{State: &doing, Version: task.Version})
	sub := f.bus.Subscribe(20)

	if err := l.Delete(ctx, drop.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.tasks.Get(ctx, task.ID)
	if len(got.LabelIDs) != 1 || got.LabelIDs[0] != keep.ID || got.State != domain.TaskDoing || got.Version != task.Version+1 {
		t.Fatalf("task after deleting a label = %+v", got)
	}
	var types []domain.EventType
	for len(types) < 2 {
		types = append(types, (<-sub.C).Type)
	}
	if types[0] != domain.EventTaskUpdated || types[1] != domain.EventLabelDeleted {
		t.Fatalf("events = %v: the tasks that lost the label must be told", types)
	}
	if err := l.Delete(ctx, drop.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestFilteringByLabelAndMode(t *testing.T) {
	f := newFixture(t)
	l, ctx := labelsOf(f), context.Background()
	p, _ := f.projects.Register(ctx, "/repos/web", "Web")
	a, b := mustLabel(t, l, "A", "#111111"), mustLabel(t, l, "B", "#222222")
	mk := func(title string, mode planning.ExecutionMode, ids ...string) {
		if _, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: title, WorkMode: mode, LabelIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
	mk("only-a", planning.ModeAgent, a.ID)
	mk("a-and-b", planning.ModeHuman, a.ID, b.ID)
	mk("only-b", planning.ModeHybrid, b.ID)
	mk("none", "")

	titles := func(fl TaskFilter) string {
		ts, err := f.tasks.ListFiltered(ctx, p.ID, fl)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, x := range ts {
			out = append(out, x.Title)
		}
		return strings.Join(out, ",")
	}
	if got := titles(TaskFilter{}); strings.Count(got, ",") != 3 {
		t.Errorf("no filter keeps everything: %s", got)
	}
	if got := titles(TaskFilter{Labels: []string{a.ID}}); got != "a-and-b,only-a" && got != "only-a,a-and-b" {
		t.Errorf("a: %s", got)
	}
	if got := titles(TaskFilter{Labels: []string{a.ID, b.ID}}); strings.Count(got, ",") != 2 {
		t.Errorf("any of a, b: %s", got)
	}
	if got := titles(TaskFilter{Labels: []string{a.ID, b.ID}, AllLabels: true}); got != "a-and-b" {
		t.Errorf("all of a, b: %s", got)
	}
	if got := titles(TaskFilter{Mode: planning.ModeHuman}); got != "a-and-b" {
		t.Errorf("human: %s", got)
	}
	if got := titles(TaskFilter{Mode: planning.ModeAgent}); !strings.Contains(got, "only-a") || !strings.Contains(got, "none") {
		t.Errorf("an unset mode is agent work: %s", got)
	}
	if got := titles(TaskFilter{Labels: []string{b.ID}, Mode: planning.ModeHuman}); got != "a-and-b" {
		t.Errorf("label and mode together: %s", got)
	}
}

func TestWorkModeIsNotALabel(t *testing.T) {
	f := newFixture(t)
	l, ctx := labelsOf(f), context.Background()
	p, _ := f.projects.Register(ctx, "/repos/web", "Web")
	// A label may be called anything, even "human"; it is still only a label.
	human := mustLabel(t, l, "human", "#123456")
	task, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: "t", LabelIDs: []string{human.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Mode() != planning.ModeAgent || task.WorkMode != planning.ModeAgent {
		t.Fatalf("a label called human changed the work mode: %+v", task)
	}
	mode := planning.ModeHuman
	task, err = f.tasks.Update(ctx, task.ID, TaskPatch{WorkMode: &mode, Version: task.Version})
	if err != nil || task.WorkMode != planning.ModeHuman || len(task.LabelIDs) != 1 {
		t.Fatalf("task = %+v, %v", task, err)
	}
	if err := labelsOf(f).Delete(ctx, human.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = f.tasks.Get(ctx, task.ID)
	if task.WorkMode != planning.ModeHuman || len(task.LabelIDs) != 0 {
		t.Fatalf("deleting a label must not touch the work mode: %+v", task)
	}
	bad := planning.ExecutionMode("robot")
	if _, err := f.tasks.Update(ctx, task.ID, TaskPatch{WorkMode: &bad, Version: task.Version}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("bad mode: %v", err)
	}
}

func TestWorkProjectsAreRepositoryFree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	w, err := f.projects.CreateWork(ctx, "  Q4 campaign ")
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != domain.ProjectWork || w.RepoPath != "" || w.Repository != nil || w.Name != "Q4 campaign" {
		t.Fatalf("work project = %+v", w)
	}
	if _, err := f.projects.CreateWork(ctx, "Q4 campaign"); err != nil {
		t.Errorf("work projects may share a name: %v", err)
	}
	if _, err := f.projects.CreateWork(ctx, " "); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("blank name: %v", err)
	}

	// Its tasks are human work by default, and have no branch or source to track.
	task, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: w.ID, Title: "Book the venue"})
	if err != nil || task.WorkMode != planning.ModeHuman {
		t.Fatalf("task = %+v, %v", task, err)
	}
	for _, in := range []NewTask{{ProjectID: w.ID, Title: "x", WorkBranch: "feature/x"}, {ProjectID: w.ID, Title: "x", BaseBranch: "main"}, {ProjectID: w.ID, Title: "x", SourceRef: "team:1"}} {
		if _, err := f.tasks.CreateTask(ctx, in); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%+v: %v", in, err)
		}
	}

	// Nothing that reads a repository is offered one.
	if _, err := f.projects.Refresh(ctx, w.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("refresh: %v", err)
	}
	gc := &GitControl{Deps: f.deps} // no Git: a work project must be refused before Git is asked anything
	if _, err := gc.resolve(ctx, w.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("git control: %v", err)
	}
	tasksCreated, err := f.tasks.IntegrationProjects(ctx)
	if err != nil || len(tasksCreated.Projects) != 0 {
		t.Errorf("a work project is not a place to hand a repository's ticket to: %+v, %v", tasksCreated, err)
	}

	// It still shows up where projects are listed, and survives a restart of the listing.
	all, err := f.projects.List(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("list = %+v, %v", all, err)
	}
}

func TestNoAgentIsEverScheduledOnHumanWork(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	work, err := f.projects.CreateWork(ctx, "Ops")
	if err != nil {
		t.Fatal(err)
	}

	// Human work in a repository project: setting it up to start by itself is refused, and the plan blocks it.
	human := planning.ModeHuman
	task, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: pid, Title: "Review the copy", WorkMode: human})
	if err != nil {
		t.Fatal(err)
	}
	if d := decisionFor(t, s, pid, task.ID); d.State != "blocked" || !strings.Contains(d.Reason, "human work") {
		t.Fatalf("decision = %+v", d)
	}
	armed := domain.Orchestration{Enabled: true}
	if _, err := f.tasks.Update(ctx, task.ID, TaskPatch{Orchestration: &armed, Version: task.Version}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("scheduling human work: %v", err)
	}
	if _, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: work.ID, Title: "x", Orchestration: armed}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("scheduling a task of a work project: %v", err)
	}

	// Switching scheduled agent work to human work switches the schedule off instead of leaving a task
	// that can never run waiting for its time.
	auto := arm(t, f, pid, "nightly", domain.Orchestration{})
	if !auto.Orchestration.Enabled {
		t.Fatal("setup: not armed")
	}
	auto, err = f.tasks.Update(ctx, auto.ID, TaskPatch{WorkMode: &human, Version: auto.Version})
	if err != nil || auto.Orchestration.Enabled {
		t.Fatalf("task = %+v, %v", auto.Orchestration, err)
	}

	// Hybrid is agent-capable, and the default is untouched.
	hybrid := planning.ModeHybrid
	h, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: pid, Title: "pair", WorkMode: hybrid, Orchestration: domain.Orchestration{Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	// (The project runs one task at a time, so the other of the two may be queued behind it: what
	// matters is that neither is blocked for its mode.)
	if d := decisionFor(t, s, pid, h.ID); d.State == "blocked" {
		t.Fatalf("hybrid decision = %+v", d)
	}
	plain := arm(t, f, pid, "plain", domain.Orchestration{})
	if plain.WorkMode != planning.ModeAgent {
		t.Fatalf("default mode = %q", plain.WorkMode)
	}
	if d := decisionFor(t, s, pid, plain.ID); d.State == "blocked" {
		t.Fatalf("an ordinary task keeps being scheduled: %+v", d)
	}
}

func TestPlansAreValidatedAndNeverReschedule(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	if _, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: pid, Title: "x", Plan: domain.Plan{Start: "2026-10-09", End: "2026-10-05"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("end before start: %v", err)
	}
	if _, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: pid, Title: "x", Plan: domain.Plan{Start: "next week"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("not a date: %v", err)
	}

	armed := arm(t, f, pid, "armed", domain.Orchestration{})
	before := decisionFor(t, s, pid, armed.ID)
	scheduledBefore := armed.Orchestration
	plan := domain.Plan{Start: "2026-12-01", End: "2026-12-05"}
	moved, err := f.tasks.Update(ctx, armed.ID, TaskPatch{Plan: &plan, Version: armed.Version})
	if err != nil || moved.Plan != plan {
		t.Fatalf("plan = %+v, %v", moved.Plan, err)
	}
	// Planning a task far in the future neither delays nor changes when its agent starts.
	if after := decisionFor(t, s, pid, armed.ID); after != before {
		t.Fatalf("a plan changed the scheduling decision: %+v → %+v", before, after)
	}
	moved.Orchestration.Key = ""
	scheduledBefore.Key = ""
	if moved.Orchestration.Enabled != scheduledBefore.Enabled || moved.Orchestration.ScheduledAt != scheduledBefore.ScheduledAt {
		t.Fatalf("orchestration changed: %+v → %+v", scheduledBefore, moved.Orchestration)
	}
}

func TestTimelineWarnsAboutBadDependenciesAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.projects.Register(ctx, "/repos/web", "Web")
	mk := func(title, start, end string, deps ...string) *domain.Task {
		task, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: title, Plan: domain.Plan{Start: start, End: end}, Orchestration: domain.Orchestration{Dependencies: deps}})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	design := mk("Design", "2026-10-05", "2026-10-09")
	build := mk("Build", "2026-10-07", "2026-10-14", design.ID) // starts before Design ends
	ship := mk("Ship", "2026-10-15", "2026-10-15", build.ID)    // fine

	tl, err := f.tasks.Timeline(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Warnings) != 1 || tl.Warnings[0].Code != planning.CodeStartsBeforeDependent || tl.Warnings[0].ItemID != build.ID || tl.Warnings[0].OtherID != design.ID {
		t.Fatalf("warnings = %+v", tl.Warnings)
	}

	// Nothing was rescheduled by finding out.
	for _, want := range []*domain.Task{design, build, ship} {
		got, _ := f.tasks.Get(ctx, want.ID)
		if got.Plan != want.Plan || got.Version != want.Version || got.State != want.State {
			t.Errorf("%s changed: %+v", want.Title, got)
		}
	}

	// A dependency cannot be set up to be invalid through the API: self, unknown and circular are refused…
	self := domain.Orchestration{Dependencies: []string{design.ID}}
	if _, err := f.tasks.Update(ctx, design.ID, TaskPatch{Orchestration: &self, Version: design.Version}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("self dependency: %v", err)
	}
	loop := domain.Orchestration{Dependencies: []string{ship.ID}}
	if _, err := f.tasks.Update(ctx, design.ID, TaskPatch{Orchestration: &loop, Version: design.Version}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("circular dependency: %v", err)
	}
	// …and a task that is finished stops being a conflict, while a closed one that was never finished is flagged.
	done := domain.TaskDone
	if _, err := f.tasks.Update(ctx, design.ID, TaskPatch{State: &done, Version: design.Version}); err != nil {
		t.Fatal(err)
	}
	tl, _ = f.tasks.Timeline(ctx, p.ID)
	if len(tl.Warnings) != 0 {
		t.Fatalf("finished dependencies are not conflicts: %+v", tl.Warnings)
	}
	closed := true
	abandoned := mk("Abandoned", "", "")
	dependent := mk("Waits", "", "", abandoned.ID)
	if _, err := f.tasks.Update(ctx, abandoned.ID, TaskPatch{Archived: &closed, Version: abandoned.Version}); err != nil {
		t.Fatal(err)
	}
	tl, _ = f.tasks.Timeline(ctx, p.ID)
	if len(tl.Warnings) != 1 || tl.Warnings[0].Code != planning.CodeArchivedDependency || tl.Warnings[0].ItemID != dependent.ID {
		t.Fatalf("warnings = %+v", tl.Warnings)
	}
	if _, err := f.tasks.Timeline(ctx, "prj_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
}

func TestTimelineReportsDependenciesTheAPIWouldRefuse(t *testing.T) {
	// Data can arrive other ways than the API (an import, a restore, an older build). The analysis is
	// what tells the person, so it must cope with a graph the write path would never produce.
	tasks := []domain.Task{
		{ID: "a", Title: "A", Orchestration: domain.Orchestration{Dependencies: []string{"b"}}},
		{ID: "b", Title: "B", Orchestration: domain.Orchestration{Dependencies: []string{"a", "ghost"}}},
		{ID: "c", Title: "C", Orchestration: domain.Orchestration{Dependencies: []string{"c"}}},
	}
	got := map[planning.Code]bool{}
	for _, w := range TimelineWarnings(tasks) {
		got[w.Code] = true
	}
	for _, code := range []planning.Code{planning.CodeDependencyCycle, planning.CodeMissingDependency, planning.CodeSelfDependency} {
		if !got[code] {
			t.Errorf("missing %s in %v", code, got)
		}
	}
}

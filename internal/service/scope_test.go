package service

import (
	"errors"
	"testing"

	"devboard/internal/domain"
)

// twoProjects is two projects, each with a task and a running run.
type twoProjects struct {
	*fixture
	a, b   *ProjectDetail
	ta, tb *domain.Task
	ra, rb *domain.Run
}

func newTwoProjects(t *testing.T) *twoProjects {
	t.Helper()
	f := newFixture(t)
	w := &twoProjects{fixture: f}
	var err error
	if w.a, err = f.projects.Register(bg, "/repos/alpha", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if w.b, err = f.projects.Register(bg, "/repos/beta", "Beta"); err != nil {
		t.Fatal(err)
	}
	if w.ta, err = f.tasks.Create(bg, w.a.ID, "Alpha task", ""); err != nil {
		t.Fatal(err)
	}
	if w.tb, err = f.tasks.Create(bg, w.b.ID, "Beta task", ""); err != nil {
		t.Fatal(err)
	}
	start := func(task *domain.Task) *domain.Run {
		r, err := f.runs.Create(bg, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "go"})
		if err != nil {
			t.Fatal(err)
		}
		if r, err = f.runs.MarkStarted(bg, r.ID, Started{PID: 1, SessionRef: "s-" + task.ID}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	w.ra, w.rb = start(w.ta), start(w.tb)
	return w
}

func TestEntitiesAreOnlyVisibleThroughTheirOwnProject(t *testing.T) {
	w := newTwoProjects(t)
	qa, _, err := w.runs.RecordQuestion(bg, w.ra.ID, NewQuestion{Prompt: "Alpha asks"})
	if err != nil {
		t.Fatal(err)
	}

	// Each is found through its own project...
	if got, err := w.tasks.GetIn(bg, w.a.ID, w.ta.ID); err != nil || got.ID != w.ta.ID {
		t.Fatalf("own task: %v", err)
	}
	if got, err := w.runs.GetIn(bg, w.a.ID, w.ra.ID); err != nil || got.ID != w.ra.ID {
		t.Fatalf("own run: %v", err)
	}
	if got, err := w.runs.GetQuestionIn(bg, w.a.ID, qa.ID); err != nil || got.ID != qa.ID {
		t.Fatalf("own question: %v", err)
	}

	// ...and through no other. The answer is "not found", the same as for something
	// that does not exist, so a request scoped to one project cannot even learn that
	// another's thing exists.
	for name, err := range map[string]error{
		"task":     second(w.tasks.GetIn(bg, w.b.ID, w.ta.ID)),
		"run":      second(w.runs.GetIn(bg, w.b.ID, w.ra.ID)),
		"question": second(w.runs.GetQuestionIn(bg, w.b.ID, qa.ID)),
		"missing":  second(w.tasks.GetIn(bg, w.b.ID, "tsk_nope")),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s of another project: err = %v, want not found", name, err)
		}
	}
	if _, err := w.tasks.GetIn(bg, "prj_missing", w.ta.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("task in an unknown project: %v", err)
	}
}

func second[T any](_ T, err error) error { return err }

func TestProjectListsContainOnlyThatProject(t *testing.T) {
	w := newTwoProjects(t)
	if _, _, err := w.runs.RecordQuestion(bg, w.ra.ID, NewQuestion{Prompt: "Alpha asks"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.runs.RecordQuestion(bg, w.rb.ID, NewQuestion{Prompt: "Beta asks"}); err != nil {
		t.Fatal(err)
	}

	for _, p := range []struct {
		project *ProjectDetail
		task    *domain.Task
		run     *domain.Run
		asks    string
	}{{w.a, w.ta, w.ra, "Alpha asks"}, {w.b, w.tb, w.rb, "Beta asks"}} {
		tasks, err := w.tasks.List(bg, p.project.ID)
		if err != nil || len(tasks) != 1 || tasks[0].ID != p.task.ID {
			t.Errorf("%s tasks = %+v, %v", p.project.Name, tasks, err)
		}
		qs, err := w.runs.ListPendingQuestionsIn(bg, p.project.ID)
		if err != nil || len(qs) != 1 || qs[0].Prompt != p.asks || qs[0].ProjectID != p.project.ID {
			t.Errorf("%s questions = %+v, %v", p.project.Name, qs, err)
		}
		latest, err := w.runs.ListLatestByProject(bg, p.project.ID)
		if err != nil || len(latest) != 1 || latest[0].ID != p.run.ID {
			t.Errorf("%s board runs = %+v, %v", p.project.Name, latest, err)
		}
		hist, err := w.runs.History(bg, p.project.ID, 50)
		if err != nil || len(hist) != 1 || hist[0].ID != p.run.ID {
			t.Errorf("%s history = %+v, %v", p.project.Name, hist, err)
		}
	}
	for _, f := range []func() error{
		func() error { return second(w.runs.ListPendingQuestionsIn(bg, "prj_missing")) },
		func() error { return second(w.runs.History(bg, "prj_missing", 10)) },
		func() error { return second(w.tasks.List(bg, "prj_missing")) },
	} {
		if err := f(); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("unknown project: %v", err)
		}
	}
}

func TestHistoryIsNewestFirstAndBounded(t *testing.T) {
	w := newTwoProjects(t)
	if _, err := w.runs.End(bg, w.ra.ID, Ended{State: domain.RunCompleted}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		r, err := w.runs.Create(bg, NewRun{TaskID: w.ta.ID, AgentID: "fake", Prompt: "again"})
		if err != nil {
			t.Fatal(err)
		}
		w.ra = r
		if _, err := w.runs.End(bg, r.ID, Ended{State: domain.RunFailed}); err != nil {
			t.Fatal(err)
		}
	}
	hist, err := w.runs.History(bg, w.a.ID, 100)
	if err != nil || len(hist) != 4 {
		t.Fatalf("history = %d runs, %v", len(hist), err)
	}
	for i := 1; i < len(hist); i++ {
		if hist[i].CreatedAt.After(hist[i-1].CreatedAt) {
			t.Fatalf("history is not newest first: %v", hist)
		}
	}
	if two, _ := w.runs.History(bg, w.a.ID, 2); len(two) != 2 || two[0].ID != hist[0].ID {
		t.Fatalf("limited history = %+v", two)
	}
}

// The Control Center is the one thing that deliberately looks across projects.
func TestControlCenterAggregatesEveryProject(t *testing.T) {
	w := newTwoProjects(t)
	cc := &ControlCenter{Deps: w.deps}

	// A third project that is quiet still shows up, with nothing going on.
	if _, err := w.projects.Register(bg, "/repos/quiet", "Quiet"); err != nil {
		t.Fatal(err)
	}
	// Alpha: one run waits on a question. Beta: one run is blocked, and another task's run is idle.
	qa, _, err := w.runs.RecordQuestion(bg, w.ra.ID, NewQuestion{Kind: domain.QuestionApproval, Prompt: "Run the tests?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.runs.Block(bg, w.rb.ID, domain.Blocker{Summary: "Which region?", Source: domain.BlockerReport}); err != nil {
		t.Fatal(err)
	}
	tb2, _ := w.tasks.Create(bg, w.b.ID, "Beta second task", "")
	r2, _ := w.runs.Create(bg, NewRun{TaskID: tb2.ID, AgentID: "fake", Prompt: "go"})
	r2, _ = w.runs.MarkStarted(bg, r2.ID, Started{PID: 2, SessionRef: "s2"})
	if _, err := w.runs.MarkIdle(bg, r2.ID); err != nil {
		t.Fatal(err)
	}

	o, err := cc.Overview(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Projects) != 3 || o.Projects[0].Name != "Alpha" || o.Projects[1].Name != "Beta" || o.Projects[2].Name != "Quiet" {
		t.Fatalf("projects = %+v", o.Projects)
	}
	if a := o.Projects[0]; a.NeedsInput != 1 || a.Blocked != 0 || a.Idle != 0 || a.Running != 0 {
		t.Errorf("alpha = %+v", a)
	}
	if b := o.Projects[1]; b.NeedsInput != 0 || b.Blocked != 1 || b.Idle != 1 || b.Running != 0 {
		t.Errorf("beta = %+v", b)
	}
	if q := o.Projects[2]; q.NeedsInput+q.Blocked+q.Idle+q.Running != 0 {
		t.Errorf("quiet = %+v", q)
	}

	// Questions of every project, each saying where it belongs.
	if len(o.Questions) != 1 || o.Questions[0].Question.ID != qa.ID || o.Questions[0].ProjectName != "Alpha" ||
		o.Questions[0].TaskTitle != "Alpha task" || o.Questions[0].AgentID != "fake" {
		t.Errorf("questions = %+v", o.Questions)
	}
	// Active runs of every project, blocked ones included, with their blocker.
	if len(o.Runs) != 3 {
		t.Fatalf("runs = %+v", o.Runs)
	}
	seen := map[string]AttentionRun{}
	for _, r := range o.Runs {
		seen[r.Run.ID] = r
	}
	if r := seen[w.rb.ID]; r.ProjectName != "Beta" || r.TaskTitle != "Beta task" || r.Run.State != domain.RunBlocked || r.Run.Blocker == nil {
		t.Errorf("blocked run = %+v", r)
	}
	if r := seen[w.ra.ID]; r.ProjectName != "Alpha" || r.Run.Waiting != domain.WaitQuestion {
		t.Errorf("alpha run = %+v", r)
	}
	if r := seen[r2.ID]; r.TaskTitle != "Beta second task" || r.Run.Waiting != domain.WaitIdle {
		t.Errorf("idle run = %+v", r)
	}

	// Finished runs drop out.
	if _, err := w.runs.End(bg, w.rb.ID, Ended{State: domain.RunStopped}); err != nil {
		t.Fatal(err)
	}
	if o, _ = cc.Overview(bg); len(o.Runs) != 2 || o.Projects[1].Blocked != 0 {
		t.Errorf("after the blocked run ended: %+v", o)
	}
}

func TestControlCenterIsEmptyNotNilWithoutProjects(t *testing.T) {
	cc := &ControlCenter{Deps: newFixture(t).deps}
	o, err := cc.Overview(bg)
	if err != nil || o.Projects == nil || o.Questions == nil || o.Runs == nil {
		t.Fatalf("overview = %+v, %v", o, err)
	}
}

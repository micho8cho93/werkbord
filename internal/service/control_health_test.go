package service

import (
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// The Control Center is meant to show exceptions: what needs an answer, what is
// stuck, what failed, what is ready, and where a repository is at risk. These
// tests pin that the exceptions appear and that ordinary, successful activity does
// not turn into items.

func (w *twoProjects) finishRun(t *testing.T, r *domain.Run, state domain.RunState) {
	t.Helper()
	if _, err := w.runs.End(bg, r.ID, Ended{State: state, Reason: "because"}); err != nil {
		t.Fatal(err)
	}
}

func (w *twoProjects) moveTask(t *testing.T, task *domain.Task, state domain.TaskState) *domain.Task {
	t.Helper()
	cur, err := w.tasks.Get(bg, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur, err = w.tasks.Update(bg, task.ID, TaskPatch{State: &state, Version: cur.Version}); err != nil {
		t.Fatal(err)
	}
	return cur
}

func (w *twoProjects) overview(t *testing.T) *Overview {
	t.Helper()
	o, err := (&ControlCenter{Deps: w.deps}).Overview(bg)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func (w *twoProjects) activity(o *Overview, id string) ProjectActivity {
	for _, p := range o.Projects {
		if p.ProjectID == id {
			return p
		}
	}
	return ProjectActivity{}
}

func (w *twoProjects) putFinding(t *testing.T, projectID, key string, sev domain.HealthSeverity, state domain.HealthState) *domain.HealthFinding {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	f := &domain.HealthFinding{
		ID: domain.HealthFindingID(projectID, domain.FindStaleBranch, key), ProjectID: projectID, Type: domain.FindStaleBranch,
		Category: domain.CatBranch, Severity: sev, Basis: domain.BasisDeterministic, Title: key, Explanation: "x",
		Evidence: []domain.HealthEvidence{{Label: "a", Value: "b"}}, Action: domain.HealthAction{Kind: domain.ActInspect, Label: "Look"},
		State: state, DetectedAt: at, UpdatedAt: at,
	}
	switch state {
	case domain.HealthResolved:
		f.ResolvedAt = &at
	case domain.HealthDismissed:
		f.DismissedAt, f.DismissedSeverity = &at, sev
	}
	if err := w.deps.Store.Update(bg, func(tx store.Tx) error { return tx.Health().Upsert(bg, f) }); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAFailedRunOnAWaitingTaskIsAnException(t *testing.T) {
	w := newTwoProjects(t)
	w.moveTask(t, w.ta, domain.TaskDoing)
	w.finishRun(t, w.ra, domain.RunFailed)

	o := w.overview(t)
	if len(o.Failed) != 1 || o.Failed[0].Run.ID != w.ra.ID || o.Failed[0].ProjectName != "Alpha" || o.Failed[0].TaskTitle != "Alpha task" {
		t.Fatalf("failed = %+v", o.Failed)
	}
	if got := w.activity(o, w.a.ID); got.Failed != 1 || got.Running != 0 {
		t.Fatalf("alpha = %+v", got)
	}
	if got := w.activity(o, w.b.ID); got.Failed != 0 || got.Running != 1 {
		t.Fatalf("beta is unaffected = %+v", got)
	}

	// Dealing with it clears it: run it again, or move the task on.
	run2, err := w.runs.Create(bg, NewRun{TaskID: w.ta.ID, AgentID: "fake", Prompt: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.runs.MarkStarted(bg, run2.ID, Started{PID: 1}); err != nil {
		t.Fatal(err)
	}
	if o := w.overview(t); len(o.Failed) != 0 {
		t.Fatalf("a task running again is not failed: %+v", o.Failed)
	}
	w.finishRun(t, run2, domain.RunCompleted)
	if o := w.overview(t); len(o.Failed) != 0 {
		t.Fatalf("the latest run succeeded: %+v", o.Failed)
	}
}

func TestAFailureTheUserHasMovedOnFromIsNotShown(t *testing.T) {
	w := newTwoProjects(t)
	w.finishRun(t, w.ra, domain.RunFailed)
	for _, state := range []domain.TaskState{domain.TaskBacklog, domain.TaskDone} {
		w.moveTask(t, w.ta, state)
		if o := w.overview(t); len(o.Failed) != 0 {
			t.Fatalf("a failed run on a task in %s is not waiting on anyone: %+v", state, o.Failed)
		}
	}
	w.moveTask(t, w.ta, domain.TaskReview)
	if o := w.overview(t); len(o.Failed) != 1 || len(o.Review) != 0 {
		t.Fatalf("failed work in Review is a failure, not 'ready': failed=%d review=%d", len(o.Failed), len(o.Review))
	}
}

func TestAStoppedRunIsNotAFailure(t *testing.T) {
	w := newTwoProjects(t)
	w.moveTask(t, w.ta, domain.TaskDoing)
	w.finishRun(t, w.ra, domain.RunStopped)
	if o := w.overview(t); len(o.Failed) != 0 {
		t.Fatalf("the user stopped it: %+v", o.Failed)
	}
}

func TestReadyForReviewListsFinishedWorkWithNothingRunning(t *testing.T) {
	w := newTwoProjects(t)
	w.finishRun(t, w.ra, domain.RunCompleted)
	w.moveTask(t, w.ta, domain.TaskReview)
	w.moveTask(t, w.tb, domain.TaskReview) // beta's run is still going: not ready yet

	o := w.overview(t)
	if len(o.Review) != 1 || o.Review[0].Task.ID != w.ta.ID || o.Review[0].ProjectName != "Alpha" || o.Review[0].LastRun == nil || o.Review[0].LastRun.ID != w.ra.ID {
		t.Fatalf("review = %+v", o.Review)
	}
	if got := w.activity(o, w.a.ID); got.Review != 1 {
		t.Fatalf("alpha = %+v", got)
	}
}

func TestSuccessfulBackgroundActivityIsNotAnException(t *testing.T) {
	w := newTwoProjects(t)
	w.finishRun(t, w.ra, domain.RunCompleted)
	w.moveTask(t, w.ta, domain.TaskDone)
	// Beta's agent is simply working.
	o := w.overview(t)
	if len(o.Failed) != 0 || len(o.Review) != 0 || len(o.Repository) != 0 || len(o.Questions) != 0 {
		t.Fatalf("a quiet, successful day produced exceptions: failed=%d review=%d repo=%d", len(o.Failed), len(o.Review), len(o.Repository))
	}
	if got := w.activity(o, w.b.ID); got.Running != 1 || got.Failed+got.Review+got.RepoAttention+got.RepoRisk+got.NeedsInput+got.Blocked != 0 {
		t.Fatalf("beta = %+v", got)
	}
}

func TestRepositoryRiskAppearsInTheControlCenter(t *testing.T) {
	w := newTwoProjects(t)
	w.putFinding(t, w.a.ID, "info", domain.HealthInfo, domain.HealthOpen)
	w.putFinding(t, w.a.ID, "attention", domain.HealthAttention, domain.HealthOpen)
	w.putFinding(t, w.a.ID, "risk", domain.HealthRisk, domain.HealthOpen)
	w.putFinding(t, w.b.ID, "critical", domain.HealthCritical, domain.HealthOpen)
	w.putFinding(t, w.b.ID, "known", domain.HealthRisk, domain.HealthDismissed)
	w.putFinding(t, w.b.ID, "fixed", domain.HealthRisk, domain.HealthResolved)

	o := w.overview(t)
	if len(o.Repository) != 2 || o.Repository[0].Severity != domain.HealthCritical || o.Repository[1].Severity != domain.HealthRisk {
		t.Fatalf("only open risk and critical findings are listed, worst first: %+v", o.Repository)
	}
	if o.Repository[0].ProjectName != "Beta" || o.Repository[1].ProjectName != "Alpha" {
		t.Fatalf("each names its project: %+v", o.Repository)
	}
	a, b := w.activity(o, w.a.ID), w.activity(o, w.b.ID)
	if a.RepoAttention != 2 || a.RepoRisk != 1 {
		t.Fatalf("alpha counts attention and above (not housekeeping) = %+v", a)
	}
	if b.RepoAttention != 1 || b.RepoRisk != 1 {
		t.Fatalf("beta ignores dismissed and resolved = %+v", b)
	}
}

func TestEmptyListsAreEmptyNotNull(t *testing.T) {
	o := (&ControlCenter{Deps: newFixture(t).deps})
	got, err := o.Overview(bg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failed == nil || got.Review == nil || got.Repository == nil {
		t.Fatalf("lists must be [] in JSON, not null: %+v", got)
	}
}

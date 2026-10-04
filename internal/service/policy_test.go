package service

import (
	"context"
	"errors"
	"testing"

	"devboard/internal/domain"
)

var bg = context.Background()

func TestTaskPolicy(t *testing.T) {
	f := newRunFixture(t)

	// The default is interactive, and it is a stored value, not an absence.
	if f.task.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("default = %+v", f.task.Policy)
	}

	task, err := f.tasks.CreateTask(bg, NewTask{ProjectID: f.project.ID, Title: "Autonomous",
		Policy: domain.ExecutionPolicy{Interaction: domain.InteractionAutonomous}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := f.tasks.Get(bg, task.ID)
	if got.Policy.Interaction != domain.InteractionAutonomous {
		t.Fatalf("stored policy = %+v", got.Policy)
	}
	if _, err := f.tasks.CreateTask(bg, NewTask{ProjectID: f.project.ID, Title: "x", Policy: domain.ExecutionPolicy{Interaction: "yolo"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown policy on create: %v", err)
	}

	// Editing the policy is a compare-and-swap update like any other; the rest of the task is untouched.
	stop := domain.ExecutionPolicy{Interaction: domain.InteractionAutonomousStopIfBlocked}
	updated, err := f.tasks.Update(bg, task.ID, TaskPatch{Policy: &stop, Version: got.Version})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked || updated.Title != "Autonomous" || updated.Version != got.Version+1 {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := f.tasks.Update(bg, task.ID, TaskPatch{Policy: &stop, Version: got.Version}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	bad := domain.ExecutionPolicy{Interaction: "yolo"}
	if _, err := f.tasks.Update(bg, task.ID, TaskPatch{Policy: &bad, Version: updated.Version}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown policy on update: %v", err)
	}
	if again, _ := f.tasks.Get(bg, task.ID); again.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked || again.Version != updated.Version {
		t.Fatalf("a refused edit changed the task: %+v", again)
	}
	// Changing something else leaves the policy alone.
	title := "Renamed"
	renamed, err := f.tasks.Update(bg, task.ID, TaskPatch{Title: &title, Version: updated.Version})
	if err != nil || renamed.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked {
		t.Fatalf("renamed = %+v, %v", renamed, err)
	}
	// And the policy is carried in the events, so every client sees it.
	f.drain()
	if _, err := f.tasks.Update(bg, task.ID, TaskPatch{Policy: &domain.ExecutionPolicy{Interaction: domain.InteractionInteractive}, Version: renamed.Version}); err != nil {
		t.Fatal(err)
	}
	evs := f.drain()
	wantTypes(t, evs, domain.EventTaskUpdated)
	if tk := payload[domain.Task](t, evs[0]); tk.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("event task = %+v", tk)
	}
}

func TestRunStoresTheGivenPolicy(t *testing.T) {
	f := newRunFixture(t)
	r, err := f.runs.Create(bg, NewRun{TaskID: f.task.ID, AgentID: "fake", Prompt: "p",
		Policy: domain.ExecutionPolicy{Interaction: domain.InteractionAutonomous}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Policy.Interaction != domain.InteractionAutonomous {
		t.Fatalf("run = %+v", r.Policy)
	}
	if got, _ := f.runs.Get(bg, r.ID); got.Policy.Interaction != domain.InteractionAutonomous {
		t.Fatalf("stored = %+v", got.Policy)
	}
	if _, err := f.runs.End(bg, r.ID, Ended{State: domain.RunFailed}); err != nil {
		t.Fatal(err)
	}
	// Unset means interactive; unknown is refused.
	plain, err := f.runs.Create(bg, NewRun{TaskID: f.task.ID, AgentID: "fake", Prompt: "p"})
	if err != nil || plain.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("plain = %+v, %v", plain, err)
	}
	_, _ = f.runs.End(bg, plain.ID, Ended{State: domain.RunFailed})
	if _, err := f.runs.Create(bg, NewRun{TaskID: f.task.ID, AgentID: "fake", Prompt: "p", Policy: domain.ExecutionPolicy{Interaction: "yolo"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown policy: %v", err)
	}
}

func TestBlockingARun(t *testing.T) {
	f := newRunFixture(t)
	r := f.running(t)
	b := domain.Blocker{Summary: "  Which region?  ", Detail: "us or eu", Options: []string{"us", "eu", " "}, Source: domain.BlockerReport}

	blocked, err := f.runs.Block(bg, r.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.State != domain.RunBlocked || blocked.Blocker.Summary != "Which region?" || len(blocked.Blocker.Options) != 2 || blocked.Blocker.RaisedAt.IsZero() {
		t.Fatalf("blocked = %+v / %+v", blocked, blocked.Blocker)
	}
	evs := f.drain()
	wantTypes(t, evs, domain.EventRunStateChanged, domain.EventAgentBlocked)
	if p := payload[struct{ Blocker domain.Blocker }](t, evs[1]); p.Blocker.Summary != "Which region?" || evs[1].RunID != r.ID || evs[1].ProjectID != f.project.ID {
		t.Fatalf("agent.blocked = %+v / %+v", p, evs[1])
	}
	if f.taskState(t) != domain.TaskDoing {
		t.Fatalf("blocking a run moves no card: %s", f.taskState(t))
	}

	// Blocking again changes nothing, and no event is made up.
	again, err := f.runs.Block(bg, r.ID, domain.Blocker{Summary: "something else", Source: domain.BlockerReport})
	if err != nil || again.Blocker.Summary != "Which region?" || again.Version != blocked.Version {
		t.Fatalf("second block = %+v, %v", again, err)
	}
	if evs := f.drain(); len(evs) != 0 {
		t.Fatalf("events = %v", types(evs))
	}

	// A blocked run takes a message, which settles it.
	if err := f.runs.CheckMessageable(bg, r.ID); err != nil {
		t.Fatalf("a blocked run takes a message: %v", err)
	}
	f.drain()
	resumed, err := f.runs.Message(bg, r.ID, "eu")
	if err != nil || resumed.State != domain.RunRunning || resumed.Blocker != nil {
		t.Fatalf("resumed = %+v, %v", resumed, err)
	}
	wantTypes(t, f.drain(), domain.EventAgentOutput, domain.EventRunStateChanged, domain.EventAgentResumed)
}

func TestOnlyARunningRunCanBeBlocked(t *testing.T) {
	f := newRunFixture(t)
	b := domain.Blocker{Summary: "stuck", Source: domain.BlockerReport}

	starting := f.newRun(t)
	if _, err := f.runs.Block(bg, starting.ID, b); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("starting: %v", err)
	}
	r, _ := f.runs.MarkStarted(bg, starting.ID, Started{PID: 1, SessionRef: "s"})
	if _, err := f.runs.MarkIdle(bg, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.Block(bg, r.ID, b); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("waiting: %v", err)
	}
	if _, err := f.runs.End(bg, r.ID, Ended{State: domain.RunCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.Block(bg, r.ID, b); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ended: %v", err)
	}
	if _, err := f.runs.Block(bg, "run_missing", b); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	r2 := f.running(t)
	if _, err := f.runs.Block(bg, r2.ID, domain.Blocker{Source: domain.BlockerReport}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("no summary: %v", err)
	}
}

func TestRecordRepliedNeverAnswersAnApproval(t *testing.T) {
	f := newRunFixture(t)
	r := f.running(t)
	_, _, err := f.runs.RecordReplied(bg, r.ID, NewQuestion{Kind: domain.QuestionApproval, Prompt: "Run `rm -rf /`?", Options: []string{"Allow", "Deny"}},
		Replied{Reply: "Allow"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a policy answered an approval: %v", err)
	}
	qs, _ := f.runs.ListPendingQuestions(bg)
	if got, _ := f.runs.Get(bg, r.ID); len(qs) != 0 || got.State != domain.RunRunning {
		t.Fatalf("a refused reply left something behind: %+v / %+v", qs, got)
	}
	if evs := f.drain(); len(evs) != 0 {
		t.Fatalf("events = %v", types(evs))
	}
}

func TestRecordRepliedRecordsTheQuestionAsAnsweredByPolicy(t *testing.T) {
	f := newRunFixture(t)
	r := f.running(t)
	q, got, err := f.runs.RecordReplied(bg, r.ID, NewQuestion{Prompt: "Which?", Options: []string{"a", "b"}}, Replied{Reply: "decide yourself"})
	if err != nil {
		t.Fatal(err)
	}
	if q.State != domain.QuestionAnswered || q.AnsweredBy != domain.AnsweredByPolicy || q.Answer != "decide yourself" || q.DeliveredAt != nil ||
		q.TaskID != f.task.ID || q.ProjectID != f.project.ID {
		t.Fatalf("question = %+v", q)
	}
	if got.State != domain.RunRunning {
		t.Fatalf("run = %+v", got)
	}
	wantTypes(t, f.drain(), domain.EventAgentQuestion, domain.EventQuestionAnswered)
	if pending, _ := f.runs.ListPendingQuestions(bg); len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
	// Delivery is recorded afterwards, like any answer, without moving the run.
	delivered, err := f.runs.ConfirmDelivery(bg, q.ID)
	if err != nil || delivered.DeliveredAt == nil {
		t.Fatalf("delivery = %+v, %v", delivered, err)
	}
	if cur, _ := f.runs.Get(bg, r.ID); cur.State != domain.RunRunning {
		t.Fatalf("run = %+v", cur)
	}

	// A run that is waiting for the user, or over, is not answered for.
	idle, _ := f.runs.MarkIdle(bg, r.ID)
	if _, _, err := f.runs.RecordReplied(bg, idle.ID, NewQuestion{Prompt: "x"}, Replied{Reply: "y"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("idle: %v", err)
	}
}

func TestRecordRepliedWithABlockerBlocksInTheSameStep(t *testing.T) {
	f := newRunFixture(t)
	r := f.running(t)
	q, blocked, err := f.runs.RecordReplied(bg, r.ID, NewQuestion{Prompt: "Which provider?", Options: []string{"Stripe", "Adyen"}},
		Replied{Reply: "stop", Blocker: &domain.Blocker{Summary: "Which provider?", Options: []string{"Stripe", "Adyen"}, Source: domain.BlockerQuestion, Kind: domain.QuestionSelection}})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.State != domain.RunBlocked || blocked.Blocker.Summary != "Which provider?" || q.AnsweredBy != domain.AnsweredByPolicy {
		t.Fatalf("blocked = %+v, q = %+v", blocked, q)
	}
	wantTypes(t, f.drain(), domain.EventAgentQuestion, domain.EventQuestionAnswered, domain.EventRunStateChanged, domain.EventAgentBlocked)

	// A second question while blocked keeps the first blocker and adds no second event.
	_, again, err := f.runs.RecordReplied(bg, r.ID, NewQuestion{Prompt: "And the region?"},
		Replied{Reply: "stop", Blocker: &domain.Blocker{Summary: "And the region?", Source: domain.BlockerQuestion}})
	if err != nil || again.Blocker.Summary != "Which provider?" {
		t.Fatalf("second = %+v, %v", again, err)
	}
	wantTypes(t, f.drain(), domain.EventAgentQuestion, domain.EventQuestionAnswered)
}

func TestBlockedRunsAreRecoveredNotFailed(t *testing.T) {
	f := newRunFixture(t)
	r := f.running(t) // has a session ref
	if _, err := f.runs.Block(bg, r.ID, domain.Blocker{Summary: "stuck", Source: domain.BlockerReport}); err != nil {
		t.Fatal(err)
	}
	f.drain()

	// Active, so it holds its worktree and counts as work in progress.
	active, _ := f.runs.ListActive(bg)
	if len(active) != 1 || active[0].State != domain.RunBlocked {
		t.Fatalf("active = %+v", active)
	}

	n, err := f.runs.RecoverAfterRestart(bg)
	if err != nil || n != 1 {
		t.Fatalf("recovered %d, %v", n, err)
	}
	got, _ := f.runs.Get(bg, r.ID)
	if got.State != domain.RunBlocked || got.PID != 0 || got.Blocker == nil || got.Blocker.Summary != "stuck" {
		t.Fatalf("recovered run = %+v", got)
	}
	wantTypes(t, f.drain(), domain.EventRunStateChanged, domain.EventAgentWaiting)
	// Recovering again does nothing: it is already between processes.
	if n, _ := f.runs.RecoverAfterRestart(bg); n != 0 {
		t.Fatalf("recovered %d the second time", n)
	}
}

func TestBlockedRunWithoutASessionFailsOnRecovery(t *testing.T) {
	f := newRunFixture(t)
	r := f.newRun(t)
	r, _ = f.runs.MarkStarted(bg, r.ID, Started{PID: 5}) // no session ref
	_, _ = f.runs.Block(bg, r.ID, domain.Blocker{Summary: "stuck", Source: domain.BlockerReport})
	if _, err := f.runs.RecoverAfterRestart(bg); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.runs.Get(bg, r.ID); got.State != domain.RunFailed || got.Blocker == nil {
		t.Fatalf("run = %+v; with nothing to resume it fails, and says why it had stopped", got)
	}
}

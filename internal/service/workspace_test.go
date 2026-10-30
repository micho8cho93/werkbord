package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/workspace"
)

func TestTheIndividualWorkspaceIsTranslatedIntoTheShellsWords(t *testing.T) {
	at := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	ov := &Overview{
		Projects: []ProjectActivity{{ProjectID: "prj_1", Name: "App"}},
		Runs: []AttentionRun{
			{Run: domain.Run{ID: "run_1", TaskID: "tsk_1", ProjectID: "prj_1", State: domain.RunRunning, UpdatedAt: at}, ProjectName: "App", TaskTitle: "Fix login"},
			{Run: domain.Run{ID: "run_2", TaskID: "tsk_2", ProjectID: "prj_1", State: domain.RunBlocked, Blocker: &domain.Blocker{Summary: "Two schemas fit"}}, ProjectName: "App", TaskTitle: "Migrate"},
		},
		Questions: []AttentionQuestion{{Question: domain.Question{ID: "q_1", RunID: "run_1", TaskID: "tsk_1", ProjectID: "prj_1", Kind: domain.QuestionDecision, Prompt: "Which database?", Context: "Compare schemas\nBefore choosing", Options: []string{"SQLite", "Postgres"}, AskedAt: at}, ProjectName: "App", TaskTitle: "Fix login"}},
		Failed:    []AttentionRun{{Run: domain.Run{ID: "run_3", TaskID: "tsk_3", ProjectID: "prj_1", State: domain.RunFailed, Reason: "tests failed"}, ProjectName: "App", TaskTitle: "Refactor"}},
		Review:    []AttentionReview{{Task: domain.Task{ID: "tsk_4", ProjectID: "prj_1", Title: "Docs"}, ProjectName: "App"}},
		Orchestration: []AttentionSchedule{{Task: domain.Task{ID: "tsk_5", ProjectID: "prj_1", Title: "Nightly", Orchestration: domain.Orchestration{ScheduledAt: &at, Timezone: "Europe/Paris"}},
			ProjectName: "App", Decision: domain.SchedulingDecision{State: "waiting"}}},
	}
	sum := SummaryOf(ov, at)
	raw, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}
	// Whatever the controller says must pass the shell's own strict reader unchanged.
	got, dropped, err := workspace.Decode(strings.NewReader(string(raw)), workspace.IndividualID)
	if err != nil || dropped != 0 {
		t.Fatalf("%v dropped %d\n%s", err, dropped, raw)
	}
	exec := map[string]workspace.Execution{}
	for _, it := range got.Work {
		exec[it.ID] = it.Execution
	}
	if exec["tsk_1"] != workspace.ExecRunning || exec["tsk_2"] != workspace.ExecBlocked || exec["tsk_3"] != workspace.ExecFailed || len(got.Work) != 4 {
		t.Fatalf("work = %+v", got.Work)
	}
	kinds := map[string]bool{}
	for _, a := range got.Attention {
		kinds[a.Kind] = true
		if a.Kind == workspace.AttentionNeedsInput && (a.Question == nil || a.Question.ID != "q_1" || a.Question.RunID != "run_1" || a.Question.Context != "Compare schemas\nBefore choosing" || len(a.Question.Options) != 2 || !a.Question.AskedAt.Equal(at)) {
			t.Fatalf("question context lost: %+v", a.Question)
		}
		if a.Kind != workspace.AttentionNeedsInput && a.Question != nil {
			t.Fatal("question on non-input item")
		}
	}
	for _, want := range []string{workspace.AttentionNeedsInput, workspace.AttentionBlocked, workspace.AttentionFailed, workspace.AttentionReview} {
		if !kinds[want] {
			t.Errorf("no %s attention in %+v", want, got.Attention)
		}
	}
	if len(got.Schedule) != 1 || got.Schedule[0].Zone != "Europe/Paris" || got.Schedule[0].State != "waiting" {
		t.Fatalf("schedule = %+v", got.Schedule)
	}
	if got.Work[0].Href != "#/p/prj_1/task/tsk_1" || got.Projects[0].Href != "#/p/prj_1/work" {
		t.Fatalf("links = %+v %+v", got.Work[0], got.Projects[0])
	}
	// Nothing that waits for the person is hidden behind something less urgent.
	if got.Attention[0].Severity == workspace.SeverityInfo && len(got.Attention) > 1 && got.Attention[len(got.Attention)-1].Severity != workspace.SeverityInfo {
		t.Fatalf("attention is not worst first: %+v", got.Attention)
	}
}

func TestAnAgentsQuestionOrATasksTitleCannotBreakTheSummary(t *testing.T) {
	at := time.Now()
	ov := &Overview{
		Projects:  []ProjectActivity{{ProjectID: "prj_1", Name: "App"}},
		Questions: []AttentionQuestion{{Question: domain.Question{ID: "q_1", TaskID: "tsk_1", ProjectID: "prj_1", Prompt: "Run `rm -rf`?\n" + strings.Repeat("x", 2000)}, ProjectName: "App", TaskTitle: "t\x00itle"}},
		Runs:      []AttentionRun{{Run: domain.Run{ID: "run_1", TaskID: "tsk_1", ProjectID: "prj_1", State: domain.RunRunning}, ProjectName: "App", TaskTitle: "<script>alert(1)</script>"}},
	}
	raw, _ := json.Marshal(SummaryOf(ov, at))
	got, _, err := workspace.Decode(strings.NewReader(string(raw)), workspace.IndividualID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got.Attention {
		if strings.ContainsAny(a.Title, "\n\x00") || len([]rune(a.Title)) > workspace.MaxText {
			t.Fatalf("attention title = %q", a.Title)
		}
	}
}

func TestSummaryPreservesIdleStartingAndUnacknowledgedRemoteState(t *testing.T) {
	at := time.Now().UTC()
	ov := &Overview{Runs: []AttentionRun{
		{Run: domain.Run{ID: "run_idle", TaskID: "tsk_idle", ProjectID: "prj_1", State: domain.RunWaitingForUser, Waiting: domain.WaitIdle, UpdatedAt: at}, TaskTitle: "Idle task"},
		{Run: domain.Run{ID: "run_ask", TaskID: "tsk_ask", ProjectID: "prj_1", State: domain.RunWaitingForUser, Waiting: domain.WaitQuestion}, TaskTitle: "Question task"},
		{Run: domain.Run{ID: "run_start", TaskID: "tsk_start", ProjectID: "prj_1", State: domain.RunStarting}, TaskTitle: "Starting task"},
		{Run: domain.Run{ID: "run_remote", TaskID: "tsk_remote", ProjectID: "prj_1", State: domain.RunRunning, Remote: true, RunnerID: "rnr_remote", UpdatedAt: at, PendingRequests: []string{"stop", "respond"}}, TaskTitle: "Remote task"},
	}}
	raw, _ := json.Marshal(SummaryOf(ov, at))
	got, dropped, err := workspace.Decode(strings.NewReader(string(raw)), workspace.IndividualID)
	if err != nil || dropped != 0 {
		t.Fatalf("%v dropped %d", err, dropped)
	}
	if got.Work[0].Execution != workspace.ExecIdle || got.Work[1].Execution != workspace.ExecNeedsInput || got.Work[2].Execution != workspace.ExecStarting {
		t.Fatalf("states lost: %+v", got.Work)
	}
	remote := got.Work[3]
	if remote.Execution != workspace.ExecRunning || !remote.Report.Stale || len(remote.Report.PendingRequests) != 2 || !remote.Report.RecordedAt.Equal(at) {
		t.Fatalf("remote report = %+v", remote)
	}
	if len(got.Attention) != 1 || got.Attention[0].Kind != workspace.AttentionIdle || got.Attention[0].Href != "#/p/prj_1/task/tsk_idle" {
		t.Fatalf("idle attention = %+v", got.Attention)
	}
	ov.Runners = []domain.Runner{{ID: "rnr_remote", Online: true}}
	if SummaryOf(ov, at).Work[3].Report.Stale {
		t.Fatal("online runner marked stale")
	}
	ov.Runners[0].Disabled = true
	if !SummaryOf(ov, at).Work[3].Report.Stale {
		t.Fatal("disabled runner marked current")
	}
}

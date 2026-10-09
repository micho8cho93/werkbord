package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/workspace"
)

func TestThePersonalWorkspaceIsTranslatedIntoTheShellsWords(t *testing.T) {
	at := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	ov := &Overview{
		Projects: []ProjectActivity{{ProjectID: "prj_1", Name: "App"}},
		Runs: []AttentionRun{
			{Run: domain.Run{ID: "run_1", TaskID: "tsk_1", ProjectID: "prj_1", State: domain.RunRunning, UpdatedAt: at}, ProjectName: "App", TaskTitle: "Fix login"},
			{Run: domain.Run{ID: "run_2", TaskID: "tsk_2", ProjectID: "prj_1", State: domain.RunBlocked, Blocker: &domain.Blocker{Summary: "Two schemas fit"}}, ProjectName: "App", TaskTitle: "Migrate"},
		},
		Questions: []AttentionQuestion{{Question: domain.Question{ID: "q_1", TaskID: "tsk_1", ProjectID: "prj_1", Prompt: "Which database?"}, ProjectName: "App", TaskTitle: "Fix login"}},
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
	got, dropped, err := workspace.Decode(strings.NewReader(string(raw)), workspace.PersonalID)
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
	}
	for _, want := range []string{workspace.AttentionNeedsInput, workspace.AttentionBlocked, workspace.AttentionFailed, workspace.AttentionReview} {
		if !kinds[want] {
			t.Errorf("no %s attention in %+v", want, got.Attention)
		}
	}
	if len(got.Schedule) != 1 || got.Schedule[0].Zone != "Europe/Paris" || got.Schedule[0].State != "waiting" {
		t.Fatalf("schedule = %+v", got.Schedule)
	}
	if got.Work[0].Href != "#/p/prj_1/task/tsk_1" || got.Projects[0].Href != "#/p/prj_1/board" {
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
	got, _, err := workspace.Decode(strings.NewReader(string(raw)), workspace.PersonalID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got.Attention {
		if strings.ContainsAny(a.Title, "\n\x00") || len([]rune(a.Title)) > workspace.MaxText {
			t.Fatalf("attention title = %q", a.Title)
		}
	}
}

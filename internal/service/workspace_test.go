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
			ProjectName: "App", Decision: domain.SchedulingDecision{State: "waiting", Reason: "Waiting for Build to complete successfully"}}},
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
	if len(got.Schedule) != 1 || got.Schedule[0].Zone != "Europe/Paris" || got.Schedule[0].State != "waiting" || got.Schedule[0].Reason != "Waiting for Build to complete successfully" || got.Schedule[0].Href != "#/p/prj_1/task/tsk_5" {
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

func TestTheSummaryNamesTheAgentOnEachRunAndNeverCallsAQuietRunnersWorkFresh(t *testing.T) {
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	ov := &Overview{
		Projects: []ProjectActivity{{ProjectID: "prj_1", Name: "App"}},
		Runners:  []domain.Runner{{ID: "rnr_gone", Online: false}, {ID: "rnr_up", Online: true}},
		Runs: []AttentionRun{
			{Run: domain.Run{ID: "run_1", TaskID: "tsk_1", ProjectID: "prj_1", AgentID: "codex", State: domain.RunRunning, UpdatedAt: at}, ProjectName: "App", TaskTitle: "Local"},
			{Run: domain.Run{ID: "run_2", TaskID: "tsk_2", ProjectID: "prj_1", AgentID: "claude-code", State: domain.RunStarting, UpdatedAt: at}, ProjectName: "App", TaskTitle: "Starting"},
			{Run: domain.Run{ID: "run_3", TaskID: "tsk_3", ProjectID: "prj_1", AgentID: "gemini-cli", State: domain.RunRunning, Remote: true, RunnerID: "rnr_gone", UpdatedAt: at}, ProjectName: "App", TaskTitle: "Quiet runner"},
			{Run: domain.Run{ID: "run_4", TaskID: "tsk_4", ProjectID: "prj_1", AgentID: "opencode", State: domain.RunRunning, Remote: true, RunnerID: "rnr_up", UpdatedAt: at}, ProjectName: "App", TaskTitle: "Live runner"},
		},
	}
	got := map[string]*workspace.AgentRef{}
	for _, it := range SummaryOf(ov, at).Work {
		got[it.ID] = it.Agent
	}
	if a := got["tsk_1"]; a == nil || a.ID != "codex" || a.State != workspace.ExecRunning || !a.Fresh {
		t.Fatalf("a local run: %+v", a)
	}
	if a := got["tsk_2"]; a == nil || a.State != workspace.ExecStarting {
		t.Fatalf("starting must be its own state, not queued: %+v", a)
	}
	if a := got["tsk_3"]; a == nil || a.Fresh {
		t.Fatalf("a run whose runner is offline must not be fresh: %+v", a)
	}
	if a := got["tsk_4"]; a == nil || !a.Fresh {
		t.Fatalf("a run on a live runner is fresh: %+v", a)
	}
	// Nothing about the run itself leaves with the reference.
	raw, _ := json.Marshal(got["tsk_1"])
	for _, banned := range []string{"run_1", "runId", "prompt", "path", "runner"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("the agent reference leaked %q: %s", banned, raw)
		}
	}
}

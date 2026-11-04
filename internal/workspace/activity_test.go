package workspace

import "testing"

func TestActivityContextIsBoundedAndPersonalOnly(t *testing.T) {
	s := Summary{Workspace: Entry{Kind: KindIndividual}, PendingRepositories: []PendingRepository{{SourceRef: "source:v1:w:p:t:m", Repository: "https://github.com/acme/app", Title: "Incoming work", From: "Design"}}, Readiness: []Readiness{{ID: "t_1", Title: "Run tests", State: "waiting_capacity", Reason: "Runner is full", Href: "#/p/p_1/task/t_1"}}, Runners: []Machine{{ID: "r_1", Name: "Mac"}}, Work: []Item{{Run: &RunContext{RunID: "run_1", RunnerID: "r_1", Choices: []string{"Continue"}}}}}
	if !s.validActivity() {
		t.Fatal("valid Personal evidence refused")
	}
	s.Workspace.Kind = KindTeam
	if s.validActivity() {
		t.Fatal("execution context accepted from coordination workspace")
	}
	s.cleanActivity()
	if len(s.PendingRepositories) > 0 || len(s.Readiness) > 0 || len(s.Runners) > 0 {
		t.Fatal("coordination-only projection retained local context")
	}
	if (&RunContext{RunID: "../escape"}).valid() {
		t.Fatal("invalid run identity accepted")
	}
	if (&RunContext{RunID: "run_1", Choices: []string{"unsafe\x00choice"}}).valid() {
		t.Fatal("control characters accepted")
	}
	if (Readiness{ID: "t_1", Title: "Run", Href: "https://example.test"}).valid() {
		t.Fatal("external readiness link accepted")
	}
}

package agent

import (
	"strings"
	"testing"

	"devboard/internal/domain"
)

func policy(i domain.InteractionPolicy) domain.ExecutionPolicy {
	return domain.ExecutionPolicy{Interaction: i}
}

func TestInstructionsPerPolicy(t *testing.T) {
	if got := Instructions(domain.ExecutionPolicy{}); got != "" {
		t.Errorf("a task with no policy gets instructions: %q", got)
	}
	if got := Instructions(policy(domain.InteractionInteractive)); got != "" {
		t.Errorf("interactive runs behave as the agent does by itself, but got: %q", got)
	}

	auto := Instructions(policy(domain.InteractionAutonomous))
	for _, want := range []string{"AUTONOMOUS", "Investigate the repository yourself", "Make reasonable implementation decisions", "complete", "destructive"} {
		if !strings.Contains(auto, want) {
			t.Errorf("autonomous instructions lack %q:\n%s", want, auto)
		}
	}
	if strings.Contains(auto, BlockerMarker) {
		t.Error("an autonomous run is not told to report blockers: it decides")
	}
	// Autonomous never promises more than the agent's permissions already allow.
	for _, bad := range []string{"bypass", "without asking for permission", "full access", "force push is fine"} {
		if strings.Contains(strings.ToLower(auto), bad) {
			t.Errorf("autonomous instructions grant something (%q)", bad)
		}
	}

	stop := Instructions(policy(domain.InteractionAutonomousStopIfBlocked))
	if !strings.HasPrefix(stop, auto) {
		t.Error("stop-if-blocked includes everything autonomous says")
	}
	for _, want := range []string{"STOP IF BLOCKED", BlockerMarker, "do not guess", `"summary"`} {
		if !strings.Contains(stop, want) {
			t.Errorf("stop-if-blocked instructions lack %q:\n%s", want, stop)
		}
	}
}

func TestInteractiveQuestionsAreAlwaysAsked(t *testing.T) {
	for _, k := range []domain.QuestionKind{domain.QuestionClarification, domain.QuestionDecision, domain.QuestionApproval, domain.QuestionSelection, domain.QuestionInstruction} {
		h := HandleQuestion(policy(domain.InteractionInteractive), Question{Kind: k, Prompt: "p"}, 0)
		if h.Action != ActionAsk || h.Reply != "" || h.Blocker != nil {
			t.Errorf("interactive %s: %+v", k, h)
		}
		if got := HandleQuestion(domain.ExecutionPolicy{}, Question{Kind: k}, 0); got.Action != ActionAsk {
			t.Errorf("unset policy %s: %+v", k, got)
		}
	}
}

// Autonomous means "do not interrupt me with routine questions". It is not
// permission: an approval reaches the user whatever the policy says.
func TestNoPolicyAnswersAnApproval(t *testing.T) {
	for _, p := range domain.InteractionPolicies {
		for _, replies := range []int{0, 1, MaxAutoReplies, 1000} {
			q := Question{Kind: domain.QuestionApproval, Prompt: "Run `rm -rf build`?", Options: []string{domain.AnswerAllow, domain.AnswerDeny}}
			h := HandleQuestion(policy(p), q, replies)
			if h.Action != ActionAsk || h.Reply != "" || h.Blocker != nil {
				t.Errorf("%s (after %d replies) handled an approval itself: %+v", p, replies, h)
			}
		}
	}
}

func TestAutonomousAnswersOrdinaryQuestionsItself(t *testing.T) {
	for _, k := range []domain.QuestionKind{domain.QuestionClarification, domain.QuestionDecision, domain.QuestionSelection, domain.QuestionInstruction} {
		h := HandleQuestion(policy(domain.InteractionAutonomous), Question{Kind: k, Prompt: "Which?", Options: []string{"a", "b"}}, 0)
		if h.Action != ActionReply || h.Blocker != nil {
			t.Fatalf("%s: %+v", k, h)
		}
		for _, want := range []string{"Decide this yourself", "continue", "does not authorise anything"} {
			if !strings.Contains(h.Reply, want) {
				t.Errorf("reply lacks %q: %s", want, h.Reply)
			}
		}
	}
	// A run whose agent will not stop asking gets put to the user rather than answered forever.
	h := HandleQuestion(policy(domain.InteractionAutonomous), Question{Kind: domain.QuestionClarification, Prompt: "again?"}, MaxAutoReplies)
	if h.Action != ActionAsk {
		t.Errorf("after %d replies the question goes to the user: %+v", MaxAutoReplies, h)
	}
}

func TestStopIfBlockedBlocksOnOrdinaryQuestions(t *testing.T) {
	q := Question{Kind: domain.QuestionSelection, Prompt: "  Which   database\nshould I use? ", Context: " Both are in the repo. ", Options: []string{"sqlite", "postgres"}}
	h := HandleQuestion(policy(domain.InteractionAutonomousStopIfBlocked), q, 0)
	if h.Action != ActionBlock || h.Blocker == nil {
		t.Fatalf("handling = %+v", h)
	}
	b := h.Blocker
	if b.Summary != "Which database should I use?" || b.Detail != "Both are in the repo." || len(b.Options) != 2 ||
		b.Source != domain.BlockerQuestion || b.Kind != domain.QuestionSelection {
		t.Errorf("blocker = %+v", b)
	}
	if !strings.Contains(h.Reply, "End your turn") || !strings.Contains(h.Reply, "Do not guess") {
		t.Errorf("the agent is not told to stop: %s", h.Reply)
	}
	q.Options[0] = "changed"
	if b.Options[0] != "sqlite" {
		t.Error("the blocker aliases the question's options")
	}
	if StopReply() != h.Reply {
		t.Error("StopReply differs from the reply given when blocking")
	}
	if blank := HandleQuestion(policy(domain.InteractionAutonomousStopIfBlocked), Question{Kind: domain.QuestionClarification}, 0); blank.Blocker == nil || blank.Blocker.Summary == "" {
		t.Errorf("a blocker always has a summary: %+v", blank)
	}
}

func TestParseBlocker(t *testing.T) {
	text := "I looked at both options.\n\nDEVBOARD_BLOCKED {\"summary\": \"Need the production API key\", \"detail\": \"The client reads KEY.\", \"options\": [\"provide it\", \"use a mock\"]}\n"
	b, ok := ParseBlocker(text)
	if !ok || b.Summary != "Need the production API key" || b.Detail != "The client reads KEY." || len(b.Options) != 2 || b.Source != domain.BlockerReport {
		t.Fatalf("ParseBlocker = %+v, %v", b, ok)
	}

	// A colon after the marker is tolerated, and the last report wins.
	b, ok = ParseBlocker("DEVBOARD_BLOCKED: {\"summary\":\"first\"}\nmore text\n  DEVBOARD_BLOCKED: {\"summary\":\"second\"}")
	if !ok || b.Summary != "second" {
		t.Fatalf("ParseBlocker = %+v, %v", b, ok)
	}

	// The agent clearly meant to stop even if its JSON is broken.
	b, ok = ParseBlocker("DEVBOARD_BLOCKED I cannot choose between the two schemas")
	if !ok || b.Summary != "I cannot choose between the two schemas" || b.Source != domain.BlockerReport {
		t.Fatalf("plain-text report = %+v, %v", b, ok)
	}
	if b, ok := ParseBlocker("DEVBOARD_BLOCKED"); !ok || b.Summary == "" {
		t.Fatalf("empty report = %+v, %v", b, ok)
	}

	// Mentioning the marker mid-line, or not at all, is not a report.
	for _, text := range []string{"", "All done.", "I will write DEVBOARD_BLOCKED if I get stuck.", "no marker here\njust prose"} {
		if _, ok := ParseBlocker(text); ok {
			t.Errorf("%q parsed as a blocker", text)
		}
	}
}

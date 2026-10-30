package domain

import (
	"errors"
	"testing"
	"time"
)

func TestASessionReferenceIsDerivedFromTheOwnersIDAndRoundTrips(t *testing.T) {
	for _, ref := range []SessionRef{RunRef("run_k3j9x2m4q7p1a8z5"), TerminalRef("00ff-12_ab")} {
		got, err := ParseSessionRef(ref.String())
		if err != nil || got != ref {
			t.Errorf("%v -> %q -> %v, %v", ref, ref.String(), got, err)
		}
	}
	if got := RunRef("run_abc").String(); got != "run:run_abc" {
		t.Fatalf("a run is named %q", got)
	}
}

func TestOnlyExactReferencesAreAccepted(t *testing.T) {
	for _, bad := range []string{"", "run", "run:", ":run_a", "task:tsk_1", "run:a/b", "run:a b", "run:../x", "terminal:a:b", "RUN:run_a", "run:" + string(make([]byte, 97))} {
		if ref, err := ParseSessionRef(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q was accepted as %v", bad, ref)
		}
	}
}

func TestOnlyTheSalieOriginNamesAConversation(t *testing.T) {
	if got := ConversationOf(SalieOrigin("ast_1")); got != "ast_1" {
		t.Fatalf("got %q", got)
	}
	for _, other := range []string{"", "schedule:x", "salie", "salie:", "mcp:ast_1"} {
		if got := ConversationOf(other); got != "" {
			t.Errorf("%q names conversation %q", other, got)
		}
	}
}

func TestARunSessionCarriesItsTaskLaunchAndCapabilities(t *testing.T) {
	now := time.Now()
	run := Run{ID: "run_1", ProjectID: "prj_1", TaskID: "tsk_1", AgentID: "codex", State: RunWaitingForUser, Waiting: WaitQuestion, Origin: SalieOrigin("ast_9"), RunnerID: "rnr_1", CreatedAt: now, UpdatedAt: now}
	s := SessionOfRun(run, "Fix login", []string{"plan_a"})
	if s.Key != "run:run_1" || s.Kind != SessionRun || s.ID != "run_1" || s.TaskID != "tsk_1" || s.LaunchedBy != "ast_9" || s.Title != "Fix login" || len(s.PlanIDs) != 1 {
		t.Fatalf("%+v", s)
	}
	if !s.Live || !s.Waiting || !s.Capabilities.Feed || !s.Capabilities.Message || !s.Capabilities.Questions || !s.Capabilities.Stop || s.Capabilities.Terminal {
		t.Fatalf("a live run can be read, messaged, answered and stopped, and is not a terminal: %+v", s)
	}
	run.Waiting = WaitIdle
	if s = SessionOfRun(run, "", nil); s.Waiting || !s.Live || !s.Capabilities.Message {
		t.Fatalf("an agent that finished its turn is idle, not asking: %+v", s)
	}
	run.State, run.Waiting = RunCompleted, WaitNone
	if s = SessionOfRun(run, "", nil); s.Live || s.Capabilities.Message || s.Capabilities.Stop || !s.Capabilities.Feed {
		t.Fatalf("a finished run keeps its feed and nothing else: %+v", s)
	}
}

package domain

import (
	"errors"
	"testing"
	"time"
)

func TestExecutionPolicyDefaultsToInteractive(t *testing.T) {
	var zero ExecutionPolicy
	if got := zero.Normalized().Interaction; got != InteractionInteractive {
		t.Fatalf("zero policy normalizes to %q", got)
	}
	if err := zero.Validate(); err != nil {
		t.Fatalf("the zero policy is valid: %v", err)
	}
	if DefaultExecutionPolicy().Interaction != InteractionInteractive {
		t.Fatal("the default is not interactive")
	}
}

func TestExecutionPolicyValidation(t *testing.T) {
	for _, p := range InteractionPolicies {
		if err := (ExecutionPolicy{Interaction: p}).Validate(); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		if got, err := ParseInteraction(string(p)); err != nil || got != p {
			t.Errorf("ParseInteraction(%q) = %q, %v", p, got, err)
		}
	}
	for _, bad := range []string{"AUTONOMOUS", "yolo", "autonomous-stop", " "} {
		if err := (ExecutionPolicy{Interaction: InteractionPolicy(bad)}).Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted: %v", bad, err)
		}
		if _, err := ParseInteraction(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseInteraction(%q): %v", bad, err)
		}
	}
	if InteractionInteractive.Autonomous() || !InteractionAutonomous.Autonomous() || !InteractionAutonomousStopIfBlocked.Autonomous() {
		t.Error("Autonomous() is wrong")
	}
}

func TestBlockedRunStateMachine(t *testing.T) {
	now := time.Now()
	blocker := Blocker{Summary: "Which provider?", Source: BlockerQuestion}

	// Only a running run can be blocked; blocked is not a Kanban state.
	for _, from := range []RunState{RunStarting, RunWaitingForUser, RunCompleted, RunFailed, RunStopped} {
		r := &Run{ID: "r", State: from}
		if from == RunWaitingForUser {
			r.Waiting = WaitIdle
		}
		if err := r.Block(blocker, now); !errors.Is(err, ErrTransition) {
			t.Errorf("%s → blocked: err = %v", from, err)
		}
	}
	if TaskState(RunBlocked).Valid() {
		t.Fatal("blocked is a run state, not a board column")
	}

	r := &Run{ID: "r", State: RunRunning}
	if err := r.Block(Blocker{Source: BlockerReport}, now); !errors.Is(err, ErrInvalid) {
		t.Errorf("a blocker needs a summary: %v", err)
	}
	if err := r.Block(Blocker{Summary: "x", Source: "wishful"}, now); !errors.Is(err, ErrInvalid) {
		t.Errorf("a blocker needs a known source: %v", err)
	}
	if r.State != RunRunning || r.Blocker != nil {
		t.Fatalf("a refused block changed the run: %+v", r)
	}
	if err := r.Block(blocker, now); err != nil {
		t.Fatal(err)
	}
	if r.State != RunBlocked || r.Blocker == nil || !r.Blocker.RaisedAt.Equal(now) || r.Waiting != WaitNone || r.State.Terminal() || !r.State.Active() {
		t.Fatalf("blocked run = %+v", r)
	}

	// A blocked run can only be settled by running again, or ended.
	if err := r.WaitFor(WaitQuestion, now); !errors.Is(err, ErrTransition) {
		t.Errorf("blocked → waiting: %v", err)
	}
	if err := r.Transition(RunRunning, "", now); err != nil || r.Blocker != nil {
		t.Fatalf("resume: %v, blocker %+v", err, r.Blocker)
	}
	for _, end := range []RunState{RunCompleted, RunFailed, RunStopped} {
		b := &Run{ID: "b", State: RunRunning}
		_ = b.Block(blocker, now)
		if err := b.Transition(end, "why", now); err != nil {
			t.Errorf("blocked → %s: %v", end, err)
		}
		if b.Blocker == nil {
			t.Errorf("a run that ended blocked keeps saying why (%s)", end)
		}
	}
}

func TestAPolicyNeverAnswersAnApproval(t *testing.T) {
	approval := &Question{ID: "q", Kind: QuestionApproval, Options: []string{AnswerAllow, AnswerDeny}, State: QuestionPending}
	if err := approval.AcceptFromPolicy("Allow", time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a policy answered an approval: %v", err)
	}
	if approval.State != QuestionPending || approval.Answer != "" {
		t.Fatalf("approval changed: %+v", approval)
	}

	ask := &Question{ID: "q", Kind: QuestionClarification, State: QuestionPending, AllowFreeText: true}
	if err := ask.AcceptFromPolicy("  decide it yourself ", time.Now()); err != nil {
		t.Fatal(err)
	}
	if ask.State != QuestionAnswered || ask.Answer != "decide it yourself" || ask.AnsweredBy != AnsweredByPolicy || ask.AnsweredAt == nil {
		t.Fatalf("question = %+v", ask)
	}
	user := &Question{ID: "u", Kind: QuestionClarification, State: QuestionPending, AllowFreeText: true}
	if err := user.Accept("yes", time.Now()); err != nil || user.AnsweredBy != AnsweredByUser {
		t.Fatalf("user's answer: %v %+v", err, user)
	}
}

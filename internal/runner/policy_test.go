package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/service"
	"devboard/internal/store"
)

// These tests are about what an execution policy does to a run: which questions
// reach the user, which the controller deals with itself, and what a run that
// must stop rather than guess looks like. Policy is only about conversation; the
// tests that matter most for safety are the ones showing that no policy answers
// a permission request.

func (e *env) taskWith(title string, p domain.InteractionPolicy) *domain.Task {
	e.t.Helper()
	tk, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: e.project.ID, Title: title, Description: "Do the thing carefully.",
		Execution: domain.ExecutionConfig{Interaction: p}})
	if err != nil {
		e.t.Fatal(err)
	}
	return tk
}

func (e *env) questionsOf(runID string) []domain.Question {
	e.t.Helper()
	var out []domain.Question
	if err := e.db.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Questions().ListByRun(ctx, runID)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) waitBlocked(runID string) *domain.Run {
	e.t.Helper()
	return e.waitState(runID, domain.RunBlocked)
}

func (e *env) waitResponses(s *fakeSession, n int) []string {
	e.t.Helper()
	eventually(e.t, fmt.Sprintf("%d reply(ies) to the agent", n), func() bool { return len(s.Responses()) >= n })
	var out []string
	for _, r := range s.Responses() {
		out = append(out, r.Answer)
	}
	return out
}

func TestTaskWithoutAPolicyRunsInteractive(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Plain"))
	if run.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("run policy = %+v", run.Policy)
	}
	if got := e.session().Req.Policy.Interaction; got != domain.InteractionInteractive {
		t.Fatalf("the agent was given policy %q", got)
	}
	if agent.Instructions(e.session().Req.Policy) != "" {
		t.Fatal("an interactive run is given instructions")
	}
}

func TestRunKeepsTheTaskPolicyItStartedWith(t *testing.T) {
	e := newEnv(t)
	task := e.taskWith("Autonomous work", domain.InteractionAutonomous)
	run := e.start(task)
	if run.Policy.Interaction != domain.InteractionAutonomous || e.session().Req.Policy.Interaction != domain.InteractionAutonomous {
		t.Fatalf("run = %+v, agent was given %+v", run.Policy, e.session().Req.Policy)
	}

	// Editing the task changes what the next run does, not the one that is working.
	stop := domain.ExecutionConfig{Interaction: domain.InteractionAutonomousStopIfBlocked}
	cur, _ := e.tasks.Get(ctx, task.ID) // the run moved the card to Doing, so it is a version on
	if _, err := e.tasks.Update(ctx, task.ID, service.TaskPatch{Execution: &stop, Version: cur.Version}); err != nil {
		t.Fatal(err)
	}
	if got := e.run(run.ID).Policy.Interaction; got != domain.InteractionAutonomous {
		t.Fatalf("the working run's policy changed to %q", got)
	}
	if _, err := e.mgr.Finish(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	next := e.start(task)
	if next.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked {
		t.Fatalf("the next run's policy = %q", next.Policy.Interaction)
	}
	if _, err := e.mgr.Finish(ctx, next.ID); err != nil {
		t.Fatal(err)
	}

	// A run can be started with a policy of its own, for that run only.
	over := domain.ExecutionPolicy{Interaction: domain.InteractionInteractive}
	third, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake", Policy: &over})
	if err != nil {
		t.Fatal(err)
	}
	if third.Policy.Interaction != domain.InteractionInteractive || e.session().Req.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("override ignored: %+v", third.Policy)
	}
	if got, _ := e.tasks.Get(ctx, task.ID); got.Execution.Interaction != domain.InteractionAutonomousStopIfBlocked {
		t.Fatalf("the override changed the task: %+v", got.Execution)
	}
	bad := domain.ExecutionPolicy{Interaction: "reckless"}
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: e.task("other").ID, AgentID: "fake", Policy: &bad}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("an unknown policy was accepted: %v", err)
	}
}

// The existing behaviour, unchanged: the agent asks, the run waits for the user,
// the user answers, and the same session carries on.
func TestInteractiveQuestionsStillReachTheUser(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Interactive"))
	s := e.session()
	s.Ask("q1", "Which database?", "sqlite", "postgres")

	waiting := e.waitWaiting(run.ID, domain.WaitQuestion)
	q := e.waitPending(1)[0]
	if q.Prompt != "Which database?" || q.AnsweredBy != "" || waiting.Blocker != nil {
		t.Fatalf("question = %+v, run = %+v", q, waiting)
	}
	if len(s.Responses()) != 0 {
		t.Fatalf("the controller answered an interactive run's question: %v", s.Responses())
	}
	if _, err := e.mgr.Answer(ctx, q.ID, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if got := s.Responses(); len(got) != 1 || got[0].Answer != "sqlite" {
		t.Fatalf("responses = %+v", got)
	}
	if got := e.question(q.ID); got.AnsweredBy != domain.AnsweredByUser || got.DeliveredAt == nil {
		t.Fatalf("question = %+v", got)
	}
	e.waitState(run.ID, domain.RunRunning)
}

func TestAutonomousRunsAnswerOrdinaryQuestionsThemselves(t *testing.T) {
	e := newEnv(t)
	task := e.taskWith("Autonomous", domain.InteractionAutonomous)
	run := e.start(task)
	s := e.session()

	s.Ask("q1", "Which database should I use?", "sqlite", "postgres")
	replies := e.waitResponses(s, 1)
	if !strings.Contains(replies[0], "Decide this yourself") {
		t.Fatalf("the agent was told: %q", replies[0])
	}
	eventually(t, "the reply to be recorded as delivered", func() bool {
		qs := e.questionsOf(run.ID)
		return len(qs) == 1 && qs[0].DeliveredAt != nil
	})

	// The user was not interrupted: no pending question, and the run never waited.
	if len(e.pending()) != 0 {
		t.Fatalf("pending = %+v", e.pending())
	}
	r := e.run(run.ID)
	if r.State != domain.RunRunning || r.Waiting != domain.WaitNone {
		t.Fatalf("run = %+v; it should have kept working", r)
	}
	if e.taskState(task.ID) != domain.TaskDoing {
		t.Fatal("the task stays in Doing")
	}

	// What was asked, and how it was dealt with, is in the record.
	q := e.questionsOf(run.ID)[0]
	if q.Prompt != "Which database should I use?" || q.State != domain.QuestionAnswered || q.AnsweredBy != domain.AnsweredByPolicy || q.Answer != replies[0] {
		t.Fatalf("question = %+v", q)
	}
	if got := e.questionEventTypes(run.ID); !sameTypes(got, []domain.EventType{domain.EventAgentQuestion, domain.EventQuestionAnswered}) {
		t.Fatalf("question events = %v", got)
	}
	for _, ty := range e.agentTypes(run.ID) {
		if ty == domain.EventAgentWaiting || ty == domain.EventAgentBlocked {
			t.Fatalf("an autonomous run reported %s", ty)
		}
	}

	// The agent carries on and finishes its turn as usual.
	s.Assistant("Went with sqlite.")
	s.TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
}

func TestAutonomousRunsAnswerEveryKindOfOrdinaryQuestion(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.taskWith("Autonomous", domain.InteractionAutonomous))
	s := e.session()
	kinds := []domain.QuestionKind{domain.QuestionClarification, domain.QuestionDecision, domain.QuestionSelection, domain.QuestionInstruction}
	for i, k := range kinds {
		s.AskQuestion(agent.Question{Ref: fmt.Sprintf("q%d", i), Kind: k, Prompt: string(k) + "?", AllowFreeText: true})
	}
	e.waitResponses(s, len(kinds))
	if len(e.pending()) != 0 || e.run(run.ID).State != domain.RunRunning {
		t.Fatalf("pending = %+v, run = %+v", e.pending(), e.run(run.ID))
	}
}

// An agent that keeps asking after being told to decide is not complying;
// answering for the user forever would hide the loop.
func TestAutonomousRunsEventuallyAskTheUser(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.taskWith("Insistent", domain.InteractionAutonomous))
	s := e.session()
	for i := 0; i < agent.MaxAutoReplies; i++ {
		s.Ask(fmt.Sprintf("q%d", i), "again?")
		e.waitResponses(s, i+1)
	}
	s.Ask("last", "once more?")
	e.waitWaiting(run.ID, domain.WaitQuestion)
	if q := e.waitPending(1)[0]; q.Prompt != "once more?" || q.AnsweredBy != "" {
		t.Fatalf("question = %+v", q)
	}
	if len(s.Responses()) != agent.MaxAutoReplies {
		t.Fatalf("responses = %d", len(s.Responses()))
	}
}

// Autonomous means "do not interrupt me with routine questions". It does not mean
// "I have approved whatever you ask for": permission requests reach the user under
// every policy, and only the user's answer is passed on.
func TestNoPolicyBypassesAPermissionRequest(t *testing.T) {
	for _, p := range domain.InteractionPolicies {
		t.Run(string(p), func(t *testing.T) {
			e := newEnv(t)
			run := e.start(e.taskWith("Needs permission", p))
			s := e.session()
			s.RequestApproval("a1", "Run `rm -rf node_modules`?")

			e.waitWaiting(run.ID, domain.WaitQuestion)
			q := e.waitPending(1)[0]
			if q.Kind != domain.QuestionApproval || q.AnsweredBy != "" || q.State != domain.QuestionPending {
				t.Fatalf("question = %+v", q)
			}
			r := e.run(run.ID)
			if r.State == domain.RunBlocked || r.Blocker != nil {
				t.Fatalf("a permission request blocked the run: %+v", r)
			}
			if len(s.Responses()) != 0 {
				t.Fatalf("the controller granted or refused a permission itself: %v", s.Responses())
			}

			// A policy cannot be made to answer it by sending it another way, either.
			if _, err := e.mgr.Answer(ctx, q.ID, "Allow"); err != nil {
				t.Fatal(err)
			}
			if got := s.Responses(); len(got) != 1 || got[0].Answer != domain.AnswerAllow {
				t.Fatalf("responses = %+v", got)
			}
			if got := e.question(q.ID); got.AnsweredBy != domain.AnsweredByUser {
				t.Fatalf("answered by %q", got.AnsweredBy)
			}
		})
	}
}

// Approvals stay the user's even when the agent asks an ordinary question in the
// same breath: the ordinary one is handled by the policy, the approval is not.
func TestAnApprovalAlongsideAnOrdinaryQuestion(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.taskWith("Both", domain.InteractionAutonomous))
	s := e.session()
	s.RequestApproval("a1", "Run the migration?")
	e.waitWaiting(run.ID, domain.WaitQuestion)
	s.Ask("q1", "Which schema?") // asked while the approval is open: the run is not working, so the user is asked
	qs := e.waitPending(2)
	if qs[0].Kind != domain.QuestionApproval || qs[1].Prompt != "Which schema?" {
		t.Fatalf("pending = %+v", qs)
	}
	if len(s.Responses()) != 0 {
		t.Fatalf("responses = %+v", s.Responses())
	}
}

func TestStopIfBlockedBlocksOnAQuestionInsteadOfAsking(t *testing.T) {
	e := newEnv(t)
	task := e.taskWith("Careful", domain.InteractionAutonomousStopIfBlocked)
	run := e.start(task)
	s := e.session()
	s.Ref("sess-b")
	s.Ask("q1", "Which payment provider should I integrate?", "Stripe", "Adyen")

	blocked := e.waitBlocked(run.ID)
	b := blocked.Blocker
	if b == nil || b.Summary != "Which payment provider should I integrate?" || len(b.Options) != 2 || b.Source != domain.BlockerQuestion ||
		b.Kind != domain.QuestionSelection || b.RaisedAt.IsZero() {
		t.Fatalf("blocker = %+v", b)
	}
	if blocked.Waiting != domain.WaitNone || len(e.pending()) != 0 {
		t.Fatalf("a blocked run is not waiting on a question: %+v / pending %+v", blocked, e.pending())
	}
	// Run state is not Kanban state: the card stays in Doing.
	if e.taskState(task.ID) != domain.TaskDoing {
		t.Fatalf("task = %s", e.taskState(task.ID))
	}
	// The agent was told to stop, not given a decision.
	replies := e.waitResponses(s, 1)
	if !strings.Contains(replies[0], "End your turn") || !strings.Contains(replies[0], "Do not guess") {
		t.Fatalf("the agent was told: %q", replies[0])
	}
	if !sameTypes(e.questionEventTypes(run.ID), []domain.EventType{domain.EventAgentQuestion, domain.EventQuestionAnswered}) {
		t.Fatalf("question events = %v", e.questionEventTypes(run.ID))
	}
	evs := e.eventsOf(run.ID, domain.EventAgentBlocked)
	if len(evs) != 1 || evs[0].ProjectID != e.project.ID || evs[0].TaskID != task.ID {
		t.Fatalf("agent.blocked events = %+v", evs)
	}

	// Ending its turn does not make a blocked run idle.
	s.Assistant("I stopped; I need a decision on the payment provider.")
	s.TurnEnd()
	// A further question while blocked is turned away the same way, without a second blocker.
	s.Ask("q2", "And the currency?")
	e.waitResponses(s, 2)
	if got := e.run(run.ID); got.State != domain.RunBlocked || got.Blocker.Summary != b.Summary {
		t.Fatalf("run = %+v / %+v", got, got.Blocker)
	}
	if n := len(e.eventsOf(run.ID, domain.EventAgentBlocked)); n != 1 {
		t.Fatalf("%d agent.blocked events", n)
	}

	// The user settles it with a message; the same session goes on, and the blocker is cleared.
	if err := e.mgr.Send(ctx, run.ID, "Use Stripe."); err != nil {
		t.Fatal(err)
	}
	resumed := e.waitState(run.ID, domain.RunRunning)
	if resumed.Blocker != nil {
		t.Fatalf("the settled blocker is still on the run: %+v", resumed.Blocker)
	}
	if got := s.Sent(); len(got) != 1 || got[0] != "Use Stripe." {
		t.Fatalf("the agent was sent %v", got)
	}
	s.TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
}

func TestStopIfBlockedBlocksOnAReportAtTheEndOfATurn(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.taskWith("Reports", domain.InteractionAutonomousStopIfBlocked))
	s := e.session()

	s.Assistant("I read the config loader and both options look plausible.")
	s.Assistant("DEVBOARD_BLOCKED {\"summary\":\"Need the production API key\",\"detail\":\"The client reads API_KEY from the environment.\",\"options\":[\"provide it\",\"use the sandbox key\"]}")
	s.TurnEnd()

	blocked := e.waitBlocked(run.ID)
	b := blocked.Blocker
	if b.Summary != "Need the production API key" || b.Detail != "The client reads API_KEY from the environment." || len(b.Options) != 2 || b.Source != domain.BlockerReport {
		t.Fatalf("blocker = %+v", b)
	}
	if len(e.questionsOf(run.ID)) != 0 || len(e.pending()) != 0 {
		t.Fatal("a reported blocker is not a question")
	}
	for _, ty := range e.agentTypes(run.ID) {
		if ty == domain.EventAgentWaiting {
			t.Fatal("the run went idle instead of blocked")
		}
	}

	// The next turn is ordinary again once the user has replied.
	if err := e.mgr.Send(ctx, run.ID, "Use the sandbox key."); err != nil {
		t.Fatal(err)
	}
	e.waitState(run.ID, domain.RunRunning)
	s.Assistant("Done with the sandbox key.")
	s.TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
}

// Only a run that must stop when blocked treats the marker as anything.
func TestBlockerReportsOnlyCountWhenThePolicyAsksForThem(t *testing.T) {
	for _, p := range []domain.InteractionPolicy{domain.InteractionInteractive, domain.InteractionAutonomous} {
		t.Run(string(p), func(t *testing.T) {
			e := newEnv(t)
			run := e.start(e.taskWith("Marker", p))
			s := e.session()
			s.Assistant("DEVBOARD_BLOCKED {\"summary\":\"stuck\"}")
			s.TurnEnd()
			e.waitWaiting(run.ID, domain.WaitIdle)
			if e.run(run.ID).Blocker != nil {
				t.Fatal("blocked without the policy asking for it")
			}
		})
	}
}

// A turn that merely ends is not a blocker, even for a run that may report one.
func TestStopIfBlockedRunsEndOrdinaryTurnsNormally(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.taskWith("Fine", domain.InteractionAutonomousStopIfBlocked))
	s := e.session()
	s.Assistant("Implemented it. Tests pass.")
	s.TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)
	if e.run(run.ID).Blocker != nil {
		t.Fatal("blocked on an ordinary turn")
	}
	// The next turn, started by the user, can still end in a blocker.
	if err := e.mgr.Send(ctx, run.ID, "Now the second part."); err != nil {
		t.Fatal(err)
	}
	e.waitState(run.ID, domain.RunRunning)
	s.Assistant("DEVBOARD_BLOCKED {\"summary\":\"second part needs a decision\"}")
	s.TurnEnd()
	if b := e.waitBlocked(run.ID).Blocker; b.Summary != "second part needs a decision" {
		t.Fatalf("blocker = %+v", b)
	}
}

func TestABlockedRunCanBeStoppedOrFinished(t *testing.T) {
	for _, how := range []string{"stop", "finish"} {
		t.Run(how, func(t *testing.T) {
			e := newEnv(t)
			task := e.taskWith("Ends blocked", domain.InteractionAutonomousStopIfBlocked)
			run := e.start(task)
			e.session().Ask("q", "Which?")
			e.waitBlocked(run.ID)

			var got *domain.Run
			var err error
			if how == "stop" {
				got, err = e.mgr.Stop(ctx, run.ID)
			} else {
				got, err = e.mgr.Finish(ctx, run.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			want := domain.RunStopped
			if how == "finish" {
				want = domain.RunCompleted
			}
			if got.State != want || got.PID != 0 {
				t.Fatalf("run = %+v", got)
			}
			if got.Blocker == nil {
				t.Fatal("a run that ended blocked still says why")
			}
			if e.taskState(task.ID) != domain.TaskDoing {
				t.Fatal("ending a run never moves a card")
			}
		})
	}
}

// A restart takes the process, not the blocker: the run is still blocked, still
// says why, and a message resumes the same session under the same policy.
func TestABlockedRunSurvivesARestart(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash=%v", crash), func(t *testing.T) {
			e := newEnv(t)
			task := e.taskWith("Survives", domain.InteractionAutonomousStopIfBlocked)
			run := e.start(task)
			e.session().Ref("sess-block")
			eventually(t, "the session ref", func() bool { return e.run(run.ID).SessionRef == "sess-block" })
			e.session().Ask("q", "Which cache backend?", "redis", "memory")
			before := e.waitBlocked(run.ID).Blocker

			e.restart(crash)

			r := e.run(run.ID)
			if r.State != domain.RunBlocked || r.PID != 0 || r.Blocker == nil || r.Blocker.Summary != before.Summary || len(r.Blocker.Options) != 2 {
				t.Fatalf("run after the restart = %+v / %+v", r, r.Blocker)
			}
			if e.taskState(task.ID) != domain.TaskDoing {
				t.Fatal("the card moved")
			}

			if err := e.mgr.Send(ctx, run.ID, "Use redis."); err != nil {
				t.Fatal(err)
			}
			if req := e.session().Req; req.ResumeRef != "sess-block" || req.RunID != run.ID || req.Prompt != "Use redis." ||
				req.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked {
				t.Fatalf("resume request = %+v", req)
			}
			if got := e.waitState(run.ID, domain.RunRunning); got.Blocker != nil {
				t.Fatalf("the blocker survived being settled: %+v", got.Blocker)
			}
		})
	}
}

func TestABlockedRunWithoutAProcessCanBeEnded(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.taskWith("Gone", domain.InteractionAutonomousStopIfBlocked))
	e.session().Ref("sess-x")
	eventually(t, "the session ref", func() bool { return e.run(run.ID).SessionRef == "sess-x" })
	e.session().Ask("q", "Which?")
	e.waitBlocked(run.ID)
	e.restart(false)
	got, err := e.mgr.Stop(ctx, run.ID)
	if err != nil || got.State != domain.RunStopped || got.Blocker == nil {
		t.Fatalf("stop = %+v, %v", got, err)
	}
}

// If the agent cannot take the controller's reply, the question is closed, not
// left looking answered.
func TestAReplyThatCannotBeDeliveredCancelsTheQuestion(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.taskWith("Undeliverable", domain.InteractionAutonomous))
	s := e.session()
	s.OnRespond = func(string, string) error { return agent.ErrUnknownQuestion }
	s.Ask("q1", "Which?")
	eventually(t, "the question to be cancelled", func() bool {
		qs := e.questionsOf(run.ID)
		return len(qs) == 1 && qs[0].State == domain.QuestionCancelled
	})
	q := e.questionsOf(run.ID)[0]
	if q.CancelReason != domain.CancelWithdrawn || q.DeliveredAt != nil || q.AnsweredBy != domain.AnsweredByPolicy {
		t.Fatalf("question = %+v", q)
	}
	if e.run(run.ID).State != domain.RunRunning || len(e.pending()) != 0 {
		t.Fatal("the run should be unaffected")
	}
}

func TestPolicyEventsCarryTheirProject(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.taskWith("Events", domain.InteractionAutonomousStopIfBlocked))
	e.session().Ask("q", "Which?")
	e.waitBlocked(run.ID)
	for _, ev := range e.eventsOf(run.ID, domain.EventAgentBlocked) {
		var p struct{ Blocker domain.Blocker }
		if err := json.Unmarshal(ev.Payload, &p); err != nil || p.Blocker.Summary != "Which?" || ev.RunID != run.ID {
			t.Fatalf("event = %+v %+v %v", ev, p, err)
		}
	}
}

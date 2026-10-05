package service

import (
	"context"
	"fmt"
	"strings"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/runnerwire"
	"devboard/internal/store"
)

func (s *Runners) observe(ctx context.Context, tx store.Tx, em *emitter, r *domain.Run, j *runnerwire.Job, ob runnerwire.Observation) error {
	if r.State.Terminal() {
		if ob.Kind != "workspace" {
			return nil
		}
		if ob.HeadCommit != "" && !validCommit(ob.HeadCommit) {
			return domain.ErrInvalid
		}
		r.HeadCommit, r.Uncommitted = ob.HeadCommit, ob.Uncommitted
		// Git reconciliation changes no execution or task state.
		if e := tx.Runs().Update(ctx, r); e != nil {
			return e
		}
		return emitRun(em, r, r.State)
	}
	from := r.State
	now := s.now()
	persist := func() error {
		r.UpdatedAt = now
		if e := tx.Runs().Update(ctx, r); e != nil {
			return e
		}
		if e := emitRun(em, r, from); e != nil {
			return e
		}
		if from != r.State {
			kind := domain.EventAgentWaiting
			if r.State == domain.RunRunning {
				kind = domain.EventAgentStarted
			}
			if r.State == domain.RunBlocked {
				kind = domain.EventAgentBlocked
			}
			return emitAgent(em, kind, r, map[string]any{"blocker": r.Blocker})
		}
		return nil
	}
	switch ob.Kind {
	case "accepted":
		if r.State != domain.RunStarting || j.Ack != 0 {
			return fmt.Errorf("%w: invalid job acceptance", domain.ErrConflict)
		}
		return nil
	case "started":
		if r.State != domain.RunStarting {
			return fmt.Errorf("%w: duplicate start", domain.ErrConflict)
		}
		expectedBranch := j.WorkBranch
		if expectedBranch == "" {
			expectedBranch = "devboard/" + r.RunnerID + "/" + r.ID
		}
		if ob.Branch != expectedBranch {
			return fmt.Errorf("%w: invalid owned branch", domain.ErrInvalid)
		}
		if ob.BaseCommit != "" && !validCommit(ob.BaseCommit) {
			return domain.ErrInvalid
		}
		r.Branch, r.BaseCommit = ob.Branch, ob.BaseCommit
		if e := r.Transition(domain.RunRunning, "", now); e != nil {
			return e
		}
		if e := moveTaskToDoing(ctx, tx, em, r.TaskID, now); e != nil {
			return e
		}
		return persist()
	case "ended":
		if ob.Result == nil || !ob.Result.State.Terminal() {
			return domain.ErrInvalid
		}
		if ob.HeadCommit != "" && !validCommit(ob.HeadCommit) {
			return domain.ErrInvalid
		}
		r.HeadCommit = ob.HeadCommit
		r.Uncommitted = ob.Uncommitted
		if ob.Usage != nil {
			if e := ob.Usage.Validate(); e != nil {
				return e
			}
			acceptance := r.Usage.Acceptance
			r.Usage = domain.MergeUsage(r.Usage, *ob.Usage)
			r.Usage.Acceptance = acceptance
		}
		code := ob.Result.ExitCode
		if e := s.Runs.end(ctx, tx, em, r, Ended{State: ob.Result.State, Reason: ob.Result.Reason, ExitCode: &code}, domain.CancelRunEnded); e != nil {
			return e
		}
		if r.State == domain.RunCompleted {
			t, e := tx.Tasks().Get(ctx, r.TaskID)
			if e != nil {
				return e
			}
			if t.State != domain.TaskDone && t.State != domain.TaskReview {
				max, e := tx.Tasks().MaxPosition(ctx, t.ProjectID, domain.TaskReview)
				if e != nil {
					return e
				}
				t.State = domain.TaskReview
				t.Position = max + 1
				t.UpdatedAt = now
				if e := tx.Tasks().Update(ctx, t); e != nil {
					return e
				}
				ev := newEvent(domain.EventTaskUpdated, t)
				ev.ProjectID = t.ProjectID
				ev.TaskID = t.ID
				return em.emit(ev)
			}
		}
		return nil
	case "command":
		var cmd *runnerwire.Command
		for i := range j.Commands {
			if j.Commands[i].ID == ob.CommandID {
				c := j.Commands[i]
				cmd = &c
				j.Commands = append(j.Commands[:i], j.Commands[i+1:]...)
				break
			}
		}
		if cmd == nil {
			return nil
		}
		if ob.Error != "" {
			if e := emitOutput(em, r, domain.AgentOutput{Stream: domain.StreamSystem, Text: truncate("Runner command failed: "+ob.Error, 1000)}); e != nil {
				return e
			}
			if cmd.QuestionID != "" {
				q, e := tx.Questions().Get(ctx, cmd.QuestionID)
				if e != nil {
					return e
				}
				if q.Blocking() {
					if e := q.Cancel(domain.CancelWithdrawn, now); e != nil {
						return e
					}
					if e := tx.Questions().Update(ctx, q); e != nil {
						return e
					}
					if e := emitClosed(em, r, q); e != nil {
						return e
					}
				}
			}
			// A response delivery failure cannot leave an invisible stuck process.
			enqueue(j, "stop", "", "", "")
			return nil
		}
		if cmd.QuestionID != "" {
			q, e := tx.Questions().Get(ctx, cmd.QuestionID)
			if e != nil {
				return e
			}
			if q.State == domain.QuestionAnswered && q.DeliveredAt == nil {
				if e := q.MarkDelivered(now); e != nil {
					return e
				}
				if e := tx.Questions().Update(ctx, q); e != nil {
					return e
				}
			}
		}
		if cmd.Kind == "send" && (r.State == domain.RunBlocked || r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitIdle) {
			return s.Runs.resume(ctx, tx, em, r, "runner delivery")
		}
		if cmd.Kind == "respond" {
			return s.Runs.resumeIfUnblocked(ctx, tx, em, r, "runner delivery")
		}
		return nil
	case "event":
		ev := ob.Event
		if ev == nil {
			return domain.ErrInvalid
		}
		switch ev.Kind {
		case agent.KindOutput:
			if j.OutputBytes >= 16<<20 {
				return nil
			}
			text := truncate(ev.Text, maxOutputBytes)
			j.OutputBytes += int64(len(text))
			if ev.Stream != domain.StreamAssistant && ev.Stream != domain.StreamSystem && ev.Stream != domain.StreamStderr && ev.Stream != domain.StreamTool {
				return domain.ErrInvalid
			}
			if ev.Stream == domain.StreamAssistant {
				j.Turn += text
				if len(j.Turn) > 16000 {
					j.Turn = j.Turn[len(j.Turn)-16000:]
				}
			}
			if e := tx.Runs().TouchActivity(ctx, r.ID, oneLine(text), now); e != nil {
				return e
			}
			return emitOutput(em, r, domain.AgentOutput{Stream: ev.Stream, Text: text})
		case agent.KindSessionRef:
			r.SessionRef = truncate(ev.SessionRef, 1000)
			return persist()
		case agent.KindQuestion:
			if ev.Question == nil || !ev.Question.Kind.Valid() || ev.Question.Ref == "" || len(ev.Question.Ref) > 500 {
				return domain.ErrInvalid
			}
			qIn := ev.Question
			if _, ok := j.Questions[qIn.Ref]; ok {
				return nil
			}
			q := s.Runs.newQuestion(r.ID, NewQuestion{Kind: qIn.Kind, Prompt: qIn.Prompt, Context: qIn.Context, Options: qIn.Options, AllowFreeText: qIn.AllowFreeText})
			q.ProjectID, q.TaskID = r.ProjectID, r.TaskID
			handling := agent.HandleQuestion(r.Policy, *qIn, j.Replies)
			if r.State == domain.RunBlocked {
				handling.Reply = agent.StopReply()
				handling.Action = agent.ActionBlock
				handling.Blocker = nil
			}
			if r.State != domain.RunRunning && r.State != domain.RunBlocked && !(r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitQuestion) {
				return fmt.Errorf("%w: cannot ask in %s", domain.ErrConflict, r.State)
			}
			if r.State == domain.RunRunning {
				if e := r.WaitFor(domain.WaitQuestion, now); e != nil {
					return e
				}
			}
			if e := tx.Questions().Create(ctx, q); e != nil {
				return e
			}
			j.Questions[qIn.Ref] = q.ID
			if e := emitAgent(em, domain.EventAgentQuestion, r, map[string]any{"question": q}); e != nil {
				return e
			}
			if handling.Action != agent.ActionAsk {
				if e := q.AcceptFromPolicy(handling.Reply, now); e != nil {
					return e
				}
				q.AnsweredBy = domain.AnsweredByPolicy
				if e := tx.Questions().Update(ctx, q); e != nil {
					return e
				}
				if e := emitAnswered(em, r, q); e != nil {
					return e
				}
				enqueue(j, "respond", qIn.Ref, q.Answer, q.ID)
				j.Replies++
				if handling.Action == agent.ActionBlock && handling.Blocker != nil {
					// The policy blocker takes precedence after response acknowledgment.
					if e := r.Transition(domain.RunRunning, "", now); e != nil {
						return e
					}
					if e := r.Block(*handling.Blocker, now); e != nil {
						return e
					}
				}
			}
			return persist()
		case agent.KindQuestionClosed:
			id := j.Questions[ev.Ref]
			if id == "" {
				return nil
			}
			delete(j.Questions, ev.Ref)
			q, e := tx.Questions().Get(ctx, id)
			if e != nil {
				return e
			}
			if q.Blocking() {
				if e := q.Cancel(domain.CancelWithdrawn, now); e != nil {
					return e
				}
				if e := tx.Questions().Update(ctx, q); e != nil {
					return e
				}
				if e := emitClosed(em, r, q); e != nil {
					return e
				}
			}
			return s.Runs.resumeIfUnblocked(ctx, tx, em, r, "question withdrawn")
		case agent.KindTurnEnd:
			if r.State != domain.RunRunning {
				return nil
			}
			if r.Policy.Interaction == domain.InteractionAutonomousStopIfBlocked {
				if b, ok := agent.ParseBlocker(j.Turn); ok {
					j.Turn = ""
					if e := r.Block(b, now); e != nil {
						return e
					}
					return persist()
				}
			}
			j.Turn = ""
			if e := r.WaitFor(domain.WaitIdle, now); e != nil {
				return e
			}
			if r.ScheduleKey != "" {
				enqueue(j, "finish", "", "", "")
			}
			return persist()
		case agent.KindUsage:
			if ev.Usage == nil {
				return domain.ErrInvalid
			}
			if e := ev.Usage.Validate(); e != nil {
				return e
			}
			acceptance := r.Usage.Acceptance
			r.Usage = domain.MergeUsage(r.Usage, *ev.Usage)
			r.Usage.Acceptance = acceptance
			return persist()
		default:
			return domain.ErrInvalid
		}
	default:
		return domain.ErrInvalid
	}
}

func (s *Runners) Command(ctx context.Context, id, kind, text string) (*domain.Run, error) {
	var out *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		r, e := tx.Runs().Get(ctx, id)
		if e != nil {
			return e
		}
		if !r.Remote || !r.State.Active() {
			return domain.ErrConflict
		}
		j, e := loadJob(ctx, tx, id)
		if e != nil {
			return e
		}
		if len(j.Commands) >= 100 {
			return fmt.Errorf("%w: too many pending commands; reconnect runner", domain.ErrConflict)
		}
		switch kind {
		case "send":
			text = strings.TrimSpace(text)
			if text == "" || len(text) > maxPromptBytes {
				return domain.ErrInvalid
			}
			if r.State != domain.RunBlocked && !(r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitIdle) && r.State != domain.RunRunning {
				return domain.ErrConflict
			}
			if e := emitOutput(em, r, domain.AgentOutput{Stream: domain.StreamUser, Text: text}); e != nil {
				return e
			}
		case "finish":
			qs, e := tx.Questions().ListByRun(ctx, id)
			if e != nil {
				return e
			}
			for _, q := range qs {
				if q.Blocking() {
					return fmt.Errorf("%w: answer pending questions before finishing", domain.ErrConflict)
				}
			}
		case "stop":
		default:
			return domain.ErrInvalid
		}
		enqueue(j, kind, "", text, "")
		if e := saveJob(ctx, tx, j, s.now()); e != nil {
			return e
		}
		out = r
		return nil
	})
	return out, err
}
func (s *Runners) Answer(ctx context.Context, id, answer string) (*domain.Question, error) {
	var out *domain.Question
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		q, e := tx.Questions().Get(ctx, id)
		if e != nil {
			return e
		}
		r, e := tx.Runs().Get(ctx, q.RunID)
		if e != nil {
			return e
		}
		if !r.Remote {
			return domain.ErrInvalid
		}
		if !q.Pending() {
			if q.SameAnswer(answer) {
				out = q
				return nil
			}
			return &domain.QuestionClosedError{Question: q}
		}
		j, e := loadJob(ctx, tx, r.ID)
		if e != nil {
			return e
		}
		ref := ""
		for k, v := range j.Questions {
			if v == id {
				ref = k
				break
			}
		}
		if ref == "" {
			return domain.ErrConflict
		}
		if e := q.Accept(answer, s.now()); e != nil {
			return e
		}
		if e := tx.Questions().Update(ctx, q); e != nil {
			return e
		}
		if e := emitAnswered(em, r, q); e != nil {
			return e
		}
		enqueue(j, "respond", ref, q.Answer, q.ID)
		if e := saveJob(ctx, tx, j, s.now()); e != nil {
			return e
		}
		out = q
		return nil
	})
	return out, err
}

func validCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	for _, c := range commit {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"werkbord/internal/domain"
	"werkbord/internal/planning"
	"werkbord/internal/store"
)

// Guided plan execution is a thin coordinator over the scheduler. A plan owns no runs: running it arms the plan's
// eligible tasks as one-shot schedules, in one transaction, and the scheduler loop dispatches them task by task. The
// unique (task_id, schedule_key) claim keeps that duplicate-free across restarts and reconnects.

// Plan run control: the one action the plan offers now.
const (
	PlanControlRun    = "run"    // Run plan
	PlanControlHold   = "hold"   // Pause after current task
	PlanControlResume = "resume" // Resume
	PlanControlNone   = "none"   // nothing left to start
)

// Step actions in a preview.
const (
	PlanStepStart = "start" // will be armed
	PlanStepRetry = "retry" // its last session failed or was stopped; a new one will be armed
	PlanStepArmed = "armed" // already armed and waiting for the scheduler
	PlanStepSkip  = "skip"  // left alone, with the reason
)

const (
	planBillingNote      = "Each step runs your own coding agent under your own account. Werkbord adds no charge and does not estimate what a provider will bill."
	planInterruptionNote = "Pausing disarms steps that have not started and never stops a running agent; the adapters support stop and restart, not pause. If the app closes, running sessions are recovered as described in Interrupted work."
	planReviewNote       = "A finished step moves to Review. The next step starts when the run before it completes successfully, not when you approve it, so review as the plan runs."
)

// PlanRunStep is one task of the plan as a run would treat it.
type PlanRunStep struct {
	TaskID   string   `json:"taskId"`
	Number   int      `json:"number"`
	Title    string   `json:"title"`
	Action   string   `json:"action"`
	Why      string   `json:"why,omitempty"`
	After    []string `json:"after"`
	Agent    string   `json:"agent,omitempty"`
	Runner   string   `json:"runner,omitempty"`
	Decision string   `json:"decision,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// PlanRunPreview is what Run would do, and the digest that binds a Run request to it.
type PlanRunPreview struct {
	PlanID      string        `json:"planId"`
	PlanVersion int64         `json:"planVersion"`
	Mode        string        `json:"mode"`
	State       string        `json:"state"` // idle, running or held
	Control     string        `json:"control"`
	Digest      string        `json:"digest"`
	Steps       []PlanRunStep `json:"steps"`
	ToStart     int           `json:"toStart"`
	Concurrency int           `json:"concurrency"`
	Billing     string        `json:"billing"`
	Interrupt   string        `json:"interruption"`
	Review      string        `json:"review"`
}

// PlanRunInput binds a request to the plan and preview a person looked at.
type PlanRunInput struct {
	Mode    string `json:"mode"`
	Version int64  `json:"version"`
	Digest  string `json:"digest"`
}

func planModeOr(mode, fallback string) (string, error) {
	switch mode {
	case "":
		return fallback, nil
	case domain.PlanModeInOrder, domain.PlanModeAsReady:
		return mode, nil
	}
	return "", fmt.Errorf("%w: mode must be %s or %s", domain.ErrInvalid, domain.PlanModeInOrder, domain.PlanModeAsReady)
}

// planRunRead works out, from stored facts only, what a run of the plan would do. Live capacity is shown, but it is not part
// of the digest, so a busy moment never invalidates a preview.
func (s *Scheduler) planRunRead(ctx context.Context, tx store.Tx, projectID, planID, mode string) (*domain.WorkPlan, PlanRunPreview, map[string]domain.Task, error) {
	var out PlanRunPreview
	x, err := scheduleRead(ctx, tx, projectID)
	if err != nil {
		return nil, out, nil, err
	}
	plans, err := tx.WorkPlans().List(ctx, projectID)
	if err != nil {
		return nil, out, nil, err
	}
	var plan *domain.WorkPlan
	for i := range plans {
		if plans[i].ID == planID {
			plan = &plans[i]
		}
	}
	if plan == nil {
		return nil, out, nil, domain.ErrNotFound
	}
	fallback := domain.PlanModeInOrder
	if plan.Run != nil {
		fallback = plan.Run.Mode
	}
	if mode, err = planModeOr(mode, fallback); err != nil {
		return nil, out, nil, err
	}
	var global domain.ExecutionConfig
	if e := tx.Settings().Get(ctx, domain.SettingExecution, &global); e != nil && !errors.Is(e, domain.ErrNotFound) {
		return nil, out, nil, e
	}
	byID := map[string]domain.Task{}
	for _, t := range x.tasks {
		byID[t.ID] = t
	}
	out = PlanRunPreview{PlanID: plan.ID, PlanVersion: plan.Version, Mode: mode, State: "idle", Steps: []PlanRunStep{}, Concurrency: x.limit, Billing: planBillingNote, Interrupt: planInterruptionNote, Review: planReviewNote}
	if plan.Run != nil {
		out.State = plan.Run.State
	}
	prev := "" // the closest earlier step still worth waiting for
	for i, id := range plan.TaskIDs {
		step := PlanRunStep{TaskID: id, Number: i + 1, After: []string{}}
		t, ok := byID[id]
		if !ok {
			step.Action, step.Why = PlanStepSkip, "This task is unavailable."
			out.Steps = append(out.Steps, step)
			continue
		}
		step.Title = t.Title
		resolved := domain.ResolveExecution(domain.Level{Config: t.Execution}, domain.Level{Config: x.project.Execution}, domain.Level{Config: global})
		step.Agent, step.Runner = resolved.Agent, resolved.Runner
		o := t.Orchestration
		armed := o.Enabled && o.Key != "" && o.RunID == "" && !o.Missed
		chained := mode == domain.PlanModeInOrder && prev != ""
		if chained {
			step.After = append(step.After, byID[prev].Title)
		}
		latest, ran := x.latest[t.ID]
		switch {
		case t.ArchivedAt != nil:
			step.Action, step.Why = PlanStepSkip, "Archived."
		case t.State == domain.TaskDone:
			step.Action, step.Why = PlanStepSkip, "Already done."
		case ran && latest.State.Active():
			step.Action, step.Why = PlanStepSkip, "An agent session is already running."
		case ran && latest.State != domain.RunFailed && latest.State != domain.RunStopped:
			step.Action, step.Why = PlanStepSkip, "Its session finished ("+string(latest.State)+"). Review it; later steps wait for it to be done."
		case o.RunID != "":
			step.Action, step.Why = PlanStepSkip, "Its scheduled run has already started."
		case !t.Mode().AllowsAgent():
			step.Action, step.Why = PlanStepSkip, "Human work: later steps wait for you to finish it."
		default:
			if why := domain.AgentRefusal(*x.project, t); why != "" {
				step.Action, step.Why = PlanStepSkip, why
			} else if armed && (!chained || slices.Contains(o.Dependencies, prev)) {
				step.Action = PlanStepArmed
			} else {
				step.Action = PlanStepStart
				if ran {
					// The last session ended badly. Running the plan, which you reviewed, is the explicit request for a new one.
					step.Action, step.Why = PlanStepRetry, "Its last session "+string(latest.State)+"; a new session will be started."
				}
				out.ToStart++
			}
		}
		if step.Action != PlanStepSkip || !ran {
			d := baseDecision(t, x, s.now(), true)
			step.Decision, step.Reason = d.State, d.Reason
		}
		out.Steps = append(out.Steps, step)
		if t.ArchivedAt == nil && t.State != domain.TaskDone {
			prev = t.ID
		}
	}
	out.Control = PlanControlNone
	switch {
	case plan.Run != nil && plan.Run.State == domain.PlanRunHeld:
		out.Control = PlanControlResume
		if out.ToStart == 0 {
			out.Control = PlanControlNone
		}
	case plan.Run != nil && plan.Run.State == domain.PlanRunRunning && out.ToStart == 0:
		out.Control = PlanControlNone
		for _, st := range out.Steps {
			if st.Action == PlanStepArmed || st.Why == "An agent session is already running." || st.Why == "Its scheduled run has already started." {
				out.Control = PlanControlHold
			}
		}
	case out.ToStart > 0:
		out.Control = PlanControlRun
	}
	type digestStep struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
		Action  string `json:"action"`
		After   string `json:"after"`
	}
	d := struct {
		Plan    string       `json:"plan"`
		Version int64        `json:"version"`
		Mode    string       `json:"mode"`
		Steps   []digestStep `json:"steps"`
	}{plan.ID, plan.Version, mode, nil}
	for _, st := range out.Steps {
		d.Steps = append(d.Steps, digestStep{st.TaskID, byID[st.TaskID].Version, st.Action, fmt.Sprint(st.After)})
	}
	raw, _ := json.Marshal(d)
	sum := sha256.Sum256(raw)
	out.Digest = hex.EncodeToString(sum[:])
	return plan, out, byID, nil
}

// PlanRunPreview shows what running the plan would do, without changing anything.
func (s *Scheduler) PlanRunPreview(ctx context.Context, projectID, planID, mode string) (PlanRunPreview, error) {
	var out PlanRunPreview
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		_, out, _, e = s.planRunRead(ctx, tx, projectID, planID, mode)
		return e
	})
	return out, err
}

// RunPlan arms the plan's eligible tasks in one transaction. It is refused if the plan or any step changed since the
// preview the person reviewed. Asking again when nothing is left to arm changes nothing.
func (s *Scheduler) RunPlan(ctx context.Context, projectID, planID string, in PlanRunInput) (domain.WorkPlan, error) {
	var result domain.WorkPlan
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		plan, pv, byID, err := s.planRunRead(ctx, tx, projectID, planID, in.Mode)
		if err != nil {
			return err
		}
		result = *plan
		if plan.Run != nil && plan.Run.State == domain.PlanRunRunning && plan.Run.Mode == pv.Mode && pv.ToStart == 0 && plan.Version == in.Version {
			return nil
		}
		if plan.Version != in.Version || pv.Digest != in.Digest {
			return fmt.Errorf("%w: the plan changed since you reviewed it; review it again", domain.ErrConflict)
		}
		if pv.ToStart == 0 {
			return fmt.Errorf("%w: no step is ready to start", domain.ErrConflict)
		}
		now := s.now()
		run := domain.PlanRun{State: domain.PlanRunRunning, Mode: pv.Mode, TaskIDs: []string{}, StartedAt: now, UpdatedAt: now}
		if plan.Run != nil {
			run.TaskIDs = append(run.TaskIDs, plan.Run.TaskIDs...)
			if plan.Run.State == domain.PlanRunRunning {
				run.StartedAt = plan.Run.StartedAt
			}
		}
		for _, st := range pv.Steps {
			if st.Action == PlanStepSkip {
				continue
			}
			if st.Action == PlanStepStart || st.Action == PlanStepRetry {
				t := byID[st.TaskID]
				task := t
				o := task.Orchestration
				o.Enabled = true
				if st.Action == PlanStepRetry || !(o.Key != "" && o.RunID == "" && !o.Missed && o.Error == "") {
					o.Rearm = true
				}
				if pv.Mode == domain.PlanModeInOrder {
					p := previousOpenStep(plan, byID, st.TaskID)
					if p != "" && !slices.Contains(o.Dependencies, p) {
						o.Dependencies = append(slices.Clone(o.Dependencies), p)
					}
				}
				if err := configureOrchestration(ctx, tx, &task, o); err != nil {
					return err
				}
				all, err := tx.Tasks().ListByProject(ctx, projectID)
				if err != nil {
					return err
				}
				if err := planning.ValidateChange(timelineItems(all), timelineItem(task)); err != nil {
					return fmt.Errorf("%w: %w", domain.ErrInvalid, err)
				}
				task.UpdatedAt = now
				if err := tx.Tasks().Update(ctx, &task); err != nil {
					return err
				}
				ev := newEvent(domain.EventTaskUpdated, task)
				ev.ProjectID, ev.TaskID = projectID, task.ID
				if err := em.emit(ev); err != nil {
					return err
				}
				byID[st.TaskID] = task
			}
			if !slices.Contains(run.TaskIDs, st.TaskID) {
				run.TaskIDs = append(run.TaskIDs, st.TaskID)
			}
		}
		plan.Run = &run
		return s.savePlanRun(ctx, tx, em, plan, &result)
	})
	return result, err
}

// previousOpenStep is the closest earlier plan task that is neither done nor archived.
func previousOpenStep(plan *domain.WorkPlan, byID map[string]domain.Task, id string) string {
	prev := ""
	for _, tid := range plan.TaskIDs {
		if tid == id {
			return prev
		}
		if t, ok := byID[tid]; ok && t.ArchivedAt == nil && t.State != domain.TaskDone {
			prev = tid
		}
	}
	return ""
}

// HoldPlan disarms the steps this run armed that have not been dispatched. A process that is already running is never
// touched: the adapters stop and restart, they do not pause.
func (s *Scheduler) HoldPlan(ctx context.Context, projectID, planID string, version int64) (domain.WorkPlan, error) {
	var result domain.WorkPlan
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		plan, _, byID, err := s.planRunRead(ctx, tx, projectID, planID, "")
		if err != nil {
			return err
		}
		result = *plan
		if plan.Run == nil || plan.Run.State == domain.PlanRunHeld {
			return nil // nothing is running, or it is already paused
		}
		if plan.Version != version {
			return fmt.Errorf("%w: the plan changed; review it again", domain.ErrConflict)
		}
		now := s.now()
		for _, id := range plan.Run.TaskIDs {
			t, ok := byID[id]
			if !ok || !t.Orchestration.Enabled || t.Orchestration.RunID != "" {
				continue
			}
			t.Orchestration.Enabled = false
			t.UpdatedAt = now
			if err := tx.Tasks().Update(ctx, &t); err != nil {
				return err
			}
			ev := newEvent(domain.EventTaskUpdated, t)
			ev.ProjectID, ev.TaskID = projectID, t.ID
			if err := em.emit(ev); err != nil {
				return err
			}
		}
		run := *plan.Run
		run.State, run.UpdatedAt = domain.PlanRunHeld, now
		plan.Run = &run
		return s.savePlanRun(ctx, tx, em, plan, &result)
	})
	return result, err
}

func (s *Scheduler) savePlanRun(ctx context.Context, tx store.Tx, em *emitter, plan *domain.WorkPlan, out *domain.WorkPlan) error {
	expected := plan.Version
	plan.Version++
	plan.UpdatedAt = s.now()
	if err := tx.WorkPlans().Save(ctx, *plan, expected); err != nil {
		return err
	}
	*out = *plan
	ev := newEvent(domain.EventWorkPlanUpdated, map[string]any{"id": plan.ID, "version": plan.Version})
	ev.ProjectID = plan.ProjectID
	return em.emit(ev)
}

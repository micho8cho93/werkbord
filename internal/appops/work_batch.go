package appops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/mcpplan"
	"werkbord/internal/planning"
	"werkbord/internal/service"
)

// The most a single import may hold: it is one confirmation the person reads, and one write.
const (
	maxBatchPlans = 10
	maxBatchTasks = mcpplan.MaxChanges
)

// batchTask and batchPlan are what Salie brings back from a planning conversation: plans, each an ordered list of tasks.
type batchTask struct {
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	PlannedStart string `json:"plannedStart,omitempty"`
	PlannedEnd   string `json:"plannedEnd,omitempty"`
}

type batchPlan struct {
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	After       string      `json:"after,omitempty"`
	Tasks       []batchTask `json:"tasks"`
}

type workBatch struct {
	ProjectID string      `json:"projectId"`
	Chain     bool        `json:"chain"`
	Key       string      `json:"-"`
	Plans     []batchPlan `json:"plans"`
}

// BatchResult is what an import reports once it has been carried out: enough for the app to open Work on what was made.
type BatchResult struct {
	ProjectID string   `json:"projectId"`
	PlanIDs   []string `json:"planIds"`
	TaskIDs   []string `json:"taskIds"`
	Chained   bool     `json:"chained"`
	Message   string   `json:"-"`
}

func workBatchOps() []*operation {
	return append(append(workStructureOps(), importOp()...), append(chainOps(), scheduleOps()...)...)
}

func importOp() []*operation {
	task := Object(map[string]*Schema{
		"title":        Str("The task's title.", titleMax),
		"description":  Str("What needs doing: the prompt or brief the task carries.", descMax),
		"plannedStart": Day("First planned day, like 2026-03-31."),
		"plannedEnd":   Day("Last planned day."),
	}, "title")
	plan := Object(map[string]*Schema{
		"title":       Str("The plan's title, such as UI or UX.", titleMax),
		"description": Str("What the plan is for.", descMax),
		"after":       Str("Optional: the title of an earlier plan in this import that must finish before this one starts.", titleMax),
		"tasks":       List("The plan's tasks in the order they are to be done.", task, maxBatchTasks),
	}, "title", "tasks")
	return []*operation{{
		Spec: Spec{Name: "import_work_batch", Title: "Import plans and tasks into Work", Kind: KindMutation, Permission: PermTicketsCreate, RequiresConfirmation: true,
			Description: "Create several plans and all their tasks in one project in a single step, once the person has settled them in conversation. Tasks go to the Backlog and no agent is started. With chain true each task waits for the one before it in its plan, and a plan with `after` waits for that plan's last task; ask the person whether to chain before importing, and say what you will import. The person sees the whole batch and confirms once; Work then opens on the new plans.",
			Input: Object(map[string]*Schema{
				"projectId": projectID,
				"chain":     Bool("Make each task depend on the one before it in its plan, and each plan on the plan named by `after`."),
				"plans":     List("The plans to create, with their tasks.", plan, maxBatchPlans),
			}, "projectId", "chain", "plans")},
		AssistantOnly: true, plan: planWorkBatch, run: runWorkBatch,
	}}
}

func decodeWorkBatch(args map[string]any) (workBatch, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return workBatch{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	var b workBatch
	if err := json.Unmarshal(raw, &b); err != nil {
		return workBatch{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if len(b.Plans) == 0 || len(b.Plans) > maxBatchPlans {
		return workBatch{}, fmt.Errorf("%w: an import holds 1 to %d plans", domain.ErrInvalid, maxBatchPlans)
	}
	total, titles := 0, map[string]int{}
	for i := range b.Plans {
		p := &b.Plans[i]
		p.Title, p.After = strings.TrimSpace(p.Title), strings.TrimSpace(p.After)
		if _, err := domain.ValidateTaskTitle(p.Title); err != nil {
			return workBatch{}, fmt.Errorf("%w: plan %d needs a title", domain.ErrInvalid, i+1)
		}
		if _, dup := titles[strings.ToLower(p.Title)]; dup {
			return workBatch{}, fmt.Errorf("%w: two plans are called %q", domain.ErrInvalid, p.Title)
		}
		titles[strings.ToLower(p.Title)] = i
		if len(p.Tasks) == 0 {
			return workBatch{}, fmt.Errorf("%w: plan %q has no tasks", domain.ErrInvalid, p.Title)
		}
		total += len(p.Tasks)
		for j := range p.Tasks {
			p.Tasks[j].Title = strings.TrimSpace(p.Tasks[j].Title)
			if _, err := domain.ValidateTaskTitle(p.Tasks[j].Title); err != nil {
				return workBatch{}, fmt.Errorf("%w: task %d of plan %q needs a title", domain.ErrInvalid, j+1, p.Title)
			}
		}
	}
	if total > maxBatchTasks {
		return workBatch{}, fmt.Errorf("%w: an import holds at most %d tasks; split it", domain.ErrInvalid, maxBatchTasks)
	}
	for i, p := range b.Plans {
		if p.After == "" {
			continue
		}
		at, ok := titles[strings.ToLower(p.After)]
		if !ok || at >= i {
			return workBatch{}, fmt.Errorf("%w: plan %q can only come after a plan listed before it", domain.ErrInvalid, p.Title)
		}
	}
	return b, nil
}

// temp names a new task inside the batch before the server gives it an id.
func temp(plan, task int) string { return fmt.Sprintf("tmp_p%d_t%d", plan, task) }

// taskPlan turns the batch into the one transaction that creates its tasks. Dependencies are only ever to an earlier task, so
// the graph cannot cycle.
func (b workBatch) taskPlan() mcpplan.Plan {
	out := mcpplan.Plan{ProjectID: b.ProjectID, IdempotencyKey: b.Key}
	titles := map[string]int{}
	for i, p := range b.Plans {
		titles[strings.ToLower(p.Title)] = i
		for j, t := range p.Tasks {
			c := mcpplan.Change{TempID: temp(i, j)}
			title, desc := t.Title, t.Description
			c.Title, c.Description = &title, &desc
			if t.PlannedStart != "" || t.PlannedEnd != "" {
				c.Plan = &planning.Range{Start: t.PlannedStart, End: t.PlannedEnd}
			}
			if b.Chain {
				deps := []string{}
				switch {
				case j > 0:
					deps = append(deps, temp(i, j-1))
				case p.After != "":
					before := titles[strings.ToLower(p.After)]
					deps = append(deps, temp(before, len(b.Plans[before].Tasks)-1))
				}
				c.Dependencies = &deps
			}
			out.Tasks = append(out.Tasks, c)
		}
	}
	return out
}

func planWorkBatch(ctx context.Context, e *env, args map[string]any) (*plan, error) {
	b, err := decodeWorkBatch(args)
	if err != nil {
		return nil, err
	}
	if _, err := e.b.Project(ctx, b.ProjectID); err != nil {
		return nil, err
	}
	backend, ok := e.b.(TaskPlanBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "importing plans is not available here")
	}
	if _, ok := e.b.(WorkPlanBackend); !ok {
		return nil, errorf(CodeUnavailable, "Work plans unavailable")
	}
	b.Key = batchKey(e.p.ID, b)
	if _, err := backend.PreviewMCPTaskPlan(ctx, b.taskPlan()); err != nil {
		return nil, err
	}
	normalized, _ := json.Marshal(b)
	var normalizedArgs map[string]any
	if err := json.Unmarshal(normalized, &normalizedArgs); err != nil {
		return nil, err
	}
	return &plan{Args: normalizedArgs, Summary: b.summary()}, nil
}

// batchKey is the same for the same batch from the same principal, so confirming it twice cannot make it twice.
func batchKey(principal string, b workBatch) string {
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(append([]byte(principal+"|"), raw...))
	return "batch_" + hex.EncodeToString(sum[:12])
}

// summary is what the person reads before confirming: every plan, its tasks in order, and what waits for what.
func (b workBatch) summary() string {
	total := 0
	for _, p := range b.Plans {
		total += len(p.Tasks)
	}
	var s strings.Builder
	fmt.Fprintf(&s, "Import %d %s with %d %s into Work", len(b.Plans), plural(len(b.Plans), "plan", "plans"), total, plural(total, "task", "tasks"))
	if b.Chain {
		s.WriteString(", each task waiting for the one before it")
	} else {
		s.WriteString(", with no dependencies")
	}
	s.WriteString(".")
	for _, p := range b.Plans {
		fmt.Fprintf(&s, "\n%s", p.Title)
		if p.After != "" && b.Chain {
			fmt.Fprintf(&s, " (starts after %s)", p.After)
		}
		for j, t := range p.Tasks {
			fmt.Fprintf(&s, "\n  %d. %s", j+1, oneLine(t.Title))
		}
	}
	return s.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func runWorkBatch(ctx context.Context, e *env, args map[string]any) (any, error) {
	b, err := decodeWorkBatch(args)
	if err != nil {
		return nil, err
	}
	tasks, ok := e.b.(TaskPlanBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "importing plans is not available here")
	}
	plans, ok := e.b.(WorkPlanBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "Work plans unavailable")
	}
	b.Key = batchKey(e.p.ID, b)
	applied, err := tasks.ApplyMCPTaskPlan(ctx, e.p.ID, b.taskPlan())
	if err != nil {
		return nil, err
	}
	out := BatchResult{ProjectID: b.ProjectID, Chained: b.Chain, PlanIDs: []string{}, TaskIDs: []string{}}
	for i, p := range b.Plans {
		ids := make([]string, 0, len(p.Tasks))
		for j := range p.Tasks {
			ids = append(ids, applied.TempIDs[temp(i, j)])
		}
		saved, err := plans.SaveWorkPlan(ctx, b.ProjectID, "", domain.WorkPlan{ProjectID: b.ProjectID, Title: p.Title, Description: p.Description, TaskIDs: ids})
		if err != nil {
			return nil, fmt.Errorf("the tasks were created, but plan %q could not be saved: %w", p.Title, err)
		}
		out.PlanIDs = append(out.PlanIDs, saved.ID)
		out.TaskIDs = append(out.TaskIDs, ids...)
	}
	out.Message = fmt.Sprintf("Imported %d %s with %d %s into Work.", len(out.PlanIDs), plural(len(out.PlanIDs), "plan", "plans"), len(out.TaskIDs), plural(len(out.TaskIDs), "task", "tasks"))
	return &Changed{Message: out.Message, Batch: &out}, nil
}

// StructureFinding is one thing about how a project's work is organised that the person may want to settle.
type StructureFinding struct {
	Kind  string   `json:"kind"`
	Count int      `json:"count"`
	Tasks []string `json:"tasks,omitempty"`
	Offer string   `json:"offer"`
}

func workStructureOps() []*operation {
	return []*operation{{
		Spec: Spec{Name: "check_work_structure", Title: "Check how the work is organised", Kind: KindRead, Permission: PermTicketsRead,
			Description: "Look for loose ends in a project: tasks that belong to no plan, plans whose tasks are not in order, tasks with no dates, and failed tasks that hold up others. Call it when plans or tasks have just been discussed or created, and offer the person the fix it names. It changes nothing.",
			Input:       Object(map[string]*Schema{"projectId": projectID}, "projectId")},
		run: opCheckStructure,
	}}
}

func opCheckStructure(ctx context.Context, e *env, a map[string]any) (any, error) {
	pid := str(a, "projectId")
	if _, err := e.b.Project(ctx, pid); err != nil {
		return nil, err
	}
	tickets, err := e.b.Tickets(ctx, pid)
	if err != nil {
		return nil, err
	}
	var plans []domain.WorkPlan
	if b, ok := e.b.(WorkPlanBackend); ok {
		if plans, err = b.WorkPlans(ctx, pid); err != nil {
			return nil, err
		}
	}
	active := map[string]domain.Task{}
	for _, t := range tickets {
		if t.ArchivedAt == nil && t.State != domain.TaskDone {
			active[t.ID] = t
		}
	}
	inPlan := map[string]bool{}
	for _, p := range plans {
		for _, id := range p.TaskIDs {
			inPlan[id] = true
		}
	}
	findings := []StructureFinding{}
	add := func(f StructureFinding) {
		if f.Count > 0 {
			if len(f.Tasks) > 10 {
				f.Tasks = f.Tasks[:10]
			}
			findings = append(findings, f)
		}
	}
	var loose []string
	for _, t := range active {
		if !inPlan[t.ID] {
			loose = append(loose, t.Title)
		}
	}
	if len(plans) > 0 || len(loose) >= 3 {
		sort.Strings(loose)
		add(StructureFinding{Kind: "tasks_outside_plans", Count: len(loose), Tasks: loose, Offer: "Group them into a plan with save_work_plan, or leave them standing alone."})
	}
	var unchained []string
	for _, p := range plans {
		for i := 1; i < len(p.TaskIDs); i++ {
			t, ok := active[p.TaskIDs[i]]
			if ok && len(t.Orchestration.Dependencies) == 0 {
				unchained = append(unchained, p.Title)
				break
			}
		}
	}
	add(StructureFinding{Kind: "plans_without_order", Count: len(unchained), Tasks: unchained, Offer: "These plans list tasks in order but no task waits for the one before it. Offer to chain them (update the tasks' dependencies in Work, or import a fresh batch with chain true)."})
	var undated []string
	for _, t := range active {
		if t.Plan.Start == "" && t.Plan.End == "" && t.Orchestration.ScheduledAt == nil {
			undated = append(undated, t.Title)
		}
	}
	sort.Strings(undated)
	if len(inPlan) > 0 {
		add(StructureFinding{Kind: "tasks_without_dates", Count: len(undated), Tasks: undated, Offer: "Ask whether to give them planned dates (update_ticket) or leave them undated; some people want to start right away."})
	}
	failedTask := map[string]bool{}
	if ov, err := e.b.Overview(ctx); err == nil {
		for _, r := range ov.Failed {
			if r.Run.ProjectID == pid {
				failedTask[r.Run.TaskID] = true
			}
		}
	}
	var held []string
	for _, t := range active {
		for _, d := range t.Orchestration.Dependencies {
			if failedTask[d] {
				held = append(held, t.Title)
				break
			}
		}
	}
	sort.Strings(held)
	var rework []string
	for id := range failedTask {
		if t, ok := active[id]; ok {
			rework = append(rework, t.Title)
		}
	}
	sort.Strings(rework)
	add(StructureFinding{Kind: "needs_rework", Count: len(rework), Tasks: rework, Offer: "The last run of each failed or stopped. Offer to look at why, then to run it again (start_agent_run); work after it resumes by itself once it finishes."})
	add(StructureFinding{Kind: "held_by_failure", Count: len(held), Tasks: held, Offer: "These wait on a task that failed. Their calendar times do not move; say so."})
	return map[string]any{"projectId": pid, "findings": findings, "observedAt": e.now}, nil
}

func chainOps() []*operation {
	return []*operation{{
		Spec: Spec{Name: "chain_work_plan", Title: "Put a plan's tasks in order", Kind: KindMutation, Permission: PermTicketsUpdate, RequiresConfirmation: true,
			Description: "Make each task in a Work plan wait for the one before it, in the plan's order, so a failed task holds up the ones after it. Existing dependencies are kept. The person confirms first. Read list_work_plans for the plan's id.",
			Input:       Object(map[string]*Schema{"projectId": projectID, "planId": Str("The plan's id (from list_work_plans).", idMax)}, "projectId", "planId")},
		AssistantOnly: true, plan: planChain, run: runChain,
	}}
}

// chainChanges are the task edits that put a plan's tasks in order: only the links that are missing.
func chainChanges(ctx context.Context, e *env, pid, planID string) (domain.WorkPlan, mcpplan.Plan, []string, error) {
	b, ok := e.b.(WorkPlanBackend)
	if !ok {
		return domain.WorkPlan{}, mcpplan.Plan{}, nil, errorf(CodeUnavailable, "Work plans unavailable")
	}
	plans, err := b.WorkPlans(ctx, pid)
	if err != nil {
		return domain.WorkPlan{}, mcpplan.Plan{}, nil, err
	}
	var found *domain.WorkPlan
	for i := range plans {
		if plans[i].ID == planID {
			found = &plans[i]
		}
	}
	if found == nil {
		return domain.WorkPlan{}, mcpplan.Plan{}, nil, domain.ErrNotFound
	}
	tickets, err := e.b.Tickets(ctx, pid)
	if err != nil {
		return domain.WorkPlan{}, mcpplan.Plan{}, nil, err
	}
	byID := map[string]domain.Task{}
	for _, t := range tickets {
		byID[t.ID] = t
	}
	out := mcpplan.Plan{ProjectID: pid}
	var titles []string
	for i := 1; i < len(found.TaskIDs); i++ {
		t, okT := byID[found.TaskIDs[i]]
		prev, okP := byID[found.TaskIDs[i-1]]
		if !okT || !okP || t.ArchivedAt != nil || prev.ArchivedAt != nil {
			continue
		}
		have := false
		for _, d := range t.Orchestration.Dependencies {
			have = have || d == prev.ID
		}
		if have {
			continue
		}
		deps := append(append([]string{}, t.Orchestration.Dependencies...), prev.ID)
		out.Tasks = append(out.Tasks, mcpplan.Change{TaskID: t.ID, Version: t.Version, Dependencies: &deps})
		titles = append(titles, fmt.Sprintf("%q waits for %q", clip(t.Title, 60), clip(prev.Title, 60)))
	}
	return *found, out, titles, nil
}

func planChain(ctx context.Context, e *env, a map[string]any) (*plan, error) {
	pid := str(a, "projectId")
	if _, err := e.b.Project(ctx, pid); err != nil {
		return nil, err
	}
	backend, ok := e.b.(TaskPlanBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "ordering tasks is not available here")
	}
	found, change, titles, err := chainChanges(ctx, e, pid, str(a, "planId"))
	if err != nil {
		return nil, err
	}
	if len(titles) == 0 {
		return nil, fmt.Errorf("%w: every task in %q already waits for the one before it", domain.ErrConflict, found.Title)
	}
	change.IdempotencyKey = "chain_probe"
	if _, err := backend.PreviewMCPTaskPlan(ctx, change); err != nil {
		return nil, err
	}
	return &plan{Args: map[string]any{"projectId": pid, "planId": found.ID}, Summary: "Put the tasks of " + found.Title + " in order:\n  " + strings.Join(titles, "\n  ")}, nil
}

func runChain(ctx context.Context, e *env, a map[string]any) (any, error) {
	backend, ok := e.b.(TaskPlanBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "ordering tasks is not available here")
	}
	pid := str(a, "projectId")
	found, change, titles, err := chainChanges(ctx, e, pid, str(a, "planId"))
	if err != nil {
		return nil, err
	}
	if len(titles) == 0 {
		return &Changed{Message: "Already in order."}, nil
	}
	sum := sha256.Sum256([]byte(e.p.ID + "|chain|" + found.ID + "|" + strings.Join(titles, "|")))
	change.IdempotencyKey = "chain_" + hex.EncodeToString(sum[:12])
	if _, err := backend.ApplyMCPTaskPlan(ctx, e.p.ID, change); err != nil {
		return nil, err
	}
	return &Changed{Message: fmt.Sprintf("Put %d tasks of %q in order.", len(titles), found.Title)}, nil
}

func scheduleOps() []*operation {
	return []*operation{{
		Spec: Spec{Name: "schedule_task_run", Title: "Schedule an agent to start on a task", Kind: KindMutation, Permission: PermTicketsUpdate, RequiresConfirmation: true,
			Description: "Set when a coding agent starts on a task by itself (or cancel that). The person confirms the exact time first. It starts nothing now, and the task's dependencies still have to be finished by then: the scheduler waits for them. Give the local time the person means, like 2026-11-02T09:00, and their time zone if you know it. Planned dates for the calendar are a different thing: use update_ticket for those.",
			Input: Object(map[string]*Schema{
				"projectId":       projectID,
				"ticketId":        ticketID,
				"startAt":         Str("Local date and time to start, like 2026-11-02T09:00. Leave out when cancelling.", 32),
				"timezone":        Str("IANA time zone of that time, like Europe/Paris. Default: this computer's.", 64),
				"cancel":          Bool("Remove the scheduled start instead."),
				"expectedVersion": Int("The ticket's version you last read. Leave out to use the current one.", 1, 1<<30),
			}, "projectId", "ticketId")},
		AssistantOnly: true, plan: planSchedule, run: runSchedule,
	}}
}

type scheduleAsk struct {
	at     time.Time
	zone   string
	cancel bool
}

func parseScheduleAsk(a map[string]any, now time.Time) (scheduleAsk, error) {
	if boolArg(a, "cancel") {
		if str(a, "startAt") != "" {
			return scheduleAsk{}, fmt.Errorf("%w: give a time or cancel, not both", domain.ErrInvalid)
		}
		return scheduleAsk{cancel: true}, nil
	}
	zone := strings.TrimSpace(str(a, "timezone"))
	if zone == "" {
		zone = time.Local.String()
		if zone == "Local" {
			zone = "UTC"
		}
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return scheduleAsk{}, fmt.Errorf("%w: unknown time zone %q", domain.ErrInvalid, zone)
	}
	raw := strings.TrimSpace(str(a, "startAt"))
	at, err := time.ParseInLocation("2006-01-02T15:04", raw, loc)
	if err != nil {
		return scheduleAsk{}, fmt.Errorf("%w: startAt must look like 2026-11-02T09:00", domain.ErrInvalid)
	}
	if !at.After(now) {
		return scheduleAsk{}, fmt.Errorf("%w: %s has already passed; pick a later time", domain.ErrInvalid, raw)
	}
	return scheduleAsk{at: at.UTC(), zone: zone}, nil
}

func planSchedule(ctx context.Context, e *env, a map[string]any) (*plan, error) {
	pid, tid := str(a, "projectId"), str(a, "ticketId")
	project, err := e.b.Project(ctx, pid)
	if err != nil {
		return nil, err
	}
	t, err := e.b.Ticket(ctx, pid, tid)
	if err != nil {
		return nil, err
	}
	if t.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: this task is archived", domain.ErrConflict)
	}
	if v, ok := a["expectedVersion"].(float64); ok && int64(v) != t.Version {
		return nil, fmt.Errorf("%w: task %s is at version %d, not %d: read it again first", domain.ErrConflict, t.ID, t.Version, int64(v))
	}
	ask, err := parseScheduleAsk(a, e.now)
	if err != nil {
		return nil, err
	}
	norm := map[string]any{"projectId": pid, "ticketId": tid, "expectedVersion": t.Version}
	if ask.cancel {
		if !t.Orchestration.Enabled {
			return nil, fmt.Errorf("%w: %q has no scheduled start", domain.ErrConflict, clip(t.Title, 80))
		}
		norm["cancel"] = true
		return &plan{Args: norm, Summary: fmt.Sprintf("Cancel the scheduled start of an agent on %q", clip(t.Title, 80))}, nil
	}
	if why := domain.AgentRefusal(project.Project, *t); why != "" {
		return nil, fmt.Errorf("%w: %s, so an agent cannot be scheduled on it", domain.ErrInvalid, strings.ToLower(why[:1])+why[1:])
	}
	if t.Orchestration.RunID != "" || t.Orchestration.Missed {
		return nil, fmt.Errorf("%w: this task's earlier schedule was already used; schedule a new run from the task in Werkbord", domain.ErrConflict)
	}
	loc, _ := time.LoadLocation(ask.zone)
	norm["startAt"], norm["timezone"] = ask.at.In(loc).Format("2006-01-02T15:04"), ask.zone
	deps := ""
	if n := len(t.Orchestration.Dependencies); n > 0 {
		deps = fmt.Sprintf(", once the %d %s it waits for %s finished", n, plural(n, "task", "tasks"), plural(n, "is", "are"))
	}
	return &plan{Args: norm, Summary: fmt.Sprintf("Start an agent on %q at %s (%s)%s", clip(t.Title, 80), ask.at.In(loc).Format("Mon 2 Jan 2006, 15:04"), ask.zone, deps)}, nil
}

func runSchedule(ctx context.Context, e *env, a map[string]any) (any, error) {
	pid, tid := str(a, "projectId"), str(a, "ticketId")
	t, err := e.b.Ticket(ctx, pid, tid)
	if err != nil {
		return nil, err
	}
	ask, err := parseScheduleAsk(a, e.now)
	if err != nil {
		return nil, err
	}
	o := t.Orchestration
	if ask.cancel {
		o.Enabled, o.ScheduledAt = false, nil
	} else {
		at := ask.at
		o.Enabled, o.ScheduledAt, o.Timezone = true, &at, ask.zone
	}
	patch := service.TaskPatch{Orchestration: &o, Version: int64(a["expectedVersion"].(float64))}
	updated, err := e.b.UpdateTicket(ctx, tid, patch)
	if err != nil {
		return nil, err
	}
	v := ticketView(*updated, listDescriptionChars)
	if ask.cancel {
		return &Changed{Message: fmt.Sprintf("Cancelled the scheduled start on %q.", clip(updated.Title, 80)), Ticket: &v}, nil
	}
	return &Changed{Message: fmt.Sprintf("Scheduled an agent to start on %q at %s (%s).", clip(updated.Title, 80), ask.at.Format(time.RFC3339), ask.zone), Ticket: &v}, nil
}

package appops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"werkbord/internal/domain"
	"werkbord/internal/mcpplan"
	"werkbord/internal/planning"
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

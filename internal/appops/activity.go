package appops

import (
	"context"
	"fmt"
	"sort"
	"time"

	"werkbord/internal/domain"
)

// ActivityModel is one agent and model, and what it was asked to do in the period.
type ActivityModel struct {
	Agent        string `json:"agent"`
	Model        string `json:"model,omitempty"`
	Runs         int    `json:"runs"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
}

// ActivityProject is what happened in one project in the period.
type ActivityProject struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Runs      int    `json:"runs"`
	// ByState counts runs by how they ended or stand: completed, failed, stopped, blocked, running…
	ByState map[string]int  `json:"byState"`
	Models  []ActivityModel `json:"models"`
	// Tokens are summed over the runs that reported them; RunsWithoutUsage did not, and are not counted as zero.
	InputTokens      int64    `json:"inputTokens"`
	OutputTokens     int64    `json:"outputTokens"`
	CachedTokens     int64    `json:"cachedTokens"`
	RunsWithoutUsage int      `json:"runsWithoutUsage"`
	CostUSD          *float64 `json:"costUsd,omitempty"`
	// CostBasis says what the cost is: provider-reported API cost, an estimate of the API equivalent, or absent (usage only).
	CostBasis      []string `json:"costBasis,omitempty"`
	TasksFinished  []string `json:"tasksFinished,omitempty"`
	TasksTouched   int      `json:"tasksTouched"`
	PlansTouched   []string `json:"plansTouched,omitempty"`
	TasksTruncated bool     `json:"tasksTruncated,omitempty"`
}

// ActivitySummary is the measured facts of a period, the same each time it is asked for the same data.
type ActivitySummary struct {
	Since    time.Time         `json:"since"`
	Until    time.Time         `json:"until"`
	Days     int               `json:"days"`
	Projects []ActivityProject `json:"projects"`
	// Limits is always stated: Werkbord does not read how much of a plan or credit balance is left.
	Limits string   `json:"limits"`
	Notes  []string `json:"notes"`
}

func activityOps() []*operation {
	return []*operation{{
		Spec: Spec{Name: "get_activity_summary", Title: "What was done lately", Kind: KindRead, Permission: PermAnalyticsRead,
			Description: "Measured facts about the last 1 to 30 days on the Personal board: per project, how many agent runs and how they ended, which agents and models were used, tokens spent (only where the run reported them) and cost, which tasks were finished or touched, and which plans they belong to. Leave projectId out for every project the person can see. It does not say how much of any plan or credit is left.",
			Input: Object(map[string]*Schema{
				"projectId": Str("One project's id; leave out for all of them.", idMax),
				"days":      Int("How many days back, 1 to 30. Default 3.", 1, 30),
			})},
		run: opActivitySummary,
	}}
}

func opActivitySummary(ctx context.Context, e *env, a map[string]any) (any, error) {
	days := intArg(a, "days", 3)
	if days < 1 || days > 30 {
		return nil, fmt.Errorf("%w: days must be 1 to 30", domain.ErrInvalid)
	}
	pid := str(a, "projectId")
	until := e.now
	since := until.Add(-time.Duration(days) * 24 * time.Hour)
	var targets []struct{ id, name string }
	if pid != "" {
		p, err := e.b.Project(ctx, pid)
		if err != nil {
			return nil, err
		}
		targets = append(targets, struct{ id, name string }{p.ID, p.Name})
	} else {
		all, err := e.b.Projects(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range all {
			if e.p.CanSee(p.ID) && !p.Inactive() {
				targets = append(targets, struct{ id, name string }{p.ID, p.Name})
			}
		}
	}
	out := ActivitySummary{Since: since, Until: until, Days: days, Projects: []ActivityProject{}, Notes: []string{},
		Limits: "unknown: Werkbord does not read how much of a subscription window or credit balance is left"}
	for _, t := range targets {
		row, err := activityOf(ctx, e, t.id, t.name, since)
		if err != nil {
			return nil, err
		}
		if row.Runs == 0 && row.TasksTouched == 0 {
			continue
		}
		out.Projects = append(out.Projects, row)
	}
	if len(out.Projects) == 0 {
		out.Notes = append(out.Notes, "Nothing was recorded in this period.")
	}
	out.Notes = append(out.Notes, "Tasks are 'finished' when they are Done and were last changed in the period; a Done task edited later counts only if it was edited in the period.")
	if e.p.Via == "assistant" {
		out.Notes = append(out.Notes, "Team workspaces are not included; ask for the person's own tasks there with list_tickets.")
	}
	return out, nil
}

func activityOf(ctx context.Context, e *env, pid, name string, since time.Time) (ActivityProject, error) {
	row := ActivityProject{ProjectID: pid, Name: name, ByState: map[string]int{}, Models: []ActivityModel{}}
	runs, err := e.b.Runs(ctx, pid, 500)
	if err != nil {
		return row, err
	}
	models := map[string]*ActivityModel{}
	var cost float64
	haveCost := false
	basis := map[string]bool{}
	for _, r := range runs {
		if r.CreatedAt.Before(since) {
			continue
		}
		row.Runs++
		row.ByState[string(r.State)]++
		key := r.AgentID + "|" + r.Model
		m := models[key]
		if m == nil {
			m = &ActivityModel{Agent: r.AgentID, Model: r.Model}
			models[key] = m
		}
		m.Runs++
		u := r.Usage
		if u.InputTokens == nil && u.OutputTokens == nil {
			row.RunsWithoutUsage++
		}
		if u.InputTokens != nil {
			row.InputTokens += *u.InputTokens
			m.InputTokens += *u.InputTokens
		}
		if u.OutputTokens != nil {
			row.OutputTokens += *u.OutputTokens
			m.OutputTokens += *u.OutputTokens
		}
		if u.CachedTokens != nil {
			row.CachedTokens += *u.CachedTokens
		}
		if u.CostUSD != nil {
			cost += *u.CostUSD
			haveCost = true
			basis[u.CostKind] = true
		}
	}
	if haveCost {
		row.CostUSD = &cost
		for k := range basis {
			row.CostBasis = append(row.CostBasis, k)
		}
		sort.Strings(row.CostBasis)
	}
	for _, m := range models {
		row.Models = append(row.Models, *m)
	}
	sort.Slice(row.Models, func(i, j int) bool {
		return row.Models[i].Runs > row.Models[j].Runs || row.Models[i].Runs == row.Models[j].Runs && row.Models[i].Agent < row.Models[j].Agent
	})

	tickets, err := e.b.Tickets(ctx, pid)
	if err != nil {
		return row, err
	}
	touched := map[string]bool{}
	for _, t := range tickets {
		if t.UpdatedAt.Before(since) {
			continue
		}
		touched[t.ID] = true
		row.TasksTouched++
		if t.State == domain.TaskDone && len(row.TasksFinished) < 12 {
			row.TasksFinished = append(row.TasksFinished, clip(t.Title, 100))
		} else if t.State == domain.TaskDone {
			row.TasksTruncated = true
		}
	}
	sort.Strings(row.TasksFinished)
	if b, ok := e.b.(WorkPlanBackend); ok {
		if plans, err := b.WorkPlans(ctx, pid); err == nil {
			for _, p := range plans {
				for _, id := range p.TaskIDs {
					if touched[id] {
						row.PlansTouched = append(row.PlansTouched, clip(p.Title, 100))
						break
					}
				}
			}
			sort.Strings(row.PlansTouched)
		}
	}
	return row, nil
}

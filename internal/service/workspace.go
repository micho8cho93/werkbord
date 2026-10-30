package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
	"werkbord/internal/workspace"
)

// WorkspaceSummary is this computer's Individual workspace in the words the desktop shell uses for every workspace
// (internal/workspace): the person's projects, the work an agent is doing or has finished, what waits for them and what
// is scheduled. It is built from the Control Center's one consistent snapshot and adds nothing to it; it is the same
// information, in a form the shell can set beside a Team workspace's.
//
// It is read-only and answers only to the controller's own credential. Individual data is never sent anywhere by this: the
// shell is a program on this computer that asks for it.
func (s *ControlCenter) WorkspaceSummary(ctx context.Context) (workspace.Summary, error) {
	ov, err := s.Overview(ctx)
	if err != nil {
		return workspace.Summary{}, err
	}
	out := SummaryOf(ov, s.now())
	err = s.Store.View(ctx, func(tx store.Tx) error {
		out.Changes, err = recentWorkspaceChanges(ctx, tx, ov, out.At)
		if err != nil {
			return err
		}
		var global domain.ExecutionConfig
		if err := tx.Settings().Get(ctx, domain.SettingExecution, &global); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		for _, p := range ov.Projects {
			project, err := tx.Projects().Get(ctx, p.ProjectID)
			if err != nil {
				return err
			}
			tasks, err := tx.Tasks().ListByProject(ctx, p.ProjectID)
			if err != nil {
				return err
			}
			latest, err := tx.Runs().ListLatestByProject(ctx, p.ProjectID)
			if err != nil {
				return err
			}
			dependencies := scheduleSnapshot{tasks: tasks, latest: map[string]domain.Run{}}
			for _, run := range latest {
				dependencies.latest[run.TaskID] = run
			}

			plans, err := tx.WorkPlans().List(ctx, p.ProjectID)
			if err != nil {
				return err
			}
			for _, plan := range plans {
				done := 0
				for _, id := range plan.TaskIDs {
					for _, t := range tasks {
						if t.ID == id && t.State == domain.TaskDone {
							done++
						}
					}
				}
				out.Queue = append(out.Queue, workspace.QueueItem{ID: plan.ID, Title: plan.Title, Project: p.Name, Href: "#/p/" + p.ProjectID + "/work?plan=" + plan.ID, Group: "plan", Detail: fmt.Sprintf("%d of %d tasks complete", done, len(plan.TaskIDs))})
			}
			for _, t := range tasks {
				if t.ArchivedAt != nil {
					continue
				}
				priority := domain.ResolveExecution(domain.Level{Config: global}, domain.Level{Config: project.Execution}, domain.Level{Config: t.Execution}).Priority
				group := "next"
				detail := string(t.State)
				if state, reason := dependencyDecision(t, dependencies); state != "" {
					group = "later"
					detail = reason
				}

				start := t.Orchestration.ScheduledAt
				if start == nil {
					start = t.Orchestration.NotBefore
				}
				if start != nil && start.After(out.At) {
					group = "later"
				}
				if t.Plan.Start > out.At.Local().Format("2006-01-02") || priority == domain.PriorityLow {
					group = "later"
				}
				if t.State == domain.TaskDoing || t.State == domain.TaskReview || priority == domain.PriorityHigh {
					group = "now"
				}
				for _, active := range out.Work {
					if active.ID == t.ID && active.Execution != "" && active.Execution != workspace.ExecCompleted && active.Execution != workspace.ExecCanceled {
						group = "now"
					}
				}
				if t.State == domain.TaskDone {
					group = "done"
				}
				out.Queue = append(out.Queue, workspace.QueueItem{ID: t.ID, Title: t.Title, Project: p.Name, Href: taskHref(t.ProjectID, t.ID), Group: group, Detail: detail})
			}
			for _, t := range tasks {
				if t.ArchivedAt != nil || (t.Plan.Start == "" && t.Plan.End == "") {
					continue
				}
				start, end := t.Plan.Start, t.Plan.End
				if start == "" {
					start = end
				}
				if end == "" || t.Plan.Milestone {
					end = start
				}
				out.Planned = append(out.Planned, workspace.Planned{ID: t.ID, Title: t.Title, Project: p.Name, Start: start, End: end, Href: taskHref(t.ProjectID, t.ID)})
				if len(out.Planned) >= workspace.MaxItems {
					return nil
				}
			}
		}
		return nil
	})
	cleaned, _ := out.Clean()
	return cleaned, err
}

func taskHref(projectID, taskID string) string { return "#/p/" + projectID + "/task/" + taskID }

func execOf(r domain.Run) workspace.Execution {
	switch r.State {
	case domain.RunStarting:
		return workspace.ExecStarting
	case domain.RunRunning:
		return workspace.ExecRunning
	case domain.RunWaitingForUser:
		if r.Waiting == domain.WaitIdle {
			return workspace.ExecIdle
		}
		return workspace.ExecNeedsInput
	case domain.RunBlocked:
		return workspace.ExecBlocked
	case domain.RunCompleted:
		return workspace.ExecCompleted
	case domain.RunFailed:
		return workspace.ExecFailed
	case domain.RunStopped:
		return workspace.ExecCanceled
	}
	return workspace.ExecNone
}

func reportOf(r domain.Run, runners []domain.Runner) *workspace.ExecutionReport {
	report := &workspace.ExecutionReport{PendingRequests: append([]string(nil), r.PendingRequests...)}
	if !r.UpdatedAt.IsZero() {
		at := r.UpdatedAt
		report.RecordedAt = &at
	}
	if r.Remote && r.State.Active() {
		report.Stale = true
		for _, runner := range runners {
			if runner.ID == r.RunnerID {
				report.Stale = !runner.Online || runner.Disabled
				break
			}
		}
	}
	return report
}

// SummaryOf translates the Control Center's snapshot. It is exported so it can be tested without a store.
func SummaryOf(ov *Overview, now time.Time) workspace.Summary {
	out := workspace.Summary{
		Schema:    workspace.Schema,
		Workspace: workspace.Entry{ID: workspace.IndividualID, Kind: workspace.KindIndividual, Name: "Personal", State: workspace.StateReady, DeviceRoles: []string{"runner"}},
		Projects:  []workspace.Project{},
		Work:      []workspace.Item{},
		Attention: []workspace.Attention{},
		Schedule:  []workspace.Scheduled{},
		At:        now.UTC(),
	}
	for _, p := range ov.Projects {
		out.Projects = append(out.Projects, workspace.Project{ID: p.ProjectID, Name: p.Name, Href: "#/p/" + p.ProjectID + "/work"})
	}

	seen := map[string]bool{}
	add := func(it workspace.Item) {
		if !seen[it.ID] {
			seen[it.ID] = true
			out.Work = append(out.Work, it)
		}
	}
	// What an agent is doing right now, then what stopped, then what finished and waits for a person.
	for _, r := range ov.Runs {
		add(workspace.Item{ID: r.Run.TaskID, Title: r.TaskTitle, Project: r.ProjectName, Status: workspace.StatusDoing, Execution: execOf(r.Run),
			Href: taskHref(r.Run.ProjectID, r.Run.TaskID), UpdatedAt: r.Run.UpdatedAt, Report: reportOf(r.Run, ov.Runners)})
	}
	for _, r := range ov.Failed {
		add(workspace.Item{ID: r.Run.TaskID, Title: r.TaskTitle, Project: r.ProjectName, Status: workspace.StatusDoing, Execution: workspace.ExecFailed,
			Href: taskHref(r.Run.ProjectID, r.Run.TaskID), UpdatedAt: r.Run.UpdatedAt, Report: reportOf(r.Run, ov.Runners)})
	}
	for _, r := range ov.Review {
		it := workspace.Item{ID: r.Task.ID, Title: r.Task.Title, Project: r.ProjectName, Status: workspace.StatusReview,
			Href: taskHref(r.Task.ProjectID, r.Task.ID), UpdatedAt: r.Task.UpdatedAt}
		if r.LastRun != nil {
			it.Execution = execOf(*r.LastRun)
			it.Report = reportOf(*r.LastRun, ov.Runners)
		}
		add(it)
	}

	for _, q := range ov.Questions {
		out.Attention = append(out.Attention, workspace.Attention{ID: "question-" + q.Question.ID, Kind: workspace.AttentionNeedsInput, Severity: workspace.SeverityWarning,
			Title: "An agent is asking: " + q.Question.Prompt, Detail: q.TaskTitle, Project: q.ProjectName,
			Href: taskHref(q.Question.ProjectID, q.Question.TaskID), At: q.Question.AskedAt,
			Question: &workspace.Question{ID: q.Question.ID, RunID: q.Question.RunID, ProjectID: q.Question.ProjectID, TaskID: q.Question.TaskID,
				Kind: string(q.Question.Kind), Prompt: q.Question.Prompt, Options: append([]string(nil), q.Question.Options...), AllowFreeText: q.Question.AllowFreeText, Context: q.Question.Context, AskedAt: q.Question.AskedAt}})
	}
	for _, r := range ov.Runs {
		if r.Run.State == domain.RunWaitingForUser && r.Run.Waiting == domain.WaitIdle {
			out.Attention = append(out.Attention, workspace.Attention{ID: "idle-" + r.Run.ID, Kind: workspace.AttentionIdle, Severity: workspace.SeverityInfo,
				Title: r.TaskTitle + " is waiting for your next message", Detail: "The agent finished its turn. Review the activity, continue the conversation, or finish the session.",
				Project: r.ProjectName, Href: taskHref(r.Run.ProjectID, r.Run.TaskID), At: r.Run.UpdatedAt})
		}
		if r.Run.State != domain.RunBlocked {
			continue
		}
		detail := "The agent stopped rather than guess."
		if r.Run.Blocker != nil && r.Run.Blocker.Summary != "" {
			detail = r.Run.Blocker.Summary
		}
		out.Attention = append(out.Attention, workspace.Attention{ID: "blocked-" + r.Run.ID, Kind: workspace.AttentionBlocked, Severity: workspace.SeverityWarning,
			Title: r.TaskTitle + " is blocked", Detail: detail, Project: r.ProjectName, Href: taskHref(r.Run.ProjectID, r.Run.TaskID), At: r.Run.UpdatedAt})
	}
	for _, r := range ov.Failed {
		out.Attention = append(out.Attention, workspace.Attention{ID: "failed-" + r.Run.ID, Kind: workspace.AttentionFailed, Severity: workspace.SeverityWarning,
			Title: r.TaskTitle + " failed", Detail: r.Run.Reason, Project: r.ProjectName, Href: taskHref(r.Run.ProjectID, r.Run.TaskID), At: r.Run.UpdatedAt})
	}
	for _, r := range ov.Review {
		out.Attention = append(out.Attention, workspace.Attention{ID: "review-" + r.Task.ID, Kind: workspace.AttentionReview, Severity: workspace.SeverityInfo,
			Title: r.Task.Title + " is ready for review", Project: r.ProjectName, Href: taskHref(r.Task.ProjectID, r.Task.ID), At: r.Task.UpdatedAt})
	}
	for _, f := range ov.Repository {
		sev := workspace.SeverityWarning
		if f.Severity.Rank() >= domain.HealthCritical.Rank() {
			sev = workspace.SeverityCritical
		}
		out.Attention = append(out.Attention, workspace.Attention{ID: "repo-" + f.ID, Kind: workspace.AttentionRepository, Severity: sev,
			Title: f.Title, Detail: f.Explanation, Project: f.ProjectName, Href: "#/p/" + f.ProjectID + "/git"})
	}

	for _, a := range ov.Orchestration {
		at := a.Task.Orchestration.ScheduledAt
		if at == nil {
			continue
		}
		out.Schedule = append(out.Schedule, workspace.Scheduled{ID: a.Task.ID, Title: a.Task.Title, Project: a.ProjectName, At: at.UTC(),
			Zone: a.Task.Orchestration.Timezone, State: a.Decision.State, Reason: a.Decision.Reason, Href: taskHref(a.Task.ProjectID, a.Task.ID)})
	}
	sort.SliceStable(out.Schedule, func(i, j int) bool { return out.Schedule[i].At.Before(out.Schedule[j].At) })
	sort.SliceStable(out.Attention, func(i, j int) bool {
		return severityRank(out.Attention[i].Severity) < severityRank(out.Attention[j].Severity)
	})
	return out
}

func severityRank(s workspace.Severity) int {
	switch s {
	case workspace.SeverityCritical:
		return 0
	case workspace.SeverityWarning:
		return 1
	}
	return 2
}

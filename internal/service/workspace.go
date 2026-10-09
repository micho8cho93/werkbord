package service

import (
	"context"
	"sort"
	"time"

	"devboard/internal/domain"
	"devboard/internal/workspace"
)

// WorkspaceSummary is this computer's Personal workspace in the words the desktop shell uses for every workspace
// (internal/workspace): the person's projects, the work an agent is doing or has finished, what waits for them and what
// is scheduled. It is built from the Control Center's one consistent snapshot and adds nothing to it; it is the same
// information, in a form the shell can set beside a Team workspace's.
//
// It is read-only and answers only to the controller's own credential. Personal data is never sent anywhere by this: the
// shell is a program on this computer that asks for it.
func (s *ControlCenter) WorkspaceSummary(ctx context.Context) (workspace.Summary, error) {
	ov, err := s.Overview(ctx)
	if err != nil {
		return workspace.Summary{}, err
	}
	return SummaryOf(ov, s.now()), nil
}

func taskHref(projectID, taskID string) string { return "#/p/" + projectID + "/task/" + taskID }

func execOf(r domain.Run) workspace.Execution {
	switch r.State {
	case domain.RunStarting:
		return workspace.ExecQueued
	case domain.RunRunning:
		return workspace.ExecRunning
	case domain.RunWaitingForUser:
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

// SummaryOf translates the Control Center's snapshot. It is exported so it can be tested without a store.
func SummaryOf(ov *Overview, now time.Time) workspace.Summary {
	out := workspace.Summary{
		Schema:    workspace.Schema,
		Workspace: workspace.Entry{ID: workspace.PersonalID, Kind: workspace.KindPersonal, Name: "Individual", State: workspace.StateReady, DeviceRoles: []string{"runner"}},
		Projects:  []workspace.Project{},
		Work:      []workspace.Item{},
		Attention: []workspace.Attention{},
		Schedule:  []workspace.Scheduled{},
		At:        now.UTC(),
	}
	for _, p := range ov.Projects {
		out.Projects = append(out.Projects, workspace.Project{ID: p.ProjectID, Name: p.Name, Href: "#/p/" + p.ProjectID + "/board"})
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
			Href: taskHref(r.Run.ProjectID, r.Run.TaskID), UpdatedAt: r.Run.UpdatedAt})
	}
	for _, r := range ov.Failed {
		add(workspace.Item{ID: r.Run.TaskID, Title: r.TaskTitle, Project: r.ProjectName, Status: workspace.StatusDoing, Execution: workspace.ExecFailed,
			Href: taskHref(r.Run.ProjectID, r.Run.TaskID), UpdatedAt: r.Run.UpdatedAt})
	}
	for _, r := range ov.Review {
		it := workspace.Item{ID: r.Task.ID, Title: r.Task.Title, Project: r.ProjectName, Status: workspace.StatusReview,
			Href: taskHref(r.Task.ProjectID, r.Task.ID), UpdatedAt: r.Task.UpdatedAt}
		if r.LastRun != nil {
			it.Execution = execOf(*r.LastRun)
		}
		add(it)
	}

	for _, q := range ov.Questions {
		out.Attention = append(out.Attention, workspace.Attention{ID: "question-" + q.Question.ID, Kind: workspace.AttentionNeedsInput, Severity: workspace.SeverityWarning,
			Title: "An agent is asking: " + q.Question.Prompt, Detail: q.TaskTitle, Project: q.ProjectName,
			Href: taskHref(q.Question.ProjectID, q.Question.TaskID), At: q.Question.AskedAt})
	}
	for _, r := range ov.Runs {
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
			Zone: a.Task.Orchestration.Timezone, State: a.Decision.State, Href: "#/p/" + a.Task.ProjectID + "/calendar"})
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

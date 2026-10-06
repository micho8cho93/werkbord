package service

import (
	"context"
	"sort"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// ControlCenter answers the one question that is deliberately not scoped to a
// project: what, anywhere, needs the user? Everything else in the API is read
// through a project; this reads across all of them, in one consistent snapshot,
// and names each item's project and task so a client needs nothing else to
// present it.
type ControlCenter struct {
	Deps
	Scheduler *Scheduler
	Runners   *Runners
}

// Overview is what the Control Center shows.
type Overview struct {
	Runners []domain.Runner `json:"runners"`
	Usage   []UsageSummary  `json:"usage"`
	// Projects has one entry per project, in registration order, with what is
	// going on in it. A project with nothing going on has all-zero counts.
	Projects      []ProjectActivity   `json:"projects"`
	Orchestration []AttentionSchedule `json:"orchestration"`
	// Questions are the pending questions of every project, oldest first.
	Questions []AttentionQuestion `json:"questions"`
	// Runs are the runs that still have a session, in every project, oldest first.
	Runs []AttentionRun `json:"runs"`
	// Failed are runs that ended in failure on a task still waiting on them: its
	// latest run failed and the task is in Doing or Review. Newest first. Moving the
	// task on (to Done, or back to the Backlog) or running it again clears it.
	Failed []AttentionRun `json:"failed"`
	// Review are tasks in the Review column with nothing running and no failure:
	// finished work waiting for a person. Oldest first.
	Review []AttentionReview `json:"review"`
	// Repository are the open repository-health findings that are a risk or
	// worse, in every project, worst first. Housekeeping and ordinary
	// "attention" items are counted per project but not listed here.
	Repository []domain.HealthFinding `json:"repository"`
}

// ProjectActivity counts what is happening in one project, by what it asks of the user.
type ProjectActivity struct {
	ProjectID         string `json:"projectId"`
	Name              string `json:"name"`
	Scheduled         int    `json:"scheduled"`
	Queued            int    `json:"queued"`
	WaitingDependency int    `json:"waitingDependency"`
	// NeedsInput: runs waiting for an answer to a question.
	NeedsInput int `json:"needsInput"`
	// Blocked: runs that stopped rather than guess.
	Blocked int `json:"blocked"`
	// Idle: runs that finished a turn and wait for the next message.
	Idle int `json:"idle"`
	// Running: runs that are working.
	Running int `json:"running"`
	// Failed: runs that failed on a task still waiting on them.
	Failed int `json:"failed"`
	// Review: tasks waiting for review with nothing running.
	Review int `json:"review"`
	// RepoAttention: open health findings of attention level or worse.
	RepoAttention int `json:"repoAttention"`
	// RepoRisk: open health findings of risk level or worse.
	RepoRisk int `json:"repoRisk"`
}

type AttentionSchedule struct {
	Task        domain.Task               `json:"task"`
	ProjectName string                    `json:"projectName"`
	Decision    domain.SchedulingDecision `json:"decision"`
}

// AttentionReview is a task waiting for review with where it belongs.
type AttentionReview struct {
	Task        domain.Task `json:"task"`
	ProjectName string      `json:"projectName"`
	// LastRun is the task's latest run, if it ever ran: who did the work.
	LastRun *domain.Run `json:"lastRun,omitempty"`
}

// failedWindow is how long a failure stays on the Control Center if nobody deals with it.
const failedWindow = 14 * 24 * time.Hour

// AttentionQuestion is a pending question with where it belongs.
type AttentionQuestion struct {
	Question    domain.Question `json:"question"`
	ProjectName string          `json:"projectName"`
	TaskTitle   string          `json:"taskTitle"`
	AgentID     string          `json:"agentId"`
}

// AttentionRun is an active run with where it belongs.
type AttentionRun struct {
	Run         domain.Run `json:"run"`
	ProjectName string     `json:"projectName"`
	TaskTitle   string     `json:"taskTitle"`
}

// Overview reads every project's active state.
func (s *ControlCenter) Overview(ctx context.Context) (*Overview, error) {
	out := &Overview{Projects: []ProjectActivity{}, Questions: []AttentionQuestion{}, Runs: []AttentionRun{},
		Orchestration: []AttentionSchedule{}, Failed: []AttentionRun{}, Review: []AttentionReview{}, Repository: []domain.HealthFinding{}}
	now := s.now()
	err := s.Store.View(ctx, func(tx store.Tx) error {
		projects, err := tx.Projects().List(ctx)
		if err != nil {
			return err
		}
		names := make(map[string]string, len(projects))
		index := make(map[string]int, len(projects))
		for _, p := range projects {
			names[p.ID] = p.Name
			index[p.ID] = len(out.Projects)
			out.Projects = append(out.Projects, ProjectActivity{ProjectID: p.ID, Name: p.Name})
		}

		titles := map[string]string{}
		title := func(taskID string) (string, error) {
			if t, ok := titles[taskID]; ok {
				return t, nil
			}
			task, err := tx.Tasks().Get(ctx, taskID)
			if err != nil {
				return "", err
			}
			titles[taskID] = task.Title
			return task.Title, nil
		}

		runs, err := tx.Runs().ListActive(ctx)
		if err != nil {
			return err
		}
		agentOf := make(map[string]string, len(runs))
		for _, r := range runs {
			agentOf[r.ID] = r.AgentID
			t, err := title(r.TaskID)
			if err != nil {
				return err
			}
			out.Runs = append(out.Runs, AttentionRun{Run: r, ProjectName: names[r.ProjectID], TaskTitle: t})
			a := &out.Projects[index[r.ProjectID]]
			switch {
			case r.State == domain.RunBlocked:
				a.Blocked++
			case r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitQuestion:
				a.NeedsInput++
			case r.State == domain.RunWaitingForUser:
				a.Idle++
			default:
				a.Running++
			}
		}

		// What stopped without being dealt with, and what is finished and waits for a person.
		activeTasks := map[string]bool{}
		for _, r := range runs {
			activeTasks[r.TaskID] = true
		}
		for _, p := range projects {
			tasks, err := tx.Tasks().ListByProject(ctx, p.ID)
			if err != nil {
				return err
			}
			latest, err := tx.Runs().ListLatestByProject(ctx, p.ID)
			if err != nil {
				return err
			}
			lastRun := make(map[string]domain.Run, len(latest))
			for _, r := range latest {
				lastRun[r.TaskID] = r
			}
			a := &out.Projects[index[p.ID]]
			for _, task := range tasks {
				if task.ArchivedAt != nil {
					continue
				}
				r, ran := lastRun[task.ID]
				switch {
				case activeTasks[task.ID]:
					// Something is going on: it is in the running list, not an exception.
				case ran && r.State == domain.RunFailed && task.State != domain.TaskDone && (task.State == domain.TaskDoing || task.State == domain.TaskReview || r.ScheduleKey != "") &&
					r.EndedAt != nil && now.Sub(*r.EndedAt) <= failedWindow:
					titles[task.ID] = task.Title
					out.Failed = append(out.Failed, AttentionRun{Run: r, ProjectName: p.Name, TaskTitle: task.Title})
					a.Failed++
				case task.State == domain.TaskReview:
					item := AttentionReview{Task: task, ProjectName: p.Name}
					if ran {
						rc := r
						item.LastRun = &rc
					}
					out.Review = append(out.Review, item)
					a.Review++
				}
			}
		}
		sort.SliceStable(out.Failed, func(i, j int) bool { return out.Failed[i].Run.EndedAt.After(*out.Failed[j].Run.EndedAt) })
		sort.SliceStable(out.Review, func(i, j int) bool { return out.Review[i].Task.UpdatedAt.Before(out.Review[j].Task.UpdatedAt) })

		// Repository health is read as it was last worked out: a database read, no Git.
		findings, err := tx.Health().ListOpen(ctx, domain.HealthAttention)
		if err != nil {
			return err
		}
		for _, f := range findings {
			i, ok := index[f.ProjectID]
			if !ok {
				continue
			}
			out.Projects[i].RepoAttention++
			if f.Severity.Rank() >= domain.HealthRisk.Rank() {
				out.Projects[i].RepoRisk++
				f.ProjectName = names[f.ProjectID]
				out.Repository = append(out.Repository, f)
			}
		}

		qs, err := tx.Questions().ListPending(ctx)
		if err != nil {
			return err
		}
		for _, q := range qs {
			t, err := title(q.TaskID)
			if err != nil {
				return err
			}
			out.Questions = append(out.Questions, AttentionQuestion{Question: q, ProjectName: names[q.ProjectID], TaskTitle: t, AgentID: agentOf[q.RunID]})
		}
		var usageErr error
		out.Usage, usageErr = RecentUsage(ctx, tx, now)
		return usageErr
	})
	if err != nil {
		return nil, err
	}
	if s.Scheduler != nil {
		for i := range out.Projects {
			p := &out.Projects[i]
			plan, e := s.Scheduler.Plan(ctx, p.ProjectID)
			if e != nil {
				return nil, e
			}
			byID := map[string]domain.Task{}
			e = s.Store.View(ctx, func(tx store.Tx) error {
				ts, e := tx.Tasks().ListByProject(ctx, p.ProjectID)
				for _, t := range ts {
					byID[t.ID] = t
				}
				return e
			})
			if e != nil {
				return nil, e
			}
			for _, d := range plan {
				t := byID[d.TaskID]
				o := t.Orchestration
				if !o.Enabled || o.RunID != "" || t.State == domain.TaskDone || t.ArchivedAt != nil {
					continue
				}
				out.Orchestration = append(out.Orchestration, AttentionSchedule{Task: t, ProjectName: p.Name, Decision: d})
				switch d.State {
				case "waiting_schedule":
					p.Scheduled++
				case "waiting_dependency":
					p.WaitingDependency++
				case "blocked":
					p.Blocked++
				default:
					p.Queued++
				}
			}
		}
	}
	if s.Runners != nil {
		var e error
		out.Runners, e = s.Runners.List(ctx)
		if e != nil {
			return nil, e
		}
	} else {
		out.Runners = []domain.Runner{}
	}
	return out, nil
}

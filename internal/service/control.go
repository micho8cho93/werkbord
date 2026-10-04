package service

import (
	"context"

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
}

// Overview is what the Control Center shows.
type Overview struct {
	// Projects has one entry per project, in registration order, with what is
	// going on in it. A project with nothing going on has all-zero counts.
	Projects []ProjectActivity `json:"projects"`
	// Questions are the pending questions of every project, oldest first.
	Questions []AttentionQuestion `json:"questions"`
	// Runs are the runs that still have a session, in every project, oldest first.
	Runs []AttentionRun `json:"runs"`
}

// ProjectActivity counts what is happening in one project, by what it asks of the user.
type ProjectActivity struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	// NeedsInput: runs waiting for an answer to a question.
	NeedsInput int `json:"needsInput"`
	// Blocked: runs that stopped rather than guess.
	Blocked int `json:"blocked"`
	// Idle: runs that finished a turn and wait for the next message.
	Idle int `json:"idle"`
	// Running: runs that are working.
	Running int `json:"running"`
}

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
	out := &Overview{Projects: []ProjectActivity{}, Questions: []AttentionQuestion{}, Runs: []AttentionRun{}}
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

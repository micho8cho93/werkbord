package appops

import (
	"time"

	"devboard/internal/domain"
	"devboard/internal/service"
)

// What an operation hands back is a view, not a domain struct. A view carries what the caller needs to talk about the
// board and nothing more: no paths on the person's disk, no process ids, no prompts the agents were started with, no
// execution settings. Free text that came from somewhere else (a title, a description, an agent's question) is
// clipped, and is data for the caller to report, never instructions for it to follow.

// ProjectView is a project.
type ProjectView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	HasRepository bool   `json:"hasRepository"`
	Branch        string `json:"currentBranch,omitempty"`
}

func projectView(d service.ProjectDetail) ProjectView {
	v := ProjectView{ID: d.ID, Name: d.Name, Kind: string(d.Kind), HasRepository: d.HasRepository()}
	if d.Repository != nil {
		v.Branch = d.Repository.CurrentBranch
	}
	return v
}

// TicketView is a ticket: a task on a project's board.
type TicketView struct {
	ID                   string     `json:"id"`
	ProjectID            string     `json:"projectId"`
	Title                string     `json:"title"`
	Description          string     `json:"description,omitempty"`
	DescriptionTruncated bool       `json:"descriptionTruncated,omitempty"`
	State                string     `json:"state"`
	WorkMode             string     `json:"workMode"`
	LabelIDs             []string   `json:"labelIds"`
	PlannedStart         string     `json:"plannedStart,omitempty"`
	PlannedEnd           string     `json:"plannedEnd,omitempty"`
	Milestone            bool       `json:"milestone,omitempty"`
	DependsOn            []string   `json:"dependsOn"`
	ScheduledAt          *time.Time `json:"scheduledAt,omitempty"`
	NotBefore            *time.Time `json:"notBefore,omitempty"`
	Deadline             *time.Time `json:"deadline,omitempty"`
	// AutoStart says an agent is set to start on it by itself when it is due.
	AutoStart bool `json:"autoStart"`
	// Source says where an imported ticket came from, such as a Team workspace's ticket.
	Source    string    `json:"source,omitempty"`
	Archived  bool      `json:"archived,omitempty"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const (
	listDescriptionChars = 300
	fullDescriptionChars = 8000
)

func ticketView(t domain.Task, descChars int) TicketView {
	desc, cut := clipFlag(t.Description, descChars)
	v := TicketView{
		ID: t.ID, ProjectID: t.ProjectID, Title: clip(t.Title, 200), Description: desc, DescriptionTruncated: cut,
		State: string(t.State), WorkMode: string(t.Mode()), LabelIDs: nonNil(t.LabelIDs),
		PlannedStart: t.Plan.Start, PlannedEnd: t.Plan.End, Milestone: t.Plan.Milestone, DependsOn: nonNil(t.Orchestration.Dependencies),
		ScheduledAt: t.Orchestration.ScheduledAt, NotBefore: t.Orchestration.NotBefore, Deadline: t.Orchestration.Deadline,
		AutoStart: t.Orchestration.Enabled, Source: clip(t.SourceRef, 200), Archived: t.ArchivedAt != nil, Version: t.Version, UpdatedAt: t.UpdatedAt,
	}
	return v
}

// BlockerView is why a run stopped rather than guess.
type BlockerView struct {
	Summary string   `json:"summary"`
	Detail  string   `json:"detail,omitempty"`
	Options []string `json:"options,omitempty"`
	Source  string   `json:"source"`
	Since   string   `json:"since"`
}

// RunView is one run of an agent on a ticket.
type RunView struct {
	ID        string       `json:"id"`
	TicketID  string       `json:"ticketId"`
	ProjectID string       `json:"projectId"`
	Agent     string       `json:"agent"`
	Model     string       `json:"model,omitempty"`
	State     string       `json:"state"`
	Waiting   string       `json:"waiting,omitempty"`
	Activity  string       `json:"activity,omitempty"`
	Reason    string       `json:"reason,omitempty"`
	Attempt   int          `json:"attempt"`
	Branch    string       `json:"branch,omitempty"`
	Blocker   *BlockerView `json:"blocker,omitempty"`
	StartedAt time.Time    `json:"startedAt"`
	EndedAt   *time.Time   `json:"endedAt,omitempty"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

func runView(r domain.Run) RunView {
	v := RunView{ID: r.ID, TicketID: r.TaskID, ProjectID: r.ProjectID, Agent: r.AgentID, Model: r.Model, State: string(r.State),
		Waiting: string(r.Waiting), Activity: clip(r.Activity, 200), Reason: clip(r.Reason, 300), Attempt: r.Attempt, Branch: r.Branch,
		StartedAt: r.CreatedAt, EndedAt: r.EndedAt, UpdatedAt: r.UpdatedAt}
	if b := r.Blocker; b != nil {
		v.Blocker = &BlockerView{Summary: clip(b.Summary, 300), Detail: clip(b.Detail, 1000), Options: b.Options, Source: string(b.Source),
			Since: b.RaisedAt.UTC().Format(time.RFC3339)}
	}
	return v
}

// QuestionView is something an agent asked.
type QuestionView struct {
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	TicketID      string    `json:"ticketId"`
	TicketTitle   string    `json:"ticketTitle,omitempty"`
	ProjectID     string    `json:"projectId"`
	ProjectName   string    `json:"projectName,omitempty"`
	Kind          string    `json:"kind"`
	Prompt        string    `json:"prompt"`
	Context       string    `json:"context,omitempty"`
	Options       []string  `json:"options,omitempty"`
	AllowFreeText bool      `json:"allowFreeText"`
	State         string    `json:"state"`
	Answer        string    `json:"answer,omitempty"`
	AskedAt       time.Time `json:"askedAt"`
	// AnswerableHere is false for an approval: permission to run a command or change files is the person's to give,
	// in Werkbord itself. Note says so.
	AnswerableHere bool   `json:"answerableHere"`
	Note           string `json:"note,omitempty"`
}

const approvalNote = "This is a request for permission to run something or change files. Only the person can give it, in Werkbord's Control Center; the assistant cannot."

func questionView(q domain.Question, project, ticket string) QuestionView {
	v := QuestionView{ID: q.ID, RunID: q.RunID, TicketID: q.TaskID, TicketTitle: clip(ticket, 200), ProjectID: q.ProjectID, ProjectName: project,
		Kind: string(q.Kind), Prompt: clip(q.Prompt, 1000), Context: clip(q.Context, 2000), Options: q.Options, AllowFreeText: q.AllowFreeText || len(q.Options) == 0,
		State: string(q.State), Answer: clip(q.Answer, 500), AskedAt: q.AskedAt, AnswerableHere: q.Pending() && q.Kind != domain.QuestionApproval}
	if q.Kind == domain.QuestionApproval {
		v.Note = approvalNote
	}
	return v
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// clipFlag is clip, and says whether it cut.
func clipFlag(s string, max int) (string, bool) {
	c := clip(s, max)
	return c, c != s
}

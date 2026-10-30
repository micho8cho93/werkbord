package teamlink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

type workPlan struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"projectId"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	TaskIDs     []string `json:"taskIds"`
	Decisions   string   `json:"decisions"`
	Questions   string   `json:"questions"`
	Version     int64    `json:"version"`
}
type initiativePlan struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Objective  string   `json:"objective"`
	Status     string   `json:"status"`
	OwnerID    string   `json:"ownerId"`
	ProjectIDs []string `json:"projectIds"`
	Tickets    []struct {
		ProjectID string `json:"projectId"`
		TicketID  string `json:"ticketId"`
	} `json:"tickets"`
	Dependencies []json.RawMessage `json:"dependencies"`
	Memories     []json.RawMessage `json:"memories"`
	Decisions    string            `json:"decisions"`
	Questions    string            `json:"questions"`
	Version      int64             `json:"version"`
}

func planMetadata(i initiativePlan, pid string) workPlan {
	p := workPlan{ID: i.ID, ProjectID: pid, Title: i.Title, Description: i.Objective, Decisions: i.Decisions, Questions: i.Questions, Version: i.Version, TaskIDs: []string{}}
	for _, t := range i.Tickets {
		p.TaskIDs = append(p.TaskIDs, t.TicketID)
	}
	return p
}
func (l *Link) assistantWorkPlans(ctx context.Context, slot, method string, in BoardArgs) (any, error) {
	base := "/w/" + slot + "/api/team/v1"
	var rows []struct {
		Initiative initiativePlan `json:"initiative"`
	}
	if err := l.call(ctx, http.MethodGet, base+"/projects/"+url.PathEscape(in.ProjectID)+"/initiatives", nil, &rows); err != nil {
		return nil, err
	}
	if method == "list_work_plans" {
		out := []workPlan{}
		for _, row := range rows {
			out = append(out, planMetadata(row.Initiative, in.ProjectID))
		}
		return out, nil
	}
	var p workPlan
	if err := json.Unmarshal([]byte(in.PlanJSON), &p); err != nil {
		return nil, err
	}
	i := initiativePlan{Title: p.Title, Objective: p.Description, Status: "active", ProjectIDs: []string{in.ProjectID}, Version: p.Version}
	if p.ID != "" {
		found := false
		for _, row := range rows {
			if row.Initiative.ID == p.ID {
				i = row.Initiative
				found = true
			}
		}
		if !found {
			return nil, errors.New("plan not found")
		}
		if len(i.ProjectIDs) != 1 || i.ProjectIDs[0] != in.ProjectID {
			return nil, errors.New("edit a cross-project plan in Work")
		}
		if i.Version != p.Version {
			return nil, errors.New("plan changed; read it again")
		}
	}
	i.Title = p.Title
	i.Objective = p.Description
	i.Decisions = p.Decisions
	i.Questions = p.Questions
	i.Tickets = nil
	for _, id := range p.TaskIDs {
		i.Tickets = append(i.Tickets, struct {
			ProjectID string `json:"projectId"`
			TicketID  string `json:"ticketId"`
		}{in.ProjectID, id})
	}
	path := base + "/initiatives"
	verb := http.MethodPost
	if p.ID != "" {
		path += "/" + url.PathEscape(p.ID)
		verb = http.MethodPut
	}
	var out initiativePlan
	if err := l.call(ctx, verb, path, i, &out); err != nil {
		return nil, err
	}
	return planMetadata(out, in.ProjectID), nil
}

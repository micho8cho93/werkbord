package teamlink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// BoardArgs is the entire semantic input surface offered to the desktop assistant. No URL, credential, repository,
// execution setting or command can be supplied. Ticket creation uses Team's ordinary service and its current permissions.
type BoardArgs struct {
	PlanJSON     string `json:"planJSON,omitempty"`
	Query        string `json:"query,omitempty"`
	TaskID       string `json:"taskId,omitempty"`
	RequestType  string `json:"requestType,omitempty"`
	TokenBudget  int    `json:"tokenBudget,omitempty"`
	ProjectID    string `json:"projectId"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	WorkMode     string `json:"workMode"`
	PlannedStart string `json:"plannedStart"`
	PlannedEnd   string `json:"plannedEnd"`
	Milestone    bool   `json:"milestone"`
}

var boardID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

func DecodeBoardArgs(method string, raw json.RawMessage) (BoardArgs, error) {
	var in BoardArgs
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var extra any
	if len(raw) > 64<<10 || d.Decode(&in) != nil || d.Decode(&extra) != io.EOF {
		return in, errors.New("invalid workspace arguments")
	}
	if method != "retrieve_memory" && (in.Query != "" || in.TaskID != "" || in.RequestType != "" || in.TokenBudget != 0) {
		return in, errors.New("unexpected memory arguments")
	}
	if method != "save_work_plan" && in.PlanJSON != "" {
		return in, errors.New("unexpected plan arguments")
	}
	switch method {
	case "retrieve_memory":
		if !boardID.MatchString(in.ProjectID) || in.TaskID != "" && !boardID.MatchString(in.TaskID) || len(in.Query) > 800 || in.TokenBudget < 512 || in.TokenBudget > 6000 || in.Title != "" || in.Description != "" || in.WorkMode != "" || in.PlannedStart != "" || in.PlannedEnd != "" || in.Milestone {
			return in, errors.New("invalid memory arguments")
		}
	case "list_projects", "get_overview":
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) {
			return in, errors.New("this workspace read takes no arguments")
		}
	case "save_work_plan", "list_work_plans":
		if !boardID.MatchString(in.ProjectID) || in.Title != "" || in.Description != "" || in.WorkMode != "" || in.PlannedStart != "" || in.PlannedEnd != "" || in.Milestone {
			return in, errors.New("invalid plan arguments")
		}
		if method == "save_work_plan" {
			var p workPlan
			if len(in.PlanJSON) > 60000 || json.Unmarshal([]byte(in.PlanJSON), &p) != nil || p.ProjectID != in.ProjectID || len(p.TaskIDs) > 200 || p.ID != "" && !boardID.MatchString(p.ID) {
				return in, errors.New("invalid Work plan")
			}
			for _, id := range p.TaskIDs {
				if !boardID.MatchString(id) {
					return in, errors.New("invalid task id")
				}
			}
		}
	case "get_project", "list_tickets", "create_ticket":
		if !boardID.MatchString(in.ProjectID) {
			return in, errors.New("invalid project ID")
		}
		if method != "create_ticket" && (in.Title != "" || in.Description != "" || in.WorkMode != "" || in.PlannedStart != "" || in.PlannedEnd != "" || in.Milestone) {
			return in, errors.New("this workspace read takes only a project ID")
		}
		if method == "create_ticket" && (strings.TrimSpace(in.Title) == "" || len([]rune(in.Title)) > 200 || len([]rune(in.Description)) > 20000) {
			return in, errors.New("invalid ticket text")
		}
	default:
		return in, errors.New("that workspace operation is not available to the assistant")
	}
	return in, nil
}

type boardProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProjectName lets the app name the project to the person without knowing this type.
func (p boardProject) ProjectName() string { return p.Name }

type boardPlan struct {
	Start     string `json:"start"`
	End       string `json:"end"`
	Milestone bool   `json:"milestone"`
}
type boardTicket struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	WorkMode    string    `json:"workMode"`
	Plan        boardPlan `json:"plan"`
	Version     int64     `json:"version"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func ticketMetadata(t boardTicket) map[string]any {
	state := map[string]string{"backlog": "backlog", "available": "backlog", "in_progress": "doing", "review": "review", "done": "done"}[t.Status]
	if state == "" {
		state = "backlog"
	}
	return map[string]any{"id": t.ID, "projectId": t.ProjectID, "title": t.Title, "description": t.Description,
		"state": state, "workMode": t.WorkMode, "plannedStart": t.Plan.Start, "plannedEnd": t.Plan.End,
		"milestone": t.Plan.Milestone, "version": t.Version, "updatedAt": t.UpdatedAt}
}

// AssistantBoard never returns the raw API result: only the neutral metadata the assistant may read.
func (l *Link) AssistantBoard(ctx context.Context, id, method string, in BoardArgs) (any, error) {
	slot, ok := SlotOf(id)
	if !ok {
		return nil, errors.New("not a Team workspace")
	}
	base := "/w/" + slot + "/api/team/v1/projects"
	switch method {
	case "save_work_plan", "list_work_plans":
		return l.assistantWorkPlans(ctx, slot, method, in)
	case "retrieve_memory":
		return l.assistantMemory(ctx, base, in)
	case "list_projects", "get_overview":
		var ps []boardProject
		if err := l.call(ctx, http.MethodGet, base, nil, &ps); err != nil {
			return nil, err
		}
		if method == "list_projects" {
			return ps, nil
		}
		out := []map[string]any{}
		for _, p := range ps {
			var b struct {
				Tickets []boardTicket `json:"tickets"`
			}
			if err := l.call(ctx, http.MethodGet, base+"/"+url.PathEscape(p.ID)+"/board", nil, &b); err != nil {
				return nil, err
			}
			counts := map[string]any{"projectId": p.ID, "name": p.Name, "review": 0, "blocked": 0, "running": 0}
			for _, t := range b.Tickets {
				if t.Status == "review" {
					counts["review"] = counts["review"].(int) + 1
				}
			}
			out = append(out, counts)
		}
		return out, nil
	case "get_project":
		var p boardProject
		err := l.call(ctx, http.MethodGet, base+"/"+url.PathEscape(in.ProjectID), nil, &p)
		return p, err
	case "list_tickets":
		var b struct {
			Tickets []boardTicket `json:"tickets"`
		}
		if err := l.call(ctx, http.MethodGet, base+"/"+url.PathEscape(in.ProjectID)+"/board", nil, &b); err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, t := range b.Tickets {
			out = append(out, ticketMetadata(t))
		}
		return out, nil
	case "create_ticket":
		var t boardTicket
		body := map[string]any{"title": in.Title, "description": in.Description, "status": "backlog", "workMode": in.WorkMode,
			"plan": boardPlan{Start: in.PlannedStart, End: in.PlannedEnd, Milestone: in.Milestone}}
		if err := l.call(ctx, http.MethodPost, base+"/"+url.PathEscape(in.ProjectID)+"/tickets", body, &t); err != nil {
			return nil, err
		}
		return ticketMetadata(t), nil
	}
	return nil, errors.New("that workspace operation is not available to the assistant")
}

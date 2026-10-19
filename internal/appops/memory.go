package appops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"devboard/internal/domain"
	"devboard/internal/service"
)

// MemoryRequest carries semantic scope only. The model cannot choose an owner,
// credential, endpoint, filesystem path or verification override.
type MemoryRequest struct {
	AllowedProjects []string `json:"-"`
	IncludePersonal bool     `json:"-"`
	ProjectID       string   `json:"projectId"`
	Query           string   `json:"query,omitempty"`
	TaskID          string   `json:"taskId,omitempty"`
	RequestType     string   `json:"requestType,omitempty"`
	TokenBudget     int      `json:"tokenBudget,omitempty"`
}

// MemoryContext and MemorySource are read projections, exposed through operations only.
type MemoryContext = service.MemoryContext
type MemorySource = service.MemoryContextItem

type MemoryBackend interface {
	MemoryContext(context.Context, MemoryRequest) (service.MemoryContext, error)
}

func opMemory(ctx context.Context, e *env, args map[string]any) (any, error) {
	pid, _ := args["projectId"].(string)
	if e.p.Via != "assistant" || !e.p.CanSee(pid) {
		return nil, errorf(CodeNotFound, "project not found")
	}
	if _, err := e.b.Project(ctx, pid); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(args)
	var in MemoryRequest
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, errorf(CodeInvalidArguments, "invalid memory request")
	}
	in.AllowedProjects = e.p.Projects
	in.IncludePersonal = e.p.Projects == nil && (in.Query != "" || in.RequestType == "next")
	if in.TokenBudget == 0 {
		in.TokenBudget = 1500
	}
	b, ok := e.b.(MemoryBackend)
	if !ok {
		return service.MemoryContext{Items: []service.MemoryContextItem{}, Status: "unavailable"}, nil
	}
	out, err := b.MemoryContext(ctx, in)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrForbidden) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		out = service.MemoryContext{Items: []service.MemoryContextItem{}, Status: "unavailable"}
	}
	// A cross-project source must fit the assistant's scope as well as the
	// authenticated person's workspace permissions.
	permitted := out.Items[:0]
	for _, item := range out.Items {
		allowed := item.Scope.ProjectID == "" || e.p.CanSee(item.Scope.ProjectID)
		for _, scope := range item.RelatedScopes {
			if !e.p.CanSee(scope.ProjectID) {
				allowed = false
			}
		}
		if allowed {
			permitted = append(permitted, item)
		}
	}
	out.Items = permitted
	out.Context = map[string]string{"principal": e.p.ID, "projectId": pid, "taskId": in.TaskID, "requestType": in.RequestType}
	if wb, ok := e.b.(*workspaceBackend); ok {
		if w, _, found := wb.locate(pid); found {
			out.Context["workspaceId"] = w.ID
		}
	}
	// Apply the same total budget to remote projections and local context.
	for {
		if len(out.Items) == 0 && out.Status == "available" {
			out.Status = "empty"
		}
		data, _ := json.Marshal(out)
		out.TokenUpperBound = len(data) + 16
		if out.TokenUpperBound > in.TokenBudget && len(out.Items) == 0 && out.Context != nil {
			out.Context = nil
			continue
		}
		if out.TokenUpperBound <= in.TokenBudget || len(out.Items) == 0 {
			break
		}
		out.Items = out.Items[:len(out.Items)-1]
		out.Truncated = true
	}
	return out, nil
}

func (b servicesBackend) MemoryContext(ctx context.Context, in MemoryRequest) (service.MemoryContext, error) {
	if b.s.Knowledge == nil {
		return service.MemoryContext{Items: []service.MemoryContextItem{}, Status: "unavailable"}, nil
	}
	if in.TaskID != "" {
		if _, err := b.Ticket(ctx, in.ProjectID, in.TaskID); err != nil {
			return service.MemoryContext{}, err
		}
	}
	out, err := b.s.Knowledge.Retrieve(ctx, service.MemoryContextQuery{PrioritizeAuthority: true, Scope: domain.MemoryScope{ProjectID: in.ProjectID}, Query: domain.MemoryQuery{Text: in.Query}, TokenBudget: in.TokenBudget, IncludeHistorical: in.RequestType == "lessons", TaskID: in.TaskID})
	if err != nil {
		return out, err
	}
	if relationships, e := b.s.Knowledge.RelationshipsScoped(ctx, in.ProjectID, 2, in.AllowedProjects); e == nil {
		for _, edge := range relationships {
			r := edge.Relationship
			out.Items = append(out.Items, service.MemoryContextItem{ID: edge.ID, Kind: "suggestion", Type: "repository_relationship", Summary: r.Source.Repository + " → " + r.Target.Repository + " (" + r.Type + "). " + edge.Recommendation, Verification: edge.Status, Freshness: edge.Status, Confidence: "low", Scope: domain.MemoryScope{ProjectID: in.ProjectID}, RelatedScopes: []domain.MemoryScope{{ProjectID: r.Source.ProjectID, Repository: r.Source.Repository}, {ProjectID: r.Target.ProjectID, Repository: r.Target.Repository}}, Source: domain.MemoryProvenance{Type: domain.MemorySourceImport, Reference: r.Evidence, GitSHA: r.SourceRevision}, VerifiedAt: r.LastVerified})
		}
	}
	for i := range out.Items {
		out.Items[i].Origin = "individual"
	}
	if in.IncludePersonal {
		personal, e := b.s.Knowledge.Retrieve(ctx, service.MemoryContextQuery{Scope: domain.MemoryScope{}, Query: domain.MemoryQuery{Text: in.Query}, TokenBudget: in.TokenBudget, PrioritizeAuthority: true})
		if e == nil {
			for i := range personal.Items {
				personal.Items[i].Origin = "individual"
			}
			out.Items = append(out.Items, personal.Items...)
			out.Truncated = out.Truncated || personal.Truncated
		}
	}
	if len(out.Items) > 0 {
		out.Status = "available"
	}
	return out, nil
}

// teamMemoryTimeout bounds a Team memory read. Team memory is advisory context, so a Workspace Host that accepts a
// connection and then does not answer must not hold the assistant for the transport's own (much longer) timeout.
var teamMemoryTimeout = 10 * time.Second

func (b *workspaceBackend) MemoryContext(ctx context.Context, in MemoryRequest) (service.MemoryContext, error) {
	w, pid, ok := b.locate(in.ProjectID)
	if !ok {
		if strings.HasPrefix(in.ProjectID, "workspace:") {
			return service.MemoryContext{}, errorf(CodeNotFound, "project not found")
		}
		if local, ok := b.Backend.(MemoryBackend); ok {
			return local.MemoryContext(ctx, in)
		}
		return service.MemoryContext{Items: []service.MemoryContextItem{}, Status: "unavailable"}, nil
	}
	in.ProjectID = pid
	var out service.MemoryContext
	bounded, cancel := context.WithTimeout(ctx, teamMemoryTimeout)
	defer cancel()
	if err := b.read(bounded, w, "retrieve_memory", in, &out); err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return service.MemoryContext{Items: []service.MemoryContextItem{}, Status: "unavailable"}, nil
	}
	for i := range out.Items {
		out.Items[i].Origin = "team"
		if pid := out.Items[i].Scope.ProjectID; pid != "" {
			out.Items[i].Scope.ProjectID = workspaceProjectID(w.ID, pid)
		}
		for j := range out.Items[i].RelatedScopes {
			out.Items[i].RelatedScopes[j].ProjectID = workspaceProjectID(w.ID, out.Items[i].RelatedScopes[j].ProjectID)
		}
	}
	return out, nil
}

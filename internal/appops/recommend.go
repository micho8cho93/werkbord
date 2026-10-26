package appops

import (
	"context"
	"errors"
	"strings"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/service"
)

// Recommendations is what recommend_next_actions returns: the rules' answer for one project, never a model's.
type Recommendations = domain.RecommendationSet

// RecommendRequest carries what the rules need to know about who asks. The model chooses a project and, at most, one kind of
// suggestion; it cannot choose what the principal may read, who the owner is, or what counts as a policy.
type RecommendRequest struct {
	ProjectID      string
	Focus          string
	Now            time.Time
	Viewer         domain.RecViewer
	PersonalMemory bool
	// CanSee is the principal's own project allow-list, used to keep a connected workspace's memory within it.
	CanSee func(projectID string) bool
	// TeamMemory is filled by a backend that can reach the linked Team workspace; the local one leaves it empty.
	TeamMemory *service.TeamMemoryInput
}

// RecommendBackend is the read-only source of suggestions.
type RecommendBackend interface {
	Recommend(context.Context, RecommendRequest) (*domain.RecommendationSet, error)
}

// teamLinkBackend lists the Team projects a local project's tasks were imported from.
type teamLinkBackend interface {
	TeamLinks(context.Context, string) ([]service.TeamLink, error)
}

// opRecommend is a read. The set it returns holds ways to open existing screens; none of them starts anything, and the
// model that receives the set is told so by its own result (domain.RecommendationBoundary).
func opRecommend(ctx context.Context, e *env, args map[string]any) (any, error) {
	pid, _ := args["projectId"].(string)
	if e.p.Via != "assistant" || !e.p.CanSee(pid) {
		return nil, errorf(CodeNotFound, "project not found")
	}
	if _, err := e.b.Project(ctx, pid); err != nil {
		return nil, err
	}
	b, ok := e.b.(RecommendBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "suggestions are not available here")
	}
	focus, _ := args["focus"].(string)
	req := RecommendRequest{ProjectID: pid, Focus: focus, Now: e.now, CanSee: e.p.CanSee,
		// A session limited to some projects does not see personal memory, as for retrieve_memory.
		PersonalMemory: e.p.Projects == nil,
		// What the principal's permissions let it read decides what evidence is gathered at all: the rest is left out of
		// the facts, and the answer says which sources it could not use.
		Viewer: domain.RecViewer{Kind: "assistant", Tasks: e.p.Can(PermTicketsRead), Runs: e.p.Can(PermRunsRead), Repo: e.p.Can(PermProjectsRead), Memory: e.p.Can(PermMemoryRead)}}
	set, err := b.Recommend(ctx, req)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrForbidden) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var ae *Error
		if errors.As(err, &ae) {
			return nil, err
		}
		return nil, errorf(CodeUnavailable, "suggestions could not be worked out just now")
	}
	return *set, nil
}

func (b servicesBackend) Recommend(ctx context.Context, in RecommendRequest) (*domain.RecommendationSet, error) {
	if b.s.Recommender == nil {
		return nil, errorf(CodeUnavailable, "suggestions are not available here")
	}
	return b.s.Recommender.Recommend(ctx, service.RecommendInput{ProjectID: in.ProjectID, Viewer: in.Viewer, PersonalMemory: in.PersonalMemory, TeamMemory: in.TeamMemory, Focus: in.Focus})
}

func (b servicesBackend) TeamLinks(ctx context.Context, projectID string) ([]service.TeamLink, error) {
	if b.s.Recommender == nil {
		return nil, nil
	}
	return b.s.Recommender.TeamLinks(ctx, projectID)
}

// Recommend adds the connected workspaces' memory to the local answer. The desktop bridge is the only way to it: the
// controller holds no Team credential, Team rechecks membership on every read, and what comes back is the same reviewed
// projection that retrieve_memory returns.
func (b *workspaceBackend) Recommend(ctx context.Context, in RecommendRequest) (*domain.RecommendationSet, error) {
	if w, teamProject, ok := b.locate(in.ProjectID); ok {
		return b.recommendConnected(ctx, in, w, teamProject)
	}
	local, ok := b.Backend.(RecommendBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "suggestions are not available here")
	}
	if links, ok := b.Backend.(teamLinkBackend); ok && in.Viewer.Memory {
		if l, err := links.TeamLinks(ctx, in.ProjectID); err == nil && len(l) > 0 {
			in.TeamMemory = b.teamMemory(ctx, in, l)
		}
	}
	return local.Recommend(ctx, in)
}

// teamMemory reads the policies of every Team project the local project came from. A workspace that is not connected in
// this session, or that cannot be read, is a gap, and the rules treat a gap as the more careful route, never as consent.
func (b *workspaceBackend) teamMemory(ctx context.Context, in RecommendRequest, links []service.TeamLink) *service.TeamMemoryInput {
	out := &service.TeamMemoryInput{Status: domain.SourceStatusEmpty}
	missing := false
	if len(links) > service.MaxTeamReads {
		// Not every linked Team project can be read in one answer: the rest is a gap, said as one.
		links, missing = links[:service.MaxTeamReads], true
	}
	for _, l := range links {
		var w *Workspace
		for i := range b.binding.workspaces {
			if b.binding.workspaces[i].ID == l.WorkspaceID {
				w = &b.binding.workspaces[i]
			}
		}
		if w == nil {
			missing = true
			continue
		}
		items, err := b.teamPolicies(ctx, in, *w, l.TeamProjectID)
		if err != nil {
			if !b.skip(ctx, *w, err) {
				return &service.TeamMemoryInput{Status: domain.SourceStatusUnavailable}
			}
			missing = true
			continue
		}
		out.Items = append(out.Items, items...)
	}
	switch {
	case missing:
		out.Status = domain.SourceStatusUnavailable
	case len(out.Items) > 0:
		out.Status = domain.SourceStatusRead
	}
	return out
}

func (b *workspaceBackend) teamPolicies(ctx context.Context, in RecommendRequest, w Workspace, teamProject string) ([]service.MemoryContextItem, error) {
	var got service.MemoryContext
	if err := b.read(ctx, w, "retrieve_memory", MemoryRequest{ProjectID: teamProject, RequestType: "policy", TokenBudget: 2000}, &got); err != nil {
		return nil, err
	}
	var items []service.MemoryContextItem
	for _, it := range got.Items {
		// Every constituent project of a cross-repository record must be one this session may see, as for retrieve_memory.
		// The linked project itself is the one the person asked about.
		visible := func(id string) bool {
			return id == "" || id == teamProject || in.CanSee == nil || in.CanSee(workspaceProjectID(w.ID, id))
		}
		ok := visible(it.Scope.ProjectID)
		for _, rel := range it.RelatedScopes {
			ok = ok && visible(rel.ProjectID)
		}
		if ok {
			items = append(items, it)
		}
	}
	return items, nil
}

// recommendConnected answers for a project that lives in a connected Team workspace. There is no repository on this
// computer to read, so only the board and the workspace's policies count: what to pick up next, and what is in flight.
func (b *workspaceBackend) recommendConnected(ctx context.Context, in RecommendRequest, w Workspace, teamProject string) (*domain.RecommendationSet, error) {
	tickets, err := b.Tickets(ctx, in.ProjectID)
	if err != nil {
		return nil, err
	}
	facts := domain.RecFacts{Now: in.Now, ProjectID: in.ProjectID, Viewer: in.Viewer, Focus: in.Focus, Workflows: map[string]domain.RecWorkflow{},
		Repo:   domain.RecRepo{Status: domain.SourceStatusNotChecked},
		Memory: domain.RecMemory{Individual: domain.SourceStatusNotChecked, Team: domain.SourceStatusEmpty}}
	if p, e := b.Project(ctx, in.ProjectID); e == nil {
		facts.ProjectName = strings.TrimSuffix(p.Name, " ("+w.Name+")")
	}
	if !in.Viewer.Tasks {
		facts.Tasks.Status = domain.SourceStatusDenied
	} else {
		facts.Tasks.Status = domain.SourceStatusRead
		for i, t := range tickets {
			if t.ArchivedAt != nil || i >= 200 {
				continue
			}
			facts.Tasks.Items = append(facts.Tasks.Items, domain.RecTask{ID: t.ID, Title: t.Title, State: t.State, Ready: t.State == domain.TaskBacklog,
				PlannedStart: t.Plan.Start, Position: float64(i), UpdatedAt: t.UpdatedAt, Team: true})
		}
	}
	var items []service.MemoryContextItem
	if in.Viewer.Memory {
		var err error
		items, err = b.teamPolicies(ctx, in, w, teamProject)
		switch {
		case err != nil && !b.skip(ctx, w, err):
			return nil, err
		case err != nil:
			facts.Memory.Team = domain.SourceStatusUnavailable
		case len(items) > 0:
			facts.Memory.Team = domain.SourceStatusRead
		}
	} else {
		facts.Memory.Team = domain.SourceStatusDenied
	}
	for _, it := range items {
		facts.Memory.Items = append(facts.Memory.Items, domain.RecMemoryItem{ID: it.ID, Kind: it.Kind, Type: it.Type, Summary: domain.ClipRecText(it.Summary, 400),
			Source: domain.RecSrcTeam, Verification: it.Verification, Freshness: it.Freshness, Reference: domain.ClipRecText(it.Source.Reference, 200)})
	}
	set := domain.Recommend(facts)
	return &set, nil
}

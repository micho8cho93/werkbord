package service

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
	"errors"
	"fmt"
	"sort"
	"time"
)

// InitiativeView is a fresh, permission-checked projection, never an execution plan.
type InitiativeView struct {
	Initiative  domain.Initiative                `json:"initiative"`
	Projects    []domain.Project                 `json:"projects"`
	Tickets     []domain.Ticket                  `json:"tickets"`
	Memories    []integration.SharedMemoryRecord `json:"memories"`
	Activity    []domain.Activity                `json:"activity"`
	Done        int                              `json:"done"`
	OpenPRs     int                              `json:"openPRs"`
	Blockers    []string                         `json:"blockers"`
	NextActions []string                         `json:"nextActions"`
	GeneratedAt time.Time                        `json:"generatedAt"`
}

func (s *Service) initiativeAccess(ctx context.Context, tx store.Tx, a Actor, i domain.Initiative) error {
	if a.MCPGrantID != "" {
		return domain.ErrForbidden
	}
	for _, pid := range i.ProjectIDs {
		x, err := s.access(ctx, tx, a, pid)
		if err != nil {
			return err
		}
		if x.Project.Archived || !x.can(domain.PPTicketsView) || !x.can(domain.PPActivityView) || !x.can(domain.PPRepositoryView) {
			return domain.ErrNotFound
		}
	}
	return nil
}

// SaveInitiative requires a complete replacement plus the current version. Only
// the coordinator or workspace project manager can edit; every linked project's
// access and the new owner's membership are checked in the write transaction.
func (s *Service) SaveInitiative(ctx context.Context, a Actor, id string, in domain.Initiative) (domain.Initiative, error) {
	var out domain.Initiative
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	err := s.db.Update(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		expected := in.Version
		if id != "" {
			old, e := tx.Initiative(ctx, a.Workspace.ID, id)
			if e != nil {
				return e
			}
			if e = s.initiativeAccess(ctx, tx, a, old); e != nil {
				return e
			}
			if old.OwnerID != a.Member.ID && !a.Member.Can(domain.PermProjectsManage) {
				return domain.ErrForbidden
			}
			if expected != old.Version {
				return domain.ErrConflict
			}
			in.ID = id
			in.CreatedAt = old.CreatedAt
			in.History = old.History
		} else {
			if expected != 0 {
				return domain.ErrInvalid
			}
			all, e := tx.Initiatives(ctx, a.Workspace.ID)
			if e != nil {
				return e
			}
			if len(all) >= 1000 {
				return domain.ErrConflict
			}
			in.ID = domain.NewID("ini")
			in.CreatedAt = s.stamp()
			in.History = nil
		}
		var e error
		if in.Title, e = domain.CleanTicketTitle(in.Title); e != nil {
			return e
		}
		if in.Objective, e = domain.CleanTicketText("objective", in.Objective); e != nil {
			return e
		}
		if in.Objective == "" || len(in.ProjectIDs) == 0 || len(in.ProjectIDs) > 30 || len(in.Tickets) > 200 || len(in.Memories) > 100 || len(in.Dependencies) > 200 {
			return domain.ErrInvalid
		}
		if in.Status == "" {
			in.Status = "active"
		}
		switch in.Status {
		case "active", "planned", "paused", "completed", "archived":
		default:
			return domain.ErrInvalid
		}
		if in.OwnerID == "" {
			in.OwnerID = a.Member.ID
		}
		if _, e = tx.Member(ctx, a.Workspace.ID, in.OwnerID); e != nil {
			return domain.ErrInvalid
		}
		projects := map[string]bool{}
		for _, pid := range in.ProjectIDs {
			if projects[pid] {
				return domain.ErrInvalid
			}
			projects[pid] = true
			x, e := s.access(ctx, tx, a, pid)
			if e != nil {
				return e
			}
			if x.Project.Archived {
				return domain.ErrInvalid
			}
			on, e := tx.IsProjectMember(ctx, a.Workspace.ID, pid, in.OwnerID)
			if e != nil {
				return e
			}
			owner, e := tx.Member(ctx, a.Workspace.ID, in.OwnerID)
			if e != nil {
				return e
			}
			if !on && !owner.Can(domain.PermProjectsManage) {
				return domain.ErrForbidden
			}
		}
		if e = s.initiativeAccess(ctx, tx, a, in); e != nil {
			return e
		}
		tickets := map[domain.InitiativeTicket]bool{}
		for _, ref := range in.Tickets {
			if !projects[ref.ProjectID] || tickets[ref] {
				return domain.ErrInvalid
			}
			if _, e = tx.Ticket(ctx, a.Workspace.ID, ref.ProjectID, ref.TicketID); e != nil {
				return e
			}
			tickets[ref] = true
		}
		graph := map[domain.InitiativeTicket][]domain.InitiativeTicket{}
		edges := map[domain.InitiativeDependency]bool{}
		for _, d := range in.Dependencies {
			if !tickets[d.Before] || !tickets[d.After] || d.Before == d.After || edges[d] {
				return domain.ErrInvalid
			}
			edges[d] = true
			graph[d.Before] = append(graph[d.Before], d.After)
		}
		for ref := range tickets {
			k, err := tx.Ticket(ctx, a.Workspace.ID, ref.ProjectID, ref.TicketID)
			if err != nil {
				return err
			}
			for _, dep := range k.Dependencies {
				before := domain.InitiativeTicket{ProjectID: ref.ProjectID, TicketID: dep}
				if tickets[before] {
					graph[before] = append(graph[before], ref)
				}
			}
		}
		visiting, seen := map[domain.InitiativeTicket]bool{}, map[domain.InitiativeTicket]bool{}
		var visit func(domain.InitiativeTicket) bool
		visit = func(k domain.InitiativeTicket) bool {
			if visiting[k] {
				return false
			}
			if seen[k] {
				return true
			}
			visiting[k] = true
			for _, n := range graph[k] {
				if !visit(n) {
					return false
				}
			}
			visiting[k] = false
			seen[k] = true
			return true
		}
		for k := range tickets {
			if !visit(k) {
				return domain.ErrInvalid
			}
		}
		memories := map[domain.InitiativeMemory]bool{}
		for _, ref := range in.Memories {
			if ref.MemoryID == "" || !projects[ref.ProjectID] || memories[ref] {
				return domain.ErrInvalid
			}
			memories[ref] = true
			page, e := tx.SearchMemories(ctx, memoryPrincipal(a), ref.ProjectID, "", "", ref.MemoryID, s.stamp())
			if e != nil {
				return e
			}
			if len(page.Records) != 1 || page.Records[0].State != "published" {
				return domain.ErrNotFound
			}
		}
		in.Version = expected + 1
		in.UpdatedAt = s.stamp()
		kind := "updated"
		if expected == 0 {
			kind = "created"
		}
		if len(in.History) >= 500 {
			return domain.ErrConflict
		}
		in.History = append(in.History, domain.InitiativeEvent{ActorID: a.Member.ID, Kind: kind, At: in.UpdatedAt})
		if e = tx.SaveInitiative(ctx, a.Workspace.ID, in, expected); e != nil {
			return e
		}
		out = in
		return nil
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

func (s *Service) Initiatives(ctx context.Context, a Actor, project, id string) ([]InitiativeView, error) {
	out := []InitiativeView{}
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	err := s.db.View(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		if project != "" {
			if _, e := s.access(ctx, tx, a, project); e != nil {
				return e
			}
		}
		var all []domain.Initiative
		var e error
		if id != "" {
			i, err := tx.Initiative(ctx, a.Workspace.ID, id)
			if err != nil {
				return err
			}
			all = []domain.Initiative{i}
		} else {
			all, e = tx.Initiatives(ctx, a.Workspace.ID)
			if e != nil {
				return e
			}
		}
		for _, i := range all {
			if project != "" {
				found := false
				for _, p := range i.ProjectIDs {
					found = found || p == project
				}
				if !found {
					continue
				}
			}
			if e = s.initiativeAccess(ctx, tx, a, i); e != nil {
				if errors.Is(e, domain.ErrNotFound) {
					continue
				}
				return e
			}
			v, e := s.initiativeView(ctx, tx, a, i)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		if id != "" && len(out) == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return []InitiativeView{}, err
	}
	return out, nil
}

func (s *Service) initiativeView(ctx context.Context, tx store.Tx, a Actor, i domain.Initiative) (InitiativeView, error) {
	v := InitiativeView{Initiative: i, Projects: []domain.Project{}, Tickets: []domain.Ticket{}, Memories: []integration.SharedMemoryRecord{}, Activity: []domain.Activity{}, Blockers: []string{}, NextActions: []string{}, GeneratedAt: s.stamp()}
	// Never emit a saved memory ID after its scope or classification is revoked.
	v.Initiative.Memories = []domain.InitiativeMemory{}
	linkedBranches := map[string]bool{}
	for _, ref := range i.Tickets {
		k, err := tx.Ticket(ctx, a.Workspace.ID, ref.ProjectID, ref.TicketID)
		if err == nil && k.Branch != "" {
			linkedBranches[ref.ProjectID+":"+k.Branch] = true
		}
	}
	linked := map[string]bool{}
	for _, r := range i.Tickets {
		linked[r.TicketID] = true
	}
	tickets := map[domain.InitiativeTicket]domain.Ticket{}
	for _, pid := range i.ProjectIDs {
		p, e := tx.Project(ctx, a.Workspace.ID, pid)
		if e != nil {
			return v, e
		}
		v.Projects = append(v.Projects, p)
		activity, e := tx.Activity(ctx, a.Workspace.ID, pid, 0, 100)
		if e != nil {
			return v, e
		}
		for _, event := range activity {
			if linked[event.TicketID] {
				v.Activity = append(v.Activity, event)
			}
		}
		branches, e := tx.Branches(ctx, a.Workspace.ID, pid)
		if e != nil {
			return v, e
		}
		for x, b := range branches {
			for _, other := range branches[x+1:] {
				if b.Name == other.Name || (!linkedBranches[pid+":"+b.Name] && !linkedBranches[pid+":"+other.Name]) {
					continue
				}
				overlap := false
				for _, f := range b.Files {
					for _, g := range other.Files {
						overlap = overlap || f == g
					}
				}
				if overlap {
					v.Blockers = append(v.Blockers, fmt.Sprintf("%s: reported file overlap between %s and %s; review compatibility", p.Name, b.Name, other.Name))
				}
			}
		}
	}
	for _, ref := range i.Tickets {
		k, e := tx.Ticket(ctx, a.Workspace.ID, ref.ProjectID, ref.TicketID)
		if errors.Is(e, domain.ErrNotFound) {
			continue
		}
		if e != nil {
			return v, e
		}
		tickets[ref] = k
		v.Tickets = append(v.Tickets, k)
		if k.Status == domain.TicketDone {
			v.Done++
		}
		if pr := k.PullRequest; pr != nil {
			if pr.State == domain.PROpen {
				v.OpenPRs++
			}
			if pr.ReportedAt.IsZero() || s.stamp().Sub(pr.ReportedAt) > 24*time.Hour {
				v.Blockers = append(v.Blockers, k.Key+": PR report needs revalidation")
			} else if pr.Mergeable == domain.MergeConflicting {
				v.Blockers = append(v.Blockers, k.Key+": reported PR conflict")
			}
		}
	}
	blocked := map[string]bool{}
	dependencies := append([]domain.InitiativeDependency{}, i.Dependencies...)
	for _, ref := range i.Tickets {
		k, ok := tickets[ref]
		if !ok {
			continue
		}
		for _, id := range k.Dependencies {
			before := domain.InitiativeTicket{ProjectID: ref.ProjectID, TicketID: id}
			if _, ok := tickets[before]; !ok {
				dependency, err := tx.Ticket(ctx, a.Workspace.ID, ref.ProjectID, id)
				if err != nil && !errors.Is(err, domain.ErrNotFound) {
					return v, err
				}
				if err == nil {
					tickets[before] = dependency
				}
			}
			dependencies = append(dependencies, domain.InitiativeDependency{Before: before, After: ref})
		}
	}
	seenDependencies := map[domain.InitiativeDependency]bool{}
	for _, d := range dependencies {
		if seenDependencies[d] {
			continue
		}
		seenDependencies[d] = true
		before, bok := tickets[d.Before]
		after, aok := tickets[d.After]
		if !bok || !aok || before.ArchivedAt != nil || after.ArchivedAt != nil {
			v.Blockers = append(v.Blockers, "Linked dependency needs revalidation")
			continue
		}
		if before.Status != domain.TicketDone {
			blocked[after.ID] = true
			v.Blockers = append(v.Blockers, after.Key+" waits for "+before.Key)
		} else if before.UpdatedAt.After(after.UpdatedAt) && after.Status != domain.TicketBacklog {
			v.Blockers = append(v.Blockers, after.Key+": upstream work changed; revalidate against "+before.Key)
		}
	}
	for _, ref := range i.Memories {
		page, e := tx.SearchMemories(ctx, memoryPrincipal(a), ref.ProjectID, "", "", ref.MemoryID, s.stamp())
		if errors.Is(e, domain.ErrNotFound) || errors.Is(e, domain.ErrForbidden) {
			continue
		}
		if e != nil {
			return v, e
		}
		for _, m := range page.Records {
			if m.State == "published" {
				v.Memories = append(v.Memories, m)
				v.Initiative.Memories = append(v.Initiative.Memories, ref)
			}
		}
	}
	sort.SliceStable(v.Activity, func(i, j int) bool { return v.Activity[i].CreatedAt.After(v.Activity[j].CreatedAt) })
	for _, k := range v.Tickets {
		if k.ArchivedAt != nil || blocked[k.ID] {
			continue
		}
		switch k.Status {
		case domain.TicketAvailable:
			if k.AssigneeID == "" {
				v.NextActions = append(v.NextActions, k.Key+": claim through its project board before starting work.")
			} else {
				v.NextActions = append(v.NextActions, k.Key+": its owner can continue through the existing local policy and approval workflow.")
			}
		case domain.TicketReview:
			v.NextActions = append(v.NextActions, k.Key+": request the project's existing reviewer workflow.")
		case domain.TicketInProgress:
			v.NextActions = append(v.NextActions, k.Key+": coordinate with its current owner before proposing overlapping work.")
		}
	}
	if len(v.Blockers) > 0 {
		v.NextActions = append(v.NextActions, "Review blockers and revalidate dependent work before requesting execution.")
	}
	if v.OpenPRs > 0 {
		v.NextActions = append(v.NextActions, "Prepare a coordinated review through the existing project review workflow; keep each project's reviewers.")
	}
	v.NextActions = append(v.NextActions, "Use Salie's existing ticket proposal and confirmation to propose missing work. Claim tickets normally; execution remains on the ticket owner's approved Individual runner.")
	return v, nil
}

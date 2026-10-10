package service

import (
	"context"
	"fmt"

	"devboard/internal/planning"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// Labels, who does a ticket, planned dates, and the order tickets depend on.
//
// The workspace owns the labels: every member sees them, and only people who may manage the workspace
// (domain.PermLabelsManage: the owner and admins) define, change or delete them. Putting a label on a ticket,
// choosing who does it, planning it and saying what it waits for are ticket edits, with the ticket's own rules:
// the creator or anyone with tickets.edit.
//
// None of it changes how a ticket moves, and Team still runs nothing: the work mode decides only whether a request
// for agent work may be made for a ticket (see SetSchedule), and the dependencies and dates are only ever shown. A
// conflict between them is a warning (Timeline) and is never resolved by moving anything.

// LabelInput is what defining a label takes.
type LabelInput struct {
	Name        string
	Color       string
	Description string
}

// LabelPatch is a partial update of a label. Version must be the version the caller last read.
type LabelPatch struct {
	Name        *string
	Color       *string
	Description *string
	Version     int64
}

// ListLabels lists the workspace's labels with how many open tickets carry each in the projects the actor can see.
// Every member may; a count never includes a project the actor cannot see.
func (s *Service) ListLabels(ctx context.Context, a Actor) ([]domain.LabelUse, error) {
	if err := a.require(domain.PermWorkspaceView, "see the workspace's labels"); err != nil {
		return nil, err
	}
	var out []domain.LabelUse
	err := s.db.View(ctx, func(tx store.Tx) error {
		ls, err := tx.Labels(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		only := a.Member.ID
		if a.Member.Can(domain.PermProjectsViewAll) {
			only = ""
		}
		use, err := tx.LabelUsage(ctx, a.Workspace.ID, only)
		if err != nil {
			return err
		}
		out = make([]domain.LabelUse, 0, len(ls))
		for _, l := range ls {
			out = append(out, domain.LabelUse{Label: l, Tickets: use[l.ID]})
		}
		return nil
	})
	return out, err
}

// CreateLabel defines a label for the whole workspace.
func (s *Service) CreateLabel(ctx context.Context, a Actor, in LabelInput) (domain.Label, error) {
	if err := a.require(domain.PermLabelsManage, "manage the workspace's labels"); err != nil {
		return domain.Label{}, err
	}
	name, color, desc, err := domain.CleanLabel(in.Name, in.Color, in.Description)
	if err != nil {
		return domain.Label{}, err
	}
	now := s.stamp()
	l := domain.Label{ID: domain.NewID(domain.PrefixLabel), WorkspaceID: a.Workspace.ID, Name: name, Color: color, Description: desc, Version: 1, CreatedAt: now, UpdatedAt: now}
	err = s.db.Update(ctx, func(tx store.Tx) error {
		n, err := tx.CountLabels(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		if n >= planning.MaxLabels {
			return fmt.Errorf("%w: the workspace already has %d labels; delete some before adding more", domain.ErrConflict, n)
		}
		return tx.InsertLabel(ctx, a.Workspace.ID, l)
	})
	s.changed(a.Workspace.ID, err)
	return l, err
}

// UpdateLabel renames, recolours or re-describes a label, wherever it is used.
func (s *Service) UpdateLabel(ctx context.Context, a Actor, id string, patch LabelPatch) (domain.Label, error) {
	if err := a.require(domain.PermLabelsManage, "manage the workspace's labels"); err != nil {
		return domain.Label{}, err
	}
	var l domain.Label
	var projects []string
	err := s.db.Update(ctx, func(tx store.Tx) (err error) {
		if l, err = tx.Label(ctx, a.Workspace.ID, id); err != nil {
			return err
		}
		if l.Version != patch.Version {
			return fmt.Errorf("%w: the label was changed by someone else; reload it and try again", domain.ErrConflict)
		}
		name, color, desc := l.Name, l.Color, l.Description
		if patch.Name != nil {
			name = *patch.Name
		}
		if patch.Color != nil {
			color = *patch.Color
		}
		if patch.Description != nil {
			desc = *patch.Description
		}
		if l.Name, l.Color, l.Description, err = domain.CleanLabel(name, color, desc); err != nil {
			return err
		}
		l.UpdatedAt = s.stamp()
		if l, err = tx.UpdateLabel(ctx, a.Workspace.ID, l); err != nil {
			return err
		}
		// A renamed or recoloured label changes what every board that shows it looks like.
		projects, err = s.touchLabelProjects(ctx, tx, a, id)
		return err
	})
	s.notifyProjects(projects)
	s.changed(a.Workspace.ID, err)
	return l, err
}

// touchLabelProjects moves the revision of every project that has a ticket carrying the label, and returns them.
func (s *Service) touchLabelProjects(ctx context.Context, tx store.Tx, a Actor, labelID string) ([]string, error) {
	ps, err := tx.Projects(ctx, a.Workspace.ID, "")
	if err != nil {
		return nil, err
	}
	var touched []string
	for _, p := range ps {
		ts, err := tx.Tickets(ctx, a.Workspace.ID, p.ID)
		if err != nil {
			return nil, err
		}
		for _, k := range ts {
			if hasString(k.LabelIDs, labelID) {
				if _, err := tx.BumpRevision(ctx, a.Workspace.ID, p.ID); err != nil {
					return nil, err
				}
				touched = append(touched, p.ID)
				break
			}
		}
	}
	return touched, nil
}

func (s *Service) notifyProjects(ids []string) {
	for _, id := range ids {
		s.hub.notify(id)
	}
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// DeleteLabel removes a label from every ticket that carries it and then the label. The tickets are otherwise
// untouched: nothing moves, nobody is told to do anything.
func (s *Service) DeleteLabel(ctx context.Context, a Actor, id string) error {
	if err := a.require(domain.PermLabelsManage, "manage the workspace's labels"); err != nil {
		return err
	}
	var projects []string
	err := s.db.Update(ctx, func(tx store.Tx) (err error) {
		if _, err := tx.Label(ctx, a.Workspace.ID, id); err != nil {
			return err
		}
		if projects, err = tx.DeleteLabel(ctx, a.Workspace.ID, id, s.stamp()); err != nil {
			return err
		}
		for _, p := range projects {
			if _, err := tx.BumpRevision(ctx, a.Workspace.ID, p); err != nil {
				return err
			}
		}
		return nil
	})
	s.notifyProjects(projects)
	s.changed(a.Workspace.ID, err)
	return err
}

// ticketPlanning is the planning side of a ticket as a person states it, for a new ticket or an edit.
type ticketPlanning struct {
	mode         *planning.ExecutionMode
	labelIDs     *[]string
	plan         *planning.Range
	dependencies *[]string
}

// applyPlanning checks what a ticket is to be planned as and puts it on k (which is saved by the caller). Labels must
// be the workspace's own; dependencies must be other tickets of the same project and must not make a circle. A ticket
// that was never planned keeps being what it was.
func (s *Service) applyPlanning(ctx context.Context, tx store.Tx, a Actor, x access, k *domain.Ticket, in ticketPlanning) error {
	if in.mode != nil {
		mode, err := planning.ParseExecutionMode(string(*in.mode))
		if err != nil {
			return domain.FromPlanning(err)
		}
		k.WorkMode = mode
	}
	if in.plan != nil {
		plan, err := planning.CleanRange(*in.plan)
		if err != nil {
			return domain.FromPlanning(err)
		}
		k.Plan = plan
	}
	if in.labelIDs != nil {
		ids, err := planning.CleanLabelIDs(*in.labelIDs)
		if err != nil {
			return domain.FromPlanning(err)
		}
		for _, id := range ids {
			if _, err := tx.Label(ctx, a.Workspace.ID, id); err != nil {
				return fmt.Errorf("%w: label %s does not exist in this workspace", domain.ErrInvalid, id)
			}
		}
		k.LabelIDs = ids
	}
	if in.dependencies != nil {
		deps, err := s.cleanDependencies(ctx, tx, a, x, *k, *in.dependencies)
		if err != nil {
			return err
		}
		k.Dependencies = deps
	}
	return nil
}

// MaxDependencies is how many tickets one ticket may wait for.
const MaxDependencies = 100

func (s *Service) cleanDependencies(ctx context.Context, tx store.Tx, a Actor, x access, k domain.Ticket, ids []string) ([]string, error) {
	if len(ids) > MaxDependencies {
		return nil, fmt.Errorf("%w: a ticket can wait for at most %d others", domain.ErrInvalid, MaxDependencies)
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		switch {
		case id == "" || len(id) > 100:
			return nil, fmt.Errorf("%w: a dependency is not a ticket", domain.ErrInvalid)
		case id == k.ID:
			return nil, fmt.Errorf("%w: a ticket cannot depend on itself", domain.ErrInvalid)
		case seen[id]:
			return nil, fmt.Errorf("%w: a ticket is listed twice as a dependency", domain.ErrInvalid)
		}
		seen[id] = true
		if _, err := tx.Ticket(ctx, a.Workspace.ID, x.Project.ID, id); err != nil {
			return nil, fmt.Errorf("%w: a dependency must be another ticket of this project (%s is not)", domain.ErrInvalid, id)
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return out, nil
	}
	all, err := tx.Tickets(ctx, a.Workspace.ID, x.Project.ID)
	if err != nil {
		return nil, err
	}
	graph := make(map[string][]string, len(all)+1)
	for _, t := range all {
		graph[t.ID] = t.Dependencies
	}
	graph[k.ID] = out
	if planning.HasCycle(graph) {
		return nil, fmt.Errorf("%w: those dependencies would make tickets wait for each other in a circle", domain.ErrInvalid)
	}
	return out, nil
}

// Timeline is what a project's timeline needs beyond its tickets, which the board already carries: what is wrong
// with the dependencies and the planned dates among them. It only reports.
type Timeline struct {
	Warnings []planning.Warning `json:"warnings"`
}

// Timeline checks a project's dependencies and planned dates.
func (s *Service) Timeline(ctx context.Context, a Actor, projectID string) (Timeline, error) {
	var out Timeline
	err := s.view(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		ts, err := tx.Tickets(ctx, a.Workspace.ID, projectID)
		if err != nil {
			return err
		}
		out.Warnings = TimelineWarnings(ts)
		return nil
	})
	return out, err
}

// TimelineWarnings is the analysis behind Timeline, on tickets already in hand.
func TimelineWarnings(tickets []domain.Ticket) []planning.Warning {
	items := make([]planning.Item, 0, len(tickets))
	for _, k := range tickets {
		items = append(items, planning.Item{
			ID: k.ID, Title: k.Key + " " + k.Title, Range: k.Plan, Done: k.Status == domain.TicketDone,
			Archived: k.ArchivedAt != nil, Dependencies: k.Dependencies,
		})
	}
	return planning.AnalyzeOpen(items)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"werkbord/internal/mcpplan"
	"werkbord/internal/planning"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
)

const (
	mcpPlanPending  = "pending"
	mcpPlanApproved = "approved"
	mcpPlanDeclined = "declined"
)

// MCPTaskPlanView is the human review shape and the external client's
// proposal-status shape. Secrets and request digests are never exposed.
type MCPTaskPlanView struct {
	ID              string             `json:"id"`
	GrantID         string             `json:"grantId"`
	Client          string             `json:"client"`
	ProjectID       string             `json:"projectId"`
	State           string             `json:"state"`
	Plan            mcpplan.Plan       `json:"plan"`
	Warnings        []planning.Warning `json:"warnings,omitempty"`
	ValidationError string             `json:"validationError,omitempty"`
	Result          *mcpplan.Applied   `json:"result,omitempty"`
	CreatedAt       string             `json:"createdAt"`
	UpdatedAt       string             `json:"updatedAt"`
	ResolvedAt      string             `json:"resolvedAt,omitempty"`
}

type teamAppliedPlan struct {
	Result mcpplan.Applied `json:"result"`
}

// ProposeMCPTaskPlan records an external plan for a human decision. Repeating
// the same grant/key/content returns the existing proposal; changing content
// under that key is a conflict.
func (s *Service) ProposeMCPTaskPlan(ctx context.Context, a Actor, raw mcpplan.Plan) (MCPTaskPlanView, error) {
	if a.MCPGrantID == "" {
		return MCPTaskPlanView{}, fmt.Errorf("%w: task plans can only be proposed by an MCP client", domain.ErrForbidden)
	}
	plan, err := mcpplan.Clean(raw)
	if err != nil {
		return MCPTaskPlanView{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	requestDigest := mcpplan.Digest(plan)
	var proposal domain.MCPTaskPlanProposal
	err = s.db.Update(ctx, func(tx store.Tx) error {
		grant, err := tx.MCPGrant(ctx, a.Workspace.ID, a.Member.ID, a.MCPGrantID)
		if err != nil || grant.RevokedAt != nil {
			return domain.ErrUnauthenticated
		}
		if !contains(grant.Projects, plan.ProjectID) || !contains(grant.Operations, "propose_task_plan") {
			return fmt.Errorf("%w: this MCP client is not granted task plans for that project", domain.ErrForbidden)
		}
		x, err := s.access(ctx, tx, a, plan.ProjectID)
		if err != nil {
			return err
		}
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		if x.Project.Archived {
			return fmt.Errorf("%w: the project is archived", domain.ErrConflict)
		}
		prior, err := tx.MCPTaskPlanByKey(ctx, a.Workspace.ID, a.MCPGrantID, plan.ProjectID, plan.IdempotencyKey)
		if err == nil {
			if prior.RequestDigest != requestDigest {
				return fmt.Errorf("%w: this idempotency key was used for a different task plan", domain.ErrConflict)
			}
			proposal = prior
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if _, _, _, err := prepareTeamMCPTaskPlan(ctx, tx, s, a, x, plan, false, false); err != nil {
			return err
		}
		encoded, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		now := s.stamp()
		proposal = domain.MCPTaskPlanProposal{ID: domain.NewID(domain.PrefixMCPPlan), WorkspaceID: a.Workspace.ID, MemberID: a.Member.ID,
			GrantID: a.MCPGrantID, ProjectID: plan.ProjectID, IdempotencyKey: plan.IdempotencyKey,
			RequestDigest: requestDigest, PlanDigest: requestDigest, PlanJSON: string(encoded), State: mcpPlanPending, ResultJSON: "{}", CreatedAt: now, UpdatedAt: now}
		return tx.InsertMCPTaskPlan(ctx, proposal)
	})
	if err != nil {
		return MCPTaskPlanView{}, err
	}
	return s.mcpTaskPlanView(ctx, a, proposal)
}

// PreviewMCPTaskPlan validates the current Team board state and reports
// non-blocking timeline conflicts for a proposed plan.
func (s *Service) PreviewMCPTaskPlan(ctx context.Context, a Actor, raw mcpplan.Plan) (mcpplan.Preview, error) {
	plan, err := mcpplan.Clean(raw)
	if err != nil {
		return mcpplan.Preview{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	var out mcpplan.Preview
	err = s.view(ctx, a, plan.ProjectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		_, _, warnings, err := prepareTeamMCPTaskPlan(ctx, tx, s, a, x, plan, false, false)
		out.Warnings = warnings
		return err
	})
	return out, err
}

// ListMCPTaskPlans returns pending plans visible to the actor. MCP clients only
// see proposals belonging to their own grant; members see proposals on projects
// they can view.
func (s *Service) ListMCPTaskPlans(ctx context.Context, a Actor) ([]MCPTaskPlanView, error) {
	var rows []domain.MCPTaskPlanProposal
	err := s.db.View(ctx, func(tx store.Tx) error {
		grantID := a.MCPGrantID
		var err error
		rows, err = tx.MCPTaskPlans(ctx, a.Workspace.ID, grantID, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]MCPTaskPlanView, 0, len(rows))
	for _, proposal := range rows {
		if a.MCPGrantID == "" {
			var visible bool
			_ = s.db.View(ctx, func(tx store.Tx) error {
				x, err := s.access(ctx, tx, a, proposal.ProjectID)
				visible = err == nil && x.can(domain.PPTicketsView)
				return nil
			})
			if !visible {
				continue
			}
		}
		view, err := s.mcpTaskPlanView(ctx, a, proposal)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// MCPTaskPlanStatus returns one proposal only to its originating MCP grant or
// a human who can currently view its project.
func (s *Service) MCPTaskPlanStatus(ctx context.Context, a Actor, id string) (MCPTaskPlanView, error) {
	var proposal domain.MCPTaskPlanProposal
	err := s.db.View(ctx, func(tx store.Tx) error {
		var err error
		proposal, err = tx.MCPTaskPlan(ctx, a.Workspace.ID, id)
		if err != nil {
			return err
		}
		if a.MCPGrantID != "" && proposal.GrantID != a.MCPGrantID {
			return domain.ErrNotFound
		}
		x, err := s.access(ctx, tx, a, proposal.ProjectID)
		if err != nil {
			return err
		}
		return x.require(domain.PPTicketsView, "see this project's tickets")
	})
	if err != nil {
		return MCPTaskPlanView{}, err
	}
	return s.mcpTaskPlanView(ctx, a, proposal)
}

// ReplaceMCPTaskPlan changes a pending proposal but keeps its grant, project
// and idempotency key. Current task versions and plan validity are checked now
// and again during approval.
func (s *Service) ReplaceMCPTaskPlan(ctx context.Context, a Actor, id string, raw mcpplan.Plan) (MCPTaskPlanView, error) {
	if a.MCPGrantID != "" {
		return MCPTaskPlanView{}, fmt.Errorf("%w: an MCP client cannot edit or approve proposals", domain.ErrForbidden)
	}
	plan, err := mcpplan.Clean(raw)
	if err != nil {
		return MCPTaskPlanView{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	var proposal domain.MCPTaskPlanProposal
	err = s.mutate(ctx, a, plan.ProjectID, func(tx store.Tx, x access) error {
		proposal, err = tx.MCPTaskPlan(ctx, a.Workspace.ID, id)
		if err != nil {
			return err
		}
		if proposal.State != mcpPlanPending || proposal.ProjectID != plan.ProjectID {
			return fmt.Errorf("%w: proposal is no longer pending in this project", domain.ErrConflict)
		}
		var old mcpplan.Plan
		if err := json.Unmarshal([]byte(proposal.PlanJSON), &old); err != nil {
			return fmt.Errorf("%w: saved proposal is invalid", domain.ErrConflict)
		}
		if old.ProjectID != plan.ProjectID || old.IdempotencyKey != plan.IdempotencyKey {
			return fmt.Errorf("%w: project and idempotency key cannot change", domain.ErrInvalid)
		}
		if x.Project.Archived {
			return fmt.Errorf("%w: the project is archived", domain.ErrConflict)
		}
		if _, _, _, err := prepareTeamMCPTaskPlan(ctx, tx, s, a, x, plan, false, true); err != nil {
			return err
		}
		encoded, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		proposal.PlanJSON, proposal.PlanDigest, proposal.UpdatedAt = string(encoded), mcpplan.Digest(plan), s.stamp()
		return tx.SaveMCPTaskPlan(ctx, proposal, true)
	})
	if err != nil {
		return MCPTaskPlanView{}, err
	}
	return s.mcpTaskPlanView(ctx, a, proposal)
}

// ResolveMCPTaskPlan applies or declines the complete plan atomically. Approval
// retries are idempotent, and all authority and versions are checked in the
// same transaction that writes the tickets.
func (s *Service) ResolveMCPTaskPlan(ctx context.Context, a Actor, id string, approve bool) (MCPTaskPlanView, error) {
	if a.MCPGrantID != "" {
		return MCPTaskPlanView{}, fmt.Errorf("%w: an MCP client cannot approve its own proposal", domain.ErrForbidden)
	}
	var prior domain.MCPTaskPlanProposal
	err := s.db.View(ctx, func(tx store.Tx) error {
		var err error
		prior, err = tx.MCPTaskPlan(ctx, a.Workspace.ID, id)
		return err
	})
	if err != nil {
		return MCPTaskPlanView{}, err
	}
	var proposal domain.MCPTaskPlanProposal
	err = s.mutate(ctx, a, prior.ProjectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		proposal, err = tx.MCPTaskPlan(ctx, a.Workspace.ID, id)
		if err != nil {
			return err
		}
		if proposal.ProjectID != prior.ProjectID {
			return domain.ErrConflict
		}
		if proposal.State == mcpPlanApproved && approve || proposal.State == mcpPlanDeclined && !approve {
			return nil // safe replay of the same decision
		}
		if proposal.State != mcpPlanPending {
			return fmt.Errorf("%w: proposal already has a different decision", domain.ErrConflict)
		}
		if !approve {
			proposal.State, proposal.ResolvedAt, proposal.UpdatedAt = mcpPlanDeclined, timePtr(s.stamp()), s.stamp()
			return tx.SaveMCPTaskPlan(ctx, proposal, true)
		}
		if x.Project.Archived {
			return fmt.Errorf("%w: the project is archived", domain.ErrConflict)
		}
		var plan mcpplan.Plan
		if err := json.Unmarshal([]byte(proposal.PlanJSON), &plan); err != nil {
			return fmt.Errorf("%w: saved proposal is invalid", domain.ErrConflict)
		}
		result, err := applyTeamMCPTaskPlan(ctx, tx, s, a, x, plan)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		proposal.State, proposal.ResultJSON = mcpPlanApproved, string(encoded)
		proposal.ResolvedAt, proposal.UpdatedAt = timePtr(s.stamp()), s.stamp()
		return tx.SaveMCPTaskPlan(ctx, proposal, true)
	})
	if err != nil {
		return MCPTaskPlanView{}, err
	}
	return s.mcpTaskPlanView(ctx, a, proposal)
}

func (s *Service) mcpTaskPlanView(ctx context.Context, a Actor, proposal domain.MCPTaskPlanProposal) (MCPTaskPlanView, error) {
	var plan mcpplan.Plan
	if err := json.Unmarshal([]byte(proposal.PlanJSON), &plan); err != nil {
		return MCPTaskPlanView{}, fmt.Errorf("%w: saved proposal could not be read", domain.ErrConflict)
	}
	view := MCPTaskPlanView{ID: proposal.ID, GrantID: proposal.GrantID, ProjectID: proposal.ProjectID, State: proposal.State,
		Plan: plan, CreatedAt: proposal.CreatedAt.Format(time.RFC3339), UpdatedAt: proposal.UpdatedAt.Format(time.RFC3339)}
	if proposal.ResolvedAt != nil {
		view.ResolvedAt = proposal.ResolvedAt.Format(time.RFC3339)
	}
	_ = s.db.View(ctx, func(tx store.Tx) error {
		if grant, err := tx.MCPGrant(ctx, proposal.WorkspaceID, proposal.MemberID, proposal.GrantID); err == nil {
			view.Client = grant.Name
		}
		return nil
	})
	if proposal.State == mcpPlanPending {
		preview, err := s.PreviewMCPTaskPlan(ctx, a, plan)
		if err != nil {
			view.ValidationError = err.Error()
		} else {
			view.Warnings = preview.Warnings
		}
	}
	if proposal.State == mcpPlanApproved && proposal.ResultJSON != "" {
		var result teamAppliedPlan
		if json.Unmarshal([]byte(proposal.ResultJSON), &result) == nil {
			view.Result = &result.Result
		}
	}
	return view, nil
}

type teamPlannedChange struct {
	key          string
	ticket       domain.Ticket
	dependencies []string
	create       bool
}

func prepareTeamMCPTaskPlan(ctx context.Context, tx store.Tx, s *Service, a Actor, x access, plan mcpplan.Plan, applying, enforceWrite bool) (map[string]teamPlannedChange, map[string]string, []planning.Warning, error) {
	if x.Project.ID != plan.ProjectID {
		return nil, nil, nil, domain.ErrNotFound
	}
	if enforceWrite {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return nil, nil, nil, err
		}
	}
	all, err := tx.Tickets(ctx, a.Workspace.ID, plan.ProjectID)
	if err != nil {
		return nil, nil, nil, err
	}
	byID := make(map[string]domain.Ticket, len(all))
	for _, ticket := range all {
		byID[ticket.ID] = ticket
	}
	ids := make(map[string]string)
	for _, change := range plan.Tasks {
		if change.TempID != "" {
			id := change.TempID
			if applying {
				id = domain.NewID(domain.PrefixTicket)
			}
			ids[change.TempID] = id
		}
	}
	changedKeys := make(map[string]bool, len(plan.Tasks))
	changes := make(map[string]teamPlannedChange, len(plan.Tasks))
	touched := map[string]bool{}
	for _, change := range plan.Tasks {
		var ticket domain.Ticket
		create := change.TempID != ""
		key := change.TaskID
		if create {
			key = change.TempID
			status := domain.TicketAvailable
			mode := planning.ModeAgent
			now := s.stamp()
			ticket = domain.Ticket{ID: ids[change.TempID], ProjectID: plan.ProjectID, Status: status, CreatorID: a.Member.ID,
				Version: 1, CreatedAt: now, UpdatedAt: now, WorkMode: mode, LabelIDs: []string{}, Dependencies: []string{}, Commits: []domain.Commit{}}
		} else {
			existing, ok := byID[change.TaskID]
			if !ok {
				return nil, nil, nil, fmt.Errorf("%w: task %s was not found in this project", domain.ErrNotFound, change.TaskID)
			}
			if existing.Version != change.Version {
				return nil, nil, nil, fmt.Errorf("%w: task %s changed; expected version %d and found %d", domain.ErrConflict, existing.ID, change.Version, existing.Version)
			}
			if existing.ArchivedAt != nil {
				return nil, nil, nil, fmt.Errorf("%w: restore task %s before editing it", domain.ErrConflict, existing.ID)
			}
			if existing.Status == domain.TicketDone {
				return nil, nil, nil, fmt.Errorf("%w: finished task %s must be reopened before editing", domain.ErrConflict, existing.Key)
			}
			ticket = existing
		}
		if enforceWrite {
			if create {
				if err := x.require(domain.PPTicketsCreate, "create tickets"); err != nil {
					return nil, nil, nil, err
				}
			} else if ticket.CreatorID != a.Member.ID && !x.can(domain.PPTicketsEdit) {
				return nil, nil, nil, forbidden("edit a ticket someone else created")
			}
		}
		if change.Title != nil {
			if ticket.Title, err = domain.CleanTicketTitle(*change.Title); err != nil {
				return nil, nil, nil, err
			}
		} else if create {
			return nil, nil, nil, fmt.Errorf("%w: new task %s needs a title", domain.ErrInvalid, change.TempID)
		}
		if change.Description != nil {
			if ticket.Description, err = domain.CleanTicketText("description", *change.Description); err != nil {
				return nil, nil, nil, err
			}
		}
		if change.Requirements != nil {
			if ticket.Requirements, err = domain.CleanTicketText("requirements", *change.Requirements); err != nil {
				return nil, nil, nil, err
			}
		}
		if change.Plan != nil {
			if ticket.Plan, err = planning.CleanRange(*change.Plan); err != nil {
				return nil, nil, nil, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
			}
		}
		if change.SourceRef != nil {
			ticket.SourceRef = strings.TrimSpace(*change.SourceRef)
		}
		ticket.UpdatedAt = s.stamp()
		if change.Dependencies != nil {
			deps := make([]string, 0, len(*change.Dependencies))
			for _, dependency := range *change.Dependencies {
				if mapped, ok := ids[dependency]; ok {
					deps = append(deps, mapped)
				} else if _, ok := byID[dependency]; ok {
					deps = append(deps, dependency)
				} else {
					return nil, nil, nil, fmt.Errorf("%w: dependency %s does not exist in this project", domain.ErrInvalid, dependency)
				}
			}
			ticket.Dependencies = deps
		}
		changes[key] = teamPlannedChange{key: key, ticket: ticket, dependencies: append([]string(nil), ticket.Dependencies...), create: create}
		changedKeys[ticket.ID] = true
		touched[ticket.ID] = true
	}

	// A source reference is a per-project stable identity. Check the entire final
	// board, not just the proposed rows, before either previewing or writing.
	refs := map[string]string{}
	for _, ticket := range all {
		if changedKeys[ticket.ID] || ticket.SourceRef == "" {
			continue
		}
		refs[ticket.SourceRef] = ticket.ID
	}
	for _, change := range changes {
		ref := change.ticket.SourceRef
		if ref == "" {
			continue
		}
		if prior := refs[ref]; prior != "" && prior != change.ticket.ID {
			return nil, nil, nil, fmt.Errorf("%w: source reference %q is already used by another task", domain.ErrConflict, ref)
		}
		refs[ref] = change.ticket.ID
	}

	items := make([]planning.Item, 0, len(all)+len(changes))
	for _, ticket := range all {
		if updated, ok := changes[ticket.ID]; ok {
			ticket = updated.ticket
		}
		items = append(items, teamPlanningItem(ticket))
	}
	for _, change := range plan.Tasks {
		if change.TempID != "" {
			items = append(items, teamPlanningItem(changes[change.TempID].ticket))
		}
	}
	warnings := planning.AnalyzeOpen(items)
	for _, warning := range warnings {
		if warning.Severity != planning.SeverityError {
			continue
		}
		relevant := touched[warning.ItemID]
		for _, cycleID := range warning.Cycle {
			relevant = relevant || touched[cycleID]
		}
		if relevant {
			return nil, nil, nil, fmt.Errorf("%w: %s", domain.ErrInvalid, warning.Message)
		}
	}
	visible := make([]planning.Warning, 0, len(warnings))
	for _, warning := range warnings {
		if touched[warning.ItemID] || touched[warning.OtherID] {
			visible = append(visible, warning)
		}
	}
	return changes, ids, visible, nil
}

func teamPlanningItem(ticket domain.Ticket) planning.Item {
	title := ticket.Title
	if ticket.Key != "" {
		title = ticket.Key + " " + title
	}
	return planning.Item{ID: ticket.ID, Title: title, Range: ticket.Plan,
		Done: ticket.Status == domain.TicketDone, Archived: ticket.ArchivedAt != nil, Dependencies: ticket.Dependencies}
}

func applyTeamMCPTaskPlan(ctx context.Context, tx store.Tx, s *Service, a Actor, x access, plan mcpplan.Plan) (mcpplan.Applied, error) {
	changes, ids, _, err := prepareTeamMCPTaskPlan(ctx, tx, s, a, x, plan, true, true)
	if err != nil {
		return mcpplan.Applied{}, err
	}
	result := mcpplan.Applied{Created: []string{}, Updated: []string{}, TempIDs: ids}
	finalDeps := make(map[string][]string, len(changes))
	for _, request := range plan.Tasks {
		key := request.TaskID
		if request.TempID != "" {
			key = request.TempID
		}
		change := changes[key]
		ticket := change.ticket
		finalDeps[ticket.ID] = append([]string(nil), change.dependencies...)
		if change.create {
			number, err := tx.NextTicketNumber(ctx, a.Workspace.ID)
			if err != nil {
				return mcpplan.Applied{}, err
			}
			ticket.Number, ticket.Key = number, domain.TicketKey(number)
			ticket.Dependencies = []string{} // Dependencies are linked after every new row exists.
			ticket.Position = float64(number)
			if err := tx.InsertTicket(ctx, a.Workspace.ID, ticket); err != nil {
				return mcpplan.Applied{}, err
			}
			result.Created = append(result.Created, ticket.ID)
			if err := s.record(ctx, tx, a, ticket, domain.ActTicketCreated, ticket.Status.Label()); err != nil {
				return mcpplan.Applied{}, err
			}
		} else {
			// SaveTicket updates text/planning in one CAS. Preserve current links
			// until all newly proposed tickets exist, then replace dependencies.
			previous, err := tx.Ticket(ctx, a.Workspace.ID, plan.ProjectID, ticket.ID)
			if err != nil {
				return mcpplan.Applied{}, err
			}
			ticket.Dependencies = previous.Dependencies
			if _, err := tx.SaveTicket(ctx, a.Workspace.ID, ticket); err != nil {
				return mcpplan.Applied{}, err
			}
			result.Updated = append(result.Updated, ticket.ID)
		}
	}
	for id, dependencies := range finalDeps {
		if err := tx.SetTicketDependencies(ctx, a.Workspace.ID, id, dependencies); err != nil {
			return mcpplan.Applied{}, err
		}
	}
	return result, nil
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func timePtr(at time.Time) *time.Time { return &at }

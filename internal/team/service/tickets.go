package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"devboard/internal/planning"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// The ticket workflow:
//
//	backlog ⇄ available ──claim/assign──▶ in_progress ──submit──▶ review ──complete──▶ done
//	                 ▲                         │   ▲                 │
//	                 └──────── release ────────┘   └─ request changes┘        done ──reopen──▶ available
//
// (A ticket in review whose owner has left the project goes back to available when it is sent back.)
//
// Every move is checked against domain.TicketStatus.CanTransitionTo, so there is
// one table of what may follow what. Nothing here runs Git or touches a
// repository: the Git facts on a ticket (branch, commits, pull request) are
// reported by the developer's own Werkbord (ReportGit) and recorded as metadata.

// Board is everything a member's board view needs in one read.
type Board struct {
	Project  domain.Project  `json:"project"`
	Revision int64           `json:"revision"`
	Statuses []statusColumn  `json:"statuses"`
	Tickets  []domain.Ticket `json:"tickets"`
	Archived []domain.Ticket `json:"archived"`
	People   []Person        `json:"people"`
	// Role is the actor's effective role on this project, and Can what it allows.
	Role domain.ProjectRole         `json:"role"`
	Can  []domain.ProjectPermission `json:"can"`
	// Member is whether the actor is on the project (and so may hold tickets).
	Member     bool              `json:"member"`
	Completion map[string]string `json:"completion"`
	// Labels are the workspace's shared labels, which any ticket here may carry.
	Labels []domain.Label `json:"labels"`
}

type statusColumn struct {
	Status domain.TicketStatus `json:"status"`
	Label  string              `json:"label"`
}

func columns() []statusColumn {
	var out []statusColumn
	for _, st := range domain.TicketStatuses() {
		out = append(out, statusColumn{Status: st, Label: st.Label()})
	}
	return out
}

// Board returns a project's board.
func (s *Service) Board(ctx context.Context, a Actor, projectID string) (Board, error) {
	var b Board
	err := s.view(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		b = Board{Project: x.Project, Revision: x.Project.Revision, Statuses: columns(), Role: x.Role, Can: x.Role.Permissions(), Member: x.Member}
		if b.Tickets, err = tx.Tickets(ctx, a.Workspace.ID, projectID); err != nil {
			return err
		}
		active := make([]domain.Ticket, 0, len(b.Tickets))
		b.Archived = []domain.Ticket{}
		for _, k := range b.Tickets {
			if k.ArchivedAt != nil {
				b.Archived = append(b.Archived, k)
			} else {
				active = append(active, k)
			}
		}
		b.Tickets = active
		b.Completion = map[string]string{}
		for _, k := range b.Tickets {
			_, reason := canComplete(k, x.Role, a.Member.ID)
			b.Completion[k.ID] = reason
		}
		if b.Labels, err = tx.Labels(ctx, a.Workspace.ID); err != nil {
			return err
		}
		b.People, err = people(ctx, tx, a.Workspace.ID, projectID)
		return err
	})
	return b, err
}

// GetTicket returns a ticket with its commits.
func (s *Service) GetTicket(ctx context.Context, a Actor, projectID, ticketID string) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.view(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		k, err = tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID)
		return err
	})
	return k, err
}

// TicketInput is what creating a ticket takes.
type TicketInput struct {
	Title        string
	Description  string
	Requirements string
	// Status is backlog (the default) or available.
	Status domain.TicketStatus
	// WorkMode is who is expected to do it; empty means agent work. LabelIDs, Plan and Dependencies are optional.
	WorkMode     planning.ExecutionMode
	LabelIDs     []string
	Plan         planning.Range
	Dependencies []string
}

// CreateTicket adds a ticket to a project's board.
func (s *Service) CreateTicket(ctx context.Context, a Actor, projectID string, in TicketInput) (domain.Ticket, error) {
	title, err := domain.CleanTicketTitle(in.Title)
	if err != nil {
		return domain.Ticket{}, err
	}
	desc, err := domain.CleanTicketText("description", in.Description)
	if err != nil {
		return domain.Ticket{}, err
	}
	req, err := domain.CleanTicketText("requirements", in.Requirements)
	if err != nil {
		return domain.Ticket{}, err
	}
	switch in.Status {
	case "":
		in.Status = domain.TicketBacklog
	case domain.TicketBacklog, domain.TicketAvailable:
	default:
		return domain.Ticket{}, fmt.Errorf("%w: a new ticket starts in the backlog or as available, not %q", domain.ErrInvalid, in.Status)
	}
	var k domain.Ticket
	err = s.mutate(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsCreate, "create tickets"); err != nil {
			return err
		}
		if x.Project.Archived {
			return fmt.Errorf("%w: the project is archived", domain.ErrConflict)
		}
		n, err := tx.NextTicketNumber(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		now := s.stamp()
		k = domain.Ticket{ID: domain.NewID(domain.PrefixTicket), ProjectID: projectID, Number: n, Key: domain.TicketKey(n), Title: title,
			Description: desc, Requirements: req, Status: in.Status, CreatorID: a.Member.ID, Version: 1, CreatedAt: now, UpdatedAt: now, Commits: []domain.Commit{},
			LabelIDs: []string{}, Dependencies: []string{}}
		// Unset is agent work, as every ticket was before modes existed, whatever the project: choosing who does it is
		// the creator's to say (the console starts a project with no repository on "Human").
		mode := in.WorkMode
		if mode == "" {
			mode = planning.ModeAgent
		}
		if err := s.applyPlanning(ctx, tx, a, x, &k, ticketPlanning{mode: &mode, plan: &in.Plan, labelIDs: &in.LabelIDs, dependencies: &in.Dependencies}); err != nil {
			return err
		}
		if err := tx.InsertTicket(ctx, a.Workspace.ID, k); err != nil {
			return err
		}
		return s.record(ctx, tx, a, k, domain.ActTicketCreated, in.Status.Label())
	})
	return k, err
}

// record appends to the project's activity.
func (s *Service) record(ctx context.Context, tx store.Tx, a Actor, k domain.Ticket, kind domain.ActivityKind, detail string) error {
	return tx.AddActivity(ctx, a.Workspace.ID, domain.Activity{ProjectID: k.ProjectID, TicketID: k.ID, TicketKey: k.Key, ActorID: a.Member.ID, Kind: kind, Detail: shorten(detail, 300), CreatedAt: s.stamp()})
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// TicketPatch is what editing a ticket may change; a nil field is left alone.
type TicketPatch struct {
	Title        *string
	Description  *string
	Requirements *string
	// WorkMode, LabelIDs, Plan and Dependencies are the planning side of the ticket. LabelIDs and Dependencies
	// replace the whole set.
	WorkMode     *planning.ExecutionMode
	LabelIDs     *[]string
	Plan         *planning.Range
	Dependencies *[]string
	// Version, if set, must equal the ticket's current version: the edit is refused
	// if someone else changed the ticket since the editor loaded it.
	Version *int64
}

// UpdateTicket edits a ticket's text. Its creator may edit it; so may anyone with tickets.edit.
func (s *Service) UpdateTicket(ctx context.Context, a Actor, projectID, ticketID string, patch TicketPatch) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if k, err = s.loadTicket(ctx, tx, a, x, ticketID); err != nil {
			return err
		}
		if k.CreatorID != a.Member.ID && !x.can(domain.PPTicketsEdit) {
			return forbidden("edit a ticket someone else created")
		}
		if patch.Version != nil && *patch.Version != k.Version {
			return fmt.Errorf("%w: the ticket was changed by someone else; reload it and try again", domain.ErrConflict)
		}
		if k.Status == domain.TicketDone {
			return fmt.Errorf("%w: a finished ticket cannot be edited; reopen it first", domain.ErrConflict)
		}
		if patch.Title != nil {
			if k.Title, err = domain.CleanTicketTitle(*patch.Title); err != nil {
				return err
			}
		}
		if patch.Description != nil {
			if k.Description, err = domain.CleanTicketText("description", *patch.Description); err != nil {
				return err
			}
		}
		if patch.Requirements != nil {
			if k.Requirements, err = domain.CleanTicketText("requirements", *patch.Requirements); err != nil {
				return err
			}
		}
		if err := s.applyPlanning(ctx, tx, a, x, &k, ticketPlanning{mode: patch.WorkMode, plan: patch.Plan, labelIDs: patch.LabelIDs, dependencies: patch.Dependencies}); err != nil {
			return err
		}
		k.UpdatedAt = s.stamp()
		k, err = s.save(ctx, tx, a, k)
		return err
	})
	return k, err
}

// loadTicket reads a ticket of the project the actor may see.
func (s *Service) loadTicket(ctx context.Context, tx store.Tx, a Actor, x access, ticketID string) (domain.Ticket, error) {
	if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
		return domain.Ticket{}, err
	}
	k, err := tx.Ticket(ctx, a.Workspace.ID, x.Project.ID, ticketID)
	if err == nil && k.ArchivedAt != nil {
		return k, fmt.Errorf("%w: restore this ticket before changing it", domain.ErrConflict)
	}
	return k, err
}

// save writes a ticket (version-checked) and keeps its commits unchanged.
func (s *Service) save(ctx context.Context, tx store.Tx, a Actor, k domain.Ticket) (domain.Ticket, error) {
	commits := k.Commits
	saved, err := tx.SaveTicket(ctx, a.Workspace.ID, k)
	saved.Commits = commits
	return saved, err
}

// transition moves k to next, refusing a move the workflow does not allow.
func transition(k *domain.Ticket, next domain.TicketStatus) error {
	if !k.Status.CanTransitionTo(next) {
		return fmt.Errorf("%w: %s is %s; a ticket cannot go from %s to %s", domain.ErrConflict, k.Key, k.Status.Label(), k.Status.Label(), next.Label())
	}
	k.Status = next
	return nil
}

// MoveTicket moves a ticket between the states that need no owner: promoting a
// backlog ticket to available, putting it back, and reopening a finished one.
// Claiming, releasing, submitting and completing have their own calls.
func (s *Service) MoveTicket(ctx context.Context, a Actor, projectID, ticketID string, to domain.TicketStatus) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if k, err = s.loadTicket(ctx, tx, a, x, ticketID); err != nil {
			return err
		}
		from := k.Status
		switch {
		case from == domain.TicketDone && to == domain.TicketAvailable:
			if err := x.require(domain.PPTicketsReopen, "reopen tickets"); err != nil {
				return err
			}
		case (from == domain.TicketBacklog && to == domain.TicketAvailable) || (from == domain.TicketAvailable && to == domain.TicketBacklog):
			if k.CreatorID != a.Member.ID && !x.can(domain.PPTicketsEdit) {
				return forbidden("move a ticket someone else created")
			}
		default:
			return fmt.Errorf("%w: %s cannot be moved from %s to %s directly; use claim, release, submit, request changes or complete", domain.ErrInvalid, k.Key, from.Label(), to.Label())
		}
		if err := transition(&k, to); err != nil {
			return err
		}
		now := s.stamp()
		k.UpdatedAt = now
		if from == domain.TicketDone {
			k.AssigneeID, k.ReviewerID, k.CompletedAt, k.SubmittedAt, k.ClaimedAt = "", "", nil, nil, nil
			k.PullRequest, k.Branch = nil, ""
			if err := tx.ReplaceCommits(ctx, k.ID, nil); err != nil {
				return err
			}
			k.Commits = []domain.Commit{}
		}
		if k, err = s.save(ctx, tx, a, k); err != nil {
			return err
		}
		kind := domain.ActTicketMoved
		if from == domain.TicketDone {
			kind = domain.ActTicketReopened
		}
		return s.record(ctx, tx, a, k, kind, from.Label()+" → "+to.Label())
	})
	return k, err
}

// ClaimTicket makes the actor the active owner of an available ticket and moves
// it to In Progress. It is atomic: when several members claim the same ticket at
// once, exactly one succeeds and the rest get a conflict naming who has it.
func (s *Service) ClaimTicket(ctx context.Context, a Actor, projectID, ticketID string) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsClaim, "claim tickets"); err != nil {
			return err
		}
		if !x.Member {
			return forbidden("claim tickets on a project you are not on")
		}
		cur, err := s.loadTicket(ctx, tx, a, x, ticketID)
		if err != nil {
			return err
		}
		if x.Project.Archived {
			return fmt.Errorf("%w: the project is archived", domain.ErrConflict)
		}
		ok, err := tx.ClaimTicket(ctx, a.Workspace.ID, projectID, ticketID, a.Member.ID, domain.BranchName(cur.Number, cur.Title), s.stamp())
		if err != nil {
			return err
		}
		if !ok {
			return s.claimConflict(ctx, tx, a, projectID, ticketID)
		}
		if k, err = tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID); err != nil {
			return err
		}
		return s.record(ctx, tx, a, k, domain.ActTicketClaimed, k.Branch)
	})
	return k, err
}

// claimConflict explains why a claim did not take: the ticket was claimed first
// by someone else, or it was never available.
func (s *Service) claimConflict(ctx context.Context, tx store.Tx, a Actor, projectID, ticketID string) error {
	cur, err := tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID)
	if err != nil {
		return err
	}
	switch {
	case cur.AssigneeID == a.Member.ID && cur.Status.Held():
		return fmt.Errorf("%w: you already hold %s", domain.ErrConflict, cur.Key)
	case cur.AssigneeID != "" && cur.Status.Held():
		return fmt.Errorf("%w: %s was already claimed by %s", domain.ErrConflict, cur.Key, s.nameOf(ctx, tx, a, cur.AssigneeID))
	}
	return fmt.Errorf("%w: %s is %s; only an available ticket can be claimed", domain.ErrConflict, cur.Key, cur.Status.Label())
}

// ReleaseTicket puts a ticket in progress back on the board as available. Its
// holder may release it; so may anyone with tickets.assign (the project owner).
// The branch name stays on the ticket, so whoever claims it next is pointed at the same work.
func (s *Service) ReleaseTicket(ctx context.Context, a Actor, projectID, ticketID string) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if k, err = s.loadTicket(ctx, tx, a, x, ticketID); err != nil {
			return err
		}
		if k.Status != domain.TicketInProgress {
			return fmt.Errorf("%w: %s is %s; only a ticket in progress can be released", domain.ErrConflict, k.Key, k.Status.Label())
		}
		if k.AssigneeID != a.Member.ID && !x.can(domain.PPTicketsAssign) {
			return forbidden("release a ticket someone else holds")
		}
		former := k.AssigneeID
		if err := transition(&k, domain.TicketAvailable); err != nil {
			return err
		}
		k.AssigneeID, k.UpdatedAt = "", s.stamp()
		if k, err = s.save(ctx, tx, a, k); err != nil {
			return err
		}
		detail := ""
		if former != a.Member.ID {
			detail = "taken from " + s.nameOf(ctx, tx, a, former)
		}
		return s.record(ctx, tx, a, k, domain.ActTicketReleased, detail)
	})
	return k, err
}

// AssignTicket gives a ticket to a member of the project, or hands it from one
// member to another. It needs tickets.assign. An unclaimed ticket moves to In
// Progress; one in progress keeps its branch and its history, and only its owner changes.
func (s *Service) AssignTicket(ctx context.Context, a Actor, projectID, ticketID, memberID string) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if err := x.require(domain.PPTicketsAssign, "assign tickets"); err != nil {
			return err
		}
		if k, err = s.loadTicket(ctx, tx, a, x, ticketID); err != nil {
			return err
		}
		role, on, err := tx.ProjectRole(ctx, a.Workspace.ID, projectID, memberID)
		if err != nil {
			return err
		}
		if !on {
			return fmt.Errorf("%w: that member is not on this project; add them first", domain.ErrInvalid)
		}
		if !role.Can(domain.PPTicketsClaim) {
			return forbidden("assign tickets to " + s.nameOf(ctx, tx, a, memberID))
		}
		prev := k.AssigneeID
		switch k.Status {
		case domain.TicketBacklog, domain.TicketAvailable:
			if err := transition(&k, domain.TicketInProgress); err != nil {
				return err
			}
			now := s.stamp()
			k.ClaimedAt = &now
			if k.Branch == "" {
				k.Branch = domain.BranchName(k.Number, k.Title)
			}
		case domain.TicketInProgress:
			if prev == memberID {
				return fmt.Errorf("%w: %s is already assigned to %s", domain.ErrConflict, k.Key, s.nameOf(ctx, tx, a, memberID))
			}
		default:
			return fmt.Errorf("%w: %s is %s; send it back or reopen it before reassigning it", domain.ErrConflict, k.Key, k.Status.Label())
		}
		k.AssigneeID, k.UpdatedAt = memberID, s.stamp()
		if k, err = s.save(ctx, tx, a, k); err != nil {
			return err
		}
		detail := "to " + s.nameOf(ctx, tx, a, memberID)
		if prev != "" {
			detail = "from " + s.nameOf(ctx, tx, a, prev) + " " + detail
		}
		return s.record(ctx, tx, a, k, domain.ActTicketReassigned, detail)
	})
	return k, err
}

// nameOf is a member's name, or "a former member".
func (s *Service) nameOf(ctx context.Context, tx store.Tx, a Actor, memberID string) string {
	if m, err := tx.Member(ctx, a.Workspace.ID, memberID); err == nil {
		return m.Name
	}
	return "a former member"
}

// SubmitInput is what submitting work for review takes.
type SubmitInput struct {
	// ReviewerID asks a particular person on the project to review it; empty asks the project.
	ReviewerID string
	// PullRequest, if set, is recorded as the ticket's pull request in the same step.
	PullRequest *domain.PullRequest
}

// SubmitTicket moves a ticket in progress to Review. Its holder submits it (or
// the project owner does); a pull request may come with it. Team does not check
// the pull request or merge anything: reviewing and merging happen in the
// project's own Git host.
func (s *Service) SubmitTicket(ctx context.Context, a Actor, projectID, ticketID string, in SubmitInput) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if k, err = s.loadTicket(ctx, tx, a, x, ticketID); err != nil {
			return err
		}
		if k.AssigneeID != a.Member.ID && !x.can(domain.PPTicketsAssign) {
			return forbidden("submit a ticket someone else holds")
		}
		if k.Status != domain.TicketInProgress {
			return fmt.Errorf("%w: %s is %s; only a ticket in progress can be submitted for review", domain.ErrConflict, k.Key, k.Status.Label())
		}
		if in.ReviewerID != "" {
			role, on, err := tx.ProjectRole(ctx, a.Workspace.ID, projectID, in.ReviewerID)
			if err != nil {
				return err
			}
			if !on || !role.Can(domain.PPTicketsReview) {
				return fmt.Errorf("%w: that person is not a reviewer on this project", domain.ErrInvalid)
			}
			k.ReviewerID = in.ReviewerID
		}
		now := s.stamp()
		madePR := false
		if in.PullRequest != nil {
			if madePR, err = s.applyPullRequest(&k, a, *in.PullRequest, now); err != nil {
				return err
			}
		}
		if err := transition(&k, domain.TicketReview); err != nil {
			return err
		}
		k.SubmittedAt, k.UpdatedAt = &now, now
		if k, err = s.save(ctx, tx, a, k); err != nil {
			return err
		}
		if madePR {
			if err := s.record(ctx, tx, a, k, domain.ActPullRequestMade, k.PullRequest.URL); err != nil {
				return err
			}
		}
		if err := s.record(ctx, tx, a, k, domain.ActWorkSubmitted, k.Branch); err != nil {
			return err
		}
		detail := ""
		if k.ReviewerID != "" {
			detail = "from " + s.nameOf(ctx, tx, a, k.ReviewerID)
		}
		return s.record(ctx, tx, a, k, domain.ActReviewRequested, detail)
	})
	return k, err
}

// RequestChanges sends a ticket in review back to its owner, In Progress. A
// reviewer does this; the note says what to change. If its owner has left the
// project the ticket goes back on the board as available instead.
func (s *Service) RequestChanges(ctx context.Context, a Actor, projectID, ticketID, note string) (domain.Ticket, error) {
	var k domain.Ticket
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 1000 {
		return k, fmt.Errorf("%w: the note is longer than 1000 characters", domain.ErrInvalid)
	}
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if err := x.require(domain.PPTicketsReview, "review tickets"); err != nil {
			return err
		}
		if k, err = s.loadTicket(ctx, tx, a, x, ticketID); err != nil {
			return err
		}
		if k.Status != domain.TicketReview {
			return fmt.Errorf("%w: %s is %s; only a ticket in review can be sent back", domain.ErrConflict, k.Key, k.Status.Label())
		}
		_, still, err := tx.ProjectRole(ctx, a.Workspace.ID, projectID, k.AssigneeID)
		if err != nil {
			return err
		}
		next := domain.TicketInProgress
		if !still {
			next = domain.TicketAvailable
			k.AssigneeID = ""
		}
		if err := transition(&k, next); err != nil {
			return err
		}
		k.SubmittedAt, k.ReviewerID, k.UpdatedAt = nil, "", s.stamp()
		if k, err = s.save(ctx, tx, a, k); err != nil {
			return err
		}
		return s.record(ctx, tx, a, k, domain.ActChangesRequested, note)
	})
	return k, err
}

// CompleteTicket marks a reviewed ticket Done. A reviewer or the project owner
// does it, after the work was merged in the project's Git host: Team never
// merges. A ticket whose pull request is still open is refused, and a reviewer
// cannot complete a ticket they worked on themselves (a project owner can, so
// that someone working alone can finish their own work).
func (s *Service) CompleteTicket(ctx context.Context, a Actor, projectID, ticketID string) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if err := x.require(domain.PPTicketsReview, "complete tickets"); err != nil {
			return err
		}
		if k, err = s.loadTicket(ctx, tx, a, x, ticketID); err != nil {
			return err
		}
		if k.Status != domain.TicketReview {
			return fmt.Errorf("%w: %s is %s; only a ticket in review can be completed", domain.ErrConflict, k.Key, k.Status.Label())
		}
		if k.AssigneeID == a.Member.ID && x.Role != domain.ProjectOwner {
			return forbidden("complete a ticket you worked on yourself; ask another reviewer")
		}
		if k.PullRequest != nil && k.PullRequest.State == domain.PROpen {
			return fmt.Errorf("%w: %s's pull request is still open; merge or close it, report that, then complete the ticket", domain.ErrConflict, k.Key)
		}
		now := s.stamp()
		if err := transition(&k, domain.TicketDone); err != nil {
			return err
		}
		k.CompletedAt, k.UpdatedAt = &now, now
		if k, err = s.save(ctx, tx, a, k); err != nil {
			return err
		}
		return s.record(ctx, tx, a, k, domain.ActTicketCompleted, "")
	})
	return k, err
}

// ---- activity ----

// ActivityEntry is a history entry with the actor's name.
type ActivityEntry struct {
	domain.Activity
	ActorName string `json:"actorName"`
}

// MaxActivityPage is the most history one call returns.
const MaxActivityPage = 200

// ListActivity returns a project's history, newest first. before (an entry id)
// pages back; limit is capped at MaxActivityPage.
func (s *Service) ListActivity(ctx context.Context, a Actor, projectID string, before int64, limit int) ([]ActivityEntry, error) {
	if limit <= 0 || limit > MaxActivityPage {
		limit = 50
	}
	var out []ActivityEntry
	err := s.view(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPActivityView, "see this project's activity"); err != nil {
			return err
		}
		acts, err := tx.Activity(ctx, a.Workspace.ID, projectID, before, limit)
		if err != nil {
			return err
		}
		names := map[string]string{}
		out = make([]ActivityEntry, 0, len(acts))
		for _, act := range acts {
			name, ok := names[act.ActorID]
			if !ok {
				name = s.nameOf(ctx, tx, a, act.ActorID)
				names[act.ActorID] = name
			}
			out = append(out, ActivityEntry{Activity: act, ActorName: name})
		}
		return nil
	})
	return out, err
}

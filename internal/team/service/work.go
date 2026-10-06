package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// The cross-project views: the workspace overview, "My Work" and "Reviews". They
// answer the questions a member opens Team to ask:
//
//	what work is available?          Overview (per project) and the board
//	what am I working on?            MyWork
//	what is everyone else doing?     Overview.Working
//	what needs review?               Reviews
//	what is the repository like?     MyWork.Repositories, and the project's repository view
//
// They are read-only and built from what is already stored: tickets, the Git
// facts members' own Werkbords reported, and the people. Like the rest of Team
// they coordinate; nothing here runs, reads or reaches anything on a computer.

// ProjectRef names a project on an item without carrying the whole project.
type ProjectRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Repository string `json:"repository,omitempty"`
	Archived   bool   `json:"archived,omitempty"`
}

// Merge states of a ticket's pull request, as last reported.
const (
	MergeNone        = "none" // no pull request reported
	MergeMergeable   = "mergeable"
	MergeConflicting = "conflicting"
	MergeUnknown     = "unknown"
	MergeMerged      = "merged"
	MergeClosed      = "closed"
)

func mergeState(k domain.Ticket) string {
	pr := k.PullRequest
	switch {
	case pr == nil:
		return MergeNone
	case pr.State == domain.PRMerged:
		return MergeMerged
	case pr.State == domain.PRClosed:
		return MergeClosed
	case pr.Mergeable == domain.MergeConflicting:
		return MergeConflicting
	case pr.Mergeable == domain.MergeClean:
		return MergeMergeable
	}
	return MergeUnknown
}

// WorkItem is a ticket seen from outside its board: with its project, who holds it,
// where the work lives and how its pull request stands.
type WorkItem struct {
	Ticket  domain.Ticket `json:"ticket"`
	Project ProjectRef    `json:"project"`
	// Author is the person holding the ticket.
	Author   string `json:"author,omitempty"`
	Reviewer string `json:"reviewer,omitempty"` // who was asked to review it, if anyone in particular
	// AssignedBy is who gave the ticket to its holder, when they did not claim it themselves.
	AssignedBy string `json:"assignedBy,omitempty"`
	// Merge is the pull request's state: none, mergeable, conflicting, unknown, merged or closed.
	Merge string       `json:"merge"`
	Links domain.Links `json:"links"`
}

// Action is something that waits for the member, with the ticket it is about.
type Action struct {
	Kind      string `json:"kind"`  // changes_requested, conflict, behind, submit, review_requested, ready_to_complete, merged, closed, stale
	Level     string `json:"level"` // problem, warning or info
	TicketID  string `json:"ticketId"`
	TicketKey string `json:"ticketKey"`
	ProjectID string `json:"projectId"`
	Message   string `json:"message"`
}

// visibleWork is what every cross-project view starts from: the projects the actor
// may see, the actor's role on each, and the people.
type visibleWork struct {
	projects []domain.Project
	byID     map[string]domain.Project
	role     map[string]domain.ProjectRole // effective role; "" when the actor can see the project without being on it
	on       map[string]bool               // projects the actor is on
	names    map[string]string
	revision int64
}

func (s *Service) loadVisible(ctx context.Context, tx store.Tx, a Actor) (visibleWork, error) {
	v := visibleWork{byID: map[string]domain.Project{}, role: map[string]domain.ProjectRole{}, on: map[string]bool{}, names: map[string]string{}}
	only := a.Member.ID
	if a.Member.Can(domain.PermProjectsViewAll) {
		only = ""
	}
	var err error
	if v.projects, err = tx.Projects(ctx, a.Workspace.ID, only); err != nil {
		return v, err
	}
	roles, err := tx.ProjectRoles(ctx, a.Workspace.ID, a.Member.ID)
	if err != nil {
		return v, err
	}
	manage := a.Member.Can(domain.PermProjectsManage)
	for _, p := range v.projects {
		v.byID[p.ID] = p
		_, on := roles[p.ID]
		v.on[p.ID] = on
		v.role[p.ID] = roles[p.ID]
		if manage {
			v.role[p.ID] = domain.ProjectOwner
		}
	}
	members, err := tx.Members(ctx, a.Workspace.ID)
	if err != nil {
		return v, err
	}
	for _, m := range members {
		v.names[m.ID] = m.Name
	}
	v.revision, err = tx.WorkspaceRevision(ctx, a.Workspace.ID)
	return v, err
}

func (v visibleWork) who(id string) string {
	if id == "" {
		return ""
	}
	if n, ok := v.names[id]; ok {
		return n
	}
	return "a former member"
}

func (v visibleWork) ref(p domain.Project) ProjectRef {
	return ProjectRef{ID: p.ID, Name: p.Name, Repository: p.Repository, Archived: p.Archived}
}

func (v visibleWork) item(k domain.Ticket) WorkItem {
	p := v.byID[k.ProjectID]
	return WorkItem{Ticket: k, Project: v.ref(p), Author: v.who(k.AssigneeID), Reviewer: v.who(k.ReviewerID), Merge: mergeState(k), Links: domain.TicketLinks(p.Repository, k)}
}

// ---- My Work ----

// MyWork is the signed-in member's own work across every project they are on.
type MyWork struct {
	Revision    int64     `json:"revision"`
	GeneratedAt time.Time `json:"generatedAt"`
	// InProgress are the tickets the member holds and is working on, including the ones somebody assigned them.
	InProgress []WorkItem `json:"inProgress"`
	// Submitted are the member's tickets waiting for a review.
	Submitted []WorkItem `json:"submitted"`
	// PullRequests are the member's tickets that have a pull request still open.
	PullRequests []WorkItem `json:"pullRequests"`
	// NeedsAction is what waits for the member, worst first.
	NeedsAction []Action `json:"needsAction"`
	// ReviewsWaiting is how many tickets in review the member may review (see Reviews).
	ReviewsWaiting int `json:"reviewsWaiting"`
	// Repositories is the repository state that concerns the member's own branches.
	Repositories []MyRepository `json:"repositories"`
}

// MyRepository is what the repository view says about the member's own branches in one project.
type MyRepository struct {
	Project   ProjectRef   `json:"project"`
	Branches  []BranchInfo `json:"branches"`
	Attention []Attention  `json:"attention"`
}

var latestKinds = []domain.ActivityKind{domain.ActTicketClaimed, domain.ActTicketReassigned, domain.ActChangesRequested, domain.ActWorkSubmitted}

// MyWork returns what the member is working on, what waits for them, and the
// state of their branches.
func (s *Service) MyWork(ctx context.Context, a Actor) (MyWork, error) {
	out := MyWork{GeneratedAt: s.stamp(), InProgress: []WorkItem{}, Submitted: []WorkItem{}, PullRequests: []WorkItem{}, NeedsAction: []Action{}, Repositories: []MyRepository{}}
	err := s.db.View(ctx, func(tx store.Tx) error {
		v, err := s.loadVisible(ctx, tx, a)
		if err != nil {
			return err
		}
		out.Revision = v.revision
		held, err := tx.HeldTickets(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		var mine []domain.Ticket
		for _, k := range held {
			if _, ok := v.byID[k.ProjectID]; !ok {
				continue
			}
			if k.AssigneeID == a.Member.ID {
				mine = append(mine, k)
			} else if k.Status == domain.TicketReview && v.role[k.ProjectID].Can(domain.PPTicketsReview) {
				out.ReviewsWaiting++
			}
		}
		ids := make([]string, len(mine))
		for i, k := range mine {
			ids[i] = k.ID
		}
		last, err := tx.LatestTicketActivity(ctx, a.Workspace.ID, ids, latestKinds)
		if err != nil {
			return err
		}
		commits, err := tx.TicketCommits(ctx, ids)
		if err != nil {
			return err
		}
		for _, k := range mine {
			if c := commits[k.ID]; c != nil {
				k.Commits = c
			}
			it := v.item(k)
			l := last[k.ID]
			if l.Kind == domain.ActTicketReassigned && l.ActorID != a.Member.ID {
				it.AssignedBy = v.who(l.ActorID)
			}
			if k.Status == domain.TicketInProgress {
				out.InProgress = append(out.InProgress, it)
			} else {
				out.Submitted = append(out.Submitted, it)
			}
			if k.PullRequest != nil && k.PullRequest.State == domain.PROpen {
				out.PullRequests = append(out.PullRequests, it)
			}
			out.NeedsAction = append(out.NeedsAction, myActions(k, l, s.stamp())...)
		}
		// Tickets in review that are the member's to review, or that they were asked to review.
		for _, k := range held {
			p, ok := v.byID[k.ProjectID]
			if !ok || k.Status != domain.TicketReview || k.AssigneeID == a.Member.ID || !v.role[k.ProjectID].Can(domain.PPTicketsReview) {
				continue
			}
			switch {
			case k.PullRequest != nil && k.PullRequest.State == domain.PRMerged:
				out.NeedsAction = append(out.NeedsAction, Action{Kind: "ready_to_complete", Level: "info", TicketID: k.ID, TicketKey: k.Key, ProjectID: p.ID,
					Message: fmt.Sprintf("%s's pull request is merged; the ticket can be marked done.", k.Key)})
			case k.ReviewerID == a.Member.ID:
				out.NeedsAction = append(out.NeedsAction, Action{Kind: "review_requested", Level: "info", TicketID: k.ID, TicketKey: k.Key, ProjectID: p.ID,
					Message: fmt.Sprintf("%s asked you to review %s: %s.", v.who(k.AssigneeID), k.Key, k.Title)})
			}
		}
		rank := map[string]int{"problem": 0, "warning": 1, "info": 2}
		sort.SliceStable(out.NeedsAction, func(i, j int) bool { return rank[out.NeedsAction[i].Level] < rank[out.NeedsAction[j].Level] })

		// The repository state that concerns the member's own branches.
		perProject := map[string][]domain.Ticket{}
		for _, k := range mine {
			perProject[k.ProjectID] = append(perProject[k.ProjectID], k)
		}
		for _, p := range v.projects {
			if len(perProject[p.ID]) == 0 {
				continue
			}
			st, err := s.repoStateOf(ctx, tx, a, p, v.who)
			if err != nil {
				return err
			}
			own := map[string]bool{}
			ownBranch := map[string]bool{}
			for _, k := range perProject[p.ID] {
				own[k.Key] = true
				if k.Branch != "" {
					ownBranch[k.Branch] = true
				}
			}
			r := MyRepository{Project: v.ref(p), Branches: []BranchInfo{}, Attention: []Attention{}}
			for _, b := range st.Branches {
				if own[b.TicketKey] || ownBranch[b.Name] {
					r.Branches = append(r.Branches, b)
				}
			}
			for _, at := range st.Attention {
				if own[at.TicketKey] || (at.Branch != "" && ownBranch[at.Branch]) {
					r.Attention = append(r.Attention, at)
				}
			}
			out.Repositories = append(out.Repositories, r)
		}
		return nil
	})
	return out, err
}

// myActions is what a ticket the member holds asks of them. last is the newest of
// the ticket's claim, reassignment, changes-requested and submit entries.
func myActions(k domain.Ticket, last domain.Activity, now time.Time) []Action {
	act := func(kind, level, msg string) Action {
		return Action{Kind: kind, Level: level, TicketID: k.ID, TicketKey: k.Key, ProjectID: k.ProjectID, Message: msg}
	}
	var out []Action
	pr := k.PullRequest
	open := pr != nil && pr.State == domain.PROpen
	switch k.Status {
	case domain.TicketInProgress:
		changes := last.Kind == domain.ActChangesRequested
		if changes {
			msg := fmt.Sprintf("Changes were requested on %s", k.Key)
			if last.Detail != "" {
				msg += ": " + last.Detail
			} else {
				msg += "."
			}
			out = append(out, act("changes_requested", "warning", msg))
		}
		if open && pr.Mergeable == domain.MergeConflicting {
			out = append(out, act("conflict", "problem", fmt.Sprintf("%s's pull request has merge conflicts with %s. Resolve them in your own checkout.", k.Key, orBase(pr.BaseBranch))))
		}
		if open && pr.Behind > 0 {
			out = append(out, act("behind", "warning", fmt.Sprintf("%s's branch is %d commit%s behind %s.", k.Key, pr.Behind, plural(pr.Behind), orBase(pr.BaseBranch))))
		}
		if open && !pr.Draft && !changes && pr.Mergeable != domain.MergeConflicting {
			out = append(out, act("submit", "info", fmt.Sprintf("%s has an open pull request. Submit it for review when it is ready.", k.Key)))
		}
		if now.Sub(k.UpdatedAt) > StaleAfter {
			out = append(out, act("stale", "warning", fmt.Sprintf("%s has had no activity for %d days. Release it if you are not working on it.", k.Key, int(now.Sub(k.UpdatedAt)/(24*time.Hour)))))
		}
	case domain.TicketReview:
		switch {
		case open && pr.Mergeable == domain.MergeConflicting:
			out = append(out, act("conflict", "problem", fmt.Sprintf("%s's pull request has merge conflicts with %s. Resolve them so it can be merged.", k.Key, orBase(pr.BaseBranch))))
		case pr != nil && pr.State == domain.PRClosed:
			out = append(out, act("closed", "warning", fmt.Sprintf("%s's pull request was closed without merging, but the ticket is still in review.", k.Key)))
		case pr != nil && pr.State == domain.PRMerged:
			out = append(out, act("merged", "info", fmt.Sprintf("%s's pull request is merged. A reviewer can mark the ticket done.", k.Key)))
		}
	}
	return out
}

// ---- Reviews ----

// ReviewItem is a ticket in review, with what a reviewer needs to decide.
type ReviewItem struct {
	WorkItem
	// Mine: the viewer submitted it themselves (they cannot sign it off, unless they own the project).
	Mine bool `json:"mine"`
	// RequestedOfMe: the author asked the viewer in particular.
	RequestedOfMe bool `json:"requestedOfMe"`
	// CanReview: the viewer's role lets them send it back or mark it done.
	CanReview bool `json:"canReview"`
	// CanComplete: the viewer may mark it done now.
	CanComplete bool `json:"canComplete"`
	// Blocker says why it cannot be marked done yet, if so.
	Blocker string `json:"blocker,omitempty"`
}

// ReviewQueue is every ticket in review that the member can review or submitted.
type ReviewQueue struct {
	Revision    int64        `json:"revision"`
	GeneratedAt time.Time    `json:"generatedAt"`
	Items       []ReviewItem `json:"items"`
}

// Reviews returns the tickets waiting for a review across every project the
// member can see: the ones they may review (asked-of-them first, then the oldest
// submission first) and the ones they submitted and wait on.
func (s *Service) Reviews(ctx context.Context, a Actor) (ReviewQueue, error) {
	out := ReviewQueue{GeneratedAt: s.stamp(), Items: []ReviewItem{}}
	err := s.db.View(ctx, func(tx store.Tx) error {
		v, err := s.loadVisible(ctx, tx, a)
		if err != nil {
			return err
		}
		out.Revision = v.revision
		held, err := tx.HeldTickets(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		var inReview []domain.Ticket
		var ids []string
		for _, k := range held {
			if _, ok := v.byID[k.ProjectID]; ok && k.Status == domain.TicketReview {
				inReview = append(inReview, k)
				ids = append(ids, k.ID)
			}
		}
		commits, err := tx.TicketCommits(ctx, ids)
		if err != nil {
			return err
		}
		for _, k := range inReview {
			role := v.role[k.ProjectID]
			it := ReviewItem{Mine: k.AssigneeID == a.Member.ID, RequestedOfMe: k.ReviewerID == a.Member.ID, CanReview: role.Can(domain.PPTicketsReview)}
			if !it.Mine && !it.CanReview {
				continue
			}
			if c := commits[k.ID]; c != nil {
				k.Commits = c
			}
			it.WorkItem = v.item(k)
			it.CanComplete, it.Blocker = canComplete(k, role, a.Member.ID)
			out.Items = append(out.Items, it)
		}
		sort.SliceStable(out.Items, func(i, j int) bool {
			x, y := out.Items[i], out.Items[j]
			if (x.CanReview && !x.Mine) != (y.CanReview && !y.Mine) { // things to review before things I wait on
				return x.CanReview && !x.Mine
			}
			if x.RequestedOfMe != y.RequestedOfMe {
				return x.RequestedOfMe
			}
			return submittedAt(x.Ticket).Before(submittedAt(y.Ticket))
		})
		return nil
	})
	return out, err
}

func submittedAt(k domain.Ticket) time.Time {
	if k.SubmittedAt != nil {
		return *k.SubmittedAt
	}
	return k.UpdatedAt
}

// canComplete is the one place that says whether a member may mark a ticket in
// review Done right now, and if not why (CompleteTicket enforces the same rules).
func canComplete(k domain.Ticket, role domain.ProjectRole, me string) (bool, string) {
	switch {
	case !role.Can(domain.PPTicketsReview):
		return false, "only a reviewer or the project owner can mark it done"
	case k.AssigneeID == me && role != domain.ProjectOwner:
		return false, "you worked on it yourself; ask another reviewer"
	case k.PullRequest != nil && k.PullRequest.State == domain.PROpen:
		return false, "the pull request is still open: merge it on your Git host, then record the merge"
	}
	return true, ""
}

// ---- overview ----

// ProjectSummary is one project at a glance.
type ProjectSummary struct {
	Project  domain.Project     `json:"project"`
	Counts   map[string]int     `json:"counts"` // tickets by status
	People   int                `json:"people"`
	Role     domain.ProjectRole `json:"role,omitempty"`
	Member   bool               `json:"member"`
	Mine     int                `json:"mine"`     // tickets the viewer holds here
	ToReview int                `json:"toReview"` // tickets in review the viewer may review
	Problems int                `json:"problems"` // repository problems as last reported
	Warnings int                `json:"warnings"`
}

// Overview answers "what is available, and what is everyone working on?".
type Overview struct {
	Revision    int64            `json:"revision"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Workspace   domain.Workspace `json:"workspace"`
	Projects    []ProjectSummary `json:"projects"`
	// Working is every ticket someone is working on or that waits for review, most recently changed first.
	Working []WorkItem `json:"working"`
	// Available counts the tickets anyone on the project could claim right now.
	Available int `json:"available"`
}

// MaxWorking bounds Overview.Working.
const MaxWorking = 60

// Overview returns the workspace at a glance across the projects the member can see.
func (s *Service) Overview(ctx context.Context, a Actor) (Overview, error) {
	out := Overview{GeneratedAt: s.stamp(), Workspace: a.Workspace, Projects: []ProjectSummary{}, Working: []WorkItem{}}
	err := s.db.View(ctx, func(tx store.Tx) error {
		v, err := s.loadVisible(ctx, tx, a)
		if err != nil {
			return err
		}
		out.Revision = v.revision
		if ws, err := tx.Workspace(ctx, a.Workspace.ID); err == nil {
			out.Workspace = ws
		}
		counts, err := tx.TicketCounts(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		held, err := tx.HeldTickets(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		idx := map[string]int{}
		for _, p := range v.projects {
			ppl, err := tx.ProjectMembers(ctx, a.Workspace.ID, p.ID)
			if err != nil {
				return err
			}
			ps := ProjectSummary{Project: p, Counts: map[string]int{}, People: len(ppl), Role: v.role[p.ID], Member: v.on[p.ID]}
			for st, n := range counts[p.ID] {
				ps.Counts[string(st)] = n
			}
			for _, st := range domain.TicketStatuses() {
				if _, ok := ps.Counts[string(st)]; !ok {
					ps.Counts[string(st)] = 0
				}
			}
			out.Available += ps.Counts[string(domain.TicketAvailable)]
			if !p.Archived {
				tickets, err := tx.Tickets(ctx, a.Workspace.ID, p.ID)
				if err != nil {
					return err
				}
				branches, err := tx.Branches(ctx, a.Workspace.ID, p.ID)
				if err != nil {
					return err
				}
				st := buildRepoState(p, tickets, branches, v.who, s.stamp())
				for _, at := range st.Attention {
					switch at.Level {
					case "problem":
						ps.Problems++
					case "warning":
						ps.Warnings++
					}
				}
			}
			idx[p.ID] = len(out.Projects)
			out.Projects = append(out.Projects, ps)
		}
		for _, k := range held {
			p, ok := v.byID[k.ProjectID]
			if !ok {
				continue
			}
			if k.AssigneeID == a.Member.ID {
				out.Projects[idx[p.ID]].Mine++
			} else if k.Status == domain.TicketReview && v.role[p.ID].Can(domain.PPTicketsReview) {
				out.Projects[idx[p.ID]].ToReview++
			}
			if len(out.Working) < MaxWorking {
				out.Working = append(out.Working, v.item(k))
			}
		}
		return nil
	})
	return out, err
}

// repoStateOf assembles one project's repository state inside a transaction.
func (s *Service) repoStateOf(ctx context.Context, tx store.Tx, a Actor, p domain.Project, who func(string) string) (RepoState, error) {
	tickets, err := tx.Tickets(ctx, a.Workspace.ID, p.ID)
	if err != nil {
		return RepoState{}, err
	}
	branches, err := tx.Branches(ctx, a.Workspace.ID, p.ID)
	if err != nil {
		return RepoState{}, err
	}
	return buildRepoState(p, tickets, branches, who, s.stamp()), nil
}

// ---- keeping every client in step ----

// Event is one coordination event, as a client sees it arrive.
type Event struct {
	domain.Activity
	ActorName   string `json:"actorName"`
	ProjectName string `json:"projectName"`
}

// ProjectRevision is a project's current revision: a client that holds an older
// one for a project it shows reloads that project.
type ProjectRevision struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

// WorkspaceSync is the answer to "has anything I can see changed since revision N?".
//
// It is a hint with a cursor, never the data: what changed is read from the
// boards, My Work and Reviews, which are the authority. Events exist so a client
// can say "Bo claimed WB-4" without having to diff two boards.
type WorkspaceSync struct {
	Revision int64 `json:"revision"`
	// Changed: the revision is not the one the client asked after.
	Changed bool `json:"changed"`
	// Reset: the server's revision is lower than the client's (a restored backup,
	// say). Nothing the client holds can be trusted; it must reload everything.
	Reset bool `json:"reset,omitempty"`
	// Cursor is the newest event id to pass as "after" next time.
	Cursor int64 `json:"cursor"`
	// Events are the entries after the client's cursor in projects it can see, oldest first.
	Events []Event `json:"events"`
	// Truncated: there were more events than fit; the client missed some and should reload everything.
	Truncated bool              `json:"truncated,omitempty"`
	Projects  []ProjectRevision `json:"projects"`
}

// MaxSyncEvents bounds the events one WorkspaceSync call returns.
const MaxSyncEvents = 100

// WaitForWorkspaceChange returns as soon as the workspace's revision differs from
// since, or after wait (at most MaxSyncWait) with Changed false. after is the
// newest event id the client has seen; a negative after returns no events and just
// the current cursor, which is how a client starts (or restarts after losing its
// place). A client that was offline simply asks again with the revision and cursor
// it last had: whatever it missed shows up as a different revision, and a long gap
// as Truncated, so it can always catch up by reloading the authoritative views.
func (s *Service) WaitForWorkspaceChange(ctx context.Context, a Actor, since, after int64, wait time.Duration) (WorkspaceSync, error) {
	if wait > MaxSyncWait {
		wait = MaxSyncWait
	}
	release, err := s.hub.acquire(a.Member.ID)
	if err != nil {
		return WorkspaceSync{}, err
	}
	defer release()
	// Subscribe before reading, so a change between the read and the wait is not lost.
	ch, cancel := s.hub.subscribe(workspaceKey(a.Workspace.ID))
	defer cancel()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		res, err := s.workspaceSyncNow(ctx, a, since, after)
		if err != nil {
			return WorkspaceSync{}, err
		}
		if res.Changed || wait <= 0 {
			return res, nil
		}
		select {
		case <-ch:
		case <-timer.C:
			return res, nil
		case <-ctx.Done():
			return res, ctx.Err()
		}
	}
}

func (s *Service) workspaceSyncNow(ctx context.Context, a Actor, since, after int64) (WorkspaceSync, error) {
	res := WorkspaceSync{Events: []Event{}, Projects: []ProjectRevision{}}
	err := s.db.View(ctx, func(tx store.Tx) error {
		v, err := s.loadVisible(ctx, tx, a)
		if err != nil {
			return err
		}
		res.Revision, res.Changed, res.Reset = v.revision, v.revision != since, since > v.revision
		for _, p := range v.projects {
			res.Projects = append(res.Projects, ProjectRevision{ID: p.ID, Revision: p.Revision})
		}
		if res.Cursor, err = tx.LatestActivityID(ctx, a.Workspace.ID); err != nil {
			return err
		}
		if after < 0 || !res.Changed {
			return nil
		}
		var only []string // nil: every project
		if !a.Member.Can(domain.PermProjectsViewAll) {
			only = make([]string, 0, len(v.projects))
			for _, p := range v.projects {
				only = append(only, p.ID)
			}
		}
		acts, err := tx.ActivityAfter(ctx, a.Workspace.ID, after, only, MaxSyncEvents)
		if err != nil {
			return err
		}
		if len(acts) > MaxSyncEvents {
			acts, res.Truncated = acts[:MaxSyncEvents], true
		}
		for _, act := range acts {
			res.Events = append(res.Events, Event{Activity: act, ActorName: v.who(act.ActorID), ProjectName: v.byID[act.ProjectID].Name})
		}
		return nil
	})
	return res, err
}

func workspaceKey(workspaceID string) string { return "ws:" + workspaceID }

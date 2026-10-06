package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// StaleAfter is how long a branch or a held ticket can go without any reported
// activity before the project is told it may be stale.
const StaleAfter = 14 * 24 * time.Hour

// RepoState is what Team can say about a project's repository: assembled from
// what the members' Werkbords reported and from the board. It is awareness, not
// authority. Team cannot see the repository, so every line here is "as last
// reported by <someone>", and nothing in it merges, rebases or resolves anything.
type RepoState struct {
	ProjectID      string            `json:"projectId"`
	Repository     string            `json:"repository,omitempty"`
	Revision       int64             `json:"revision"`
	GeneratedAt    time.Time         `json:"generatedAt"`
	StaleAfterDays int               `json:"staleAfterDays"`
	Branches       []BranchInfo      `json:"branches"`
	PullRequests   []PullRequestInfo `json:"pullRequests"`
	Attention      []Attention       `json:"attention"`
}

// BranchInfo is one branch: reported by a member, linked to a ticket, or both.
type BranchInfo struct {
	Name         string              `json:"name"`
	HeadSHA      string              `json:"headSha,omitempty"`
	BaseBranch   string              `json:"baseBranch,omitempty"`
	Ahead        int                 `json:"ahead"`
	Behind       int                 `json:"behind"` // -1: not known
	LastActivity time.Time           `json:"lastActivity"`
	ReportedBy   string              `json:"reportedBy,omitempty"`
	TicketID     string              `json:"ticketId,omitempty"`
	TicketKey    string              `json:"ticketKey,omitempty"`
	TicketStatus domain.TicketStatus `json:"ticketStatus,omitempty"`
	Owner        string              `json:"owner,omitempty"` // who holds the ticket
	ChangedFiles int                 `json:"changedFiles"`
	// Base is a branch others are based on (main): never "stale" and never an orphan.
	Base bool `json:"base"`
	// Active is a branch someone is working on now.
	Active bool `json:"active"`
	Stale  bool `json:"stale"`
	// Orphan is a branch no ticket is linked to.
	Orphan bool `json:"orphan"`
}

// PullRequestInfo is a ticket's pull request.
type PullRequestInfo struct {
	TicketID    string              `json:"ticketId"`
	TicketKey   string              `json:"ticketKey"`
	Title       string              `json:"title"`
	Status      domain.TicketStatus `json:"ticketStatus"`
	Branch      string              `json:"branch"`
	Owner       string              `json:"owner,omitempty"`
	PullRequest domain.PullRequest  `json:"pullRequest"`
}

// Attention is something the project should look at.
type Attention struct {
	Level     string `json:"level"` // problem, warning or info
	Kind      string `json:"kind"`  // conflict, behind, stale, overlap, merged, closed, leftover, orphan
	Message   string `json:"message"`
	TicketKey string `json:"ticketKey,omitempty"`
	Branch    string `json:"branch,omitempty"`
}

// RepositoryState assembles the project's repository view.
func (s *Service) RepositoryState(ctx context.Context, a Actor, projectID string) (RepoState, error) {
	var st RepoState
	now := s.stamp()
	err := s.view(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPRepositoryView, "see the repository state"); err != nil {
			return err
		}
		tickets, err := tx.Tickets(ctx, a.Workspace.ID, projectID)
		if err != nil {
			return err
		}
		branches, err := tx.Branches(ctx, a.Workspace.ID, projectID)
		if err != nil {
			return err
		}
		ppl, err := people(ctx, tx, a.Workspace.ID, projectID)
		if err != nil {
			return err
		}
		names := map[string]string{}
		for _, p := range ppl {
			names[p.ID] = p.Name
		}
		who := func(id string) string {
			if id == "" {
				return ""
			}
			if n, ok := names[id]; ok {
				return n
			}
			return "a former member"
		}
		st = buildRepoState(x.Project, tickets, branches, who, now)
		return nil
	})
	return st, err
}

func buildRepoState(p domain.Project, tickets []domain.Ticket, branches []domain.ProjectBranch, who func(string) string, now time.Time) RepoState {
	st := RepoState{ProjectID: p.ID, Repository: p.Repository, Revision: p.Revision, GeneratedAt: now, StaleAfterDays: int(StaleAfter / (24 * time.Hour)),
		Branches: []BranchInfo{}, PullRequests: []PullRequestInfo{}, Attention: []Attention{}}

	byBranch := map[string]*domain.Ticket{}
	byNumber := map[int]*domain.Ticket{}
	for i := range tickets {
		k := &tickets[i]
		byNumber[k.Number] = k
		if k.Branch != "" {
			byBranch[k.Branch] = k
		}
	}
	bases := map[string]bool{}
	for _, b := range branches {
		if b.BaseBranch != "" {
			bases[b.BaseBranch] = true
		}
	}
	for _, k := range tickets {
		if k.PullRequest != nil && k.PullRequest.BaseBranch != "" {
			bases[k.PullRequest.BaseBranch] = true
		}
	}

	files := map[string]map[string]bool{}
	seen := map[string]bool{}
	add := func(info BranchInfo, k *domain.Ticket) {
		if k != nil {
			info.TicketID, info.TicketKey, info.TicketStatus, info.Owner = k.ID, k.Key, k.Status, who(k.AssigneeID)
		}
		info.Base = bases[info.Name]
		info.Orphan = k == nil && !info.Base
		idle := now.Sub(info.LastActivity)
		info.Stale = !info.Base && idle > StaleAfter && (k == nil || k.Status != domain.TicketDone)
		info.Active = !info.Base && !info.Stale && (k == nil || k.Status.Held())
		if k != nil && k.ArchivedAt != nil {
			info.Active, info.Stale = false, false
		}
		st.Branches = append(st.Branches, info)
	}
	for _, b := range branches {
		seen[b.Name] = true
		k := byBranch[b.Name]
		if k == nil {
			if n := domain.TicketNumberFromBranch(b.Name); n != 0 {
				k = byNumber[n]
			}
		}
		last := b.LastCommitAt
		if last.IsZero() || last.UnixMilli() == 0 {
			last = b.ReportedAt
		}
		add(BranchInfo{Name: b.Name, HeadSHA: b.HeadSHA, BaseBranch: b.BaseBranch, Ahead: b.Ahead, Behind: b.Behind, LastActivity: last,
			ReportedBy: who(b.ReportedBy), ChangedFiles: len(b.Files)}, k)
		if len(b.Files) > 0 {
			set := make(map[string]bool, len(b.Files))
			for _, f := range b.Files {
				set[f] = true
			}
			files[b.Name] = set
		}
	}
	for i := range tickets {
		k := &tickets[i]
		if k.Branch == "" || seen[k.Branch] || k.Status == domain.TicketDone || k.Status == domain.TicketBacklog || k.Status == domain.TicketAvailable {
			continue
		}
		info := BranchInfo{Name: k.Branch, Behind: -1, LastActivity: k.UpdatedAt}
		if pr := k.PullRequest; pr != nil {
			info.BaseBranch, info.Ahead, info.Behind = pr.BaseBranch, pr.Ahead, pr.Behind
		}
		add(info, k)
	}
	sort.Slice(st.Branches, func(i, j int) bool {
		a, b := st.Branches[i], st.Branches[j]
		if a.Base != b.Base {
			return a.Base
		}
		if a.Active != b.Active {
			return a.Active
		}
		return a.Name < b.Name
	})

	for _, k := range tickets {
		if k.PullRequest == nil {
			continue
		}
		st.PullRequests = append(st.PullRequests, PullRequestInfo{TicketID: k.ID, TicketKey: k.Key, Title: k.Title, Status: k.Status, Branch: k.Branch, Owner: who(k.AssigneeID), PullRequest: *k.PullRequest})
	}
	sort.SliceStable(st.PullRequests, func(i, j int) bool {
		oi, oj := st.PullRequests[i].PullRequest.State == domain.PROpen, st.PullRequests[j].PullRequest.State == domain.PROpen
		return oi && !oj
	})

	st.Attention = attention(tickets, st.Branches, st.PullRequests, files, who, now)
	return st
}

func attention(tickets []domain.Ticket, branches []BranchInfo, prs []PullRequestInfo, files map[string]map[string]bool, who func(string) string, now time.Time) []Attention {
	// Closed work remains in repository history, but no longer asks the team
	// for review, synchronization or overlap decisions.
	archived := map[string]bool{}
	activeTickets := make([]domain.Ticket, 0, len(tickets))
	for _, k := range tickets {
		if k.ArchivedAt != nil {
			archived[k.ID] = true
		} else {
			activeTickets = append(activeTickets, k)
		}
	}
	activeBranches := make([]BranchInfo, 0, len(branches))
	for _, b := range branches {
		if !archived[b.TicketID] {
			activeBranches = append(activeBranches, b)
		}
	}
	activePRs := make([]PullRequestInfo, 0, len(prs))
	for _, pr := range prs {
		if !archived[pr.TicketID] {
			activePRs = append(activePRs, pr)
		}
	}
	tickets, branches, prs = activeTickets, activeBranches, activePRs
	var out []Attention
	label := func(key, branch string) string {
		if key != "" {
			return key
		}
		return branch
	}
	for _, pr := range prs {
		switch p := pr.PullRequest; {
		case p.State == domain.PROpen && p.Mergeable == domain.MergeConflicting:
			out = append(out, Attention{Level: "problem", Kind: "conflict", TicketKey: pr.TicketKey, Branch: pr.Branch,
				Message: fmt.Sprintf("%s's pull request has merge conflicts with %s. %s needs to resolve them in their own checkout.", pr.TicketKey, orBase(p.BaseBranch), orSomeone(pr.Owner))})
		case p.State == domain.PRMerged && pr.Status != domain.TicketDone:
			out = append(out, Attention{Level: "info", Kind: "merged", TicketKey: pr.TicketKey, Branch: pr.Branch,
				Message: fmt.Sprintf("%s's pull request is merged; the ticket can be marked done.", pr.TicketKey)})
		case p.State == domain.PRClosed && pr.Status == domain.TicketReview:
			out = append(out, Attention{Level: "warning", Kind: "closed", TicketKey: pr.TicketKey, Branch: pr.Branch,
				Message: fmt.Sprintf("%s's pull request was closed without merging, but the ticket is still in review.", pr.TicketKey)})
		}
	}
	for _, b := range branches {
		if b.Base {
			continue
		}
		pr := (*domain.PullRequest)(nil)
		for i := range prs {
			if prs[i].Branch == b.Name {
				pr = &prs[i].PullRequest
			}
		}
		if b.Behind > 0 && (b.TicketStatus.Held() || b.TicketID == "") && (pr == nil || pr.State == domain.PROpen) {
			out = append(out, Attention{Level: "warning", Kind: "behind", TicketKey: b.TicketKey, Branch: b.Name,
				Message: fmt.Sprintf("%s is %d commit%s behind %s%s.", label(b.TicketKey, b.Name), b.Behind, plural(b.Behind), orBase(b.BaseBranch), ownerSuffix(b.Owner, b.ReportedBy))})
		}
		switch {
		case b.Stale && b.TicketStatus != domain.TicketDone:
			out = append(out, Attention{Level: "warning", Kind: "stale", TicketKey: b.TicketKey, Branch: b.Name,
				Message: fmt.Sprintf("%s has had no activity for %d days%s; it may be stale.", label(b.TicketKey, b.Name), int(now.Sub(b.LastActivity)/(24*time.Hour)), ownerSuffix(b.Owner, b.ReportedBy))})
		case b.TicketStatus == domain.TicketDone:
			out = append(out, Attention{Level: "info", Kind: "leftover", TicketKey: b.TicketKey, Branch: b.Name,
				Message: fmt.Sprintf("Branch %s belongs to the finished ticket %s; it can probably be deleted.", b.Name, b.TicketKey)})
		case b.Orphan && b.Active:
			out = append(out, Attention{Level: "info", Kind: "orphan", Branch: b.Name,
				Message: fmt.Sprintf("Branch %s is not linked to any ticket%s.", b.Name, ownerSuffix("", b.ReportedBy))})
		}
	}
	// A held ticket nobody has touched for a long time, with no branch to show for it.
	reported := map[string]bool{}
	for _, b := range branches {
		reported[b.TicketID] = true
	}
	for _, k := range tickets {
		if k.Status == domain.TicketInProgress && !reported[k.ID] && now.Sub(k.UpdatedAt) > StaleAfter {
			out = append(out, Attention{Level: "warning", Kind: "stale", TicketKey: k.Key,
				Message: fmt.Sprintf("%s has been in progress with %s for %d days with no reported activity.", k.Key, orSomeone(who(k.AssigneeID)), int(now.Sub(k.UpdatedAt)/(24*time.Hour)))})
		}
	}
	// Two live branches that change the same files are likely to conflict later.
	var active []BranchInfo
	for _, b := range branches {
		if b.Active && len(files[b.Name]) > 0 {
			active = append(active, b)
		}
	}
	for i := range active {
		for j := i + 1; j < len(active); j++ {
			a, b := active[i], active[j]
			if a.TicketID != "" && a.TicketID == b.TicketID {
				continue
			}
			var shared []string
			for f := range files[a.Name] {
				if files[b.Name][f] {
					shared = append(shared, f)
				}
			}
			if len(shared) == 0 {
				continue
			}
			sort.Strings(shared)
			more := ""
			if len(shared) > 5 {
				more, shared = fmt.Sprintf(" and %d more", len(shared)-5), shared[:5]
			}
			out = append(out, Attention{Level: "warning", Kind: "overlap", TicketKey: a.TicketKey, Branch: a.Name,
				Message: fmt.Sprintf("%s and %s both change %s%s. Whoever merges second may have conflicts to resolve.", describe(a), describe(b), strings.Join(shared, ", "), more)})
		}
	}
	rank := map[string]int{"problem": 0, "warning": 1, "info": 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Level] < rank[out[j].Level] })
	if out == nil {
		out = []Attention{}
	}
	return out
}

func describe(b BranchInfo) string {
	s := b.Name
	if b.TicketKey != "" {
		s = b.TicketKey + " (" + b.Name + ")"
	}
	if who := firstNonEmpty(b.Owner, b.ReportedBy); who != "" {
		s += " by " + who
	}
	return s
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func ownerSuffix(owner, reporter string) string {
	if w := firstNonEmpty(owner, reporter); w != "" {
		return " (" + w + ")"
	}
	return ""
}

func orBase(b string) string {
	if b == "" {
		return "its base branch"
	}
	return b
}

func orSomeone(n string) string {
	if n == "" {
		return "The owner"
	}
	return n
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

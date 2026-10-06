package service

import (
	"context"
	"fmt"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// The Git facts on a ticket are reported, not discovered. Team holds no Git or
// GitHub credential and never opens a repository, so it cannot look: the
// developer's own Werkbord, which has their credentials, tells Team the branch,
// the commits and the pull request, and Team records and shows them.

// BranchState is what a developer's Werkbord knows about a branch's position.
type BranchState struct {
	HeadSHA      string
	BaseBranch   string
	Ahead        *int
	Behind       *int // how far behind the base branch; nil: not known
	LastCommitAt time.Time
	// Files are the repository-relative paths changed on the branch. They let Team
	// warn when two active branches touch the same files; contents are never sent.
	Files []string
}

// GitReport is one report about a ticket's Git work. Empty or nil parts are left as they are.
type GitReport struct {
	Branch      string
	Commits     *[]domain.Commit
	PullRequest *domain.PullRequest
	State       *BranchState
}

// ReportGit records the branch, commits and pull request of a ticket in progress
// or in review. The ticket's holder may report; so may a reviewer or the project
// owner (to record, for instance, that the pull request was merged).
func (s *Service) ReportGit(ctx context.Context, a Actor, projectID, ticketID string, in GitReport) (domain.Ticket, error) {
	var k domain.Ticket
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		if err := x.require(domain.PPGitReport, "report Git work"); err != nil {
			return err
		}
		if k, err = s.loadTicket(ctx, tx, a, x, ticketID); err != nil {
			return err
		}
		if k.AssigneeID != a.Member.ID && !x.can(domain.PPGitReportAny) {
			return forbidden("report Git work for a ticket someone else holds")
		}
		if !k.Status.Held() {
			return fmt.Errorf("%w: %s is %s; Git work is reported for tickets in progress or in review", domain.ErrConflict, k.Key, k.Status.Label())
		}
		now := s.stamp()
		if in.Branch != "" {
			b, err := domain.CleanBranch(in.Branch)
			if err != nil {
				return err
			}
			if b != k.Branch {
				others, err := tx.Tickets(ctx, a.Workspace.ID, projectID)
				if err != nil {
					return err
				}
				for _, o := range others {
					if o.ID != k.ID && o.Branch == b {
						return fmt.Errorf("%w: branch %q already belongs to %s", domain.ErrConflict, b, o.Key)
					}
				}
				k.Branch = b
			}
		}
		var madePR, merged bool
		if in.PullRequest != nil {
			wasMerged := k.PullRequest != nil && k.PullRequest.State == domain.PRMerged
			if madePR, err = s.applyPullRequest(&k, a, *in.PullRequest, now); err != nil {
				return err
			}
			merged = k.PullRequest.State == domain.PRMerged && !wasMerged
		}
		if in.Commits != nil {
			if len(*in.Commits) > domain.MaxCommitsPerTick {
				return fmt.Errorf("%w: more than %d commits reported for one ticket", domain.ErrInvalid, domain.MaxCommitsPerTick)
			}
			clean := make([]domain.Commit, 0, len(*in.Commits))
			for _, c := range *in.Commits {
				cc, err := domain.CleanCommit(c)
				if err != nil {
					return err
				}
				clean = append(clean, cc)
			}
			if err := tx.ReplaceCommits(ctx, k.ID, clean); err != nil {
				return err
			}
			k.Commits = clean
		}
		k.UpdatedAt = now
		if k, err = s.save(ctx, tx, a, k); err != nil {
			return err
		}
		if k.Branch != "" && (in.State != nil || in.PullRequest != nil) {
			b, err := branchRecord(projectID, k.Branch, a.Member.ID, now, in.State, k.PullRequest)
			if err != nil {
				return err
			}
			if err := tx.UpsertBranch(ctx, a.Workspace.ID, b); err != nil {
				return err
			}
		}
		if madePR {
			if err := s.record(ctx, tx, a, k, domain.ActPullRequestMade, k.PullRequest.URL); err != nil {
				return err
			}
		}
		if merged {
			return s.record(ctx, tx, a, k, domain.ActPullRequestMerged, k.PullRequest.URL)
		}
		return nil
	})
	return k, err
}

// branchRecord builds the project-level record of a ticket's branch from what was reported.
func branchRecord(projectID, name, reporter string, now time.Time, st *BranchState, pr *domain.PullRequest) (domain.ProjectBranch, error) {
	b := domain.ProjectBranch{ProjectID: projectID, Name: name, Behind: -1, ReportedBy: reporter, ReportedAt: now}
	if pr != nil {
		b.BaseBranch, b.Ahead, b.Behind = pr.BaseBranch, pr.Ahead, pr.Behind
	}
	if st == nil {
		return b, nil
	}
	if st.HeadSHA != "" {
		sha, err := domain.CleanSHA(st.HeadSHA)
		if err != nil {
			return b, err
		}
		b.HeadSHA = sha
	}
	if st.BaseBranch != "" {
		base, err := domain.CleanBranch(st.BaseBranch)
		if err != nil {
			return b, err
		}
		b.BaseBranch = base
	}
	if st.Ahead != nil {
		b.Ahead = *st.Ahead
	}
	if st.Behind != nil {
		b.Behind = *st.Behind
	}
	if b.Ahead < 0 || b.Ahead > 1_000_000 || b.Behind < -1 || b.Behind > 1_000_000 {
		return b, fmt.Errorf("%w: ahead/behind counts are out of range", domain.ErrInvalid)
	}
	b.LastCommitAt = st.LastCommitAt.UTC().Truncate(time.Millisecond)
	files, err := domain.CleanFiles(st.Files)
	if err != nil {
		return b, err
	}
	b.Files = files
	return b, nil
}

// applyPullRequest validates a reported pull request and sets it on the ticket.
// It reports whether this is a pull request the ticket did not have before.
func (s *Service) applyPullRequest(k *domain.Ticket, a Actor, in domain.PullRequest, now time.Time) (created bool, err error) {
	if old := k.PullRequest; old != nil && in.Fields != nil && (!in.Fields["url"] || old.URL == in.URL) {
		if !in.Fields["url"] {
			in.URL = old.URL
		}
		if !in.Fields["number"] {
			in.Number = old.Number
		}
		if !in.Fields["state"] {
			in.State = old.State
		}
		if !in.Fields["draft"] {
			in.Draft = old.Draft
		}
		if !in.Fields["mergeable"] {
			in.Mergeable = old.Mergeable
		}
		if !in.Fields["baseBranch"] {
			in.BaseBranch = old.BaseBranch
		}
		if !in.Fields["behind"] {
			in.Behind = old.Behind
		}
		if !in.Fields["ahead"] {
			in.Ahead = old.Ahead
		}
	}
	in.Fields = nil
	pr, err := domain.CleanPullRequest(in)
	if err != nil {
		return false, err
	}
	created = k.PullRequest == nil || k.PullRequest.URL != pr.URL
	// A merge cannot be undone (a revert is a new pull request), so a report that
	// says otherwise about the same pull request is a stale or replayed one: a
	// slow Werkbord must not turn a merged pull request back into an open one.
	if old := k.PullRequest; old != nil && old.URL == pr.URL && old.State == domain.PRMerged && pr.State != domain.PRMerged {
		return false, fmt.Errorf("%w: %s's pull request is already recorded as merged; a merge cannot be undone", domain.ErrConflict, k.Key)
	}
	pr.ReportedBy, pr.ReportedAt = a.Member.ID, now
	pr.CreatedAt = now
	if !created {
		pr.CreatedAt = k.PullRequest.CreatedAt
	}
	k.PullRequest = &pr
	return created, nil
}

// BranchInput is one branch in a ReportBranches call.
type BranchInput struct {
	Name string
	BranchState
}

// MaxBranchesPerReport bounds one ReportBranches call.
const MaxBranchesPerReport = 200

// ReportBranches records the branches a member's Werkbord sees on the project's
// repository, and forgets the ones named in gone (deleted or merged). A branch
// reported again replaces its earlier report.
func (s *Service) ReportBranches(ctx context.Context, a Actor, projectID string, branches []BranchInput, gone []string) error {
	if len(branches)+len(gone) > MaxBranchesPerReport {
		return fmt.Errorf("%w: more than %d branches in one report", domain.ErrInvalid, MaxBranchesPerReport)
	}
	return s.mutate(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPGitReport, "report branches"); err != nil {
			return err
		}
		if !x.Member {
			return forbidden("report branches for a project you are not on")
		}
		now := s.stamp()
		for _, in := range branches {
			name, err := domain.CleanBranch(in.Name)
			if err != nil {
				return err
			}
			st := in.BranchState
			b, err := branchRecord(projectID, name, a.Member.ID, now, &st, nil)
			if err != nil {
				return err
			}
			if err := tx.UpsertBranch(ctx, a.Workspace.ID, b); err != nil {
				return err
			}
		}
		for _, name := range gone {
			name, err := domain.CleanBranch(name)
			if err != nil {
				return err
			}
			if _, err := tx.DeleteBranch(ctx, a.Workspace.ID, projectID, name); err != nil {
				return err
			}
		}
		return nil
	})
}

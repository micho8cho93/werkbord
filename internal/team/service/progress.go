package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// ReportProgress stores execution observations independently of board/review
// status. Replayed and older observations acknowledge the durable high-water mark.
func (s *Service) ReportProgress(ctx context.Context, a Actor, pid, tid string, in domain.Progress) (domain.ProgressAck, error) {
	out := domain.ProgressAck{}
	raw, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	err = s.mutate(ctx, a, pid, func(tx store.Tx, x access) error {
		if a.Device == nil {
			return forbidden("report progress without an enrolled device")
		}
		if in.Schema != integration.Schema || !in.Execution.Valid() || in.Sequence < 1 || in.Sequence > 1<<53 || !integration.Identifier(in.TaskID) || !integration.Identifier(in.ProjectID) || (in.Execution.RunID != "" && !integration.Identifier(in.Execution.RunID)) {
			return domain.ErrInvalid
		}
		if in.Execution.Branch != "" {
			if _, err := domain.CleanBranch(in.Execution.Branch); err != nil {
				return err
			}
		}
		if in.Execution.HeadCommit != "" {
			if _, err := domain.CleanSHA(in.Execution.HeadCommit); err != nil {
				return err
			}
		}

		dev, err := tx.Device(ctx, a.Workspace.ID, a.Device.ID)
		if err != nil || dev.Revoked() || dev.MemberID != a.Member.ID || dev.PublicKey != a.Device.PublicKey {
			return domain.ErrUnauthenticated
		}
		if !x.Member || x.Project.Archived {
			return forbidden("report progress outside an active project membership")
		}
		if err := x.require(domain.PPGitReport, "report progress"); err != nil {
			return err
		}
		k, err := s.loadTicket(ctx, tx, a, x, tid)
		if err != nil {
			return err
		}
		if k.AssigneeID != a.Member.ID || !k.Status.Held() || k.ClaimedAt == nil || k.Assignment != in.Assignment || !k.ClaimedAt.Equal(in.ClaimAt) {
			return fmt.Errorf("%w: ticket assignment changed", domain.ErrConflict)
		}
		old, err := tx.Progress(ctx, a.Workspace.ID, pid, tid, dev.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err == nil {
			if in.Sequence <= old.Sequence {
				if in.Sequence == old.Sequence && digest != old.Digest {
					return fmt.Errorf("%w: sequence reused with different content", domain.ErrConflict)
				}
				out.Sequence = old.Sequence
				return nil
			}
			if old.Assignment == in.Assignment && (old.TaskID != in.TaskID || old.ProjectID != in.ProjectID) {
				return fmt.Errorf("%w: task association changed", domain.ErrConflict)
			}
		}
		if in.Git != nil {
			g := in.Git
			commits := make([]domain.Commit, 0, len(g.Commits))
			for _, c := range g.Commits {
				commits = append(commits, domain.Commit{SHA: c.SHA, CommittedAt: c.CommittedAt})
			}
			rep := GitReport{Branch: g.Branch, Commits: &commits, State: &BranchState{HeadSHA: g.HeadSHA, BaseBranch: g.BaseBranch, Ahead: &g.Ahead, Behind: &g.Behind}}
			if g.PullRequest != nil {
				p := g.PullRequest
				rep.PullRequest = &domain.PullRequest{Number: p.Number, URL: p.URL, State: domain.PRState(p.State), Draft: p.Draft, BaseBranch: p.BaseBranch, Mergeable: domain.Mergeable(p.Mergeable), Ahead: g.Ahead, Behind: g.Behind}
			}
			if err := s.applyGitReport(ctx, tx, a, &k, rep); err != nil {
				return err
			}
		}
		r := domain.ProgressRecord{Progress: in, DeviceID: dev.ID, MemberID: a.Member.ID, ReportedAt: s.stamp(), Digest: digest}
		if err := tx.SaveProgress(ctx, a.Workspace.ID, pid, tid, r); err != nil {
			return err
		}
		out.Sequence, out.Applied = in.Sequence, true
		return nil
	})
	return out, err
}

func (s *Service) TicketProgress(ctx context.Context, a Actor, pid, tid string) ([]domain.ProgressRecord, error) {
	out := []domain.ProgressRecord{}
	err := s.view(ctx, a, pid, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "view progress"); err != nil {
			return err
		}
		k, err := tx.Ticket(ctx, a.Workspace.ID, pid, tid)
		if err != nil {
			return err
		}
		rs, err := tx.TicketProgress(ctx, a.Workspace.ID, pid, tid)
		if err != nil {
			return err
		}
		for _, r := range rs {
			d, err := tx.Device(ctx, a.Workspace.ID, r.DeviceID)
			if err != nil || d.Revoked() || d.MemberID != r.MemberID || x.Project.Archived {
				continue
			}
			member, err := tx.IsProjectMember(ctx, a.Workspace.ID, pid, r.MemberID)
			if err != nil {
				return err
			}
			if !member {
				continue
			}
			r.Stale = s.stamp().Sub(r.ReportedAt) > domain.DeviceOnlineWindow
			if k.Status.Held() && k.ArchivedAt == nil && k.ClaimedAt != nil && k.Assignment == r.Assignment && k.AssigneeID == r.MemberID && k.ClaimedAt.Equal(r.ClaimAt) {
				out = append(out, r)
			}
		}
		return nil
	})
	return out, err
}

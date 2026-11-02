package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"werkbord/internal/integration"
	"werkbord/internal/planning"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
	"werkbord/internal/workspace"
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
		if k.AssigneeID != a.Member.ID || !k.Claimed() || k.ClaimedAt == nil || k.Assignment != in.Assignment || !k.ClaimedAt.Equal(in.ClaimAt) {
			return fmt.Errorf("%w: ticket assignment changed", domain.ErrConflict)
		}
		old, err := tx.Progress(ctx, a.Workspace.ID, pid, tid, dev.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		seenBefore := err == nil
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
		if k.Status == domain.TicketAvailable && in.Execution.RunID != "" && in.Execution.StartedAt != nil && !in.Execution.StartedAt.Before(*k.ClaimedAt) {
			k.Status, k.UpdatedAt = domain.TicketInProgress, s.stamp()
			if k, err = s.save(ctx, tx, a, k); err != nil {
				return err
			}
			if err = s.record(ctx, tx, a, k, domain.ActTicketMoved, "Available → In Progress"); err != nil {
				return err
			}
		}
		// The agent finished its run for this claim: the work goes to Review, as a finished run does in the person's own
		// Werkbord. Only the first report of a run completing does it, so sending a ticket back for changes is not undone by
		// the same finished run being reported again, and only for work an agent does alone (a person finishes hybrid work).
		if k.Status == domain.TicketInProgress && finishedRun(in.Execution, k) && (!seenBefore || old.Execution.RunID != in.Execution.RunID || old.Execution.State != "completed") &&
			(k.WorkMode == planning.ModeAgent || k.WorkMode == "") {
			now := s.stamp()
			if err = transition(&k, domain.TicketReview); err != nil {
				return err
			}
			k.SubmittedAt, k.UpdatedAt = &now, now
			if k, err = s.save(ctx, tx, a, k); err != nil {
				return err
			}
			if err = s.record(ctx, tx, a, k, domain.ActWorkSubmitted, k.Branch); err != nil {
				return err
			}
			if err = s.record(ctx, tx, a, k, domain.ActReviewRequested, "the agent finished"); err != nil {
				return err
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

// finishedRun reports whether in says the agent run that began after k was claimed has completed.
func finishedRun(e integration.Execution, k domain.Ticket) bool {
	return e.State == "completed" && e.RunID != "" && e.StartedAt != nil && k.ClaimedAt != nil && !e.StartedAt.Before(*k.ClaimedAt)
}

// TicketProgress is what the owner's own Werkbords report about the work on a ticket. It is the owner's alone: what an agent
// is doing, when and where is nobody else's business, so anyone else gets none. What the team may know is only whether the
// owner's agent is active, which Board says.
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
		if k.AssigneeID != a.Member.ID {
			return nil
		}
		rs, err := tx.TicketProgress(ctx, a.Workspace.ID, pid, tid)
		if err != nil {
			return err
		}
		out, err = s.currentProgress(ctx, tx, a, x, k, rs)
		return err
	})
	return out, err
}

// currentProgress keeps the reports that belong to the ticket's present claim, from a device that is still the owner's, and
// marks the ones that are overdue.
func (s *Service) currentProgress(ctx context.Context, tx store.Tx, a Actor, x access, k domain.Ticket, rs []domain.ProgressRecord) ([]domain.ProgressRecord, error) {
	out := []domain.ProgressRecord{}
	for _, r := range rs {
		d, err := tx.Device(ctx, a.Workspace.ID, r.DeviceID)
		if err != nil || d.Revoked() || d.MemberID != r.MemberID || x.Project.Archived {
			continue
		}
		member, err := tx.IsProjectMember(ctx, a.Workspace.ID, k.ProjectID, r.MemberID)
		if err != nil {
			return nil, err
		}
		if !member {
			continue
		}
		r.Stale = s.stamp().Sub(r.ReportedAt) > domain.DeviceOnlineWindow
		if k.Claimed() && k.ArchivedAt == nil && k.ClaimedAt != nil && k.Assignment == r.Assignment && k.AssigneeID == r.MemberID && k.ClaimedAt.Equal(r.ClaimAt) {
			out = append(out, r)
		}
	}
	return out, nil
}

// ExecutionOf puts a reported execution state in the shell's words. It is the one translation, so a summary and a board never
// disagree about what a state means. A state it does not know is no state.
func ExecutionOf(state string) workspace.Execution {
	switch state {
	case "queued", "pending":
		return workspace.ExecQueued
	case "starting":
		return workspace.ExecStarting
	case "running":
		return workspace.ExecRunning
	case "needs_input", "waiting_for_user":
		// A report intentionally carries no question text or waiting kind.
		return workspace.ExecWaiting
	case "blocked":
		return workspace.ExecBlocked
	case "completed", "succeeded":
		return workspace.ExecCompleted
	case "failed":
		return workspace.ExecFailed
	case "stopped", "canceled":
		return workspace.ExecCanceled
	}
	return workspace.ExecNone
}

// peerExecutions says, for each of a project's in-progress tickets that has an owner with an agent run reported, which agent it is
// and what state it is in. It is all anyone else on the project may see of someone's agent: the agent's id (which the owner may
// withhold), a state, when the server heard it, and whether that is current. No run, branch, path, prompt, question or usage.
//
// A report counts only if it belongs to the ticket's present claim, from a device that is still the owner's, from someone who is
// still on the project. The newest such report wins, so a switch of agent replaces the old one, and an old replay cannot bring it
// back (the host keeps one row per device and only a higher sequence replaces it). Fresh is the server's own word, on its own clock:
// the report is no older than DeviceOnlineWindow and says the owner's runner was online. An overdue or runner-offline report is
// not fresh, and nothing here ever turns it into "done" or "offline".
//
// One read per project, not one per ticket; device and membership checks are remembered across the tickets.
func (s *Service) peerExecutions(ctx context.Context, tx store.Tx, a Actor, p domain.Project, tickets []domain.Ticket) (map[string]workspace.AgentRef, error) {
	out := map[string]workspace.AgentRef{}
	if p.Archived {
		return out, nil
	}
	want := false
	for _, k := range tickets {
		if k.Status == domain.TicketInProgress && k.AssigneeID != "" && k.ArchivedAt == nil {
			want = true
			break
		}
	}
	if !want {
		return out, nil
	}
	all, err := tx.ProjectProgress(ctx, a.Workspace.ID, p.ID)
	if err != nil {
		return nil, err
	}
	now := s.stamp()
	devices, members := map[string]bool{}, map[string]bool{}
	for _, k := range tickets {
		if k.Status != domain.TicketInProgress || k.AssigneeID == "" || k.ArchivedAt != nil || !k.Claimed() || k.ClaimedAt == nil {
			continue
		}
		var best *domain.ProgressRecord
		for i := range all[k.ID] {
			r := &all[k.ID][i]
			if r.Assignment != k.Assignment || r.MemberID != k.AssigneeID || !k.ClaimedAt.Equal(r.ClaimAt) || r.Execution.RunID == "" || ExecutionOf(r.Execution.State) == workspace.ExecNone {
				continue
			}
			ok, seen := devices[r.DeviceID]
			if !seen {
				d, err := tx.Device(ctx, a.Workspace.ID, r.DeviceID)
				ok = err == nil && !d.Revoked() && d.MemberID == r.MemberID
				devices[r.DeviceID] = ok
			}
			if !ok {
				continue
			}
			member, seen := members[r.MemberID]
			if !seen {
				var err error
				if member, err = tx.IsProjectMember(ctx, a.Workspace.ID, p.ID, r.MemberID); err != nil {
					return nil, err
				}
				members[r.MemberID] = member
			}
			if !member {
				continue
			}
			if best == nil || r.ReportedAt.After(best.ReportedAt) || r.ReportedAt.Equal(best.ReportedAt) && r.Sequence > best.Sequence {
				best = r
			}
		}
		if best != nil {
			out[k.ID] = workspace.AgentRef{ID: best.Execution.AgentID, State: ExecutionOf(best.Execution.State), ObservedAt: best.ReportedAt,
				Fresh: now.Sub(best.ReportedAt) <= domain.DeviceOnlineWindow && best.Execution.RunnerOnline}
		}
	}
	return out, nil
}

// activeFrom is the older yes-or-no, derived from the same projection so the two cannot disagree: the owner's agent has work in
// hand (queued, starting, running or waiting for them) and the report is fresh.
func activeFrom(agents map[string]workspace.AgentRef) map[string]bool {
	out := map[string]bool{}
	for id, r := range agents {
		switch r.State {
		case workspace.ExecQueued, workspace.ExecStarting, workspace.ExecRunning, workspace.ExecWaiting:
			out[id] = r.Fresh
		}
	}
	for id, on := range out {
		if !on {
			delete(out, id)
		}
	}
	return out
}

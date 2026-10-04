package runner

import (
	"context"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/runnerwire"
	"devboard/internal/service"
)

func (m *Manager) startRemote(ctx context.Context, in StartInput, task *domain.Task, project *service.ProjectDetail, selected *domain.Runner, resolved domain.Resolved) (*domain.Run, error) {
	if in.Resume {
		return nil, fmt.Errorf("%w: remote retries start a fresh session with an explicit handoff", domain.ErrInvalid)
	}
	repo := project.Repository
	if repo == nil {
		return nil, domain.ErrInvalid
	}
	remote := ""
	for _, r := range repo.Remotes {
		if r.Name == "origin" {
			remote = r.URL
		}
	}
	if remote != "" && !runnerwire.SafeRemote(remote) {
		return nil, fmt.Errorf("%w: remote URL contains credentials or an unsupported transport", domain.ErrInvalid)
	}
	target := repo.DefaultBranch
	if target == "" {
		target = repo.CurrentBranch
	}
	if target == "" {
		return nil, fmt.Errorf("%w: choose a target branch before remote execution", domain.ErrInvalid)
	}
	prior, e := m.opt.Runs.ListByTask(ctx, task.ID)
	if e != nil {
		return nil, e
	}
	previous, previousCommit := "", ""
	published := false
	if last := latestWorkspaceRun(prior); last != nil {
		previous = last.Branch
		previousCommit = last.HeadCommit
		published = !last.Remote || last.RunnerID != selected.ID
		if last.Remote && last.RunnerID != selected.ID && previous != "" && (last.Uncommitted == nil || *last.Uncommitted || last.HeadCommit == "") {
			return nil, fmt.Errorf("%w: prior runner work is uncommitted or unknown; reconcile it on its owning runner before changing machines", domain.ErrConflict)
		}
		if last.WorktreeID != "" {
			wt, e := m.opt.Worktrees.Get(ctx, last.WorktreeID)
			if e != nil {
				return nil, e
			}
			if state, e := (&gitrepo.CLI{}).Status(ctx, wt.Path); e != nil || state.Counts.Staged+state.Counts.Unstaged+state.Counts.Untracked+state.Counts.Conflicted > 0 {
				return nil, fmt.Errorf("%w: commit the previous local worktree before changing machines", domain.ErrConflict)
			}
			previous = wt.Branch
			previousCommit, e = (&gitrepo.CLI{}).HeadCommit(ctx, wt.Path)
			if e != nil {
				return nil, e
			}
		}
	}
	if in.ParentRunID != "" {
		if m.opt.Handoffs == nil {
			return nil, domain.ErrInvalid
		}
		parent, e := m.opt.Runs.Get(ctx, in.ParentRunID)
		if e != nil {
			return nil, e
		}
		if parent.TaskID != task.ID || !parent.State.Terminal() {
			return nil, domain.ErrInvalid
		}
		handoff, e := m.opt.Handoffs.Context(ctx, parent.ID, in.Purpose, in.SelectedContext)
		if e != nil {
			return nil, e
		}
		in.Instructions += "\n\n" + handoff
	}
	manual := in.ScheduleKey == ""
	if manual && task.Orchestration.Enabled && task.Orchestration.RunID == "" && !task.Orchestration.Missed && task.Orchestration.Error == "" {
		in.ScheduleKey = task.Orchestration.Key
	}
	job := &runnerwire.Job{RemoteURL: remote, TargetBranch: target, ExpectedCommit: task.Orchestration.TargetCommit, AllowClone: selected.AllowClone, PreviousBranch: previous, PreviousCommit: previousCommit, PreviousPublished: published}
	return m.opt.Runs.Create(ctx, service.NewRun{TaskID: task.ID, AgentID: resolved.Agent, Prompt: buildPrompt(task, in.Instructions, false), Policy: resolved.Policy(), Model: resolved.Model, Reasoning: resolved.Reasoning, ParentRunID: in.ParentRunID, Purpose: in.Purpose, ScheduleKey: in.ScheduleKey, EnforceGates: m.opt.Scheduler != nil, Manual: manual, RunnerID: selected.ID, Remote: true, Claim: m.opt.Distributed.Claim(selected, job)})
}

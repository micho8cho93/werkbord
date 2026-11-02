package runner

import (
	"context"
	"errors"

	"werkbord/internal/domain"
	"werkbord/internal/service"
)

// fallbackProject continues runs stopped by a provider limit with another agent where the project has opted in. Everything it needs to decide is
// in the service; this only starts the continuation. It runs from the scheduler tick, so a browser is never involved.
func (m *Manager) fallbackProject(ctx context.Context, projectID string) error {
	if m.opt.Scheduler == nil || m.opt.Handoffs == nil {
		return nil
	}
	usable := func(id string) bool { _, err := m.opt.Agents.Available(ctx, id); return err == nil }
	choices, err := m.opt.Scheduler.FallbackCandidates(ctx, projectID, usable)
	if err != nil {
		return err
	}
	for _, c := range choices {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := m.fallbackOne(ctx, projectID, c.Run, c.Agent); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) fallbackOne(ctx context.Context, projectID string, parent domain.Run, agentID string) error {
	s := m.opt.Scheduler
	choice := service.FallbackChoice{Run: parent, Agent: agentID}
	if err := s.ClaimFallback(ctx, projectID, choice); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil // already considered, off, not allowed, or the day's allowance is spent
		}
		return err
	}
	decline := func(why string) error {
		m.log().Warn("automatic fallback declined", "run", parent.ID, "why", why)
		return s.ResolveFallback(ctx, projectID, parent.ID, domain.FallbackDeclined, why, "")
	}
	// A rate limit usually leaves the session open and idle, not ended. The person opted in to this and the grace period has
	// passed, so it is ended here: Stop keeps the worktree and records a handoff, which a continuation needs from a finished run.
	if !parent.State.Terminal() {
		if _, err := m.Stop(ctx, parent.ID); err != nil {
			if now, e := m.opt.Runs.Get(ctx, parent.ID); e != nil || !now.State.Terminal() {
				return decline("the session stuck on the provider limit could not be stopped: " + err.Error())
			}
		}
	}
	captured, err := m.opt.Handoffs.Generate(ctx, parent.ID)
	if err != nil {
		return decline("the handoff could not be captured: " + err.Error())
	}
	if why := domain.HandoffUnverified(captured.Handoff); why != "" {
		return decline("not continued automatically: " + why)
	}
	run, err := m.Start(ctx, StartInput{
		TaskID: parent.TaskID, AgentID: agentID, ParentRunID: parent.ID,
		Purpose: "continue", Origin: domain.FallbackOrigin(parent.ID), Policy: &domain.ExecutionPolicy{Interaction: parent.Policy.Interaction},
	})
	switch {
	case err == nil:
		return s.ResolveFallback(ctx, projectID, parent.ID, domain.FallbackStarted, "", run.ID)
	case errors.Is(err, domain.ErrConflict):
		// A gate that may clear (capacity, an operation in progress): try again on a later tick.
		return s.ResolveFallback(ctx, projectID, parent.ID, domain.FallbackRetry, err.Error(), "")
	default:
		return decline("could not start " + agentID + ": " + err.Error())
	}
}

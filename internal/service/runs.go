package service

import (
	"context"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// Runs exposes run and question state. Starting runs is not implemented yet;
// see docs/ARCHITECTURE.md, "Deferred".
type Runs struct {
	Deps
}

// interruptedReason is recorded on runs that were in flight when the
// controller stopped.
const interruptedReason = "interrupted: controller restarted"

// RecoverAfterRestart reconciles persisted runs with reality at startup.
//
// Agent processes are children of the controller, so a run that was starting
// or running when the controller stopped no longer has a process. Those runs
// are marked failed. Runs waiting for the user are left alone: their question
// is still answerable, and an adapter can continue the conversation from the
// stored SessionRef once execution exists.
func (s *Runs) RecoverAfterRestart(ctx context.Context) (int, error) {
	recovered := 0
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		active, err := tx.Runs().ListActive(ctx)
		if err != nil {
			return err
		}
		now := s.now()
		for i := range active {
			r := &active[i]
			if r.State == domain.RunWaitingForUser {
				continue
			}
			from := r.State
			if err := r.Transition(domain.RunFailed, interruptedReason, now); err != nil {
				return err
			}
			if err := tx.Runs().Update(ctx, r); err != nil {
				return err
			}
			ev := newEvent(domain.EventRunStateChanged, map[string]any{"run": r, "from": from})
			ev.ProjectID, ev.TaskID, ev.RunID = r.ProjectID, r.TaskID, r.ID
			if err := em.emit(ev); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	if recovered > 0 {
		s.log().Warn("marked interrupted runs as failed", "count", recovered)
	}
	return recovered, err
}

// ListActive returns runs that have not reached a terminal state.
func (s *Runs) ListActive(ctx context.Context) ([]domain.Run, error) {
	var out []domain.Run
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Runs().ListActive(ctx)
		return err
	})
	return out, err
}

// ListPendingQuestions returns questions still waiting for the user.
func (s *Runs) ListPendingQuestions(ctx context.Context) ([]domain.Question, error) {
	var out []domain.Question
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Questions().ListPending(ctx)
		return err
	})
	return out, err
}

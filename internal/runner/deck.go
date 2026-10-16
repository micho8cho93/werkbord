package runner

import (
	"context"

	"devboard/internal/domain"
	"devboard/internal/service"
)

// StartDeckWorkflow serialises retries before preparing a task or starting a run.
// Replaying a completed invocation returns its original run, never reruns it.
func (m *Manager) StartDeckWorkflow(ctx context.Context, settings *service.Settings, owner, slotID, requestID string) (*domain.Task, *domain.Run, error) {
	unlock, err := m.lock(ctx, "deck:"+service.DeckOwnerKey(owner))
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	task, err := settings.PrepareDeckTask(context.WithoutCancel(ctx), m.opt.Tasks, owner, slotID, requestID)
	if err != nil {
		return nil, nil, err
	}
	runs, err := m.opt.Runs.ListByTask(ctx, task.ID)
	if err != nil {
		return task, nil, err
	}
	if len(runs) > 0 {
		return task, &runs[0], nil
	}
	run, err := m.Start(ctx, StartInput{TaskID: task.ID})
	return task, run, err
}

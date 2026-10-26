package runner

import (
	"context"

	"devboard/internal/domain"
	"devboard/internal/service"
)

// Every target uses the normal durable task/run machinery. Retries skip runs
// already created, including terminal runs, and resume any partially started batch.
func (m *Manager) StartDeckWorkflows(ctx context.Context, settings *service.Settings, owner, slotID, requestID string, input service.DeckStart) ([]*domain.Task, []*domain.Run, error) {
	unlock, err := m.lock(ctx, "deck:"+service.DeckOwnerKey(owner))
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	ctx = context.WithoutCancel(ctx)
	tasks, err := settings.PrepareDeckTasks(ctx, m.opt.Tasks, owner, slotID, requestID, input)
	if err != nil {
		return nil, nil, err
	}
	runs := []*domain.Run{}
	for _, task := range tasks {
		existing, e := m.opt.Runs.ListByTask(ctx, task.ID)
		if e != nil {
			return tasks, runs, e
		}
		if len(existing) > 0 {
			runs = append(runs, &existing[0])
			continue
		}
		run, e := m.Start(ctx, StartInput{TaskID: task.ID})
		if e != nil {
			return tasks, runs, e
		}
		runs = append(runs, run)
	}
	return tasks, runs, nil
}

package runner

import (
	"context"
	"errors"
	"time"

	"devboard/internal/domain"
)

// ScheduleOnce executes a snapshot's runnable tasks. Start rechecks every gate
// and claims the schedule transactionally with the Run. No browser is involved.
func (m *Manager) ScheduleOnce(ctx context.Context) error {
	if m.opt.Scheduler == nil {
		return nil
	}
	projects, err := m.opt.Projects.List(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		plan, err := m.opt.Scheduler.Plan(ctx, p.ID)
		if err != nil {
			return err
		}
		for _, d := range plan {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			t, err := m.opt.Tasks.Get(ctx, d.TaskID)
			if err != nil {
				return err
			}
			o := t.Orchestration
			if !o.Enabled || o.Key == "" || o.RunID != "" || o.Missed || o.Error != "" {
				continue
			}
			if d.State == "blocked" && d.Reason == "Schedule missed its allowed start window" {
				if err := m.opt.Scheduler.RecordDispatchError(ctx, t.ID, o.Key, d.Reason, true); err != nil {
					return err
				}
				continue
			}
			if d.State != "runnable" {
				continue
			}
			_, err = m.Start(ctx, StartInput{TaskID: t.ID, ScheduleKey: o.Key})
			if err != nil && !errors.Is(err, domain.ErrConflict) {
				// Pre-launch failures are durable and require an explicit rearm; no retry storm.
				if e := m.opt.Scheduler.RecordDispatchError(ctx, t.ID, o.Key, err.Error(), false); e != nil {
					return e
				}
				m.log().Warn("scheduled task could not start", "task", t.ID, "err", err)
			}
		}
	}
	return nil
}

func (m *Manager) ScheduleLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := m.ScheduleOnce(ctx); err != nil && ctx.Err() == nil {
			m.log().Warn("scheduler tick failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

package runner

import (
	"context"
	"errors"
	"time"
	"werkbord/internal/domain"
	"werkbord/internal/integration"
	"werkbord/internal/store"
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
	if m.opt.Distributed != nil {
		if err := m.opt.Distributed.ReleaseRevoked(ctx); err != nil {
			return err
		}
	}
	m.mu.Lock()
	start := m.scheduleCursor
	if len(projects) > 0 {
		m.scheduleCursor = (start + 1) % len(projects)
	}
	m.mu.Unlock()
	for i := range projects {
		p := projects[(start+i)%len(projects)]
		if !p.HasRepository() {
			continue // no agent works on a project with no repository
		}
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
			if !o.Enabled || o.Key == "" || o.RunID != "" || o.Missed || o.Error != "" && o.RetryAt == nil {
				continue
			}
			if d.State == "waiting_runner" {
				if err := m.opt.Scheduler.RecordWaitingRunner(ctx, t.ID, o.Key); err != nil {
					return err
				}
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
			if err != nil {
				// A changed schedule or newly occupied gate is retried from a fresh
				// snapshot. It is not a permanent launch failure.
				if errors.Is(err, domain.ErrConflict) {
					continue
				}
				// Only unclaimed temporary rate limits can retry, using durable bounded backoff.
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
	nextRetention := time.Time{}
	for {
		if ctx.Err() != nil {
			return
		}
		if time.Now().After(nextRetention) {
			n := 0
			err := m.opt.Runs.Store.Update(ctx, func(tx store.Tx) error { var e error; n, e = tx.Events().Prune(ctx, time.Now().UTC()); return e })
			if err != nil {
				m.log().Warn("event retention failed", "err", err)
			}
			nextRetention = time.Now().Add(24 * time.Hour)
			if n >= 10000 {
				nextRetention = time.Now().Add(time.Minute)
			}
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

// ScheduleAuthorized dispatches an externally coordinated, locally approved
// request through Start's scheduler, capacity, repository and runtime gates.
// No external context alters the existing Individual scheduling configuration.
func (m *Manager) ScheduleAuthorized(ctx context.Context, in integration.ExecutionDispatch) (*domain.Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.Start(ctx, StartInput{Authorization: &in})
}

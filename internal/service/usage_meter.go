package service

import (
	"context"
	"errors"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

// Usage meter (see domain/usage_meter.go): this reads what runs reported and the person's own budgets, and says which agents Saver keeps
// automatic work off. It writes only the person's settings. Nothing here reaches Team.

// usageRunsMax bounds one read. A week of runs is far below it.
const usageRunsMax = 5000

func (s *Scheduler) UsageSettings(ctx context.Context) (domain.UsageSettings, error) {
	var out domain.UsageSettings
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		out, e = readUsageSettings(ctx, tx)
		return e
	})
	return out, err
}

func readUsageSettings(ctx context.Context, tx store.Tx) (domain.UsageSettings, error) {
	var out domain.UsageSettings
	if e := tx.Settings().Get(ctx, domain.SettingUsage, &out); e != nil && !errors.Is(e, domain.ErrNotFound) {
		return out, e
	}
	out.Normalize()
	return out, nil
}

func (s *Scheduler) SetUsageSettings(ctx context.Context, in domain.UsageSettings) (domain.UsageSettings, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return in, err
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if e := tx.Settings().Set(ctx, domain.SettingUsage, in, s.now()); e != nil {
			return e
		}
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]any{"usageSettings": in}))
	})
	return in, err
}

// UsageMeter measures the last five hours and the last week against the person's budgets.
func (s *Scheduler) UsageMeter(ctx context.Context) (domain.UsageMeter, error) {
	var out domain.UsageMeter
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		out, e = s.meterIn(ctx, tx, s.now())
		return e
	})
	return out, err
}

func (s *Scheduler) meterIn(ctx context.Context, tx store.Tx, now time.Time) (domain.UsageMeter, error) {
	cfg, err := readUsageSettings(ctx, tx)
	if err != nil {
		return domain.UsageMeter{}, err
	}
	runs, err := tx.Analytics().Usage(ctx, now.Add(-domain.UsageWeek), usageRunsMax)
	if err != nil {
		return domain.UsageMeter{}, err
	}
	return domain.MeasureUsage(runs, cfg, now), nil
}

// usageHolds are the agents Saver keeps automatic work off now. In standard mode it reads one setting and nothing else.
func (s *Scheduler) usageHolds(ctx context.Context, tx store.Tx, now time.Time) (held map[string]string, allBudgetedHeld bool, err error) {
	cfg, err := readUsageSettings(ctx, tx)
	if err != nil || cfg.Mode != domain.UsageSaver {
		return nil, false, err
	}
	m, err := s.meterIn(ctx, tx, now)
	if err != nil {
		return nil, false, err
	}
	held, all := m.Holds()
	return held, all, nil
}

// UsageHeld says why automatic work should not start on this agent now, or "" when it may. Used by automatic fallback, which must not
// move work onto an agent that is itself near its budget.
func (s *Scheduler) UsageHeld(ctx context.Context, agentID string) string {
	var why string
	_ = s.Store.View(ctx, func(tx store.Tx) error {
		held, _, e := s.usageHolds(ctx, tx, s.now())
		why = held[agentID]
		return e
	})
	return why
}

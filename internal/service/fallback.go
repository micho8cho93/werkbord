package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

// Automatic provider fallback (see domain/fallback.go). This file decides and records; the runner starts the continuation.
// The failed run's ID is the idempotency key: a claim is written before anything starts, so a crash or a repeated poll can
// never start a second continuation of the same run. The price is that a crash between claim and start leaves the work for a
// person to continue by hand, which the ledger says.

const fallbackLedgerMax = 100

// FallbackChoice is a failed run and the agent that may take it over.
type FallbackChoice struct {
	Run   domain.Run
	Agent string
}

func (s *Scheduler) FallbackSettings(ctx context.Context, projectID string) (domain.FallbackSettings, error) {
	out := domain.FallbackSettings{Providers: []string{}, MaxSwitches: 1, DailyLimit: 3}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, e := tx.Projects().Get(ctx, projectID); e != nil {
			return e
		}
		e := tx.Settings().Get(ctx, domain.SettingFallbackPrefix+projectID, &out)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		return e
	})
	out.Normalize()
	return out, err
}

func (s *Scheduler) SetFallbackSettings(ctx context.Context, projectID string, in domain.FallbackSettings) (domain.FallbackSettings, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return in, err
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		p, e := tx.Projects().Get(ctx, projectID)
		if e != nil {
			return e
		}
		if p.Inactive() {
			return fmt.Errorf("%w: restore this project before changing it", domain.ErrConflict)
		}
		if e := tx.Settings().Set(ctx, domain.SettingFallbackPrefix+projectID, in, s.now()); e != nil {
			return e
		}
		ev := newEvent(domain.EventSettingsUpdated, map[string]any{"fallbackSettings": in})
		ev.ProjectID = projectID
		return em.emit(ev)
	})
	return in, err
}

func readLedger(ctx context.Context, tx store.Tx, projectID string) (domain.FallbackLedger, error) {
	l := domain.FallbackLedger{Entries: []domain.FallbackEntry{}}
	e := tx.Settings().Get(ctx, domain.SettingFallbackLedgerPrefix+projectID, &l)
	if errors.Is(e, domain.ErrNotFound) {
		e = nil
	}
	if l.Entries == nil {
		l.Entries = []domain.FallbackEntry{}
	}
	return l, e
}

func (s *Scheduler) FallbackLedger(ctx context.Context, projectID string) (domain.FallbackLedger, error) {
	var l domain.FallbackLedger
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, e := tx.Projects().Get(ctx, projectID); e != nil {
			return e
		}
		var e error
		l, e = readLedger(ctx, tx, projectID)
		return e
	})
	return l, err
}

// attempted reports whether a ledger entry stops another attempt for its run. Only a retry leaves it open.
func attempted(e domain.FallbackEntry) bool { return e.Result != domain.FallbackRetry }

// spentToday counts attempts in the last 24 hours that started or may have started a run.
func spentToday(l domain.FallbackLedger, now time.Time) int {
	n := 0
	for _, e := range l.Entries {
		if (e.Result == domain.FallbackClaimed || e.Result == domain.FallbackStarted) && now.Sub(e.At) < 24*time.Hour {
			n++
		}
	}
	return n
}

func ledgerEntry(l domain.FallbackLedger, runID string) (int, bool) {
	for i, e := range l.Entries {
		if e.RunID == runID {
			return i, true
		}
	}
	return -1, false
}

// FallbackCandidates are the failed runs of a project that automatic fallback may continue now, each with the agent that
// would take it. usable says whether an agent can be started now on this computer. Nothing is written.
func (s *Scheduler) FallbackCandidates(ctx context.Context, projectID string, usable func(agentID string) bool) ([]FallbackChoice, error) {
	var out []FallbackChoice
	err := s.Store.View(ctx, func(tx store.Tx) error {
		p, e := tx.Projects().Get(ctx, projectID)
		if e != nil {
			return e
		}
		cfg := domain.FallbackSettings{}
		if e := tx.Settings().Get(ctx, domain.SettingFallbackPrefix+projectID, &cfg); errors.Is(e, domain.ErrNotFound) {
			return nil
		} else if e != nil {
			return e
		}
		cfg.Normalize()
		if !cfg.Enabled || p.Inactive() || cfg.Validate() != nil {
			return nil
		}
		ledger, e := readLedger(ctx, tx, projectID)
		if e != nil {
			return e
		}
		if spentToday(ledger, s.now()) >= cfg.DailyLimit {
			return nil
		}
		tasks, e := tx.Tasks().ListByProject(ctx, projectID)
		if e != nil {
			return e
		}
		latest, e := tx.Runs().ListLatestByProject(ctx, projectID)
		if e != nil {
			return e
		}
		byTask := map[string]domain.Run{}
		for _, r := range latest {
			byTask[r.TaskID] = r
		}
		for _, t := range tasks {
			r, ok := byTask[t.ID]
			if !ok || s.now().Sub(r.UpdatedAt) > 24*time.Hour || t.ArchivedAt != nil || t.State == domain.TaskDone || domain.AgentRefusal(*p, t) != "" {
				continue
			}
			// A run that has ended on a provider limit, or a session left open and idle by one. A rate limit that ends a turn
			// leaves the process waiting for a message, so the usual case is blocked, not failed. That session is only a candidate
			// once it has sat for the grace period, which is the person's time to reply or wait the limit out.
			if r.Remote || r.Recovery != "" || r.Blocker == nil || !domain.FallbackKind(r.Blocker.ProviderKind) {
				continue
			}
			switch r.State {
			case domain.RunFailed:
			case domain.RunBlocked:
				if s.now().Sub(r.Blocker.RaisedAt) < domain.FallbackGrace {
					continue
				}
			default:
				continue
			}
			if i, seen := ledgerEntry(ledger, r.ID); seen && attempted(ledger.Entries[i]) {
				continue
			}
			history, e := tx.Runs().ListByTask(ctx, t.ID)
			if e != nil {
				return e
			}
			switches, used := 0, map[string]bool{}
			for _, h := range history {
				used[h.AgentID] = true
				if domain.FallbackParent(h.Origin) != "" {
					switches++
				}
			}
			if switches >= cfg.MaxSwitches {
				continue
			}
			for _, id := range cfg.Providers {
				if !used[id] && usable(id) {
					out = append(out, FallbackChoice{Run: r, Agent: id})
					break
				}
			}
		}
		return nil
	})
	return out, err
}

// ClaimFallback reserves the attempt before anything starts. It is refused if the run was already attempted, if the daily
// limit is spent, or if the settings changed so that the attempt is no longer allowed.
func (s *Scheduler) ClaimFallback(ctx context.Context, projectID string, c FallbackChoice) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		cfg := domain.FallbackSettings{}
		if e := tx.Settings().Get(ctx, domain.SettingFallbackPrefix+projectID, &cfg); e != nil {
			if errors.Is(e, domain.ErrNotFound) {
				return fmt.Errorf("%w: automatic fallback is off", domain.ErrConflict)
			}
			return e
		}
		cfg.Normalize()
		if !cfg.Enabled {
			return fmt.Errorf("%w: automatic fallback is off", domain.ErrConflict)
		}
		allowed := false
		for _, id := range cfg.Providers {
			allowed = allowed || id == c.Agent
		}
		if !allowed {
			return fmt.Errorf("%w: %s is not a fallback agent for this project", domain.ErrConflict, c.Agent)
		}
		ledger, e := readLedger(ctx, tx, projectID)
		if e != nil {
			return e
		}
		i, seen := ledgerEntry(ledger, c.Run.ID)
		if seen && attempted(ledger.Entries[i]) {
			return fmt.Errorf("%w: this run was already considered for fallback", domain.ErrConflict)
		}
		if spentToday(ledger, s.now()) >= cfg.DailyLimit {
			return fmt.Errorf("%w: today's automatic switches are used", domain.ErrConflict)
		}
		entry := domain.FallbackEntry{RunID: c.Run.ID, TaskID: c.Run.TaskID, Agent: c.Agent, Result: domain.FallbackClaimed, At: s.now()}
		if seen {
			ledger.Entries[i] = entry
		} else {
			ledger.Entries = append(ledger.Entries, entry)
		}
		return writeLedger(ctx, tx, s, projectID, ledger)
	})
}

// ResolveFallback records how a claimed attempt ended. A retry reopens it; everything else closes it for good.
func (s *Scheduler) ResolveFallback(ctx context.Context, projectID, runID, result, reason, newRunID string) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		ledger, e := readLedger(ctx, tx, projectID)
		if e != nil {
			return e
		}
		i, seen := ledgerEntry(ledger, runID)
		if !seen {
			return domain.ErrNotFound
		}
		ledger.Entries[i].Result, ledger.Entries[i].Reason, ledger.Entries[i].NewRun = result, truncate(reason, 300), newRunID
		entry := ledger.Entries[i]
		if e := writeLedger(ctx, tx, s, projectID, ledger); e != nil {
			return e
		}
		if result != domain.FallbackStarted && result != domain.FallbackDeclined {
			return nil // a retry is not news; the next tick tries again
		}
		// Say it where the person will see it: the run's feed and an alert. Fallback acts while nobody is looking.
		ev := newEvent(domain.EventAgentFallback, domain.FallbackNotice{Result: result, Agent: entry.Agent, Reason: entry.Reason, NewRun: newRunID})
		ev.ProjectID, ev.TaskID, ev.RunID = projectID, entry.TaskID, runID
		return em.emit(ev)
	})
}

func writeLedger(ctx context.Context, tx store.Tx, s *Scheduler, projectID string, l domain.FallbackLedger) error {
	if len(l.Entries) > fallbackLedgerMax {
		l.Entries = l.Entries[len(l.Entries)-fallbackLedgerMax:]
	}
	return tx.Settings().Set(ctx, domain.SettingFallbackLedgerPrefix+projectID, l, s.now())
}

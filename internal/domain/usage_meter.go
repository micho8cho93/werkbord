package domain

import (
	"fmt"
	"sort"
	"time"
)

// Usage meter: how many tokens the runs Werkbord started have used in the last five hours and the last seven days, per agent, set
// against budgets the person chose. A provider does not tell Werkbord what is left of a plan's allowance, so the meter never claims to:
// it counts what these runs reported, and "percent" is of the person's own budget. Work done in a provider's own app, or in a run that
// reported no tokens, is not here. The meter lives on this computer and is never part of anything Team shares.

// SettingUsage is the settings key for UsageSettings.
const SettingUsage = "usage"

// UsageMode is how Werkbord acts on the meter. Standard only measures. Saver also keeps automatic work off an agent that is near its
// budget; a start the person makes themselves is never refused.
type UsageMode string

const (
	UsageStandard UsageMode = "standard"
	UsageSaver    UsageMode = "saver"
)

const (
	UsageFiveHours = 5 * time.Hour
	UsageWeek      = 7 * 24 * time.Hour
	// UsageDefaultSaverAt is the share of a budget at which Saver holds automatic work.
	UsageDefaultSaverAt = 80
)

// UsageBudget is a person's own token budget for one agent. Zero means none is set for that window.
type UsageBudget struct {
	FiveHour int64 `json:"fiveHour,omitempty"`
	Weekly   int64 `json:"weekly,omitempty"`
}

// UsageSettings are the person's choices. The zero value measures and acts on nothing.
type UsageSettings struct {
	Mode UsageMode `json:"mode"`
	// SaverAt is the percent of a budget at which Saver holds automatic work, 50 to 99.
	SaverAt int `json:"saverAt"`
	// Budgets are keyed by agent ID.
	Budgets map[string]UsageBudget `json:"budgets"`
}

func (s *UsageSettings) Normalize() {
	if s.Mode == "" {
		s.Mode = UsageStandard
	}
	if s.SaverAt == 0 {
		s.SaverAt = UsageDefaultSaverAt
	}
	if s.Budgets == nil {
		s.Budgets = map[string]UsageBudget{}
	}
}

func (s UsageSettings) Validate() error {
	if s.Mode != UsageStandard && s.Mode != UsageSaver {
		return fmt.Errorf("%w: usage mode must be standard or saver", ErrInvalid)
	}
	if s.SaverAt < 50 || s.SaverAt > 99 {
		return fmt.Errorf("%w: the saver threshold must be 50 to 99 percent", ErrInvalid)
	}
	if len(s.Budgets) > 16 {
		return fmt.Errorf("%w: set budgets for at most 16 agents", ErrInvalid)
	}
	for id, b := range s.Budgets {
		if err := (ExecutionConfig{Agent: id}).Validate(); err != nil || id == "" {
			return fmt.Errorf("%w: %q is not an agent ID", ErrInvalid, id)
		}
		const most = int64(1_000_000_000_000)
		if b.FiveHour < 0 || b.Weekly < 0 || b.FiveHour > most || b.Weekly > most {
			return fmt.Errorf("%w: a budget is 0 (none) up to a trillion tokens", ErrInvalid)
		}
	}
	return nil
}

// UsageWindow is one agent's tokens in one rolling window.
type UsageWindow struct {
	Tokens int64 `json:"tokens"`
	// Runs is how many runs counted; Unreported how many of those reported no token count at all.
	Runs       int `json:"runs"`
	Unreported int `json:"unreported"`
	// Budget is the person's own, 0 when none is set. Percent is Tokens of Budget, capped at 999, nil without a budget.
	Budget  int64 `json:"budget"`
	Percent *int  `json:"percent,omitempty"`
	// EasesAt is when the oldest counted run leaves the window, so the figure starts to fall. A rolling window has no single reset.
	EasesAt *time.Time `json:"easesAt,omitempty"`
}

// AgentUsage is one agent's meter.
type AgentUsage struct {
	Agent    string      `json:"agent"`
	FiveHour UsageWindow `json:"fiveHour"`
	Weekly   UsageWindow `json:"weekly"`
	// Held says why Saver keeps automatic work off this agent now, or is empty.
	Held string `json:"held,omitempty"`
}

// UsageMeter is what the person sees.
type UsageMeter struct {
	At      time.Time    `json:"at"`
	Mode    UsageMode    `json:"mode"`
	SaverAt int          `json:"saverAt"`
	Agents  []AgentUsage `json:"agents"`
}

// Holds lists the agents Saver keeps automatic work off, with the reason, and whether every budgeted agent is held.
func (m UsageMeter) Holds() (held map[string]string, allBudgetedHeld bool) {
	held = map[string]string{}
	budgeted := 0
	for _, a := range m.Agents {
		if a.FiveHour.Budget > 0 || a.Weekly.Budget > 0 {
			budgeted++
		}
		if a.Held != "" {
			held[a.Agent] = a.Held
		}
	}
	return held, budgeted > 0 && len(held) == budgeted
}

// tokensOf is what a run reported using: input plus output. Cached tokens may overlap input and are not added.
func tokensOf(u Usage) (int64, bool) {
	if u.InputTokens == nil && u.OutputTokens == nil {
		return 0, false
	}
	var n int64
	if u.InputTokens != nil {
		n += *u.InputTokens
	}
	if u.OutputTokens != nil {
		n += *u.OutputTokens
	}
	return n, true
}

// MeasureUsage sums what the runs used in the two windows ending at now. A run that started before a window opened counts only its
// share of its time inside it, which is an estimate: a run reports one running total, not when each token was used.
func MeasureUsage(runs []Run, s UsageSettings, now time.Time) UsageMeter {
	s.Normalize()
	type acc struct {
		w     [2]UsageWindow
		first [2]time.Time
	}
	byAgent := map[string]*acc{}
	spans := [2]time.Duration{UsageFiveHours, UsageWeek}
	for _, r := range runs {
		if r.AgentID == "" {
			continue
		}
		start := r.CreatedAt
		end := now
		if r.EndedAt != nil {
			end = *r.EndedAt
		} else if r.State.Terminal() && !r.UpdatedAt.IsZero() {
			end = r.UpdatedAt
		}
		if end.After(now) {
			end = now
		}
		tokens, reported := tokensOf(r.Usage)
		for i, span := range spans {
			open := now.Add(-span)
			if !end.After(open) && !(end.Equal(start) && !start.Before(open)) {
				continue
			}
			from := start
			if from.Before(open) {
				from = open
			}
			share := 1.0
			if total := end.Sub(start); total > 0 {
				share = float64(end.Sub(from)) / float64(total)
				if share > 1 {
					share = 1
				}
			}
			if share < 0 {
				continue
			}
			a := byAgent[r.AgentID]
			if a == nil {
				a = &acc{}
				byAgent[r.AgentID] = a
			}
			w := &a.w[i]
			w.Runs++
			if !reported {
				w.Unreported++
			} else {
				w.Tokens += int64(float64(tokens)*share + 0.5)
			}
			if a.first[i].IsZero() || from.Before(a.first[i]) {
				a.first[i] = from
			}
		}
	}
	for id := range s.Budgets {
		if byAgent[id] == nil {
			byAgent[id] = &acc{}
		}
	}
	out := UsageMeter{At: now, Mode: s.Mode, SaverAt: s.SaverAt, Agents: []AgentUsage{}}
	ids := make([]string, 0, len(byAgent))
	for id := range byAgent {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a := byAgent[id]
		b := s.Budgets[id]
		u := AgentUsage{Agent: id, FiveHour: a.w[0], Weekly: a.w[1]}
		fill := func(w *UsageWindow, budget int64, first time.Time, span time.Duration) {
			w.Budget = budget
			if budget > 0 {
				p := int(w.Tokens * 100 / budget)
				if p > 999 {
					p = 999
				}
				w.Percent = &p
			}
			if !first.IsZero() {
				at := first.Add(span)
				w.EasesAt = &at
			}
		}
		fill(&u.FiveHour, b.FiveHour, a.first[0], UsageFiveHours)
		fill(&u.Weekly, b.Weekly, a.first[1], UsageWeek)
		if s.Mode == UsageSaver {
			switch {
			case u.FiveHour.Percent != nil && *u.FiveHour.Percent >= s.SaverAt:
				u.Held = fmt.Sprintf("Usage saver: %s has used %d%% of your 5-hour budget", id, *u.FiveHour.Percent)
			case u.Weekly.Percent != nil && *u.Weekly.Percent >= s.SaverAt:
				u.Held = fmt.Sprintf("Usage saver: %s has used %d%% of your weekly budget", id, *u.Weekly.Percent)
			}
		}
		out.Agents = append(out.Agents, u)
	}
	return out
}

package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
	"werkbord/internal/domain"
	"werkbord/internal/store"
)

const selectionSettingsKey = "agent_selection"

func (s *Settings) Selection(ctx context.Context) (domain.SelectionSettings, error) {
	out := domain.SelectionSettings{Agents: map[string]domain.AgentSelectionPreference{}}
	err := s.Store.View(ctx, func(tx store.Tx) error { return tx.Settings().Get(ctx, selectionSettingsKey, &out) })
	if errors.Is(err, domain.ErrNotFound) {
		for _, id := range []string{"claude-code", "codex", "gemini-cli", "opencode"} {
			out.Agents[id] = domain.AgentSelectionPreference{Enabled: true, Billing: "auto"}
		}
		return out, nil
	}
	if out.Agents == nil {
		out.Agents = map[string]domain.AgentSelectionPreference{}
	}
	return out, err
}
func (s *Settings) SetSelection(ctx context.Context, in domain.SelectionSettings) (domain.SelectionSettings, error) {
	if len(in.Agents) > 100 {
		return in, invalid("too many agents")
	}
	for id, p := range in.Agents {
		if s.Catalog != nil && !s.Catalog.Known(id) {
			return in, invalid("unknown agent " + id)
		}
		switch p.Billing {
		case "auto", "subscription", "metered", "excluded":
		default:
			return in, invalid("choose a billing type for " + id)
		}
		if len(p.Models) > 300 {
			return in, invalid("too many model preferences")
		}
		for model, e := range p.Models {
			if !domain.ValidModelName(model) || e.Intelligence < 0 || e.Intelligence > 3 {
				return in, invalid("invalid model preference")
			}
			for _, v := range []*float64{e.Input, e.Output} {
				if v != nil && (*v < 0 || *v > 1e6 || math.IsNaN(*v) || math.IsInf(*v, 0)) {
					return in, invalid("invalid price estimate")
				}
			}
		}
	}
	err := s.Store.Update(ctx, func(tx store.Tx) error { return tx.Settings().Set(ctx, selectionSettingsKey, in, time.Now().UTC()) })
	if err != nil {
		return in, fmt.Errorf("save agent selection: %w", err)
	}
	return in, nil
}

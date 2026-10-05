package service

import (
	"context"
	"sort"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func (s *Runs) SetUsage(ctx context.Context, id string, u domain.Usage) (*domain.Run, error) {
	if e := u.Validate(); e != nil {
		return nil, e
	}
	if u.CostKind == "" {
		u.CostKind = "usage_only"
	}
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var e error
		r, e = tx.Runs().Get(ctx, id)
		if e != nil {
			return e
		}
		u.Acceptance = r.Usage.Acceptance
		r.Usage = domain.MergeUsage(r.Usage, u)
		r.UpdatedAt = s.now()
		if e := tx.Runs().Update(ctx, r); e != nil {
			return e
		}
		return emitRun(em, r, r.State)
	})
	return r, err
}

type UsageSummary struct {
	ActualCostRuns    int     `json:"actualCostRuns"`
	EstimatedCostRuns int     `json:"estimatedCostRuns"`
	ProjectID         string  `json:"projectId"`
	RunnerID          string  `json:"runnerId"`
	AgentID           string  `json:"agentId"`
	Runs              int     `json:"runs"`
	Retries           int     `json:"retries"`
	RuntimeSeconds    int64   `json:"runtimeSeconds"`
	InputTokens       int64   `json:"inputTokens"`
	OutputTokens      int64   `json:"outputTokens"`
	TokenRuns         int     `json:"tokenRuns"`
	ActualCostUSD     float64 `json:"actualCostUsd"`
	EstimatedCostUSD  float64 `json:"estimatedCostUsd"`
	CostRuns          int     `json:"costRuns"`
	Accepted          int     `json:"accepted"`
	Rejected          int     `json:"rejected"`
}

// RecentUsage covers the past 30 days, bounded to the latest 500 runs per
// project. Coverage counts make partial/unknown measurements explicit.
func RecentUsage(ctx context.Context, tx store.Tx, now time.Time) ([]UsageSummary, error) {
	projects, e := tx.Projects().List(ctx)
	if e != nil {
		return nil, e
	}
	groups := map[string]*UsageSummary{}
	out := []UsageSummary{}
	for _, p := range projects {
		runs, e := tx.Runs().ListByProject(ctx, p.ID, 500)
		if e != nil {
			return nil, e
		}
		for _, r := range runs {
			if now.Sub(r.CreatedAt) > 30*24*time.Hour {
				continue
			}
			key := r.ProjectID + "/" + r.RunnerID + "/" + r.AgentID
			g := groups[key]
			if g == nil {
				g = &UsageSummary{ProjectID: r.ProjectID, RunnerID: r.RunnerID, AgentID: r.AgentID}
				groups[key] = g
			}
			g.Runs++
			if r.Attempt > 1 {
				g.Retries++
			}
			end := now
			if r.EndedAt != nil {
				end = *r.EndedAt
			}
			if end.After(r.CreatedAt) {
				g.RuntimeSeconds += int64(end.Sub(r.CreatedAt).Seconds())
			}
			if r.Usage.InputTokens != nil || r.Usage.OutputTokens != nil {
				g.TokenRuns++
			}
			if r.Usage.InputTokens != nil {
				g.InputTokens += *r.Usage.InputTokens
			}
			if r.Usage.OutputTokens != nil {
				g.OutputTokens += *r.Usage.OutputTokens
			}
			if r.Usage.CostUSD != nil {
				g.CostRuns++
				if r.Usage.CostKind == "actual_api" {
					g.ActualCostRuns++
					g.ActualCostUSD += *r.Usage.CostUSD
				} else if r.Usage.CostKind == "estimated_api_equivalent" {
					g.EstimatedCostRuns++
					g.EstimatedCostUSD += *r.Usage.CostUSD
				}
			}
			switch r.Usage.Acceptance {
			case "accepted":
				g.Accepted++
			case "rejected":
				g.Rejected++
			}
		}
	}
	for _, g := range groups {
		out = append(out, *g)
	}
	sortUsage(out)
	return out, nil
}

func sortUsage(out []UsageSummary) {
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ProjectID != b.ProjectID {
			return a.ProjectID < b.ProjectID
		}
		if a.RunnerID != b.RunnerID {
			return a.RunnerID < b.RunnerID
		}
		return a.AgentID < b.AgentID
	})
}

func (s *Runs) SetAcceptance(ctx context.Context, id, acceptance string) (*domain.Run, error) {
	if e := (domain.Usage{Acceptance: acceptance}).Validate(); e != nil {
		return nil, e
	}
	var r *domain.Run
	e := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var e error
		r, e = tx.Runs().Get(ctx, id)
		if e != nil {
			return e
		}
		r.Usage.Acceptance = acceptance
		r.UpdatedAt = s.now()
		if e := tx.Runs().Update(ctx, r); e != nil {
			return e
		}
		return emitRun(em, r, r.State)
	})
	return r, e
}

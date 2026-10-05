package domain

import (
	"fmt"
	"math"
)

// Unknown measurements stay nil. Cost is denominated in USD only when reported.
type Usage struct {
	Partial      bool     `json:"partial,omitempty"`
	InputTokens  *int64   `json:"inputTokens,omitempty"`
	OutputTokens *int64   `json:"outputTokens,omitempty"`
	CachedTokens *int64   `json:"cachedTokens,omitempty"`
	APICalls     *int64   `json:"apiCalls,omitempty"`
	CostUSD      *float64 `json:"costUsd,omitempty"`
	CostKind     string   `json:"costKind"` // actual_api, estimated_api_equivalent, usage_only
	Source       string   `json:"source,omitempty"`
	Acceptance   string   `json:"acceptance,omitempty"` // accepted, rejected, unset
}

func (u Usage) Validate() error {
	for _, n := range []*int64{u.InputTokens, u.OutputTokens, u.CachedTokens, u.APICalls} {
		if n != nil && *n < 0 {
			return fmt.Errorf("%w: negative usage", ErrInvalid)
		}
	}
	if u.CostUSD != nil && (*u.CostUSD < 0 || math.IsNaN(*u.CostUSD) || math.IsInf(*u.CostUSD, 0)) {
		return fmt.Errorf("%w: invalid cost", ErrInvalid)
	}
	if u.CostKind != "" && u.CostKind != "actual_api" && u.CostKind != "estimated_api_equivalent" && u.CostKind != "usage_only" {
		return fmt.Errorf("%w: invalid cost basis", ErrInvalid)
	}
	if u.CostUSD != nil && (u.CostKind == "" || u.CostKind == "usage_only" || u.Source == "") {
		return fmt.Errorf("%w: cost requires a source and basis", ErrInvalid)
	}
	if u.Acceptance != "" && u.Acceptance != "accepted" && u.Acceptance != "rejected" {
		return fmt.Errorf("%w: invalid human assessment", ErrInvalid)
	}
	if len(u.Source) > 200 {
		return fmt.Errorf("%w: usage source too long", ErrInvalid)
	}
	return nil
}

type RoutingRule struct {
	Name        string `json:"name"`
	Contains    string `json:"contains,omitempty"`
	Retry       bool   `json:"retry"`
	Agent       string `json:"agent,omitempty"`
	Model       string `json:"model,omitempty"`
	Reasoning   string `json:"reasoning,omitempty"`
	MinCPU      int    `json:"minCpu,omitempty"`
	MinRAMBytes int64  `json:"minRamBytes,omitempty"`
}

// MergeUsage retains known cumulative measurements across incomplete snapshots.
func MergeUsage(old, next Usage) Usage {
	max := func(a, b *int64) *int64 {
		if b == nil || a != nil && *a > *b {
			return a
		}
		return b
	}
	next.InputTokens = max(old.InputTokens, next.InputTokens)
	next.OutputTokens = max(old.OutputTokens, next.OutputTokens)
	next.CachedTokens = max(old.CachedTokens, next.CachedTokens)
	next.APICalls = max(old.APICalls, next.APICalls)
	if next.CostUSD == nil || old.CostUSD != nil && (old.CostKind != next.CostKind || *old.CostUSD > *next.CostUSD) {
		next.CostUSD, next.CostKind, next.Source = old.CostUSD, old.CostKind, old.Source
	}
	if next.Source == "" {
		next.Source = old.Source
	}
	next.Partial = old.Partial || next.Partial
	next.Acceptance = old.Acceptance
	return next
}

// UsageAfterBaseline reports only verifiable increments in cumulative counters.
// Unknown baseline fields cannot be attributed to the resumed invocation.
func UsageAfterBaseline(base, now Usage) Usage {
	delta := func(a, b *int64) *int64 {
		if a == nil || b == nil || *b < *a {
			return nil
		}
		v := *b - *a
		return &v
	}
	now.InputTokens = delta(base.InputTokens, now.InputTokens)
	now.OutputTokens = delta(base.OutputTokens, now.OutputTokens)
	now.CachedTokens = delta(base.CachedTokens, now.CachedTokens)
	now.APICalls = delta(base.APICalls, now.APICalls)
	if base.CostUSD != nil && now.CostUSD != nil && base.CostKind == now.CostKind && *now.CostUSD >= *base.CostUSD {
		v := *now.CostUSD - *base.CostUSD
		now.CostUSD = &v
	} else {
		now.CostUSD = nil
		now.CostKind = "usage_only"
	}
	now.Partial = true
	return now
}

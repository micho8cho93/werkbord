package domain

import (
	"fmt"
	"math"
)

// Unknown measurements stay nil. Cost is denominated in USD only when reported.
type Usage struct {
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

// Package modelroute selects only from verified, owner-enabled runtime options.
// It has no execution or board authority. Its optional classifier returns a
// difficulty assessment; eligibility, billing and reasoning remain local rules.
package modelroute

import (
	"context"
	"devboard/internal/domain"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

type Candidate struct {
	Agent        string   `json:"agent"`
	Model        string   `json:"model"`
	Name         string   `json:"name"`
	Reasoning    []string `json:"reasoning,omitempty"`
	Runner       string   `json:"runner,omitempty"`
	Subscription bool     `json:"subscription"`
	Metered      bool     `json:"metered"`
}
type Assessment struct {
	Difficulty int `json:"difficulty"` // 1 routine, 2 substantial, 3 complex
	// Preferred is an index into the supplied eligible candidates; -1 means no preference.
	Preferred int `json:"preferred"`
}
type Router struct {
	Settings   func(context.Context) (domain.SelectionSettings, error)
	Candidates func(context.Context, domain.SelectionRequest) ([]Candidate, error)
	Classify   func(context.Context, string, domain.SelectionMode, []Candidate) (Assessment, error)
}
type ranked struct {
	Candidate
	tier  int
	cost  float64
	known bool
	index int
}

func (r *Router) Resolve(ctx context.Context, req domain.SelectionRequest) (domain.SelectionDecision, error) {
	if !req.Mode.Valid() || req.Mode == domain.SelectionManual {
		return domain.SelectionDecision{}, fmt.Errorf("%w: an automatic mode is required", domain.ErrInvalid)
	}
	settings, err := r.Settings(ctx)
	if err != nil {
		return domain.SelectionDecision{}, err
	}
	candidates, err := r.Candidates(ctx, req)
	if err != nil {
		return domain.SelectionDecision{}, err
	}
	eligible := []ranked{}
	for _, c := range candidates {
		pref, ok := settings.Agents[c.Agent]
		if !ok || !pref.Enabled || pref.Billing == "excluded" || slices.Contains(req.Excluded, c.Agent+"/"+c.Model) {
			continue
		}
		if !domain.ValidModelName(c.Model) || c.Model == domain.AgentDefault || c.Model == "auto" {
			continue
		} // an unknown default cannot be ranked honestly
		subscription, metered := c.Subscription, c.Metered
		if pref.Billing == "subscription" && !c.Metered {
			subscription, metered = true, false
		}
		if pref.Billing == "metered" {
			subscription, metered = false, true
		}
		if !subscription && (!metered || !settings.AllowMetered) {
			continue
		}
		economics := pref.Models[c.Model]
		known := economics.Input != nil && economics.Output != nil
		if !subscription && !known {
			continue
		} // no invented API price, nor a pretend zero
		tier := economics.Intelligence
		if tier == 0 {
			tier = modelTier(c.Model)
		}
		if req.Mode == domain.SelectionMaxReasoning && len(realLevels(c.Reasoning)) == 0 {
			continue
		}
		cost := 0.0
		if known {
			cost = *economics.Input + *economics.Output
		}
		c.Subscription, c.Metered = subscription, metered
		eligible = append(eligible, ranked{Candidate: c, tier: tier, cost: cost, known: known, index: len(eligible)})
	}
	if len(eligible) == 0 {
		return domain.SelectionDecision{}, fmt.Errorf("%w: no eligible model; enable an agent in Settings → Agents and confirm its billing. Metered access also requires permission and price estimates. Max reasoning requires reported reasoning controls", domain.ErrConflict)
	}
	// Stable order also makes classifier candidate indices deterministic.
	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.Agent != b.Agent {
			return a.Agent < b.Agent
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Runner < b.Runner
	})
	if len(eligible) > 200 {
		eligible = eligible[:200]
	}
	assessment := Assessment{Difficulty: difficulty(req.Text), Preferred: -1}
	method := "local_rules"
	if r.Classify != nil {
		list := make([]Candidate, len(eligible))
		for i, x := range eligible {
			list[i] = x.Candidate
		}
		if a, e := r.Classify(ctx, clip(req.Text, 6000), req.Mode, list); e == nil && a.Difficulty >= 1 && a.Difficulty <= 3 {
			assessment = a
			method = "classifier"
		}
	}
	// The strongest available model is the upper bound. Economy modes may use a
	// stronger tier when that is all the owner has enabled.
	target := assessment.Difficulty
	if req.Mode == domain.SelectionIntelligence {
		target = 3
	}
	maxTier := 0
	for _, c := range eligible {
		if c.tier > maxTier {
			maxTier = c.tier
		}
	}
	if target > maxTier {
		target = maxTier
	}
	shortlist := []ranked{}
	for i, c := range eligible {
		if c.tier >= target {
			c.index = i
			shortlist = append(shortlist, c)
		}
	}
	sort.SliceStable(shortlist, func(i, j int) bool {
		a, b := shortlist[i], shortlist[j]
		if req.Mode == domain.SelectionIntelligence && a.tier != b.tier {
			return a.tier > b.tier
		}
		if a.Subscription != b.Subscription {
			return a.Subscription
		}
		if a.cost != b.cost {
			return a.cost < b.cost
		}
		if req.Mode != domain.SelectionIntelligence && a.tier != b.tier {
			return a.tier < b.tier
		}
		// AI may break a policy-equivalent tie, never a billing/capability rule.
		if a.index == assessment.Preferred {
			return true
		}
		if b.index == assessment.Preferred {
			return false
		}
		return false
	})
	winner := shortlist[0]
	levels := realLevels(winner.Reasoning)
	reasoning := ""
	if len(levels) > 0 {
		sort.SliceStable(levels, func(i, j int) bool { return effort(levels[i]) < effort(levels[j]) })
		index := 0
		if req.Mode == domain.SelectionMaxReasoning || req.Mode == domain.SelectionIntelligence {
			index = len(levels) - 1
		} else if assessment.Difficulty > 1 {
			index = (len(levels) - 1) * (assessment.Difficulty - 1) / 2
		}
		reasoning = levels[index]
	}
	billing := "included subscription usage"
	if !winner.Subscription {
		billing = "owner-supplied API price estimate (actual usage may differ)"
	}
	return domain.SelectionDecision{Agent: winner.Agent, Model: winner.Model, Reasoning: reasoning, Runner: winner.Runner, Mode: req.Mode, Method: method, Reason: fmt.Sprintf("Task complexity %d/3; %s; selection stays fixed for this conversation or run", assessment.Difficulty, billing)}, nil
}
func modelTier(id string) int {
	s := strings.ToLower(id)
	for _, x := range []string{"haiku", "luna", "flash", "mini", "nano", "lite", "small"} {
		if strings.Contains(s, x) {
			return 1
		}
	}
	for _, x := range []string{"opus", "astra", "ultra", "max", "pro", "large"} {
		if strings.Contains(s, x) {
			return 3
		}
	}
	return 2
}
func difficulty(text string) int {
	s := strings.ToLower(text)
	for _, x := range []string{"security", "architecture", "race condition", "concurrency", "migration", "distributed", "cryptograph", "vulnerability", "redesign"} {
		if strings.Contains(s, x) {
			return 3
		}
	}
	if utf8.RuneCountInString(s) > 1000 {
		return 3
	}
	if utf8.RuneCountInString(s) > 200 {
		return 2
	}
	for _, x := range []string{"implement", "feature", "refactor", "debug", "integrat", "test"} {
		if strings.Contains(s, x) {
			return 2
		}
	}
	return 1
}
func realLevels(in []string) []string {
	out := []string{}
	for _, x := range in {
		if x != "" && x != domain.AgentDefault && domain.ValidReasoning(x) {
			out = append(out, x)
		}
	}
	return slices.Compact(out)
}
func effort(s string) int {
	switch strings.ToLower(s) {
	case "none", "off", "minimal":
		return 0
	case "low":
		return 1
	case "medium", "normal":
		return 2
	case "high":
		return 3
	case "xhigh", "extra-high":
		return 4
	case "max", "maximum", "ultra":
		return 5
	}
	return 2
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

package claude

import (
	"devboard/internal/agent"
	"encoding/json"
	"testing"
)

func TestCumulativeModelUsageAndCostAreEstimates(t *testing.T) {
	s := newSession(agent.ProcSpec{}, "")
	var e envelope
	if err := json.Unmarshal([]byte(`{"type":"result","total_cost_usd":0.25,"modelUsage":{"sonnet":{"inputTokens":20,"outputTokens":5,"cacheReadInputTokens":10,"cacheCreationInputTokens":7}}}`), &e); err != nil {
		t.Fatal(err)
	}
	s.onResult(&e)
	ev := next(t, s, kind(agent.KindUsage))
	if *ev.Usage.InputTokens != 37 || *ev.Usage.OutputTokens != 5 || *ev.Usage.CachedTokens != 10 || ev.Usage.CostKind != "estimated_api_equivalent" {
		t.Fatalf("usage %+v", ev.Usage)
	}
	json.Unmarshal([]byte(`{"type":"result","total_cost_usd":0.50,"modelUsage":{"sonnet":{"inputTokens":40,"outputTokens":10}}}`), &e)
	s.onResult(&e)
	ev = next(t, s, kind(agent.KindUsage))
	if *ev.Usage.CostUSD != .50 || *ev.Usage.InputTokens != 40 {
		t.Fatalf("cumulative results added twice %+v", ev.Usage)
	}
}

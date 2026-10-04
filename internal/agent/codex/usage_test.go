package codex

import (
	"devboard/internal/agent"
	"encoding/json"
	"testing"
)

func TestTokenUsageSnapshotKeepsZeroAndUnknownSeparate(t *testing.T) {
	s := newSession(agent.ProcSpec{}, "", Config{}, "", "")
	s.onNotification("thread/tokenUsage/updated", json.RawMessage(`{"tokenUsage":{"total":{"inputTokens":120,"outputTokens":0,"cachedInputTokens":40}}}`))
	ev := next(t, s, kind(agent.KindUsage))
	if ev.Usage == nil || *ev.Usage.InputTokens != 120 || ev.Usage.OutputTokens == nil || *ev.Usage.OutputTokens != 0 || ev.Usage.CostUSD != nil {
		t.Fatalf("usage %+v", ev.Usage)
	}
	s.onNotification("thread/tokenUsage/updated", json.RawMessage(`{"tokenUsage":{"total":{"inputTokens":150,"outputTokens":8}}}`))
	ev = next(t, s, kind(agent.KindUsage))
	if *ev.Usage.InputTokens != 150 || ev.Usage.CachedTokens != nil {
		t.Fatalf("snapshot inflated or unknown cache fabricated %+v", ev.Usage)
	}
}

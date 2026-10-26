package assistant

import (
	"context"
	"testing"
	"werkbord/internal/domain"
)

func TestAutomaticConversationClassifiesOnceAndKeepsItsDecision(t *testing.T) {
	f := newEfx(t)
	calls := 0
	f.eng.cfg.Selection = func(_ context.Context, in domain.SelectionRequest) (domain.SelectionDecision, error) {
		calls++
		if in.Text != "First question" {
			t.Fatalf("classification text: %q", in.Text)
		}
		return domain.SelectionDecision{Agent: f.prov.ID(), Model: "chosen", Reasoning: "", Mode: in.Mode, Method: "local_rules", Reason: "test selection"}, nil
	}
	s, err := f.eng.CreateSession(context.Background(), CreateRequest{Provider: f.prov.ID(), Mode: domain.SelectionLowestCost})
	if err != nil {
		t.Fatal(err)
	}
	f.send(s.ID, "First question")
	f.send(s.ID, "A harder follow-up")
	got, err := f.eng.Session(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || got.Model != "chosen" || got.Selection == nil || got.Selection.Method != "local_rules" {
		t.Fatalf("calls=%d session=%+v", calls, got)
	}
	if _, err := f.eng.Configure(context.Background(), s.ID, s.Provider, "", "", domain.SelectionIntelligence); err == nil {
		t.Fatal("automatic mode must stay fixed during the conversation")
	}
}

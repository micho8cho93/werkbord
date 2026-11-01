package appops

import (
	"encoding/json"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func TestActivitySummaryCountsRunsTokensAndModelsAndSaysLimitsAreUnknown(t *testing.T) {
	f := newFx(t)
	proj := f.repoProject("App")
	task := f.ticket(proj.ID, "Fix the login")
	in, out := int64(1000), int64(250)
	mk := func(state domain.RunState, model string, usage domain.Usage, age int) *domain.Run {
		at := f.now().AddDate(0, 0, -age)
		return &domain.Run{ID: domain.NewID(domain.PrefixRun), TaskID: task.ID, ProjectID: proj.ID, AgentID: "claude-code", Model: model, State: state, Usage: usage, CreatedAt: at, UpdatedAt: at}
	}
	if err := f.db.Update(bg, func(tx store.Tx) error {
		for _, r := range []*domain.Run{
			mk(domain.RunCompleted, "sonnet", domain.Usage{InputTokens: &in, OutputTokens: &out}, 1),
			mk(domain.RunFailed, "sonnet", domain.Usage{}, 2),
			mk(domain.RunCompleted, "opus", domain.Usage{InputTokens: &in, OutputTokens: &out}, 20),
		} {
			if err := tx.Runs().Create(bg, r); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p := f.session("ast_activity")
	res := f.mustCall(p, "get_activity_summary", map[string]any{"days": 3})
	raw, _ := json.Marshal(res.Data)
	var got ActivitySummary
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Projects) != 1 {
		t.Fatalf("%s", raw)
	}
	row := got.Projects[0]
	if row.Runs != 2 || row.ByState["completed"] != 1 || row.ByState["failed"] != 1 || row.InputTokens != 1000 || row.OutputTokens != 250 || row.RunsWithoutUsage != 1 {
		t.Fatalf("%+v", row)
	}
	if len(row.Models) != 1 || row.Models[0].Model != "sonnet" || row.Models[0].Runs != 2 {
		t.Fatalf("the run twenty days ago is outside the period: %+v", row.Models)
	}
	if got.Limits == "" {
		t.Fatal("it does not say that remaining usage is unknown")
	}
	if _, err := f.call(p, "get_activity_summary", map[string]any{"days": 90}); err == nil {
		t.Fatal("accepted a period it does not cover")
	}
}

package domain

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// docs/HEALTH.md says which signals are deterministic and which are heuristic. That
// is a promise to the user, so it is checked against the rules themselves: a rule
// cannot be added, or change its basis, without the document saying so.
func TestHealthDocumentationListsEveryRuleWithItsBasis(t *testing.T) {
	b, err := os.ReadFile("../../docs/HEALTH.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for _, r := range HealthRules {
		prefix := fmt.Sprintf("| `%s` | %s | %s |", r.Type, r.Category, r.Basis)
		if !strings.Contains(doc, prefix) {
			t.Errorf("docs/HEALTH.md must list the rule with its category and basis, as %q", prefix)
		}
	}
	// Nothing the document lists is a rule that no longer exists.
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cell := strings.SplitN(strings.TrimPrefix(line, "| `"), "`", 2)[0]
		if !strings.Contains(cell, "_") {
			continue // a threshold or an action kind, not a rule type
		}
		if _, ok := HealthRuleFor(HealthFindingType(cell)); ok {
			continue
		}
		if strings.Contains(line, "deterministic") || strings.Contains(line, "heuristic") {
			t.Errorf("docs/HEALTH.md lists %q, which is not a rule", cell)
		}
	}
}

// Every number a rule uses is documented, so none is a surprise.
func TestHealthDocumentationListsEveryThreshold(t *testing.T) {
	b, err := os.ReadFile("../../docs/HEALTH.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	typ := reflect.TypeOf(HealthThresholds{})
	for i := 0; i < typ.NumField(); i++ {
		if name := typ.Field(i).Name; !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("docs/HEALTH.md does not document the threshold %s", name)
		}
	}
}

// The documented defaults are the real defaults.
func TestDocumentedThresholdsAreTheDefaults(t *testing.T) {
	b, _ := os.ReadFile("../../docs/HEALTH.md")
	doc := string(b)
	d := DefaultHealthThresholds()
	for _, want := range []string{
		fmt.Sprintf("`SignificantUntracked` | %d files", d.SignificantUntracked),
		fmt.Sprintf("`FarBehind` / `VeryFarBehind` | %d / %d commits", d.FarBehind, d.VeryFarBehind),
		fmt.Sprintf("`MaxOverlapBranches` | %d |", d.MaxOverlapBranches),
		fmt.Sprintf("`WorktreeGrace` | %d minutes", int(d.WorktreeGrace.Minutes())),
		fmt.Sprintf("`LockStaleAfter` | %d minutes", int(d.LockStaleAfter.Minutes())),
		fmt.Sprintf("`StuckRemovalAfter` | %d minutes", int(d.StuckRemovalAfter.Minutes())),
		fmt.Sprintf("`IdleDirtyRisk` | %d hours", int(d.IdleDirtyRisk.Hours())),
		fmt.Sprintf("`FinishedUnmergedAfter` | %d hours", int(d.FinishedUnmergedAfter.Hours())),
		fmt.Sprintf("`AbandonedAfter` | %d days", int(d.AbandonedAfter.Hours()/24)),
		fmt.Sprintf("`FetchStaleAfter` | %d days", int(d.FetchStaleAfter.Hours()/24)),
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/HEALTH.md should say %q", want)
		}
	}
}

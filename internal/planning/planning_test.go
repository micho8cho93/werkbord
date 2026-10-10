package planning

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLabelNamesAreWhateverThePersonWantsButTidy(t *testing.T) {
	for in, want := range map[string]string{"Design": "Design", "  Q4   launch ": "Q4 launch", "日本語": "日本語", "Ops & Support": "Ops & Support"} {
		got, err := CleanLabelName(in)
		if err != nil || got != want {
			t.Errorf("CleanLabelName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "a\x00b", "a\u0007b", strings.Repeat("x", MaxLabelNameLen+1)} {
		if _, err := CleanLabelName(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("CleanLabelName(%q) accepted", in)
		}
	}
	if LabelKey("Design") != LabelKey("  design ") || LabelKey("a b") != LabelKey("A   B") || LabelKey("a") == LabelKey("b") {
		t.Fatal("names that differ only in case or spacing must be the same label, and others must not")
	}
}

func TestLabelColours(t *testing.T) {
	for in, want := range map[string]string{"#ABC": "#aabbcc", "#1a2B3c": "#1a2b3c", " #ffffff ": "#ffffff"} {
		if got, err := CleanLabelColor(in); err != nil || got != want {
			t.Errorf("CleanLabelColor(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	// Anything that is not a plain hex colour could carry markup into a style attribute.
	for _, in := range []string{"", "red", "#12", "#12345", "#gggggg", "123456", "#123456;x", "url(x)", "#12345678"} {
		if _, err := CleanLabelColor(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("CleanLabelColor(%q) accepted", in)
		}
	}
}

func TestLabelSetsAreBoundedAndDeduplicated(t *testing.T) {
	got, err := CleanLabelIDs([]string{"a", "b", "a", " c "})
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := CleanLabelIDs(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil must be an empty, non-nil set: %v, %v", got, err)
	}
	many := make([]string, MaxLabelsPerTask+1)
	for i := range many {
		many[i] = string(rune('a'+i)) + "x"
	}
	if _, err := CleanLabelIDs(many); !errors.Is(err, ErrInvalid) {
		t.Fatal("too many labels accepted")
	}
	if _, err := CleanLabelIDs([]string{""}); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty label ID accepted")
	}
}

func TestExecutionModeIsItsOwnThingAndNotALabel(t *testing.T) {
	for _, m := range ExecutionModes {
		got, err := ParseExecutionMode(string(m))
		if err != nil || got != m || !m.Valid() {
			t.Errorf("%s: %v", m, err)
		}
	}
	for _, s := range []string{"", "Human", "robot", "design"} {
		if _, err := ParseExecutionMode(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted", s)
		}
	}
	if ModeHuman.AllowsAgent() || !ModeAgent.AllowsAgent() || !ModeHybrid.AllowsAgent() {
		t.Fatal("only human work refuses an agent")
	}
}

func TestRanges(t *testing.T) {
	ok := []struct{ in, want Range }{
		{Range{}, Range{}},
		{Range{Start: "2026-10-05"}, Range{Start: "2026-10-05"}},
		{Range{End: "2026-10-09"}, Range{End: "2026-10-09"}},
		{Range{Start: "2026-10-05", End: "2026-10-05"}, Range{Start: "2026-10-05", End: "2026-10-05"}},
		{Range{Start: "2026-10-05", End: "2026-10-09"}, Range{Start: "2026-10-05", End: "2026-10-09"}},
		{Range{Start: "2026-10-05", Milestone: true}, Range{Start: "2026-10-05", Milestone: true}},
		{Range{End: "2026-10-05", Milestone: true}, Range{Start: "2026-10-05", Milestone: true}},
		{Range{Start: "2026-10-05", End: "2026-10-05", Milestone: true}, Range{Start: "2026-10-05", Milestone: true}},
	}
	for _, c := range ok {
		if got, err := CleanRange(c.in); err != nil || got != c.want {
			t.Errorf("CleanRange(%+v) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	bad := []Range{
		{Start: "2026-10-09", End: "2026-10-05"},
		{Start: "10/05/2026"}, {Start: "2026-13-01"}, {Start: "2026-02-30"}, {End: "yesterday"},
		{Start: "1969-12-31"}, {Start: "2201-01-01"},
		{Start: "2026-10-05", End: "2026-10-06", Milestone: true},
	}
	for _, r := range bad {
		if _, err := CleanRange(r); !errors.Is(err, ErrInvalid) {
			t.Errorf("CleanRange(%+v) accepted", r)
		}
	}
	a, b, okSpan := Range{End: "2026-10-09"}.Span()
	if !okSpan || a != b || a.Format(DayLayout) != "2026-10-09" {
		t.Fatal("an item with only an end is a one-day item on that day")
	}
	if _, _, okSpan := (Range{}).Span(); okSpan {
		t.Fatal("nothing planned has no span")
	}
}

func item(id string, start, end string, deps ...string) Item {
	return Item{ID: id, Title: strings.ToUpper(id), Range: Range{Start: start, End: end}, Dependencies: deps}
}

func codes(ws []Warning) []string {
	out := []string{}
	for _, w := range ws {
		out = append(out, string(w.Code)+":"+w.ItemID+">"+w.OtherID)
	}
	return out
}

func TestAnalyzeAcceptsAConsistentPlan(t *testing.T) {
	ws := Analyze([]Item{
		item("a", "2026-10-05", "2026-10-09"),
		item("b", "2026-10-09", "2026-10-12", "a"), // may start the day its dependency ends
		item("c", "2026-10-13", "2026-10-13", "a", "b"),
		{ID: "m", Title: "M", Range: Range{Start: "2026-10-13", Milestone: true}, Dependencies: []string{"b"}},
	})
	if len(ws) != 0 {
		t.Fatalf("a consistent plan has no warnings: %v", codes(ws))
	}
}

func TestAnalyzeFlagsDateConflictsWithoutChangingAnything(t *testing.T) {
	items := []Item{
		item("a", "2026-10-05", "2026-10-09"),
		item("b", "2026-10-07", "2026-10-12", "a"),
	}
	before := append([]Item(nil), items...)
	ws := Analyze(items)
	if got := codes(ws); !reflect.DeepEqual(got, []string{"starts_before_dependency_ends:b>a"}) {
		t.Fatalf("got %v", got)
	}
	if ws[0].Severity != SeverityWarning || !strings.Contains(ws[0].Message, "2026-10-07") || !strings.Contains(ws[0].Message, "2026-10-09") {
		t.Fatalf("message should say both dates: %+v", ws[0])
	}
	if !reflect.DeepEqual(items, before) {
		t.Fatal("analysis must never move a date")
	}
	// A finished dependency no longer constrains the plan.
	items[0].Done = true
	if ws := Analyze(items); len(ws) != 0 {
		t.Fatalf("finished dependencies are not conflicts: %v", codes(ws))
	}
}

func TestAnalyzeFlagsInvalidDependencies(t *testing.T) {
	arch := item("arch", "", "")
	arch.Archived = true
	done := item("d", "", "")
	done.Done = true
	finished := item("fin", "", "", "slow")
	finished.Done = true
	ws := Analyze([]Item{
		item("self", "", "", "self"),
		item("gone", "", "", "nowhere"),
		item("dup", "", "", "arch", "arch"),
		arch, done,
		item("slow", "", ""),
		finished,
		item("ok", "", "", "d"),
	})
	want := []string{
		"missing_dependency:gone>nowhere",
		"self_dependency:self>self",
		"archived_dependency:dup>arch",
		"done_before_dependency_done:fin>slow",
		"duplicate_dependency:dup>arch",
	}
	got := codes(ws)
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Errorf("missing %s in %v", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected extra warnings: %v", got)
	}
	// Errors are listed before warnings.
	if ws[0].Severity != SeverityError || ws[len(ws)-1].Severity != SeverityWarning {
		t.Fatalf("errors must come first: %v", codes(ws))
	}
}

func TestAnalyzeFindsCyclesOnceEach(t *testing.T) {
	ws := Analyze([]Item{
		item("a", "", "", "c"), item("b", "", "", "a"), item("c", "", "", "b"), // a → c → b → a
		item("x", "", "", "y"), item("y", "", "", "x"),
		item("z", "", "", "a"), // depends on a cycle but is not in one
	})
	var cyc []Warning
	for _, w := range ws {
		if w.Code == CodeDependencyCycle {
			cyc = append(cyc, w)
		}
	}
	if len(cyc) != 2 {
		t.Fatalf("want two cycles, got %v", codes(ws))
	}
	if !reflect.DeepEqual(cyc[0].Cycle, []string{"a", "b", "c"}) || !reflect.DeepEqual(cyc[1].Cycle, []string{"x", "y"}) {
		t.Fatalf("cycles: %+v", cyc)
	}
	if !HasCycle(map[string][]string{"a": {"b"}, "b": {"a"}}) || HasCycle(map[string][]string{"a": {"b"}, "b": nil, "c": {"a", "b", "gone"}}) {
		t.Fatal("HasCycle")
	}
}

func TestAnalyzeIsStableAndSurvivesLongChains(t *testing.T) {
	var items []Item
	for i := 0; i < 20000; i++ {
		it := Item{ID: "t" + itoa(i), Title: "T"}
		if i > 0 {
			it.Dependencies = []string{"t" + itoa(i-1)}
		}
		items = append(items, it)
	}
	if ws := Analyze(items); len(ws) != 0 {
		t.Fatalf("a long chain is not a problem: %d warnings", len(ws))
	}
	items[0].Dependencies = []string{"t19999"}
	if ws := Analyze(items); len(ws) != 1 || len(ws[0].Cycle) != 20000 {
		t.Fatalf("one cycle through the whole chain, got %d warnings", len(ws))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

func TestAnalyzeReportsDatesItCannotUse(t *testing.T) {
	ws := Analyze([]Item{
		{ID: "r", Title: "R", Range: Range{Start: "2026-10-09", End: "2026-10-05"}},
		{ID: "d", Title: "D", Range: Range{Start: "soon"}},
		{ID: "m", Title: "M", Range: Range{Milestone: true}},
		item("u", "2026-10-05", "2026-10-06", "free"),
		item("free", "", ""),
	})
	got := codes(ws)
	want := map[string]bool{"invalid_range:r>": true, "invalid_date:d>": true, "milestone_without_date:m>": true, "unscheduled_dependency:u>free": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected %s", g)
		}
	}
}

func TestAnalyzeOpenReportsOnlyOnOpenWork(t *testing.T) {
	gone := item("gone", "", "", "nowhere")
	gone.Archived = true
	ws := AnalyzeOpen([]Item{gone, item("open", "", "", "nowhere")})
	if got := codes(ws); !reflect.DeepEqual(got, []string{"missing_dependency:open>nowhere"}) {
		t.Fatalf("got %v", got)
	}
	if ws == nil {
		t.Fatal("no warnings is an empty list, not nil: it is sent as JSON")
	}
	if AnalyzeOpen(nil) == nil {
		t.Fatal("nil items: still an empty list")
	}
}

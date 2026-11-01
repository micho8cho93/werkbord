package planning

import (
	"errors"
	"testing"
)

// A plan may carry the time of day it is planned for. It is still a plan for people: it never starts an agent.
func TestCleanRangeKeepsAPlannedTimeOnASingleDay(t *testing.T) {
	ok := []struct{ in, want Range }{
		{Range{Start: "2026-10-05", Time: "09:30"}, Range{Start: "2026-10-05", Time: "09:30"}},
		{Range{Start: "2026-10-05", Time: " 17:00 "}, Range{Start: "2026-10-05", Time: "17:00"}},
		{Range{Start: "2026-10-05", End: "2026-10-05", Time: "00:00"}, Range{Start: "2026-10-05", End: "2026-10-05", Time: "00:00"}},
		{Range{End: "2026-10-05", Time: "23:59"}, Range{End: "2026-10-05", Time: "23:59"}},
		{Range{Start: "2026-10-05", Milestone: true, Time: "12:15"}, Range{Start: "2026-10-05", Milestone: true, Time: "12:15"}},
		{Range{Start: "2026-10-05"}, Range{Start: "2026-10-05"}},
	}
	for _, c := range ok {
		if got, err := CleanRange(c.in); err != nil || got != c.want {
			t.Errorf("CleanRange(%+v) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	bad := []Range{
		{Time: "09:30"}, // a time needs a day
		{Start: "2026-10-05", End: "2026-10-07", Time: "09:30"}, // and only one
		{Start: "2026-10-05", Time: "9:30"},                     // HH:MM
		{Start: "2026-10-05", Time: "24:00"},
		{Start: "2026-10-05", Time: "12:60"},
		{Start: "2026-10-05", Time: "noon"},
		{Start: "2026-10-05", Time: "09:30:00"},
	}
	for _, r := range bad {
		if _, err := CleanRange(r); !errors.Is(err, ErrInvalid) {
			t.Errorf("CleanRange(%+v) accepted", r)
		}
	}
}

func TestPlannedTimeDoesNotChangeTheDaysARangeCovers(t *testing.T) {
	a, b, ok := Range{Start: "2026-10-05", Time: "09:30"}.Span()
	if !ok || a != b || a.Format(DayLayout) != "2026-10-05" {
		t.Fatalf("a timed plan is still a one-day item: %v %v %v", a, b, ok)
	}
	if (Range{Time: "09:30"}).IsZero() {
		t.Fatal("a range with a time is not nothing planned")
	}
}

func TestInvalidPlannedTimeIdentifiesItsField(t *testing.T) {
	r := Range{Start: "2026-10-05", Time: "late"}
	_, err := CleanRange(r)
	issue := InvalidRange("task", "Review", r, err)
	if len(issue.Issues) != 1 || issue.Issues[0].Field != "plan.time" {
		t.Fatalf("got %+v", issue)
	}
}

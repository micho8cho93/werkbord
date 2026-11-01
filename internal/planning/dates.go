package planning

import (
	"strings"
	"time"
)

// DayLayout is how a planned date is written: a calendar day with no time and no zone, which is
// what work that is not run by a clock needs. ("2026-10-05" is the same day wherever it is read.)
const DayLayout = "2006-01-02"

// Range is when a piece of work is planned to happen, in whole days. It is a plan for people and
// has nothing to do with when an agent is started (that is a task's schedule, which this never
// changes).
//
// Start and End are inclusive days, each optional: only a Start is a one-day item, only an End is
// something due on that day. A Milestone is a single date, kept in Start.
type Range struct {
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
	Milestone bool   `json:"milestone,omitempty"`
	// Time is the time of day the work is planned for ("09:30"), a wall time with no zone, like the day it belongs to. It is
	// only for a single day, and like the rest of a Range it never starts an agent.
	Time string `json:"time,omitempty"`
}

// IsZero reports whether nothing is planned.
func (r Range) IsZero() bool { return r == Range{} }

// ParseDay reads a planned day. It accepts only 2006-01-02, within years that make sense for planning.
func ParseDay(s string) (time.Time, error) {
	t, err := time.Parse(DayLayout, s)
	if err != nil || t.Year() < 1970 || t.Year() > 2200 {
		return time.Time{}, invalid("%q is not a date (use YYYY-MM-DD)", s)
	}
	return t, nil
}

// CleanRange validates and normalises a planned range: dates well formed, the end not before the
// start, and a milestone a single date. It does not look at other work.
func CleanRange(r Range) (Range, error) {
	r.Start, r.End, r.Time = strings.TrimSpace(r.Start), strings.TrimSpace(r.End), strings.TrimSpace(r.Time)
	var start, end time.Time
	var err error
	if r.Start != "" {
		if start, err = ParseDay(r.Start); err != nil {
			return Range{}, err
		}
	}
	if r.End != "" {
		if end, err = ParseDay(r.End); err != nil {
			return Range{}, err
		}
	}
	if r.Milestone {
		if r.Start == "" {
			r.Start, start = r.End, end
		}
		if r.End != "" && r.End != r.Start {
			return Range{}, invalid("a milestone is a single date: set the date, not a start and an end")
		}
		r.End = ""
		return cleanTime(r)
	}
	if r.Start != "" && r.End != "" && end.Before(start) {
		return Range{}, invalid("the end date %s is before the start date %s", r.End, r.Start)
	}
	return cleanTime(r)
}

// ValidTime reports whether s is a time of day written HH:MM on a 24-hour clock.
func ValidTime(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	_, err := time.Parse("15:04", s)
	return err == nil
}

// cleanTime checks a planned time of day: well formed, and only on a range that is a single day.
func cleanTime(r Range) (Range, error) {
	if r.Time == "" {
		return r, nil
	}
	if !ValidTime(r.Time) {
		return Range{}, invalid("the time %q is not a time of day: write it as HH:MM", r.Time)
	}
	first, last, ok := r.Span()
	if !ok {
		return Range{}, invalid("a time needs a day: set the date it is planned for")
	}
	if !first.Equal(last) {
		return Range{}, invalid("a time is for a single day: set one date, not a start and an end")
	}
	return r, nil
}

// Span returns the first and last day a range covers. ok is false when nothing is planned or a date
// cannot be read.
func (r Range) Span() (first, last time.Time, ok bool) {
	s, e := r.Start, r.End
	if s == "" {
		s = e
	}
	if e == "" || r.Milestone {
		e = s
	}
	if s == "" {
		return time.Time{}, time.Time{}, false
	}
	a, err1 := ParseDay(s)
	b, err2 := ParseDay(e)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, false
	}
	return a, b, true
}

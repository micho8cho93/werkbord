// Package doctor checks that Werkbord is healthy: the controller, its
// database, Git, the agents, the private network, GitHub, the runner and the
// projects. Each check says what it found and, when something is wrong, what to
// do about it.
//
// A report never contains a secret. Checks are written to say what state a
// thing is in, not to echo it: the access token, the sign-in link for the private
// network, GitHub's one-time code and any key are never read into a report, and
// a test holds the whole of it to that.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Status is the outcome of one check.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn" // works, but something deserves attention, or an optional thing is not set up
	Fail Status = "fail" // something that Werkbord needs is wrong
	Skip Status = "skip" // not checked, with the reason
)

// Check is one finding.
type Check struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Summary string `json:"summary"`
	// Fix says what to do, when something is wrong.
	Fix string `json:"fix,omitempty"`
}

// Report is the outcome of all the checks.
type Report struct {
	Version string  `json:"version"`
	Checks  []Check `json:"checks"`
}

// Worst returns the most serious status in the report.
func (r Report) Worst() Status {
	worst := OK
	for _, c := range r.Checks {
		switch {
		case c.Status == Fail:
			return Fail
		case c.Status == Warn:
			worst = Warn
		}
	}
	return worst
}

// Add appends checks.
func (r *Report) Add(cs ...Check) { r.Checks = append(r.Checks, cs...) }

// Find returns the check with an ID.
func (r Report) Find(id string) (Check, bool) {
	for _, c := range r.Checks {
		if c.ID == id {
			return c, true
		}
	}
	return Check{}, false
}

// JSON writes the report as JSON.
func (r Report) JSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Text writes the report for a person. With colour it uses ANSI, which the caller
// asks for only when writing to a terminal.
func (r Report) Text(w io.Writer, colour bool) {
	paint := func(code, s string) string {
		if !colour {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	width := 0
	for _, c := range r.Checks {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range r.Checks {
		mark := map[Status]string{OK: paint("32", "✓"), Warn: paint("33", "!"), Fail: paint("31", "✗"), Skip: paint("2", "-")}[c.Status]
		fmt.Fprintf(w, "  %s %-*s  %s\n", mark, width, c.Name, c.Summary)
		if c.Fix != "" && c.Status != OK {
			for _, line := range strings.Split(c.Fix, "\n") {
				fmt.Fprintf(w, "    %s %s\n", strings.Repeat(" ", width), paint("2", line))
			}
		}
	}
	var fails, warns int
	for _, c := range r.Checks {
		switch c.Status {
		case Fail:
			fails++
		case Warn:
			warns++
		}
	}
	switch {
	case fails > 0:
		fmt.Fprintf(w, "\n%s\n", paint("31", fmt.Sprintf("%d problem(s) to fix, %d to look at.", fails, warns)))
	case warns > 0:
		fmt.Fprintf(w, "\n%s\n", paint("33", fmt.Sprintf("Working, with %d thing(s) to look at.", warns)))
	default:
		fmt.Fprintf(w, "\n%s\n", paint("32", "Everything looks good."))
	}
}

// Func is one check, which may produce several findings.
type Func func(ctx context.Context) []Check

// Run runs the checks in order. A check that panics is reported as failed rather
// than taking the whole report down.
func Run(ctx context.Context, version string, checks ...Func) Report {
	r := Report{Version: version}
	for _, f := range checks {
		func() {
			defer func() {
				if v := recover(); v != nil {
					r.Add(Check{ID: "internal", Name: "doctor", Status: Fail, Summary: fmt.Sprintf("a check crashed: %v", v)})
				}
			}()
			r.Add(f(ctx)...)
		}()
	}
	return r
}

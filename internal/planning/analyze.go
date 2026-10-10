package planning

import (
	"fmt"
	"sort"
	"strings"
)

// Item is one piece of work as the timeline sees it. Dependencies are the IDs of the work that must
// finish before this one starts.
type Item struct {
	ID           string
	Title        string
	Range        Range
	Done         bool
	Archived     bool
	Dependencies []string
}

// Severity says how seriously a Warning should be taken.
type Severity string

const (
	// SeverityError is a dependency or date that cannot work as written.
	SeverityError Severity = "error"
	// SeverityWarning is a plan that contradicts itself: it can be carried out, but not as planned.
	SeverityWarning Severity = "warning"
	// SeverityInfo is something that could not be checked.
	SeverityInfo Severity = "info"
)

// Code names a kind of warning. They are part of the API: clients choose wording and icons by code.
type Code string

const (
	CodeInvalidDate           Code = "invalid_date"
	CodeInvalidRange          Code = "invalid_range"
	CodeMilestoneWithoutDate  Code = "milestone_without_date"
	CodeSelfDependency        Code = "self_dependency"
	CodeDuplicateDependency   Code = "duplicate_dependency"
	CodeMissingDependency     Code = "missing_dependency"
	CodeDependencyCycle       Code = "dependency_cycle"
	CodeArchivedDependency    Code = "archived_dependency"
	CodeStartsBeforeDependent Code = "starts_before_dependency_ends"
	CodeDoneBeforeDependency  Code = "done_before_dependency_done"
	CodeUnscheduledDependency Code = "unscheduled_dependency"
)

// Warning is one thing that looks wrong. It names the item it is about (ItemID) and, for a problem
// between two items, the other (OtherID); for a cycle, Cycle lists every item in it.
//
// Warnings are only ever shown. Nothing here moves a date or removes a dependency: what to do about
// a conflict is the person's decision.
type Warning struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	ItemID   string   `json:"itemId"`
	OtherID  string   `json:"otherId,omitempty"`
	Cycle    []string `json:"cycle,omitempty"`
	Message  string   `json:"message"`
}

// Analyze looks for invalid and conflicting dependencies and dates among items, and returns what it
// finds in a stable order (errors first, then by item). The same items always give the same warnings.
//
// A dependent may start on the day its dependency ends; it conflicts only when it starts earlier.
// Dependencies on work outside items (another project's, one that no longer exists) are reported
// as missing.
func Analyze(items []Item) []Warning {
	byID := make(map[string]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	var out []Warning
	add := func(code Code, sev Severity, it Item, other string, format string, a ...any) {
		out = append(out, Warning{Code: code, Severity: sev, ItemID: it.ID, OtherID: other, Message: fmt.Sprintf(format, a...)})
	}
	name := func(it Item) string { return fmt.Sprintf("“%s”", it.Title) }

	for _, it := range items {
		r := it.Range
		if _, err := CleanRange(r); err != nil {
			code := CodeInvalidRange
			if (r.Start != "" && invalidDay(r.Start)) || (r.End != "" && invalidDay(r.End)) {
				code = CodeInvalidDate
			}
			add(code, SeverityError, it, "", "%s has an unusable planned date: %s", name(it), strings.TrimPrefix(err.Error(), "invalid: "))
		} else if r.Milestone && r.Start == "" {
			add(CodeMilestoneWithoutDate, SeverityWarning, it, "", "%s is a milestone with no date, so it cannot be placed on the timeline", name(it))
		}

		seen := map[string]bool{}
		for _, id := range it.Dependencies {
			if seen[id] {
				add(CodeDuplicateDependency, SeverityWarning, it, id, "%s lists the same dependency more than once", name(it))
				continue
			}
			seen[id] = true
			if id == it.ID {
				add(CodeSelfDependency, SeverityError, it, id, "%s depends on itself", name(it))
				continue
			}
			dep, ok := byID[id]
			if !ok {
				add(CodeMissingDependency, SeverityError, it, id, "%s depends on work that is not here (it may belong to another project or no longer exist)", name(it))
				continue
			}
			if dep.Archived && !dep.Done {
				add(CodeArchivedDependency, SeverityWarning, it, id, "%s depends on %s, which was closed without being finished", name(it), name(dep))
			}
			if it.Done && !dep.Done && !dep.Archived {
				add(CodeDoneBeforeDependency, SeverityWarning, it, id, "%s is done but %s, which it depends on, is not", name(it), name(dep))
			}
			out = append(out, dateConflict(it, dep)...)
		}
	}

	for _, cyc := range cycles(items) {
		first := byID[cyc[0]]
		titles := make([]string, len(cyc))
		for i, id := range cyc {
			titles[i] = name(byID[id])
		}
		out = append(out, Warning{
			Code: CodeDependencyCycle, Severity: SeverityError, ItemID: first.ID, Cycle: cyc,
			Message: "These depend on each other in a circle, so none of them can start: " + strings.Join(titles, " → ") + " → " + titles[0],
		})
	}

	rank := map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case rank[a.Severity] != rank[b.Severity]:
			return rank[a.Severity] < rank[b.Severity]
		case a.ItemID != b.ItemID:
			return a.ItemID < b.ItemID
		case a.Code != b.Code:
			return a.Code < b.Code
		}
		return a.OtherID < b.OtherID
	})
	return out
}

// AnalyzeOpen is Analyze for a board: closed (archived) items stay in the picture, since open work may depend
// on one, but are not themselves reported on.
func AnalyzeOpen(items []Item) []Warning {
	closed := make(map[string]bool, len(items))
	for _, it := range items {
		closed[it.ID] = it.Archived
	}
	out := []Warning{}
	for _, w := range Analyze(items) {
		if !closed[w.ItemID] {
			out = append(out, w)
		}
	}
	return out
}

func invalidDay(s string) bool { _, err := ParseDay(s); return err != nil }

// dateConflict compares the planned days of an item and one dependency. Work with no usable dates is
// not compared; a dependency that has none while its dependent has some is said so, once, as information.
func dateConflict(it, dep Item) []Warning {
	itFirst, _, itOK := it.Range.Span()
	_, depLast, depOK := dep.Range.Span()
	switch {
	case !itOK:
		return nil
	case !depOK:
		if dep.Done {
			return nil
		}
		return []Warning{{
			Code: CodeUnscheduledDependency, Severity: SeverityInfo, ItemID: it.ID, OtherID: dep.ID,
			Message: fmt.Sprintf("“%s” has dates but “%s”, which it depends on, has none, so the order cannot be checked", it.Title, dep.Title),
		}}
	case dep.Done:
		return nil // finished: when it was planned to end no longer constrains anything
	case itFirst.Before(depLast):
		return []Warning{{
			Code: CodeStartsBeforeDependent, Severity: SeverityWarning, ItemID: it.ID, OtherID: dep.ID,
			Message: fmt.Sprintf("“%s” starts on %s, before “%s”, which it depends on, ends on %s",
				it.Title, itFirst.Format(DayLayout), dep.Title, depLast.Format(DayLayout)),
		}}
	}
	return nil
}

// HasCycle reports whether the dependency graph (item → what it depends on) contains a cycle,
// including an item that depends on itself. Dependencies on items that are not in the graph are ignored.
func HasCycle(graph map[string][]string) bool {
	items := make([]Item, 0, len(graph))
	for id, deps := range graph {
		items = append(items, Item{ID: id, Dependencies: deps})
	}
	return len(cycles(items)) > 0
}

// cycles finds every group of items that depend on each other in a circle (the strongly connected
// components with more than one item, and items that depend on themselves, which Analyze reports
// separately and cycles leaves out unless they are part of a larger circle). Each cycle's IDs are
// sorted, and the cycles are ordered by their first ID, so the result is stable.
func cycles(items []Item) [][]string {
	ids := make(map[string]bool, len(items))
	for _, it := range items {
		ids[it.ID] = true
	}
	adj := make(map[string][]string, len(items))
	for _, it := range items {
		for _, d := range it.Dependencies {
			if ids[d] {
				adj[it.ID] = append(adj[it.ID], d)
			}
		}
	}
	order := make([]string, 0, len(items))
	for id := range ids {
		order = append(order, id)
	}
	sort.Strings(order)

	// Tarjan's algorithm, recursion unrolled so a long chain of dependencies cannot exhaust the stack.
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string
	next := 0
	type frame struct {
		id string
		i  int
	}
	for _, root := range order {
		if _, seen := index[root]; seen {
			continue
		}
		work := []frame{{id: root}}
		index[root], low[root] = next, next
		next++
		stack = append(stack, root)
		onStack[root] = true
		for len(work) > 0 {
			f := &work[len(work)-1]
			if f.i < len(adj[f.id]) {
				w := adj[f.id][f.i]
				f.i++
				if _, seen := index[w]; !seen {
					index[w], low[w] = next, next
					next++
					stack = append(stack, w)
					onStack[w] = true
					work = append(work, frame{id: w})
				} else if onStack[w] && index[w] < low[f.id] {
					low[f.id] = index[w]
				}
				continue
			}
			v := f.id
			work = work[:len(work)-1]
			if len(work) > 0 {
				p := work[len(work)-1].id
				if low[v] < low[p] {
					low[p] = low[v]
				}
			}
			if low[v] == index[v] {
				var comp []string
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				if len(comp) > 1 {
					sort.Strings(comp)
					out = append(out, comp)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

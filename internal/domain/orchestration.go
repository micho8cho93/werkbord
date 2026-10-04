package domain

import (
	"fmt"
	"path"
	"strings"
	"time"
	_ "time/tzdata"
)

// Orchestration belongs to a Task, never to a separate calendar job. Key names
// one explicitly armed, one-shot attempt. Dispatch metadata is controller-owned.
type Orchestration struct {
	Enabled        bool       `json:"enabled"`
	ScheduledAt    *time.Time `json:"scheduledAt,omitempty"`
	NotBefore      *time.Time `json:"notBefore,omitempty"`
	Deadline       *time.Time `json:"deadline,omitempty"`
	Timezone       string     `json:"timezone,omitempty"`
	ExecutionOrder *int       `json:"executionOrder,omitempty"`
	Dependencies   []string   `json:"dependencies"`
	ExpectedPaths  []string   `json:"expectedPaths"`
	TargetCommit   string     `json:"targetCommit,omitempty"`
	MissedPolicy   string     `json:"missedPolicy,omitempty"`
	GraceSeconds   int        `json:"graceSeconds,omitempty"`
	Key            string     `json:"key,omitempty"`
	DispatchedAt   *time.Time `json:"dispatchedAt,omitempty"`
	RunID          string     `json:"runId,omitempty"`
	Missed         bool       `json:"missed"`
	Error          string     `json:"error,omitempty"`
}

func (o *Orchestration) Normalize() {
	if o.Timezone == "" {
		o.Timezone = "UTC"
	}
	if o.MissedPolicy == "" {
		o.MissedPolicy = "run_late"
	}
	if o.Dependencies == nil {
		o.Dependencies = []string{}
	}
	if o.ExpectedPaths == nil {
		o.ExpectedPaths = []string{}
	}
	for _, p := range []**time.Time{&o.ScheduledAt, &o.NotBefore, &o.Deadline} {
		if *p != nil {
			t := (*p).UTC().Truncate(time.Millisecond)
			*p = &t
		}
	}
}

func (o Orchestration) Validate() error {
	if _, err := time.LoadLocation(o.Timezone); err != nil {
		return fmt.Errorf("%w: unknown IANA timezone %q", ErrInvalid, o.Timezone)
	}
	if o.MissedPolicy != "run_late" && o.MissedPolicy != "skip" {
		return fmt.Errorf("%w: missedPolicy must be run_late or skip", ErrInvalid)
	}
	if o.GraceSeconds < 0 || o.GraceSeconds > 86400*30 {
		return fmt.Errorf("%w: graceSeconds must be between 0 and 2592000", ErrInvalid)
	}
	if o.ExecutionOrder != nil && (*o.ExecutionOrder < 0 || *o.ExecutionOrder > 1000000) {
		return fmt.Errorf("%w: execution order must be between 0 and 1000000", ErrInvalid)
	}
	if len(o.Dependencies) > 100 || len(o.ExpectedPaths) > 100 {
		return fmt.Errorf("%w: at most 100 dependencies or paths", ErrInvalid)
	}
	for _, p := range o.ExpectedPaths {
		if p == "" || len(p) > 500 || strings.ContainsAny(p, "\\\n\r\x00") || strings.HasPrefix(p, "/") || p == ".." || strings.HasPrefix(p, "../") || path.Clean(p) != strings.TrimSuffix(p, "/") {
			return fmt.Errorf("%w: expected paths must be relative repository paths", ErrInvalid)
		}
	}
	if o.TargetCommit != "" {
		if len(o.TargetCommit) != 40 && len(o.TargetCommit) != 64 {
			return fmt.Errorf("%w: targetCommit must be a full commit ID", ErrInvalid)
		}
		for _, c := range o.TargetCommit {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return fmt.Errorf("%w: invalid target commit", ErrInvalid)
			}
		}
	}
	if o.Deadline != nil {
		for _, t := range []*time.Time{o.ScheduledAt, o.NotBefore} {
			if t != nil && t.After(*o.Deadline) {
				return fmt.Errorf("%w: deadline precedes earliest start", ErrInvalid)
			}
		}
	}
	return nil
}

// DependencyCycle checks the entire project graph after a proposed edit.
func DependencyCycle(tasks []Task) bool {
	graph := map[string][]string{}
	for _, t := range tasks {
		graph[t.ID] = t.Orchestration.Dependencies
	}
	seen := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if seen[id] == 1 {
			return true
		}
		if seen[id] == 2 {
			return false
		}
		seen[id] = 1
		for _, dep := range graph[id] {
			if visit(dep) {
				return true
			}
		}
		seen[id] = 2
		return false
	}
	for id := range graph {
		if visit(id) {
			return true
		}
	}
	return false
}

type SchedulingDecision struct {
	TaskID string `json:"taskId"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// Handoff is a compact, editable record of evidence. Empty evidence is reported
// as unknown, never invented from a successful exit code.
type Handoff struct {
	Objective    string    `json:"objective"`
	Summary      string    `json:"summary"`
	FilesChanged []string  `json:"filesChanged"`
	Decisions    []string  `json:"decisions"`
	Tests        []string  `json:"tests"`
	Results      string    `json:"results"`
	GitState     string    `json:"gitState"`
	KnownIssues  []string  `json:"knownIssues"`
	Blockers     []string  `json:"blockers"`
	Questions    []string  `json:"questions"`
	NextAction   string    `json:"nextAction"`
	GeneratedAt  time.Time `json:"generatedAt"`
}

func (h *Handoff) Normalize() {
	if h.FilesChanged == nil {
		h.FilesChanged = []string{}
	}
	if h.Decisions == nil {
		h.Decisions = []string{}
	}
	if h.Tests == nil {
		h.Tests = []string{}
	}
	if h.KnownIssues == nil {
		h.KnownIssues = []string{}
	}
	if h.Blockers == nil {
		h.Blockers = []string{}
	}
	if h.Questions == nil {
		h.Questions = []string{}
	}
}

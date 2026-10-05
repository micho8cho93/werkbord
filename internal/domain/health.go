package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Repository health answers one question for someone running several coding
// agents at once: "what Git state now needs my attention?". It is a list of
// findings, each of which names what is wrong, why, the evidence, and the next
// useful step. A score exists, but it is a footnote: the findings are the point.
//
// Everything here is plain data and pure rules. Nothing runs Git and nothing
// calls a model: service.GitHealth gathers facts from Git metadata and Dev
// Board's own records, and EvaluateHealth turns them into findings. That is
// what keeps a recalculation cheap, and what lets every rule be tested with a
// hand-built input.
//
// Two honesty rules run through all of it.
//
//   - Every finding says whether it rests on a DETERMINISTIC signal (a fact Git or
//     Werkbord's records state outright: a merge is unfinished, a branch has
//     commits no remote has) or a HEURISTIC one (a reasoned guess: two branches
//     touch the same files, a branch looks abandoned). A heuristic finding is
//     worded as a possibility, never as a verdict.
//   - A finding never claims more than its evidence. "These branches modify the
//     same files and may conflict" is what the file lists support;
//     "these branches conflict" is said only when Git's own in-memory merge says so.

// HealthSeverity orders findings by how much they matter. Info is housekeeping:
// listed, never counted as needing attention. The report as a whole is "healthy"
// when nothing is above info.
type HealthSeverity string

const (
	HealthInfo      HealthSeverity = "info"      // worth knowing; no action is owed
	HealthAttention HealthSeverity = "attention" // something to look at today
	HealthRisk      HealthSeverity = "risk"      // work could be lost or stranded, or Git is blocked
	HealthCritical  HealthSeverity = "critical"  // the repository is stuck in a state that blocks everything
)

// Rank orders severities, lowest first. An unknown severity ranks below info.
func (s HealthSeverity) Rank() int {
	switch s {
	case HealthInfo:
		return 1
	case HealthAttention:
		return 2
	case HealthRisk:
		return 3
	case HealthCritical:
		return 4
	}
	return 0
}

// Valid reports whether s is a known severity.
func (s HealthSeverity) Valid() bool { return s.Rank() > 0 }

// HealthBasis says how sure a signal is.
type HealthBasis string

const (
	// BasisDeterministic: Git or Werkbord's records state it outright.
	BasisDeterministic HealthBasis = "deterministic"
	// BasisHeuristic: a reasoned guess from patterns. The finding says "may".
	BasisHeuristic HealthBasis = "heuristic"
)

// HealthState is where a finding is in its life. Findings are recomputed from
// the repository each time, so "resolved" is not something a person does: a
// finding resolves when what it described is no longer true. A person can only
// dismiss one, which hides it until it gets worse or goes away and comes back.
type HealthState string

const (
	HealthOpen      HealthState = "open"
	HealthResolved  HealthState = "resolved"  // it stopped being true
	HealthDismissed HealthState = "dismissed" // the user said they know; hidden until it gets worse
)

// Valid reports whether s is a known state.
func (s HealthState) Valid() bool {
	return s == HealthOpen || s == HealthResolved || s == HealthDismissed
}

// HealthCategory groups finding types, for the screen and the documentation.
type HealthCategory string

const (
	CatUncommitted   HealthCategory = "uncommitted"
	CatUnsynced      HealthCategory = "unsynced"
	CatBranch        HealthCategory = "branch"
	CatWorktree      HealthCategory = "worktree"
	CatOrchestration HealthCategory = "orchestration"
	CatOperation     HealthCategory = "operation"
)

// HealthFindingType names one rule. Types are stable: the interface words some
// of them and docs/HEALTH.md documents each.
type HealthFindingType string

const (
	// uncommitted work
	FindUncommittedWork HealthFindingType = "uncommitted_work"

	// unsynced work
	FindUnpushedCommits     HealthFindingType = "unpushed_commits"
	FindBranchNotPushed     HealthFindingType = "branch_not_pushed"
	FindRemoteAhead         HealthFindingType = "remote_ahead"
	FindUpstreamDiverged    HealthFindingType = "upstream_diverged"
	FindRemoteBranchDeleted HealthFindingType = "remote_branch_deleted"
	FindRemoteStateStale    HealthFindingType = "remote_state_stale"

	// branch hygiene
	FindMergedBranch       HealthFindingType = "merged_branch_present"
	FindContentOnTarget    HealthFindingType = "branch_content_on_target"
	FindAbandonedBranch    HealthFindingType = "abandoned_branch"
	FindStaleBranch        HealthFindingType = "stale_branch"
	FindBranchFarBehind    HealthFindingType = "branch_far_behind"
	FindTaskDoneUnmerged   HealthFindingType = "task_done_unmerged"
	FindFinishedUnmerged   HealthFindingType = "finished_work_unmerged"
	FindMissingBranch      HealthFindingType = "missing_branch_for_task"
	FindBranchOverlap      HealthFindingType = "branch_overlap"
	FindBranchConflict     HealthFindingType = "branch_conflict"
	FindOrphanedWorktree   HealthFindingType = "orphaned_worktree"
	FindWorktreeNoRun      HealthFindingType = "worktree_without_run"
	FindWorktreeAfterDone  HealthFindingType = "worktree_retained_after_done"
	FindWorktreeMismatch   HealthFindingType = "worktree_metadata_mismatch"
	FindOperationInterrupt HealthFindingType = "operation_interrupted"
	FindUnresolvedConflict HealthFindingType = "unresolved_conflicts"
	FindAutomationBlocked  HealthFindingType = "automation_blocked"
	FindRepositoryUnread   HealthFindingType = "repository_unreadable"
)

// HealthActionKind is the next useful step a finding recommends.
type HealthActionKind string

const (
	ActReviewChanges   HealthActionKind = "review_changes"   // look at the diff or the working changes
	ActPushBranch      HealthActionKind = "push_branch"      // opens Push, which never forces
	ActSyncBranch      HealthActionKind = "sync_branch"      // bring a branch level with its remote: done in a terminal
	ActMergeBranch     HealthActionKind = "merge_branch"     // opens Merge safely, which checks first
	ActDeleteBranch    HealthActionKind = "delete_branch"    // opens Delete branch, which refuses unless nothing would be lost
	ActCleanWorktree   HealthActionKind = "clean_worktree"   // opens Clean worktree, which refuses unless nothing would be lost
	ActFinishOperation HealthActionKind = "finish_operation" // resolve or abort a merge/rebase: done in a terminal
	ActFetch           HealthActionKind = "fetch"            // update what is known about the remote
	ActCreateTask      HealthActionKind = "create_task"      // a new task on the board, prefilled
	ActAskAgent        HealthActionKind = "ask_agent"        // a new task prefilled as an investigation for an agent
	ActInspect         HealthActionKind = "inspect"          // nothing Werkbord can do; says what to check
)

// HealthEvidence is one fact behind a finding.
type HealthEvidence struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// HealthSubject says what a finding is about. Only what applies is set.
type HealthSubject struct {
	Branch       string   `json:"branch,omitempty"`
	Related      []string `json:"related,omitempty"` // other branches involved (an overlap)
	WorktreeID   string   `json:"worktreeId,omitempty"`
	WorktreePath string   `json:"worktreePath,omitempty"`
	TaskID       string   `json:"taskId,omitempty"`
	TaskTitle    string   `json:"taskTitle,omitempty"`
	RunID        string   `json:"runId,omitempty"`
}

// HealthAction is the recommended next step, and whether Werkbord can do it.
//
// CanPerform means: Werkbord has a guarded operation for this, and what it
// needs is true right now. It does not mean Werkbord will do it: nothing here
// is ever executed by the health system. The screen opens the same confirmation
// sheet a person would reach from the branch list, and the operation checks
// everything again when confirmed. When CanPerform is false, Reason says why and
// what to do instead.
type HealthAction struct {
	Kind       HealthActionKind `json:"kind"`
	Label      string           `json:"label"`
	Detail     string           `json:"detail,omitempty"`
	CanPerform bool             `json:"canPerform"`
	Reason     string           `json:"reason,omitempty"`
	// Destructive: carrying it out removes something (a branch, a worktree directory).
	Destructive bool   `json:"destructive,omitempty"`
	Branch      string `json:"branch,omitempty"`
	WorktreeID  string `json:"worktreeId,omitempty"`
	// TaskTitle and TaskDescription prefill a task for ActCreateTask and ActAskAgent.
	TaskTitle       string `json:"taskTitle,omitempty"`
	TaskDescription string `json:"taskDescription,omitempty"`
}

// HealthFinding is one thing about a project's Git state that is worth knowing.
type HealthFinding struct {
	// ID is stable for the same thing: it is derived from the project, the rule
	// and what it is about, so the same problem is the same finding on every
	// recalculation, and its DetectedAt and any dismissal carry over.
	ID          string            `json:"id"`
	ProjectID   string            `json:"projectId"`
	ProjectName string            `json:"projectName,omitempty"` // set when findings are shown outside their project
	Type        HealthFindingType `json:"type"`
	Category    HealthCategory    `json:"category"`
	Severity    HealthSeverity    `json:"severity"`
	Basis       HealthBasis       `json:"basis"`
	Title       string            `json:"title"`
	Explanation string            `json:"explanation"`
	Subject     HealthSubject     `json:"subject"`
	Evidence    []HealthEvidence  `json:"evidence"`
	Action      HealthAction      `json:"action"`

	State HealthState `json:"state"`
	// DetectedAt is when this finding was first seen in its current life: it is
	// reset if the finding resolves and later comes back.
	DetectedAt  time.Time  `json:"detectedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"` // the last recalculation that still saw it
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	DismissedAt *time.Time `json:"dismissedAt,omitempty"`
	// DismissedSeverity is how severe it was when dismissed: it comes back if it gets worse.
	DismissedSeverity HealthSeverity `json:"dismissedSeverity,omitempty"`
}

// NeedsAttention reports whether the finding counts as something to act on:
// above housekeeping, and still open.
func (f HealthFinding) NeedsAttention() bool {
	return f.State == HealthOpen && f.Severity.Rank() >= HealthAttention.Rank()
}

// HealthFindingID derives a finding's identity from what it is about. The key is
// whatever distinguishes this instance of the rule: a branch name, a worktree
// path, a pair of branches.
func HealthFindingID(projectID string, t HealthFindingType, key string) string {
	sum := sha256.Sum256([]byte(projectID + "\x00" + string(t) + "\x00" + key))
	return "hf_" + hex.EncodeToString(sum[:8])
}

// ---- summary ----

// HealthCounts are the open findings by severity.
type HealthCounts struct {
	Info      int `json:"info"`
	Attention int `json:"attention"`
	Risk      int `json:"risk"`
	Critical  int `json:"critical"`
}

// HealthSummary is what the Git screen leads with: Healthy, or what is wrong.
type HealthSummary struct {
	// State is "healthy" or the worst open severity: attention, risk or critical.
	State string `json:"state"`
	// Headline is the sentence for the top of the screen: "Healthy", or for
	// example "1 risk · 3 items need attention".
	Headline string       `json:"headline"`
	Counts   HealthCounts `json:"counts"`
	// NeedsAttention is how many open findings are above housekeeping.
	NeedsAttention int `json:"needsAttention"`
	Dismissed      int `json:"dismissed"`
	// Score is 0-100, derived from the counts. It is secondary: a number cannot
	// say what is wrong, only how much, and it is never used to rank findings.
	Score int `json:"score"`
}

// Overall health states, in addition to the severities.
const HealthHealthy = "healthy"

// SummarizeHealth counts a project's findings. Resolved and dismissed findings
// are not part of the state: only open ones are.
func SummarizeHealth(findings []HealthFinding) HealthSummary {
	var s HealthSummary
	for _, f := range findings {
		switch {
		case f.State == HealthDismissed:
			s.Dismissed++
			continue
		case f.State != HealthOpen:
			continue
		}
		switch f.Severity {
		case HealthInfo:
			s.Counts.Info++
		case HealthAttention:
			s.Counts.Attention++
		case HealthRisk:
			s.Counts.Risk++
		case HealthCritical:
			s.Counts.Critical++
		}
	}
	s.NeedsAttention = s.Counts.Attention + s.Counts.Risk + s.Counts.Critical
	s.State = HealthHealthy
	switch {
	case s.Counts.Critical > 0:
		s.State = string(HealthCritical)
	case s.Counts.Risk > 0:
		s.State = string(HealthRisk)
	case s.Counts.Attention > 0:
		s.State = string(HealthAttention)
	}
	s.Score = max(0, 100-s.Counts.Critical*35-s.Counts.Risk*15-s.Counts.Attention*5)
	s.Headline = healthHeadline(s)
	return s
}

func healthHeadline(s HealthSummary) string {
	if s.NeedsAttention == 0 {
		return "Healthy"
	}
	var parts []string
	if n := s.Counts.Critical; n > 0 {
		parts = append(parts, fmt.Sprintf("%d critical", n))
	}
	if n := s.Counts.Risk; n > 0 {
		parts = append(parts, fmt.Sprintf("%d risk%s", n, pluralES(n)))
	}
	if n := s.Counts.Attention; n > 0 {
		if n == 1 {
			parts = append(parts, "1 item needs attention")
		} else {
			parts = append(parts, fmt.Sprintf("%d items need attention", n))
		}
	}
	return strings.Join(parts, " · ")
}

func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// SortHealth orders findings for reading: the worst first, then by category so
// the like sit together, then the oldest first (it has waited longest), then by
// title for a stable order.
func SortHealth(fs []HealthFinding) {
	cats := map[HealthCategory]int{CatOperation: 0, CatOrchestration: 1, CatUncommitted: 2, CatUnsynced: 3, CatBranch: 4, CatWorktree: 5}
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() > b.Severity.Rank()
		}
		if a.Category != b.Category {
			return cats[a.Category] < cats[b.Category]
		}
		if !a.DetectedAt.Equal(b.DetectedAt) {
			return a.DetectedAt.Before(b.DetectedAt)
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.ID < b.ID
	})
}

// ---- thresholds ----

// HealthThresholds are every number a rule uses. They are named and gathered here
// so none is buried in a rule, tests can state their own, and docs/HEALTH.md can
// list them.
type HealthThresholds struct {
	// SignificantUntracked: fewer untracked files than this are not "work": a stray
	// note or scratch file must not raise a finding on its own.
	SignificantUntracked int
	// IdleDirtyRisk: uncommitted work in a worktree whose run ended this long ago
	// is at risk of being forgotten.
	IdleDirtyRisk time.Duration
	// FarBehind and VeryFarBehind: commits the target has that an unmerged branch lacks.
	FarBehind     int
	VeryFarBehind int
	// AbandonedAfter: a Werkbord branch nobody is working on, with no task waiting on it.
	AbandonedAfter time.Duration
	// FinishedUnmergedAfter: finished work that has sat unmerged this long is worth a nudge.
	FinishedUnmergedAfter time.Duration
	// WorktreeGrace: a worktree record is given this long to get its first run
	// before it is called unused (creating one and starting the run are two steps).
	WorktreeGrace time.Duration
	// StuckRemovalAfter: a removal begun and never finished.
	StuckRemovalAfter time.Duration
	// LockStaleAfter: an index.lock older than this is probably from a crashed Git.
	LockStaleAfter time.Duration
	// FetchStaleAfter: what is known of the remote is old enough to mislead.
	FetchStaleAfter time.Duration
	// MaxOverlapBranches bounds the pairwise overlap check, which is quadratic.
	MaxOverlapBranches int
}

// DefaultHealthThresholds are what Werkbord runs with.
func DefaultHealthThresholds() HealthThresholds {
	return HealthThresholds{
		SignificantUntracked:  5,
		IdleDirtyRisk:         24 * time.Hour,
		FarBehind:             25,
		VeryFarBehind:         100,
		AbandonedAfter:        3 * 24 * time.Hour,
		FinishedUnmergedAfter: 24 * time.Hour,
		WorktreeGrace:         15 * time.Minute,
		StuckRemovalAfter:     10 * time.Minute,
		LockStaleAfter:        10 * time.Minute,
		FetchStaleAfter:       7 * 24 * time.Hour,
		MaxOverlapBranches:    10,
	}
}

// ---- the rules, as documentation ----

// HealthRule documents one finding type: what it detects, how sure that is, how
// severe it can be, and what it recommends. docs/HEALTH.md is checked against
// this list by a test, so a rule cannot be added without being documented, and
// another test requires every rule to have a test.
type HealthRule struct {
	Type     HealthFindingType
	Category HealthCategory
	Basis    HealthBasis
	// Signal is what is looked at, in a sentence.
	Signal string
	// Severity is the range the rule produces, lowest to highest.
	Severity string
	Action   HealthActionKind
}

// HealthRules lists every rule.
var HealthRules = []HealthRule{
	{FindUncommittedWork, CatUncommitted, BasisDeterministic, "Staged, modified or (at least the threshold) untracked files in a checkout no agent is working in", "info–risk", ActReviewChanges},
	{FindUnpushedCommits, CatUnsynced, BasisDeterministic, "A branch has commits its upstream (as of the last fetch) lacks", "info–risk", ActPushBranch},
	{FindBranchNotPushed, CatUnsynced, BasisDeterministic, "A branch with its own commits has no upstream and no remote has those commits", "attention–risk", ActPushBranch},
	{FindRemoteAhead, CatUnsynced, BasisDeterministic, "The upstream has commits the local branch lacks (as of the last fetch)", "info–attention", ActSyncBranch},
	{FindUpstreamDiverged, CatUnsynced, BasisDeterministic, "The local branch and its upstream each have commits the other lacks", "attention–risk", ActSyncBranch},
	{FindRemoteBranchDeleted, CatUnsynced, BasisDeterministic, "The upstream branch was deleted while the local branch still has commits not in the target", "attention", ActReviewChanges},
	{FindRemoteStateStale, CatUnsynced, BasisDeterministic, "The repository never fetched, or not for a week, so every remote comparison is out of date", "info", ActFetch},
	{FindMergedBranch, CatBranch, BasisDeterministic, "A Werkbord branch whose commits are all in the target (ancestry) is still present", "info", ActDeleteBranch},
	{FindContentOnTarget, CatBranch, BasisDeterministic, "Git's in-memory merge of the branch into the target changes nothing: its content is already there (squash or rebase merge, or superseded)", "info", ActDeleteBranch},
	{FindAbandonedBranch, CatBranch, BasisHeuristic, "A Werkbord branch with no live run, no task waiting on its review, and no activity for days", "info–attention", ActReviewChanges},
	{FindStaleBranch, CatBranch, BasisDeterministic, "Unmerged work untouched for two weeks while the target moved on", "attention", ActReviewChanges},
	{FindBranchFarBehind, CatBranch, BasisDeterministic, "An unmerged branch lacks many commits the target has", "attention–risk", ActReviewChanges},
	{FindTaskDoneUnmerged, CatOrchestration, BasisDeterministic, "A task is Done but its branch has commits that are not in the target and would change it", "attention–risk", ActMergeBranch},
	{FindFinishedUnmerged, CatOrchestration, BasisHeuristic, "A run finished a day or more ago and its commits are not in the target, and nothing says they are unwanted", "attention", ActMergeBranch},
	{FindMissingBranch, CatOrchestration, BasisDeterministic, "An active task's worktree record names a branch Git does not have", "risk", ActAskAgent},
	{FindBranchOverlap, CatOrchestration, BasisHeuristic, "Two in-flight Werkbord branches change some of the same files", "attention", ActCreateTask},
	{FindBranchConflict, CatOrchestration, BasisDeterministic, "Git's in-memory merge of two in-flight branches reports conflicts", "risk", ActCreateTask},
	{FindOrphanedWorktree, CatWorktree, BasisDeterministic, "Git lists a worktree inside Werkbord's worktree directory that no Werkbord record owns", "attention", ActInspect},
	{FindWorktreeNoRun, CatWorktree, BasisDeterministic, "A Werkbord worktree record exists and no run ever used it", "attention", ActCleanWorktree},
	{FindWorktreeAfterDone, CatWorktree, BasisDeterministic, "A clean worktree is kept for a task that is Done", "info", ActCleanWorktree},
	{FindWorktreeMismatch, CatWorktree, BasisDeterministic, "A worktree record disagrees with Git: unknown to it, on another branch or detached, directory gone, or a removal never finished", "attention", ActCleanWorktree},
	{FindOperationInterrupt, CatOperation, BasisDeterministic, "A merge, rebase, cherry-pick or revert is unfinished in a checkout no agent is using", "attention–risk", ActFinishOperation},
	{FindUnresolvedConflict, CatOperation, BasisDeterministic, "Files are in conflict in a checkout no agent is using", "risk–critical", ActFinishOperation},
	{FindAutomationBlocked, CatOperation, BasisDeterministic, "Something about the repository prevents Werkbord's own operations: no usable target, a detached HEAD, a stale lock", "info–attention", ActInspect},
	{FindRepositoryUnread, CatOperation, BasisDeterministic, "Werkbord cannot read the repository at all", "risk", ActInspect},
}

// HealthRuleFor returns the documentation of a finding type.
func HealthRuleFor(t HealthFindingType) (HealthRule, bool) {
	for _, r := range HealthRules {
		if r.Type == t {
			return r, true
		}
	}
	return HealthRule{}, false
}

// HealthCheck records when a project's health was last worked out.
type HealthCheck struct {
	ProjectID  string    `json:"projectId"`
	CheckedAt  time.Time `json:"checkedAt"`
	DurationMS int64     `json:"durationMs"`
	// Error is why the last check could not read the repository, when it could not.
	Error string `json:"error,omitempty"`
}

// ---- the report ----

// HealthReport is a project's health as the Git screen shows it.
type HealthReport struct {
	ProjectID string        `json:"projectId"`
	Summary   HealthSummary `json:"summary"`
	// Findings are the open ones, worst first. Info findings (housekeeping) are
	// among them: Summary says how many of them need attention.
	Findings []HealthFinding `json:"findings"`
	// Dismissed are the ones the user said they know about, hidden until they get worse.
	Dismissed []HealthFinding `json:"dismissed"`
	// Resolved are findings that stopped being true in the last day, so "what
	// just got fixed" is visible and a fix is not silent.
	Resolved []HealthFinding `json:"resolved"`
	// Check says when this was last worked out. Nil if it never has been.
	Check *HealthCheck `json:"check,omitempty"`
}

// EventGitHealthChanged is published when a project's open findings change: one
// opened, resolved or changed severity. Recalculating without any change
// publishes nothing.
const EventGitHealthChanged EventType = "git.health_changed"

// HealthChange is the payload of EventGitHealthChanged.
type HealthChange struct {
	Summary  HealthSummary `json:"summary"`
	Opened   int           `json:"opened"`
	Resolved int           `json:"resolved"`
	Changed  int           `json:"changed"`
}

// HealthReconciliation is what a recalculation changes in the stored findings.
type HealthReconciliation struct {
	// Write lists every finding to store, in its new state.
	Write []HealthFinding
	// Opened counts findings that are new, or came back after resolving, or were
	// dismissed and got worse. Resolved counts those that stopped being true.
	// Changed counts open findings whose severity moved.
	Opened, Resolved, Changed int
}

// Any reports whether the set of open findings changed in a way a person would notice.
func (r HealthReconciliation) Any() bool { return r.Opened+r.Resolved+r.Changed > 0 }

// ReconcileHealth merges what a recalculation found (current) into what was
// stored (existing). It gives each finding its life:
//
//   - a finding seen for the first time opens, with DetectedAt now;
//   - one still true keeps its DetectedAt, however many recalculations it survives;
//   - one that stopped being true resolves, and if it later comes back it opens
//     again as a new finding (DetectedAt now);
//   - one the user dismissed stays dismissed while it is no worse than when it was
//     dismissed, comes back if it gets worse, and resolves if it goes away.
//
// Findings are matched by ID, which the rules derive from the project, the rule
// and what it is about. Resolved findings that are not seen again are left as
// they are (they are not written again), so old history is not churned.
func ReconcileHealth(now time.Time, existing, current []HealthFinding) HealthReconciliation {
	var r HealthReconciliation
	have := make(map[string]HealthFinding, len(existing))
	for _, e := range existing {
		have[e.ID] = e
	}
	seen := make(map[string]bool, len(current))
	for _, c := range current {
		seen[c.ID] = true
		c.UpdatedAt = now
		e, known := have[c.ID]
		switch {
		case !known || e.State == HealthResolved:
			c.State, c.DetectedAt = HealthOpen, now
			c.ResolvedAt, c.DismissedAt, c.DismissedSeverity = nil, nil, ""
			r.Opened++
		case e.State == HealthDismissed && c.Severity.Rank() > e.DismissedSeverity.Rank():
			c.State, c.DetectedAt = HealthOpen, e.DetectedAt
			c.ResolvedAt, c.DismissedAt, c.DismissedSeverity = nil, nil, ""
			r.Opened++
		case e.State == HealthDismissed:
			c.State, c.DetectedAt = HealthDismissed, e.DetectedAt
			c.DismissedAt, c.DismissedSeverity = e.DismissedAt, e.DismissedSeverity
			c.ResolvedAt = nil
		default: // still open
			c.State, c.DetectedAt = HealthOpen, e.DetectedAt
			c.ResolvedAt, c.DismissedAt, c.DismissedSeverity = nil, nil, ""
			if c.Severity != e.Severity {
				r.Changed++
			}
		}
		r.Write = append(r.Write, c)
	}
	for _, e := range existing {
		if seen[e.ID] || e.State == HealthResolved {
			continue
		}
		t := now
		e.State, e.ResolvedAt, e.UpdatedAt = HealthResolved, &t, now
		e.DismissedAt, e.DismissedSeverity = nil, ""
		r.Write = append(r.Write, e)
		r.Resolved++
	}
	return r
}

// DismissHealth marks an open finding as known. It does nothing to a finding
// that is not open (a resolved one cannot be dismissed: it is gone already).
func DismissHealth(f HealthFinding, now time.Time) (HealthFinding, bool) {
	if f.State != HealthOpen {
		return f, false
	}
	t := now
	f.State, f.DismissedAt, f.DismissedSeverity, f.UpdatedAt = HealthDismissed, &t, f.Severity, now
	return f, true
}

// ReopenHealth undoes a dismissal.
func ReopenHealth(f HealthFinding, now time.Time) (HealthFinding, bool) {
	if f.State != HealthDismissed {
		return f, false
	}
	f.State, f.DismissedAt, f.DismissedSeverity, f.UpdatedAt = HealthOpen, nil, "", now
	return f, true
}

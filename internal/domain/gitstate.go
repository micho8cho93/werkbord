package domain

import (
	"fmt"
	"strings"
	"time"
)

// This file describes what the Git Control Center shows. The types are plain
// data, shaped for the phone UI, and the functions are the pure rules that turn
// numbers from Git into words (is a branch merged, stale, in need of attention).
// Nothing here runs Git: gitrepo gathers the facts and service.GitControl joins
// them with tasks, runs and worktrees.
//
// A rule that runs through all of it: LOCAL state and REMOTE state are different
// things and are never merged into one. What is known about a remote comes from
// remote-tracking refs, which are only as fresh as the last fetch, and from
// GitHub, which is asked separately and may be unavailable. An action's result
// reports its local effect and its remote confirmation separately, so a change
// to the local repository is never presented as something that happened on a
// remote.

// ---- commits ----

// GitCommit is one commit, shown in lists. Message bodies are never included.
type GitCommit struct {
	SHA     string    `json:"sha"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"` // the committer date: it moves when a commit is rebased
	Merge   bool      `json:"merge,omitempty"`
}

// GitCommitPage is a bounded list of commits and how many there really are.
type GitCommitPage struct {
	Items     []GitCommit `json:"items"`
	Total     int         `json:"total"`
	Truncated bool        `json:"truncated"` // Items holds fewer than Total
}

// ---- working tree ----

// File statuses, for both the index and the working tree.
const (
	FileAdded      = "added"
	FileModified   = "modified"
	FileDeleted    = "deleted"
	FileRenamed    = "renamed"
	FileCopied     = "copied"
	FileTypeChange = "typechange"
	FileConflicted = "conflicted"
	FileUntracked  = "untracked"
)

// GitFileChange is one changed path.
type GitFileChange struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"` // for a rename or copy
	Status  string `json:"status"`
}

// GitChangeCounts are how many files are in each state. They are exact unless
// the status listing was truncated.
type GitChangeCounts struct {
	Staged     int `json:"staged"`
	Unstaged   int `json:"unstaged"`
	Untracked  int `json:"untracked"`
	Conflicted int `json:"conflicted"`
}

// Total is the number of distinct kinds of change, summed. A file that is both
// staged and modified counts twice, as it shows in two lists.
func (c GitChangeCounts) Total() int { return c.Staged + c.Unstaged + c.Untracked + c.Conflicted }

// Dirty reports whether anything is uncommitted.
func (c GitChangeCounts) Dirty() bool { return c.Total() > 0 }

// TrackedDirty reports uncommitted changes to files Git tracks: the changes a
// merge could mix its own into. Untracked files are not among them.
func (c GitChangeCounts) TrackedDirty() bool { return c.Staged+c.Unstaged+c.Conflicted > 0 }

// GitWorkingTree is the state of one checkout: which branch, and what is
// staged, modified and untracked in it.
type GitWorkingTree struct {
	Branch     string          `json:"branch"` // empty when HEAD is detached
	Head       string          `json:"head"`   // empty before the first commit
	Detached   bool            `json:"detached"`
	Upstream   string          `json:"upstream,omitempty"`
	Ahead      int             `json:"ahead"`  // of the upstream, as of the last fetch
	Behind     int             `json:"behind"` // of the upstream, as of the last fetch
	Staged     []GitFileChange `json:"staged"`
	Unstaged   []GitFileChange `json:"unstaged"`
	Untracked  []GitFileChange `json:"untracked"`
	Conflicted []GitFileChange `json:"conflicted"`
	Counts     GitChangeCounts `json:"counts"`
	Clean      bool            `json:"clean"`
	// Truncated: the lists were cut to a bounded length (Counts are then lower
	// bounds if Git's own output was cut as well).
	Truncated bool `json:"truncated"`
	// Operation is "merge", "rebase", "cherry-pick", "revert" or "bisect" while
	// one is in progress in this checkout, and empty otherwise.
	Operation string `json:"operation,omitempty"`
}

// ---- branches ----

// Branch scopes.
const (
	ScopeLocal  = "local"
	ScopeRemote = "remote"
)

// UpstreamState is how a local branch stands against its upstream. It is
// computed from the remote-tracking ref, so it is as of the last fetch.
type UpstreamState string

const (
	UpstreamNone     UpstreamState = "none"     // no upstream is configured
	UpstreamGone     UpstreamState = "gone"     // configured, but the remote branch no longer exists
	UpstreamInSync   UpstreamState = "in_sync"  // the same commit
	UpstreamAhead    UpstreamState = "ahead"    // local has commits the remote lacks
	UpstreamBehind   UpstreamState = "behind"   // the remote has commits local lacks
	UpstreamDiverged UpstreamState = "diverged" // both
)

// ClassifyUpstream names the state of a branch with an upstream.
func ClassifyUpstream(ahead, behind int) UpstreamState {
	switch {
	case ahead > 0 && behind > 0:
		return UpstreamDiverged
	case ahead > 0:
		return UpstreamAhead
	case behind > 0:
		return UpstreamBehind
	}
	return UpstreamInSync
}

// GitUpstream is a local branch's upstream.
type GitUpstream struct {
	Name   string        `json:"name,omitempty"` // e.g. origin/devboard/fix-1a2b3c
	State  UpstreamState `json:"state"`
	Ahead  int           `json:"ahead"`
	Behind int           `json:"behind"`
}

// TargetRelation is how a branch stands against the target branch.
type TargetRelation string

const (
	RelTarget TargetRelation = "target" // it is the target branch
	RelSame   TargetRelation = "same"   // the same commit as the target: nothing on it
	RelMerged TargetRelation = "merged" // everything on it is in the target, which has moved on
	// RelBehind: the same ancestry as merged, but the branch never had a commit of its own: it is
	// still where it was cut and the target has moved on. Nothing was merged, because nothing was done.
	RelBehind   TargetRelation = "behind"
	RelAhead    TargetRelation = "ahead"    // it has commits the target lacks, and the target has none it lacks
	RelDiverged TargetRelation = "diverged" // each has commits the other lacks
	RelUnknown  TargetRelation = "unknown"  // no target, or it could not be measured
)

// ClassifyRelation names a branch's position against the target from its
// commit counts: ahead is how many commits the branch has that the target does
// not, behind how many the target has that the branch does not.
func ClassifyRelation(ahead, behind int) TargetRelation {
	switch {
	case ahead == 0 && behind == 0:
		return RelSame
	case ahead == 0:
		return RelMerged
	case behind == 0:
		return RelAhead
	}
	return RelDiverged
}

// GitVsTarget is a branch measured against the target branch.
type GitVsTarget struct {
	Ahead    int            `json:"ahead"`
	Behind   int            `json:"behind"`
	Relation TargetRelation `json:"relation"`
}

// Fully merged means every commit of the branch is reachable from the target.
// It is a statement about ancestry: a branch that was squash-merged or
// rebase-merged on GitHub is NOT merged in this sense, because its commits are
// not in the target (the pull request says so separately).
func (v GitVsTarget) FullyMerged() bool {
	return v.Relation == RelSame || v.Relation == RelMerged || v.Relation == RelBehind
}

// GitBranchWorktree says where a branch is checked out.
type GitBranchWorktree struct {
	Path       string           `json:"path"`
	Primary    bool             `json:"primary"` // the repository's main checkout
	Owned      bool             `json:"owned"`   // a worktree Werkbord created and recorded
	WorktreeID string           `json:"worktreeId,omitempty"`
	Missing    bool             `json:"missing,omitempty"` // registered with Git, but the directory is gone
	Locked     bool             `json:"locked,omitempty"`
	Dirty      *GitChangeCounts `json:"dirty,omitempty"` // nil when not inspected
	Operation  string           `json:"operation,omitempty"`
}

// TaskPhase is how a task relates to its branch's life.
type TaskPhase string

const (
	PhaseNone      TaskPhase = "none"      // no task is known to own the branch
	PhaseActive    TaskPhase = "active"    // the task is in Doing, or one of its runs is live
	PhaseReview    TaskPhase = "review"    // the task is in Review and nothing is running
	PhaseCompleted TaskPhase = "completed" // the task is Done
	PhaseIdle      TaskPhase = "idle"      // the task is in Backlog with nothing running
)

// GitBranchOwnership is what Werkbord knows about a branch.
type GitBranchOwnership struct {
	// Created: Werkbord made this branch. It requires both the devboard/ name
	// and a worktree record in this project for exactly this branch, because a
	// name alone proves nothing: anyone can type devboard/ into a branch name.
	// Only such branches may ever be deleted by Werkbord.
	Created   bool `json:"created"`
	Namespace bool `json:"namespace"` // the name is under devboard/, with or without a record

	WorktreeIDs []string  `json:"worktreeIds,omitempty"`
	TaskID      string    `json:"taskId,omitempty"`
	TaskTitle   string    `json:"taskTitle,omitempty"`
	TaskState   TaskState `json:"taskState,omitempty"`
	Phase       TaskPhase `json:"phase"`
	RunID       string    `json:"runId,omitempty"` // the latest run on the branch
	RunState    RunState  `json:"runState,omitempty"`
	// RunWaiting says what a waiting latest run waits for (an answer, or the next message).
	RunWaiting WaitingKind `json:"runWaiting,omitempty"`
	AgentID    string      `json:"agentId,omitempty"`
	ActiveRun  bool        `json:"activeRun"` // some run on the branch has a live session
}

// Attention kinds and severities. Severity orders what is shown first.
const (
	AttentionReview    = "review"    // finished work waiting for a human decision
	AttentionUnpushed  = "unpushed"  // commits that exist on no remote
	AttentionDiverged  = "diverged"  // the target moved on in a way that needs a decision
	AttentionBehind    = "behind"    // the remote has commits not pulled
	AttentionCleanup   = "cleanup"   // merged and finished: can be deleted
	AttentionStale     = "stale"     // untouched while the target moved on
	AttentionDirty     = "dirty"     // uncommitted work in a worktree
	AttentionNoRemote  = "no_remote" // no upstream: nobody else has this work
	AttentionGone      = "gone"      // the upstream branch was deleted
	AttentionMissing   = "missing"   // a worktree whose directory is gone
	AttentionOperation = "operation" // a merge or rebase left unfinished

	SeverityAction = "action" // a decision is waiting for the user
	SeverityWarn   = "warn"   // something looks wrong or risky
	SeverityInfo   = "info"   // worth knowing
)

// GitAttention is one reason a branch is worth looking at.
type GitAttention struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// GitBranch is one branch, local or remote-tracking.
type GitBranch struct {
	Name       string    `json:"name"`             // local: devboard/fix-1a2b3c; remote: origin/devboard/fix-1a2b3c
	Ref        string    `json:"ref"`              // refs/heads/... or refs/remotes/...
	Scope      string    `json:"scope"`            // ScopeLocal or ScopeRemote
	Remote     string    `json:"remote,omitempty"` // the remote's name, for a remote branch
	Sha        string    `json:"sha"`
	Subject    string    `json:"subject"`
	CommitDate time.Time `json:"commitDate"` // of the tip: its age is "latest commit age"

	Head      bool   `json:"head"`                // checked out in the project's own checkout
	Target    bool   `json:"target"`              // the target branch
	Protected bool   `json:"protected"`           // never deleted by Werkbord, whoever asks
	Unusual   string `json:"unusual,omitempty"`   // why every action on it is refused: a name that cannot be passed safely
	LocalName string `json:"localName,omitempty"` // for a remote branch: the local branch of the same name

	Upstream  GitUpstream `json:"upstream"` // local branches only
	VsTarget  GitVsTarget `json:"vsTarget"`
	Merged    bool        `json:"merged"` // fully merged into the target: see GitVsTarget.FullyMerged
	NotPushed int         `json:"notPushed"`
	Stale     bool        `json:"stale"`
	StaleWhy  string      `json:"staleWhy,omitempty"`

	Worktree  *GitBranchWorktree `json:"worktree,omitempty"`
	DevBoard  GitBranchOwnership `json:"devboard"`
	Attention []GitAttention     `json:"attention"`
}

// Naming rules for what Werkbord treats as its own.
const (
	// BranchNamespace is the prefix of every branch Werkbord creates.
	BranchNamespace = "devboard/"

	// StaleAfter is how long a branch can sit untouched, while the target moves
	// on, before it is called stale.
	StaleAfter = 14 * 24 * time.Hour
)

// InDevBoardNamespace reports whether a branch name is under devboard/.
func InDevBoardNamespace(name string) bool { return strings.HasPrefix(name, BranchNamespace) }

// conventionalTargets are names that are never deleted, whatever the project's
// target is, because deleting one is a mistake in nearly every repository.
var conventionalTargets = []string{"main", "master", "trunk", "develop", "development", "dev", "release", "production", "prod", "staging"}

// ProtectedBranch reports whether a local branch may never be deleted by Dev
// Board: the target, the branch checked out in the project's checkout, and the
// usual long-lived names.
func ProtectedBranch(name, target, head string) bool {
	if name == "" || name == target || name == head {
		return true
	}
	for _, p := range conventionalTargets {
		if name == p {
			return true
		}
	}
	return strings.HasPrefix(name, "release/") || strings.HasPrefix(name, "hotfix/")
}

// StaleReason says whether a branch is stale and why. A branch is stale when it
// has work the target lacks, has not been committed to for StaleAfter, and the
// target has moved on since: it is the branch that is rotting. A merged branch
// is not stale (it is finished), and a branch on its own target is not either.
func StaleReason(rel TargetRelation, behind int, tip time.Time, now time.Time) (bool, string) {
	if rel != RelAhead && rel != RelDiverged {
		return false, ""
	}
	if behind == 0 || tip.IsZero() || now.Sub(tip) < StaleAfter {
		return false, ""
	}
	return true, fmt.Sprintf("no commits for %d days while the target moved on by %d", int(now.Sub(tip).Hours()/24), behind)
}

// BranchAttention lists why a local branch deserves a look, most pressing
// first. It is advice for ordering a phone screen, not a verdict: nothing here
// blocks an action, and every item is a plain statement of fact.
func BranchAttention(b *GitBranch) []GitAttention {
	var out []GitAttention
	add := func(kind, sev, msg string) { out = append(out, GitAttention{Kind: kind, Severity: sev, Message: msg}) }
	if b.Scope != ScopeLocal || b.Target {
		return []GitAttention{}
	}
	if w := b.Worktree; w != nil {
		if w.Operation != "" {
			add(AttentionOperation, SeverityWarn, fmt.Sprintf("a %s is unfinished in its worktree", w.Operation))
		}
		if w.Missing {
			add(AttentionMissing, SeverityWarn, "its worktree directory is gone")
		}
	}
	dirty := b.Worktree != nil && b.Worktree.Dirty != nil && b.Worktree.Dirty.Dirty()
	own := b.DevBoard

	switch {
	case (b.VsTarget.Relation == RelSame || b.VsTarget.Relation == RelBehind) && own.Created && !own.ActiveRun && own.Phase != PhaseActive:
		add(AttentionCleanup, SeverityInfo, "no commits of its own: there is nothing on it to merge")
	case b.Merged && b.VsTarget.Relation == RelMerged && own.Created && !b.Head && b.Worktree == nil && !own.ActiveRun:
		add(AttentionCleanup, SeverityAction, "merged into the target: safe to delete")
	case b.Merged && b.VsTarget.Relation == RelMerged && own.Created && !own.ActiveRun:
		add(AttentionCleanup, SeverityInfo, "merged into the target: clean up its worktree, then delete it")
	case b.VsTarget.Relation == RelDiverged && own.Created && !own.ActiveRun:
		// Still finished work to review. That the target moved on is a fact, not a problem:
		// whether the two actually conflict is what the merge check says, with Git's own simulation.
		add(AttentionReview, SeverityAction, fmt.Sprintf("%d commit%s ready to review; the target has moved on by %d", b.VsTarget.Ahead, plural(b.VsTarget.Ahead), b.VsTarget.Behind))
	case b.VsTarget.Relation == RelDiverged:
		add(AttentionDiverged, SeverityWarn, fmt.Sprintf("diverged from the target: %d ahead, %d behind", b.VsTarget.Ahead, b.VsTarget.Behind))
	case b.VsTarget.Relation == RelAhead && own.Created && !own.ActiveRun:
		add(AttentionReview, SeverityAction, fmt.Sprintf("%d commit%s ready to review", b.VsTarget.Ahead, plural(b.VsTarget.Ahead)))
	}
	if dirty {
		add(AttentionDirty, SeverityWarn, fmt.Sprintf("%d uncommitted change%s in its worktree", b.Worktree.Dirty.Total(), plural(b.Worktree.Dirty.Total())))
	}
	switch b.Upstream.State {
	case UpstreamAhead, UpstreamDiverged:
		add(AttentionUnpushed, SeverityWarn, fmt.Sprintf("%d commit%s not pushed", b.Upstream.Ahead, plural(b.Upstream.Ahead)))
	case UpstreamBehind:
		add(AttentionBehind, SeverityInfo, fmt.Sprintf("the remote has %d commit%s you do not", b.Upstream.Behind, plural(b.Upstream.Behind)))
	case UpstreamGone:
		add(AttentionGone, SeverityWarn, "its remote branch was deleted")
	case UpstreamNone:
		if b.NotPushed > 0 && !b.Merged {
			add(AttentionNoRemote, SeverityWarn, fmt.Sprintf("%d commit%s exist on no remote", b.NotPushed, plural(b.NotPushed)))
		}
	}
	if b.Stale {
		add(AttentionStale, SeverityWarn, b.StaleWhy)
	}
	if out == nil {
		return []GitAttention{}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// AttentionRank orders branches for the "needs attention" list: the larger the
// worse. Werkbord's own branches come first, then the most pressing reason,
// and the caller breaks ties by recency.
func AttentionRank(b *GitBranch) int {
	rank := 0
	if b.DevBoard.Created {
		rank += 1000
	} else if b.DevBoard.Namespace {
		rank += 500
	}
	best := 0
	for _, a := range b.Attention {
		switch a.Severity {
		case SeverityAction:
			best = max(best, 300)
		case SeverityWarn:
			best = max(best, 200)
		case SeverityInfo:
			best = max(best, 100)
		}
	}
	return rank + best
}

// ---- worktrees ----

// GitWorktree is a worktree of the repository as Git lists it, joined with what
// Werkbord knows.
type GitWorktree struct {
	Path       string           `json:"path"`
	Head       string           `json:"head,omitempty"`
	Branch     string           `json:"branch,omitempty"` // empty when detached
	Detached   bool             `json:"detached"`
	Primary    bool             `json:"primary"`
	Locked     bool             `json:"locked,omitempty"`
	Prunable   bool             `json:"prunable,omitempty"`
	Missing    bool             `json:"missing,omitempty"`
	Owned      bool             `json:"owned"`
	WorktreeID string           `json:"worktreeId,omitempty"`
	Dirty      *GitChangeCounts `json:"dirty,omitempty"`
	Operation  string           `json:"operation,omitempty"`
	TaskID     string           `json:"taskId,omitempty"`
	TaskTitle  string           `json:"taskTitle,omitempty"`
	RunID      string           `json:"runId,omitempty"`
	RunState   RunState         `json:"runState,omitempty"`
	ActiveRun  bool             `json:"activeRun"`
}

// ---- overview ----

// GitTarget is the branch work is measured against and merged into.
type GitTarget struct {
	Name        string `json:"name"`   // e.g. main; empty when none could be determined
	Source      string `json:"source"` // how it was found: origin/HEAD, init.defaultBranch, convention, current branch, none
	Sha         string `json:"sha,omitempty"`
	LocalExists bool   `json:"localExists"` // false when only origin/<name> exists: nothing can be merged into it here
	CheckedOut  string `json:"checkedOut,omitempty"`
	// Upstream is the local target against its remote-tracking branch, as of the last fetch.
	Upstream GitUpstream `json:"upstream"`
}

// GitHead is the project's own checkout.
type GitHead struct {
	Branch   string `json:"branch"`
	Commit   string `json:"commit"`
	Subject  string `json:"subject,omitempty"`
	Detached bool   `json:"detached"`
	Unborn   bool   `json:"unborn"` // no commits yet
}

// GitLocal is everything that is true on this computer.
type GitLocal struct {
	Name          string         `json:"name"` // the repository's name
	RootPath      string         `json:"rootPath"`
	Head          GitHead        `json:"head"`
	Target        GitTarget      `json:"target"`
	WorkingTree   GitWorkingTree `json:"workingTree"`
	RecentCommits []GitCommit    `json:"recentCommits"`
}

// GitSync is the current branch against the remote-tracking refs: what is
// unpushed and what is not pulled. It is derived from refs that only a fetch
// updates, so it says nothing about what the remote has right now.
type GitSync struct {
	Branch    string        `json:"branch"`
	Upstream  GitUpstream   `json:"upstream"`
	NotPushed GitCommitPage `json:"notPushed"`
	NotPulled GitCommitPage `json:"notPulled"`
	// Basis explains how NotPushed was found, since it differs with and without an upstream.
	Basis string `json:"basis"`
}

// GitRemoteInfo is what is known of the remotes, with how old that knowledge is.
type GitRemoteInfo struct {
	Remotes []GitRemote `json:"remotes"`
	// LastFetchedAt is when this repository last fetched from any remote. Everything
	// below about remote branches is as of then. Nil if it never has.
	LastFetchedAt *time.Time `json:"lastFetchedAt,omitempty"`
	Sync          *GitSync   `json:"sync,omitempty"` // nil when HEAD is detached or unborn
	GitHub        GitHubRepo `json:"github"`
}

// GitHubRepo is whether the repository looks like a GitHub one. It is read from
// the remote URL alone: nothing was asked of GitHub.
type GitHubRepo struct {
	Detected bool   `json:"detected"`
	Host     string `json:"host,omitempty"`
	Repo     string `json:"repo,omitempty"` // owner/name
	Remote   string `json:"remote,omitempty"`
}

// GitSummary counts what needs the user.
type GitSummary struct {
	Branches   int `json:"branches"`   // local branches
	DevBoard   int `json:"devboard"`   // owned by Werkbord
	NeedsYou   int `json:"needsYou"`   // branches with an action-level reason
	Warnings   int `json:"warnings"`   // branches with a warning-level reason
	Mergeable  int `json:"mergeable"`  // owned, ahead of the target, not running
	Cleanup    int `json:"cleanup"`    // owned and merged
	Unpushed   int `json:"unpushed"`   // branches with commits on no remote
	Worktrees  int `json:"worktrees"`  // worktrees besides the main checkout
	DirtyTrees int `json:"dirtyTrees"` // worktrees with uncommitted work
}

// GitOverview is the Git Control Center's first screen, for one project.
type GitOverview struct {
	ProjectID   string        `json:"projectId"`
	GeneratedAt time.Time     `json:"generatedAt"`
	Local       GitLocal      `json:"local"`
	Remote      GitRemoteInfo `json:"remote"`
	// Branches are every local branch, then the remote branches no local branch
	// tracks, ordered with the ones needing attention first.
	Branches  []GitBranch   `json:"branches"`
	Worktrees []GitWorktree `json:"worktrees"`
	Summary   GitSummary    `json:"summary"`
	// Notes are things the user should know about how complete this picture is.
	Notes []string `json:"notes,omitempty"`
}

// ---- comparison ----

// GitDiffFile is one file changed between two commits.
type GitDiffFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

// GitComparison is what a branch would bring to a target. The diff is the one
// GitHub shows for a pull request: from the point the branch left the target to
// the branch's tip, so changes made on the target since do not appear in it.
type GitComparison struct {
	Branch     string         `json:"branch"`
	BranchSha  string         `json:"branchSha"`
	Target     string         `json:"target"` // the comparison target
	TargetSha  string         `json:"targetSha"`
	MergeBase  string         `json:"mergeBase,omitempty"`
	Basis      string         `json:"basis"` // what the diff is taken against, in words
	Ahead      int            `json:"ahead"`
	Behind     int            `json:"behind"`
	Relation   TargetRelation `json:"relation"`
	Unique     GitCommitPage  `json:"unique"`  // commits on the branch that the target lacks
	Missing    GitCommitPage  `json:"missing"` // commits on the target that the branch lacks
	Files      []GitDiffFile  `json:"files"`
	FilesTotal int            `json:"filesTotal"`
	Offset     int            `json:"offset"`
	Limit      int            `json:"limit"`
	Additions  int            `json:"additions"` // over all files, not only this page
	Deletions  int            `json:"deletions"`
	Binary     int            `json:"binaryFiles"`
	Truncated  bool           `json:"truncated"` // the file list itself was cut at the hard limit
}

// GitFileDiff is a window onto one file's unified diff. Large diffs are never
// returned whole: the caller pages with Offset until HasMore is false.
type GitFileDiff struct {
	Path       string `json:"path"`
	OldPath    string `json:"oldPath,omitempty"`
	Status     string `json:"status,omitempty"`
	Binary     bool   `json:"binary"`
	Additions  int    `json:"additions"`
	Deletions  int    `json:"deletions"`
	Diff       string `json:"diff"`   // unified diff text for this window
	Offset     int    `json:"offset"` // first line of the window
	Lines      int    `json:"lines"`  // lines in the window
	TotalLines int    `json:"totalLines"`
	HasMore    bool   `json:"hasMore"`
	// Truncated: Git's output was cut at the hard limit, so TotalLines is a lower bound
	// and the end of the diff cannot be shown.
	Truncated bool `json:"truncated"`
}

// ---- actions ----

// Outcomes of an action.
const (
	OutcomeDone        = "done"        // it happened and, where there is a remote, was confirmed
	OutcomeRefused     = "refused"     // a safety check stopped it before anything changed
	OutcomeNoop        = "noop"        // there was nothing to do
	OutcomeConflict    = "conflict"    // a merge stopped on conflicts; it was undone
	OutcomeRejected    = "rejected"    // the remote refused it
	OutcomeUnavailable = "unavailable" // a remote or tool could not be reached
	OutcomeAuth        = "auth_failed" // the remote did not accept the credentials
	OutcomeFailed      = "failed"      // Git failed for another reason; what changed, if anything, is stated
	// OutcomeUnverified: the command reported success but the remote did not confirm it
	// when asked. It may have worked; it is not reported as having worked.
	OutcomeUnverified = "unverified"
)

// Blocker codes. They are stable: the UI words some of them itself.
const (
	BlockInvalidBranch   = "invalid_branch"
	BlockBranchMissing   = "branch_missing"
	BlockBranchMoved     = "branch_moved"
	BlockTargetMissing   = "target_missing"
	BlockTargetMoved     = "target_moved"
	BlockTargetMismatch  = "target_mismatch"
	BlockIsTarget        = "is_target"
	BlockProtected       = "protected"
	BlockNotOwned        = "not_owned"
	BlockNotMerged       = "not_merged"
	BlockAlreadyMerged   = "already_merged"
	BlockUnrelated       = "unrelated_histories"
	BlockNotFastForward  = "not_fast_forward"
	BlockNotCheckedOut   = "target_not_checked_out"
	BlockDirty           = "target_dirty"
	BlockOperation       = "operation_in_progress"
	BlockUntrackedClash  = "untracked_clash"
	BlockRunActive       = "run_active"
	BlockConflicts       = "conflicts"
	BlockCheckedOut      = "checked_out"
	BlockNoRemote        = "no_remote"
	BlockAmbiguousRemote = "ambiguous_remote"
	BlockDetached        = "detached_head"
	BlockWorktreeDirty   = "worktree_dirty"
	BlockWorktreeLocked  = "worktree_locked"
	BlockWorktreeChanged = "worktree_changed"
	BlockWorktreeUnknown = "worktree_unknown"
	BlockWorktreeOutside = "worktree_outside_root"
	BlockRemoteMoved     = "remote_moved"
	BlockRemoteUnmerged  = "remote_unmerged"
	BlockNotPushed       = "not_pushed"
	BlockRemoteDiffers   = "remote_differs"
	BlockPRExists        = "pr_exists"
	BlockNoGitHub        = "no_github"
	BlockInvalidInput    = "invalid_input"
	BlockUnverifiable    = "unverifiable"
	BlockBehindRemote    = "behind_remote"
)

// GitBlocker is one reason an action will not proceed.
type GitBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// GitConflictCheck is what is known about whether a merge would conflict, and
// how that is known. The method matters: a simulation is Git's own merge run
// without touching anything and is as sure as an answer about a moving target
// can be; an overlap check only notices that both sides edited the same files,
// which is a hint and never a verdict.
type GitConflictCheck struct {
	// Method: "simulation" (git merge-tree), "overlap" (a heuristic) or "none"
	// (it could not be checked).
	Method string `json:"method"`
	// Result: "clean", "conflicts", "possible" (overlap only) or "unknown".
	Result string   `json:"result"`
	Files  []string `json:"files,omitempty"`
	Note   string   `json:"note"`
}

// Conflict-check methods and results.
const (
	CheckSimulation = "simulation"
	CheckOverlap    = "overlap"
	CheckNone       = "none"

	ConflictClean     = "clean"
	ConflictConflicts = "conflicts"
	ConflictPossible  = "possible"
	ConflictUnknown   = "unknown"
)

// Merge strategies.
const (
	MergeCommit      = "merge"   // always make a merge commit (--no-ff)
	MergeFastForward = "ff-only" // only move the target forward; never make a commit
)

// ValidateMergeStrategy checks a strategy from outside the process.
func ValidateMergeStrategy(s string) (string, error) {
	switch s {
	case "":
		return MergeCommit, nil
	case MergeCommit, MergeFastForward:
		return s, nil
	}
	return "", fmt.Errorf("%w: unknown merge strategy %q", ErrInvalid, s)
}

// GitMergePlan is everything a merge needs checked, and the verdict. Planning
// changes nothing; the merge itself plans again, under a lock, and proceeds
// only if the plan has no blockers and the commits are still the ones the user saw.
type GitMergePlan struct {
	Branch          string           `json:"branch"`
	BranchSha       string           `json:"branchSha"`
	Target          string           `json:"target"`
	TargetSha       string           `json:"targetSha"`
	Strategy        string           `json:"strategy"`
	TargetWorktree  string           `json:"targetWorktree,omitempty"` // where the merge would run
	Ahead           int              `json:"ahead"`
	Behind          int              `json:"behind"`
	Relation        TargetRelation   `json:"relation"`
	FastForwardable bool             `json:"fastForwardable"` // the target is an ancestor of the branch
	FilesChanged    int              `json:"filesChanged"`
	Additions       int              `json:"additions"`
	Deletions       int              `json:"deletions"`
	Conflicts       GitConflictCheck `json:"conflicts"`
	Blockers        []GitBlocker     `json:"blockers"`
	Warnings        []string         `json:"warnings"`
	CanMerge        bool             `json:"canMerge"`
	CheckedAt       time.Time        `json:"checkedAt"`
}

// GitDeletePlan is the verdict on deleting a branch.
type GitDeletePlan struct {
	Branch    string `json:"branch"`
	BranchSha string `json:"branchSha"`
	Target    string `json:"target"`
	Merged    bool   `json:"merged"`
	// MergedVia says what established "merged": "ancestry" (every commit is in the
	// target) or "pull_request" (GitHub says a pull request with exactly this tip was merged).
	MergedVia   string       `json:"mergedVia,omitempty"`
	RemoteExist bool         `json:"remoteExists"`
	RemoteRef   string       `json:"remoteRef,omitempty"`
	RemoteSha   string       `json:"remoteSha,omitempty"`
	Blockers    []GitBlocker `json:"blockers"`
	Warnings    []string     `json:"warnings"`
	CanDelete   bool         `json:"canDelete"`
	// CanDeleteRemote: the remote branch may be deleted too, on the same evidence.
	CanDeleteRemote bool      `json:"canDeleteRemote"`
	CheckedAt       time.Time `json:"checkedAt"`
}

// GitCleanPlan is the verdict on removing a Werkbord worktree.
type GitCleanPlan struct {
	WorktreeID string           `json:"worktreeId"`
	Path       string           `json:"path"`
	Branch     string           `json:"branch"`
	Head       string           `json:"head,omitempty"`
	Missing    bool             `json:"missing"`
	Dirty      *GitChangeCounts `json:"dirty,omitempty"`
	Blockers   []GitBlocker     `json:"blockers"`
	Warnings   []string         `json:"warnings"`
	CanClean   bool             `json:"canClean"`
	CheckedAt  time.Time        `json:"checkedAt"`
}

// GitLocalEffect is what an action changed in the local repository.
type GitLocalEffect struct {
	Ref    string `json:"ref,omitempty"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Note   string `json:"note,omitempty"`
}

// GitRemoteEffect is what the remote confirms. Verified is true only when the
// remote itself was asked afterwards and agreed; a command that exited with
// success is not confirmation.
type GitRemoteEffect struct {
	Remote    string    `json:"remote,omitempty"`
	Ref       string    `json:"ref,omitempty"`
	Sha       string    `json:"sha,omitempty"` // what the remote reports, when asked
	Verified  bool      `json:"verified"`
	CheckedAt time.Time `json:"checkedAt"`
	Note      string    `json:"note,omitempty"`
}

// GitActionResult is the answer to every action. OK is true only for
// OutcomeDone. Local and Remote are separate on purpose: a push that changed
// local tracking refs but could not be confirmed on the remote has Local set
// and Remote.Verified false.
type GitActionResult struct {
	Action      string           `json:"action"`
	Outcome     string           `json:"outcome"`
	OK          bool             `json:"ok"`
	Message     string           `json:"message"`
	Blockers    []GitBlocker     `json:"blockers,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
	Local       *GitLocalEffect  `json:"local,omitempty"`
	Remote      *GitRemoteEffect `json:"remote,omitempty"`
	PullRequest *GitHubPR        `json:"pullRequest,omitempty"`
	// Git is the (redacted) error output of the command that failed, for the user to read.
	Git string `json:"git,omitempty"`
}

// ---- GitHub ----

// GitHubChecks summarises a pull request's checks.
type GitHubChecks struct {
	State   string `json:"state"` // passing, failing, pending, none
	Total   int    `json:"total"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Pending int    `json:"pending"`
}

// GitHubPR is a pull request, as GitHub reports it.
type GitHubPR struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"` // open, closed, merged
	Draft      bool   `json:"draft"`
	HeadBranch string `json:"headBranch"`
	BaseBranch string `json:"baseBranch"`
	HeadSha    string `json:"headSha,omitempty"`
	Author     string `json:"author,omitempty"`
	// CrossRepo: the head is a branch of a fork, so its name says nothing about
	// a branch of this repository.
	CrossRepo bool `json:"crossRepo,omitempty"`
	// Review is approved, changes_requested, review_required, or empty when the
	// repository does not require review (or GitHub did not say).
	Review string `json:"review,omitempty"`
	// Mergeable is mergeable or conflicting, or empty: GitHub computes it lazily
	// and often has not, and an unknown is never shown as a yes.
	Mergeable string       `json:"mergeable,omitempty"`
	Checks    GitHubChecks `json:"checks"`
	CreatedAt *time.Time   `json:"createdAt,omitempty"`
	UpdatedAt *time.Time   `json:"updatedAt,omitempty"`
	MergedAt  *time.Time   `json:"mergedAt,omitempty"`
	ClosedAt  *time.Time   `json:"closedAt,omitempty"`

	// What Werkbord knows about the branch, when the head is one of this project's.
	TaskID    string `json:"taskId,omitempty"`
	TaskTitle string `json:"taskTitle,omitempty"`
	RunID     string `json:"runId,omitempty"`
}

// GitHubState is what GitHub says about a project's repository. It is asked for
// separately from the local picture and can fail without affecting it.
type GitHubState struct {
	Available    bool       `json:"available"`
	Reason       string     `json:"reason,omitempty"` // no_remote, not_github, gh_missing, unauthenticated, error
	Message      string     `json:"message,omitempty"`
	Host         string     `json:"host,omitempty"`
	Repo         string     `json:"repo,omitempty"`
	PullRequests []GitHubPR `json:"pullRequests"`
	FetchedAt    time.Time  `json:"fetchedAt"`
}

// GitHub unavailability reasons.
const (
	GHNoRemote        = "no_remote"
	GHNotGitHub       = "not_github"
	GHMissing         = "gh_missing"
	GHUnauthenticated = "unauthenticated"
	GHError           = "error"
)

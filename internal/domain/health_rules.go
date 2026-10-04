package domain

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// HealthInput is everything the rules look at. The service gathers it from Git
// metadata and Dev Board's records; the rules themselves run no command and
// read no file, so each can be tested with a hand-built input.
type HealthInput struct {
	Now        time.Time
	Thresholds HealthThresholds
	ProjectID  string
	// Overview is the project's Git picture. Nil when the repository could not be
	// read, in which case ReadError says why and only that is reported.
	Overview  *GitOverview
	ReadError string

	// WorktreeRoot is the directory Dev Board creates worktrees in. Empty if unknown.
	WorktreeRoot string
	// Records are every worktree record of the project, removed ones included.
	Records []Worktree
	// RunsByWorktree are the runs that used each worktree, oldest first.
	RunsByWorktree map[string][]Run
	Tasks          map[string]Task

	// Overlaps are pairs of in-flight Dev Board branches that change some of the
	// same files, as found by the service (see BranchOverlap).
	Overlaps []BranchOverlap
	// OnTarget says, per local branch, whether merging it into the target would
	// change nothing. Only branches the service measured are present.
	OnTarget map[string]TargetContent
	// IndexLock is the modification time of the repository's index.lock, when it exists.
	IndexLock *time.Time
}

// BranchOverlap is a pair of in-flight branches whose changes touch the same
// files. Files holds the shared paths, with generated and lock files that
// every branch tends to touch already left out.
type BranchOverlap struct {
	A, B  string
	Files []string
	// Uncommitted: part of the overlap is uncommitted work, which Git cannot merge-test.
	Uncommitted bool
	// Simulated: Git merged the two branches' commits in memory. Only then are
	// Clean and Conflicts meaningful.
	Simulated     bool
	Conflicts     bool
	ConflictFiles []string
}

// TargetContent is the result of merging one branch into the target in memory.
type TargetContent struct {
	// Checked: Git ran the simulation. False when it could not (an old Git, unrelated histories).
	Checked bool
	// Unchanged: the merged result is exactly the target's tree, so the branch adds
	// nothing to the target.
	Unchanged bool
}

// EvaluateHealth runs every rule over the input and returns the findings it
// supports, worst first. It never calls Git, never reads a file, and never
// performs anything: it only describes.
//
// The stance is conservative. A rule fires on what its evidence supports and no
// more, skips a branch an agent is working on (a branch in motion is not a
// problem), skips what could not be measured, and keeps housekeeping (info)
// apart from what needs a person (attention and above).
func EvaluateHealth(in HealthInput) []HealthFinding {
	if in.Thresholds == (HealthThresholds{}) {
		in.Thresholds = DefaultHealthThresholds()
	}
	c := newHealthCtx(in)
	if in.Overview == nil {
		c.unreadable()
		return c.finish()
	}
	c.uncommitted()
	c.operations()
	c.automation()
	c.unsynced()
	c.branches()
	c.orchestration()
	c.worktrees()
	return c.finish()
}

type healthCtx struct {
	in  HealthInput
	o   *GitOverview
	th  HealthThresholds
	now time.Time
	out []HealthFinding

	local   map[string]*GitBranch
	records map[string]Worktree     // active records by id
	wtByID  map[string]*GitWorktree // Git's worktrees that a record owns, by record id
	primary *GitWorktree
	// emitted remembers which branches already have a hygiene finding, so one
	// branch does not collect three findings that say the same thing.
	emitted map[string]HealthFindingType
}

func newHealthCtx(in HealthInput) *healthCtx {
	c := &healthCtx{
		in: in, o: in.Overview, th: in.Thresholds, now: in.Now,
		local: map[string]*GitBranch{}, records: map[string]Worktree{}, wtByID: map[string]*GitWorktree{},
		emitted: map[string]HealthFindingType{},
	}
	for _, w := range in.Records {
		if w.State == WorktreeActive {
			c.records[w.ID] = w
		}
	}
	if c.o == nil {
		return c
	}
	for i := range c.o.Branches {
		if b := &c.o.Branches[i]; b.Scope == ScopeLocal {
			c.local[b.Name] = b
		}
	}
	for i := range c.o.Worktrees {
		w := &c.o.Worktrees[i]
		if w.Primary {
			c.primary = w
		}
		if w.WorktreeID != "" {
			c.wtByID[w.WorktreeID] = w
		}
	}
	return c
}

// add stamps a finding with its identity, category and life, and keeps it.
func (c *healthCtx) add(t HealthFindingType, key string, f HealthFinding) {
	rule, ok := HealthRuleFor(t)
	if !ok {
		panic("health: finding type without a rule: " + string(t))
	}
	f.Type, f.Category = t, rule.Category
	if f.Basis == "" {
		f.Basis = rule.Basis
	}
	f.ID = HealthFindingID(c.in.ProjectID, t, key)
	f.ProjectID = c.in.ProjectID
	f.State, f.DetectedAt, f.UpdatedAt = HealthOpen, c.now, c.now
	if f.Evidence == nil {
		f.Evidence = []HealthEvidence{}
	}
	if !f.Severity.Valid() {
		panic("health: finding without a severity: " + string(t))
	}
	c.out = append(c.out, f)
}

func (c *healthCtx) finish() []HealthFinding {
	SortHealth(c.out)
	if c.out == nil {
		return []HealthFinding{}
	}
	return c.out
}

// ---- small helpers ----

func ev(label, format string, args ...any) HealthEvidence {
	return HealthEvidence{Label: label, Value: fmt.Sprintf(format, args...)}
}

func n(count int, one, many string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", count, many)
}

// ago words a duration for a person: "20 minutes", "5 hours", "3 days".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return n(int(d.Minutes()), "minute", "minutes")
	case d < 48*time.Hour:
		return n(int(d.Hours()), "hour", "hours")
	}
	return n(int(d.Hours()/24), "day", "days")
}

func countsPhrase(c GitChangeCounts) string {
	var parts []string
	if c.Staged > 0 {
		parts = append(parts, fmt.Sprintf("%d staged", c.Staged))
	}
	if c.Unstaged > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", c.Unstaged))
	}
	if c.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", c.Untracked))
	}
	if c.Conflicted > 0 {
		parts = append(parts, fmt.Sprintf("%d in conflict", c.Conflicted))
	}
	return strings.Join(parts, ", ")
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// owned: a local branch Dev Board created (its name and a record agree), that is
// not the target. These are the branches the health rules are about; the
// user's own branches are theirs to manage.
func ownedBranch(b *GitBranch) bool {
	return b.Scope == ScopeLocal && !b.Target && b.DevBoard.Created && b.Unusual == ""
}

// busy: an agent has a live session on the branch, so what looks unfinished is
// work in motion and not a problem.
func busy(b *GitBranch) bool { return b.DevBoard.ActiveRun }

// measured: the branch could be compared with the target. Without that a rule
// would be guessing from nothing, so it stays silent.
func measured(b *GitBranch) bool {
	return b.VsTarget.Relation != RelUnknown && b.VsTarget.Relation != ""
}

// ownedBranches returns the owned branches in a stable order.
func (c *healthCtx) ownedBranches() []*GitBranch {
	var out []*GitBranch
	for _, b := range c.local {
		if ownedBranch(b) {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// runsOf returns the runs that used the branch's worktrees, oldest first.
func (c *healthCtx) runsOf(b *GitBranch) []Run {
	var out []Run
	for _, id := range b.DevBoard.WorktreeIDs {
		out = append(out, c.in.RunsByWorktree[id]...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func latestRun(runs []Run) *Run {
	if len(runs) == 0 {
		return nil
	}
	return &runs[len(runs)-1]
}

// settled is when the run last did anything: when it ended, else when it last changed.
func settled(r *Run) time.Time {
	if r.EndedAt != nil {
		return *r.EndedAt
	}
	return r.UpdatedAt
}

// lastActivity is the latest of the branch's tip and its last run.
func (c *healthCtx) lastActivity(b *GitBranch) time.Time {
	t := b.CommitDate
	if r := latestRun(c.runsOf(b)); r != nil && settled(r).After(t) {
		t = settled(r)
	}
	return t
}

// subjectOf fills what is known of a branch's task and run.
func (c *healthCtx) subjectOf(b *GitBranch) HealthSubject {
	s := HealthSubject{Branch: b.Name, TaskID: b.DevBoard.TaskID, TaskTitle: b.DevBoard.TaskTitle, RunID: b.DevBoard.RunID}
	if len(b.DevBoard.WorktreeIDs) > 0 {
		s.WorktreeID = b.DevBoard.WorktreeIDs[len(b.DevBoard.WorktreeIDs)-1]
	}
	if b.Worktree != nil {
		s.WorktreePath = b.Worktree.Path
		if b.Worktree.WorktreeID != "" {
			s.WorktreeID = b.Worktree.WorktreeID
		}
	}
	return s
}

func taskEv(b *GitBranch) HealthEvidence { return ev("Task", "%s", orNone(taskLine(b))) }

func taskLine(b *GitBranch) string {
	if b.DevBoard.TaskTitle == "" {
		return ""
	}
	return fmt.Sprintf("%q (%s)", b.DevBoard.TaskTitle, b.DevBoard.TaskState)
}

// ---- action builders ----
//
// Each says whether Dev Board can do the step. They mirror the preconditions of
// the guarded operations (see service.GitControl), but they can only use what the
// overview knows: the operation itself looks again and refuses if anything has changed.

func reviewBranch(label, detail string, b *GitBranch) HealthAction {
	a := HealthAction{Kind: ActReviewChanges, Label: label, Detail: detail, CanPerform: true}
	if b != nil {
		a.Branch = b.Name
	}
	return a
}

func reviewWorktree(label, detail string, wtID string) HealthAction {
	return HealthAction{Kind: ActReviewChanges, Label: label, Detail: detail, CanPerform: true, WorktreeID: wtID}
}

// askAgent recommends having an agent look into something puzzling. All Dev Board does is add
// a prefilled task to the board: an agent runs only when a person presses Run on it, so
// recalculating health never costs a model call.
func askAgent(taskTitle, taskDescription string) HealthAction {
	return HealthAction{Kind: ActAskAgent, Label: "Ask an agent to investigate", CanPerform: true,
		Detail:    "Adds a task describing this to the board. Nothing runs until you press Run on it.",
		TaskTitle: taskTitle, TaskDescription: taskDescription}
}

func terminalOnly(kind HealthActionKind, label, detail, reason string) HealthAction {
	return HealthAction{Kind: kind, Label: label, Detail: detail, CanPerform: false, Reason: reason}
}

// pushable says whether Push would be offered for the branch, and if not why.
func (c *healthCtx) pushAction(b *GitBranch, label string) HealthAction {
	a := HealthAction{Kind: ActPushBranch, Label: label, Branch: b.Name,
		Detail: "Opens Push. It never forces: a remote that has moved on makes it stop."}
	fail := func(why string) HealthAction {
		a.CanPerform, a.Reason = false, why
		return a
	}
	switch {
	case b.Unusual != "":
		return fail("the branch name cannot be passed safely to Git")
	case len(c.o.Remote.Remotes) == 0:
		return fail("this repository has no remote")
	case b.Upstream.State == UpstreamDiverged || b.Upstream.State == UpstreamBehind:
		return fail("the remote has commits this branch lacks, so a plain push would be rejected; Dev Board never forces")
	case b.Upstream.Name != "" && !strings.HasSuffix(b.Upstream.Name, "/"+b.Name):
		return fail("the branch tracks " + b.Upstream.Name + ", a branch of a different name; push it from your terminal")
	case b.Upstream.Name == "" && len(c.o.Remote.Remotes) > 1 && !hasRemote(c.o.Remote.Remotes, "origin"):
		return fail("there are several remotes and none is called origin")
	}
	a.CanPerform = true
	return a
}

func hasRemote(rs []GitRemote, name string) bool {
	for _, r := range rs {
		if r.Name == name {
			return true
		}
	}
	return false
}

// mergeAction says whether Merge safely would be offered. Conflicts are not
// known here: the merge check tests for them with Git before anything happens.
func (c *healthCtx) mergeAction(b *GitBranch, label string) HealthAction {
	a := HealthAction{Kind: ActMergeBranch, Label: label, Branch: b.Name,
		Detail: "Opens Merge. It asks Git whether the merge would conflict before doing anything, and never pushes."}
	fail := func(why string) HealthAction {
		a.CanPerform, a.Reason = false, why
		return a
	}
	t := c.o.Local.Target
	switch {
	case t.Name == "":
		return fail("no target branch could be determined")
	case !t.LocalExists:
		return fail(t.Name + " exists only on the remote here; check it out first")
	case t.CheckedOut == "":
		return fail(t.Name + " is not checked out anywhere, and Git can only merge into a checked-out branch")
	case busy(b):
		return fail("an agent is still working on the branch")
	case b.VsTarget.Ahead == 0:
		return fail("there is nothing on the branch to merge")
	}
	if w := c.worktreeAt(t.CheckedOut); w != nil {
		if w.Operation != "" {
			return fail(fmt.Sprintf("a %s is unfinished in %s", w.Operation, w.Path))
		}
		if w.Dirty != nil && w.Dirty.TrackedDirty() {
			return fail(fmt.Sprintf("%s has uncommitted changes to tracked files; commit or stash them first", w.Path))
		}
	}
	a.CanPerform = true
	return a
}

func (c *healthCtx) worktreeAt(p string) *GitWorktree {
	for i := range c.o.Worktrees {
		if c.o.Worktrees[i].Path == p {
			return &c.o.Worktrees[i]
		}
	}
	return nil
}

// deleteAction says whether Delete branch would be offered. Deleting is
// destructive and the operation refuses unless nothing would be lost; this
// only says whether it is worth offering.
func (c *healthCtx) deleteAction(b *GitBranch, label string) HealthAction {
	a := HealthAction{Kind: ActDeleteBranch, Label: label, Branch: b.Name, Destructive: true,
		Detail: "Opens Delete branch. It refuses unless every commit is in the target, and the commits stay in Git's object database."}
	fail := func(why string) HealthAction {
		a.CanPerform, a.Reason = false, why
		return a
	}
	switch {
	case b.Protected:
		return fail("it is a protected branch")
	case !b.DevBoard.Created:
		return fail("Dev Board did not create it, so it will not delete it")
	case busy(b):
		return fail("an agent still has a session on it")
	case b.Worktree != nil:
		// The worktree has to go first; the action says so rather than pretending.
		a = HealthAction{Kind: ActCleanWorktree, Label: "Clean its worktree, then delete the branch", Branch: b.Name,
			WorktreeID: b.Worktree.WorktreeID, Destructive: true,
			Detail: "Opens Clean worktree, which refuses if anything is uncommitted. The branch stays until you delete it."}
		if b.Worktree.WorktreeID == "" || !b.Worktree.Owned {
			return fail("it is checked out in a worktree Dev Board did not create")
		}
		if why := cleanBlocker(b.Worktree.Locked, b.Worktree.Missing, b.Worktree.Dirty, b.Worktree.Operation, busy(b)); why != "" {
			return fail(why)
		}
		a.CanPerform = true
		return a
	case !b.Merged:
		return fail("not every commit on it is in the target, and Dev Board only deletes what it can show is merged")
	}
	a.CanPerform = true
	return a
}

// cleanBlocker is why a worktree cannot be cleaned, or "". A directory that is
// already gone can be cleaned: only its record is retired.
func cleanBlocker(locked, missing bool, dirty *GitChangeCounts, op string, active bool) string {
	switch {
	case active:
		return "a run still has a session on the worktree"
	case locked:
		return "the worktree is locked"
	case missing:
		return ""
	case op != "":
		return fmt.Sprintf("a %s is unfinished in the worktree", op)
	case dirty == nil:
		return "the worktree could not be read, so it cannot be shown to be clean"
	case dirty.Dirty():
		return "it has uncommitted work, which removing it would destroy; commit or discard it yourself first"
	}
	return ""
}

func (c *healthCtx) cleanAction(w *GitWorktree, label string) HealthAction {
	a := HealthAction{Kind: ActCleanWorktree, Label: label, WorktreeID: w.WorktreeID, Destructive: true, Branch: w.Branch,
		Detail: "Opens Clean worktree. It refuses if anything is uncommitted; the branch is kept."}
	if !w.Owned || w.WorktreeID == "" {
		a.Reason = "Dev Board has no record of making this worktree, so it will not remove it"
		return a
	}
	if why := cleanBlocker(w.Locked, w.Missing, w.Dirty, w.Operation, w.ActiveRun); why != "" {
		a.Reason = why
		return a
	}
	a.CanPerform = true
	return a
}

// ---- uncommitted work ----

func (c *healthCtx) uncommitted() {
	// The checkout the user works in.
	if p := c.primary; p != nil && p.Dirty != nil && p.Operation == "" && p.Dirty.Conflicted == 0 && c.dirtyEnough(*p.Dirty) {
		sev, why := HealthInfo, "Your own checkout has uncommitted work. That is normal while you are working in it."
		if p.Dirty.TrackedDirty() {
			if names := c.readyToMerge(); len(names) > 0 {
				sev = HealthAttention
				why = fmt.Sprintf("It has uncommitted changes to tracked files, which Merge refuses to run over, and %s ready to merge.", n(len(names), "branch is", "branches are"))
			}
		}
		c.add(FindUncommittedWork, "primary", HealthFinding{
			Severity:    sev,
			Title:       "Your checkout has uncommitted work",
			Explanation: why + " Dev Board never commits, stashes or discards for you.",
			Subject:     HealthSubject{WorktreePath: p.Path, Branch: p.Branch},
			Evidence:    []HealthEvidence{ev("Files", "%s", countsPhrase(*p.Dirty)), ev("Checkout", "%s", p.Path)},
			Action:      reviewWorktree("Review changes", "Opens the working changes of your checkout.", ""),
		})
	}
	// Dev Board's own worktrees. A run with a live session is working: not a finding.
	for i := range c.o.Worktrees {
		w := &c.o.Worktrees[i]
		if w.Primary || !w.Owned || w.Missing || w.ActiveRun || w.Dirty == nil || w.Operation != "" || w.Dirty.Conflicted > 0 {
			continue
		}
		if !c.dirtyEnough(*w.Dirty) {
			continue
		}
		b := c.local[w.Branch]
		sev := HealthAttention
		var idle time.Duration
		done := false
		var subj HealthSubject
		if b != nil {
			subj = c.subjectOf(b)
			done = b.DevBoard.Phase == PhaseCompleted
			if r := latestRun(c.runsOf(b)); r != nil && r.State.Terminal() {
				idle = c.now.Sub(settled(r))
			}
		} else {
			subj = HealthSubject{Branch: w.Branch}
			if r := c.latestRunIn(w.WorktreeID); r != nil && r.State.Terminal() {
				idle = c.now.Sub(settled(r))
			}
		}
		subj.WorktreeID, subj.WorktreePath = w.WorktreeID, w.Path
		if done || idle >= c.th.IdleDirtyRisk {
			sev = HealthRisk
		}
		ex := []HealthEvidence{ev("Files", "%s", countsPhrase(*w.Dirty)), ev("Worktree", "%s", w.Path)}
		why := "The agent is not working in it, and these files are not on the branch, so merging, pushing or deleting the branch would leave them out."
		if idle > 0 {
			ex = append(ex, ev("Run ended", "%s ago", ago(idle)))
		}
		if done {
			ex = append(ex, taskEv(b))
			why = "The task is Done, but this work was never committed. " + why
		} else if idle >= c.th.IdleDirtyRisk {
			why = fmt.Sprintf("It has sat for %s. %s", ago(idle), why)
		}
		c.add(FindUncommittedWork, "wt:"+w.WorktreeID, HealthFinding{
			Severity:    sev,
			Title:       fmt.Sprintf("Uncommitted work in %s", branchOr(w.Branch, w.Path)),
			Explanation: why,
			Subject:     subj,
			Evidence:    ex,
			Action:      reviewWorktree("Review changes", "Opens the uncommitted changes in this worktree. Commit them from the agent or a terminal.", w.WorktreeID),
		})
	}
}

func (c *healthCtx) latestRunIn(wtID string) *Run { return latestRun(c.in.RunsByWorktree[wtID]) }

func branchOr(branch, p string) string {
	if branch != "" {
		return branch
	}
	return path.Base(p)
}

// dirtyEnough: staged or modified files always count; untracked files only in
// number, because one scratch file is not "work".
func (c *healthCtx) dirtyEnough(d GitChangeCounts) bool {
	return d.Staged > 0 || d.Unstaged > 0 || d.Untracked >= c.th.SignificantUntracked
}

// readyToMerge lists the owned branches with finished work that could be merged now.
func (c *healthCtx) readyToMerge() []string {
	var out []string
	for _, b := range c.ownedBranches() {
		if !busy(b) && measured(b) && b.VsTarget.Ahead > 0 && !b.Merged {
			out = append(out, b.Name)
		}
	}
	return out
}

// ---- interrupted operations ----

func (c *healthCtx) operations() {
	for i := range c.o.Worktrees {
		w := &c.o.Worktrees[i]
		if w.Missing || w.ActiveRun || w.Dirty == nil {
			continue
		}
		if !w.Primary && !w.Owned {
			continue // a worktree of the user's, somewhere else
		}
		key := "wt:" + w.WorktreeID
		if w.Primary {
			key = "primary"
		}
		where := "your checkout"
		if !w.Primary {
			where = "the worktree of " + branchOr(w.Branch, w.Path)
		}
		subj := HealthSubject{Branch: w.Branch, WorktreeID: w.WorktreeID, WorktreePath: w.Path}
		if b := c.local[w.Branch]; b != nil && !w.Primary {
			subj = c.subjectOf(b)
		}
		const never = "Dev Board never resolves conflicts or aborts an operation in a checkout; that is your decision"
		act := func(op string) HealthAction {
			switch op {
			case "":
				return terminalOnly(ActFinishOperation, "Resolve the conflicts",
					fmt.Sprintf("Resolve the files in %s and commit, or discard the change that caused them.", w.Path), never)
			case "bisect":
				return terminalOnly(ActFinishOperation, "End the bisect", fmt.Sprintf("In %s, run `git bisect reset` when you are done with it.", w.Path), never)
			}
			return terminalOnly(ActFinishOperation, "Finish or abort the "+op,
				fmt.Sprintf("In %s, finish it (resolve, then `git %s --continue`) or abort it (`git %s --abort`).", w.Path, op, op), never)
		}

		if w.Dirty.Conflicted > 0 {
			sev, op := HealthRisk, w.Operation
			if w.Primary {
				sev = HealthCritical
			}
			why := "Nothing can be merged, switched or cleaned there until the conflict is resolved."
			ex := []HealthEvidence{ev("Conflicted files", "%d", w.Dirty.Conflicted), ev("Checkout", "%s", w.Path)}
			if op != "" {
				ex = append(ex, ev("Unfinished operation", "%s", op))
				why = fmt.Sprintf("A %s stopped on a conflict and was never finished. %s", op, why)
			}
			c.add(FindUnresolvedConflict, key, HealthFinding{
				Severity: sev, Title: fmt.Sprintf("%s in %s", n(w.Dirty.Conflicted, "file is in conflict", "files are in conflict"), where),
				Explanation: why, Subject: subj, Evidence: ex, Action: act(op),
			})
			continue
		}
		if w.Operation == "" {
			continue
		}
		sev := HealthAttention
		switch {
		case w.Operation == "bisect":
			sev = HealthInfo
		case w.Operation == "rebase" || w.Primary:
			sev = HealthRisk
		}
		c.add(FindOperationInterrupt, key, HealthFinding{
			Severity:    sev,
			Title:       fmt.Sprintf("A %s is unfinished in %s", w.Operation, where),
			Explanation: "Git is part-way through it, so what is on disk is neither the old state nor the new one, and Dev Board will not merge or clean there until it is finished or aborted.",
			Subject:     subj,
			Evidence:    []HealthEvidence{ev("Operation", "%s", w.Operation), ev("Checkout", "%s", w.Path)},
			Action:      act(w.Operation),
		})
	}
}

// ---- things that stop Dev Board's own operations ----

func (c *healthCtx) automation() {
	o := c.o
	pending := c.readyToMerge()
	soft := func() HealthSeverity { // matters only if there is something to merge
		if len(pending) > 0 {
			return HealthAttention
		}
		return HealthInfo
	}
	add := func(key string, sev HealthSeverity, title, why string, ex []HealthEvidence, a HealthAction) {
		c.add(FindAutomationBlocked, key, HealthFinding{Severity: sev, Title: title, Explanation: why, Evidence: ex, Action: a})
	}
	t := o.Local.Target
	switch {
	case t.Name == "" && !o.Local.Head.Unborn:
		add("no_target", HealthAttention, "No target branch could be found",
			"Branches are not compared with anything, so merged, behind and ready-to-merge cannot be told. Dev Board looks for origin/HEAD, init.defaultBranch, then main, master, trunk and develop.",
			[]HealthEvidence{ev("Target", "none found")},
			terminalOnly(ActInspect, "Set a default branch", "Run `git remote set-head origin -a`, or create a branch named main.", "Dev Board does not change repository configuration"))
	case t.Name != "" && !t.LocalExists:
		add("target_remote_only", HealthAttention, fmt.Sprintf("%s exists only on the remote here", t.Name),
			"Branches are compared with it, but nothing can be merged until a local branch of that name exists.",
			[]HealthEvidence{ev("Target", "%s (from %s)", t.Name, t.Source)},
			terminalOnly(ActInspect, "Check the target out", fmt.Sprintf("Run `git checkout %s` in your checkout.", t.Name), "Dev Board will not create or check out the target for you"))
	case t.Name != "" && t.CheckedOut == "" && len(pending) > 0:
		add("target_not_checked_out", HealthAttention, fmt.Sprintf("%s is not checked out anywhere", t.Name),
			fmt.Sprintf("Git can only merge into a checked-out branch, and Dev Board will not switch your checkout. %s ready to merge.", n(len(pending), "branch is", "branches are")),
			[]HealthEvidence{ev("Target", "%s", t.Name), ev("Your checkout", "on %s", branchOr(o.Local.Head.Branch, "a detached HEAD"))},
			terminalOnly(ActInspect, "Check the target out", fmt.Sprintf("Run `git checkout %s` in your checkout.", t.Name), "Dev Board will not switch your checkout"))
	}
	if o.Local.Head.Detached {
		add("detached_head", soft(), "Your checkout is on a detached HEAD",
			"It is on no branch, so commits made there belong to no branch, and the target cannot be checked out there for a merge.",
			[]HealthEvidence{ev("HEAD", "%s", short(o.Local.Head.Commit))},
			terminalOnly(ActInspect, "Switch to a branch", "Run `git switch <branch>` in your checkout.", "Dev Board will not switch your checkout"))
	}
	if lock := c.in.IndexLock; lock != nil && c.now.Sub(*lock) >= c.th.LockStaleAfter {
		add("index_lock", HealthAttention, "A Git lock file has been left behind",
			"Git creates index.lock while it works and removes it when it finishes. One this old is probably from a Git process that crashed, and it makes later Git commands in your checkout fail.",
			[]HealthEvidence{ev("Lock file", "index.lock"), ev("Age", "%s", ago(c.now.Sub(*lock)))},
			terminalOnly(ActInspect, "Check no Git is running, then remove the lock",
				"If no Git command is running, delete .git/index.lock yourself.", "Dev Board never deletes files inside .git"))
	}
}

func (c *healthCtx) unreadable() {
	c.add(FindRepositoryUnread, "repo", HealthFinding{
		Severity:    HealthRisk,
		Title:       "Dev Board cannot read this repository",
		Explanation: "Nothing about its Git state can be shown or acted on, and the health below is unknown until it can be read again. Existing findings are left as they were.",
		Evidence:    []HealthEvidence{ev("Error", "%s", c.in.ReadError)},
		Action:      terminalOnly(ActInspect, "Check the repository", "Make sure the folder still exists and is a Git repository.", "Dev Board cannot repair a repository"),
	})
}

// ---- unsynced work ----

// syncScope: the branches whose sync state matters. The user's other branches
// are theirs; what matters here is Dev Board's work, the target (a merge made
// here is not on the remote until pushed) and the branch the user is on.
func (c *healthCtx) syncScope() []*GitBranch {
	var out []*GitBranch
	for _, b := range c.local {
		if b.Unusual != "" {
			continue
		}
		if ownedBranch(b) || b.Target || b.Head {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *healthCtx) unsynced() {
	hasRemote := len(c.o.Remote.Remotes) > 0
	for _, b := range c.syncScope() {
		if busy(b) {
			continue
		}
		own := ownedBranch(b)
		role := "Branch"
		if b.Target {
			role = "The target"
		}
		if own && b.Merged {
			continue // everything on it is in the target: whether the target is pushed is the target's finding
		}
		// Dev Board's work and the target matter; the branch the user happens to be on is theirs,
		// and having committed locally before pushing is the normal way to work.
		mine := own || b.Target
		switch b.Upstream.State {
		case UpstreamAhead:
			sev, why := HealthAttention, fmt.Sprintf("%s here and not on %s (as of the last fetch).", capitalize(n(b.Upstream.Ahead, "commit exists", "commits exist")), b.Upstream.Name)
			if !mine {
				sev = HealthInfo
			}
			if b.Target {
				why += " A merge made here is not on the remote until it is pushed."
			}
			if own && b.DevBoard.Phase == PhaseCompleted {
				sev = HealthRisk
				why += " The task is Done, so this work lives only on this computer."
			}
			f := HealthFinding{
				Severity: sev, Title: fmt.Sprintf("%s has %s not pushed", b.Name, n(b.Upstream.Ahead, "commit", "commits")), Explanation: why,
				Subject: c.subjectOf(b), Action: c.pushAction(b, "Push branch"),
				Evidence: []HealthEvidence{ev("Ahead of upstream", "%d", b.Upstream.Ahead), ev("Upstream", "%s", b.Upstream.Name)},
			}
			if own {
				f.Evidence = append(f.Evidence, taskEv(b))
			}
			c.add(FindUnpushedCommits, b.Name, f)
		case UpstreamDiverged:
			sev := HealthRisk
			if !mine {
				sev = HealthAttention
			}
			c.add(FindUpstreamDiverged, b.Name, HealthFinding{
				Severity: sev,
				Title:    fmt.Sprintf("%s has diverged from %s", b.Name, b.Upstream.Name),
				Explanation: fmt.Sprintf("It has %d commit%s the remote lacks and the remote has %d it lacks. A plain push would be rejected, and Dev Board never forces, pulls or rebases.",
					b.Upstream.Ahead, plural(b.Upstream.Ahead), b.Upstream.Behind),
				Subject:  c.subjectOf(b),
				Evidence: []HealthEvidence{ev("Ahead", "%d", b.Upstream.Ahead), ev("Behind", "%d", b.Upstream.Behind), ev("Upstream", "%s", b.Upstream.Name)},
				Action: terminalOnly(ActSyncBranch, "Sync the branch", fmt.Sprintf("In a terminal, rebase or merge %s onto the branch, then push.", b.Upstream.Name),
					"Dev Board never pulls, rebases or force-pushes"),
			})
		case UpstreamBehind:
			sev := HealthInfo
			if b.Target {
				sev = HealthAttention
			}
			c.add(FindRemoteAhead, b.Name, HealthFinding{
				Severity: sev,
				Title:    fmt.Sprintf("%s is %s behind %s", role+" "+b.Name, n(b.Upstream.Behind, "commit", "commits"), b.Upstream.Name),
				Explanation: "The remote has commits this branch lacks (as of the last fetch). " + func() string {
					if b.Target {
						return "Merging into it now builds on a target that is out of date."
					}
					return "Someone else pushed to it."
				}(),
				Subject:  c.subjectOf(b),
				Evidence: []HealthEvidence{ev("Behind upstream", "%d", b.Upstream.Behind), ev("Upstream", "%s", b.Upstream.Name)},
				Action: terminalOnly(ActSyncBranch, "Update the branch", "In a terminal, run `git pull --ff-only` on it.",
					"Dev Board does not pull or fast-forward branches"),
			})
		case UpstreamGone:
			if b.Merged || b.Target || b.NotPushed == 0 || !measured(b) || b.VsTarget.Ahead == 0 || b.DevBoard.Phase == PhaseCompleted {
				continue // finished work whose remote branch went with its pull request: housekeeping, or said by the Done task's finding
			}
			if c.contentOnTarget(b) {
				continue
			}
			c.add(FindRemoteBranchDeleted, b.Name, HealthFinding{
				Severity: HealthAttention,
				Title:    fmt.Sprintf("The remote branch of %s was deleted", b.Name),
				Explanation: fmt.Sprintf("It still has %s that are not in %s. If its pull request was squash-merged they are there under other commits, but Dev Board cannot tell without GitHub.",
					n(b.VsTarget.Ahead, "commit", "commits"), c.o.Local.Target.Name),
				Subject:  c.subjectOf(b),
				Evidence: []HealthEvidence{ev("Upstream", "%s (gone)", b.Upstream.Name), ev("Commits not in target", "%d", b.VsTarget.Ahead), ev("Commits on no remote", "%d", b.NotPushed)},
				Action:   reviewBranch("Review the branch", "Look at what is on it before deciding.", b),
			})
		case UpstreamNone:
			if !hasRemote || !own || b.Merged || b.NotPushed == 0 {
				continue
			}
			sev := HealthAttention
			why := fmt.Sprintf("It has %s that no remote has. Until it is pushed, this computer is the only place the work exists.", n(b.NotPushed, "commit", "commits"))
			if b.DevBoard.Phase == PhaseCompleted {
				sev = HealthRisk
				why += " The task is Done."
			}
			c.add(FindBranchNotPushed, b.Name, HealthFinding{
				Severity: sev, Title: fmt.Sprintf("%s was never pushed", b.Name), Explanation: why,
				Subject:  c.subjectOf(b),
				Evidence: []HealthEvidence{ev("Commits on no remote", "%d", b.NotPushed), taskEv(b)},
				Action:   c.pushAction(b, "Push branch"),
			})
		}
	}

	// How old what is known of the remote is matters for everything above, but only
	// when there is Dev Board work to be wrong about.
	if hasRemote && len(c.ownedBranches()) > 0 {
		lf := c.o.Remote.LastFetchedAt
		switch {
		case lf == nil:
			c.add(FindRemoteStateStale, "fetch", HealthFinding{
				Severity:    HealthInfo,
				Title:       "This repository has never fetched",
				Explanation: "Every comparison with the remote (unpushed, not pulled, remote branch deleted) is from whenever this clone was made. Fetch updates only what is known about the remote; it changes none of your branches.",
				Evidence:    []HealthEvidence{ev("Last fetch", "never")},
				Action:      HealthAction{Kind: ActFetch, Label: "Fetch", Detail: "Updates remote-tracking branches only.", CanPerform: true},
			})
		case c.now.Sub(*lf) >= c.th.FetchStaleAfter:
			c.add(FindRemoteStateStale, "fetch", HealthFinding{
				Severity:    HealthInfo,
				Title:       fmt.Sprintf("Last fetched %s ago", ago(c.now.Sub(*lf))),
				Explanation: "Comparisons with the remote may be out of date. Fetch updates only what is known about the remote; it changes none of your branches.",
				Evidence:    []HealthEvidence{ev("Last fetch", "%s ago", ago(c.now.Sub(*lf)))},
				Action:      HealthAction{Kind: ActFetch, Label: "Fetch", Detail: "Updates remote-tracking branches only.", CanPerform: true},
			})
		}
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (c *healthCtx) contentOnTarget(b *GitBranch) bool {
	t, ok := c.in.OnTarget[b.Name]
	return ok && t.Checked && t.Unchanged
}

// ---- branch hygiene ----

func (c *healthCtx) branches() {
	target := c.o.Local.Target.Name
	for _, b := range c.ownedBranches() {
		if busy(b) || !measured(b) {
			continue
		}
		subj := c.subjectOf(b)
		run := latestRun(c.runsOf(b))
		ahead := b.VsTarget.Ahead
		unmerged := ahead > 0 && !b.Merged

		// Already in the target: housekeeping.
		if b.Merged && b.VsTarget.Relation == RelMerged {
			a := c.deleteAction(b, "Delete the branch")
			c.add(FindMergedBranch, b.Name, HealthFinding{
				Severity:    HealthInfo,
				Title:       fmt.Sprintf("%s is merged and can be deleted", b.Name),
				Explanation: fmt.Sprintf("Every commit on it is in %s. Keeping it only adds noise to the branch list.", target),
				Subject:     subj,
				Evidence:    []HealthEvidence{ev("Target", "%s", target), taskEv(b), ev("Worktree", "%s", wtLine(b))},
				Action:      a,
			})
			c.emitted[b.Name] = FindMergedBranch
			continue
		}
		if !unmerged {
			// Same as, or never advanced beyond, the target: it holds no work of its own.
			c.emptyBranch(b, subj, run)
			continue
		}

		// Unmerged work. First: is it really missing from the target?
		if c.contentOnTarget(b) {
			why := fmt.Sprintf("Git merged it into %s in memory and the result is exactly %s: it adds nothing. That is what a squash or rebase merge looks like, or its changes were later superseded.", target, target)
			a := c.deleteAction(b, "Delete the branch")
			if !a.CanPerform && a.Kind == ActDeleteBranch && a.Reason != "" && b.Worktree == nil {
				a.Reason = "its commits are not in " + target + " by history (a squash or rebase merge), and Dev Board will only delete a branch it can show is merged without GitHub's confirmation; delete it from your terminal once you are sure"
			}
			c.add(FindContentOnTarget, b.Name, HealthFinding{
				Severity: HealthInfo, Title: fmt.Sprintf("%s is already represented in %s", b.Name, target), Explanation: why,
				Subject:  subj,
				Evidence: []HealthEvidence{ev("Commits not in target by history", "%d", ahead), ev("Merge into target", "changes nothing"), taskEv(b)},
				Action:   a,
			})
			c.emitted[b.Name] = FindContentOnTarget
			continue
		}

		last := c.lastActivity(b)
		quiet := c.now.Sub(last)

		switch {
		// The task says it is finished but the work is not in the target.
		case b.DevBoard.Phase == PhaseCompleted:
			sev := HealthRisk
			why := fmt.Sprintf("The task is Done, but %s on its branch would change %s and are not in it.", n(ahead, "commit", "commits"), target)
			if b.Upstream.State == UpstreamGone {
				sev = HealthAttention
				why += " Its remote branch was deleted, which usually follows a merged pull request; if that was a squash or rebase merge the work may be there under other commits."
			}
			why += " If it was merged on GitHub, Dev Board cannot see that from here."
			ex := []HealthEvidence{taskEv(b), ev("Commits not in target", "%d", ahead), ev("Upstream", "%s", upstreamWord(b))}
			if t, ok := c.in.OnTarget[b.Name]; ok && t.Checked {
				ex = append(ex, ev("Merge into target", "would change it"))
			}
			c.add(FindTaskDoneUnmerged, b.Name, HealthFinding{
				Severity: sev, Title: fmt.Sprintf("%q is Done but %s is not merged", b.DevBoard.TaskTitle, b.Name), Explanation: why,
				Subject: subj, Evidence: ex, Action: c.mergeActionOr(b, "Merge the branch"),
			})
			c.emitted[b.Name] = FindTaskDoneUnmerged

		// Nobody is working on it, no task is waiting on it, and it has been quiet.
		case quiet >= c.th.AbandonedAfter && (b.DevBoard.Phase == PhaseNone || b.DevBoard.Phase == PhaseIdle || b.DevBoard.Phase == PhaseActive):
			ex := []HealthEvidence{ev("Last activity", "%s ago", ago(quiet)), ev("Commits not in target", "%d", ahead)}
			if t := taskLine(b); t != "" {
				ex = append(ex, ev("Task", "%s", t))
			} else {
				ex = append(ex, ev("Task", "none known"))
			}
			if run != nil {
				ex = append(ex, ev("Last run", "%s", strings.ReplaceAll(string(run.State), "_", " ")))
			}
			c.add(FindAbandonedBranch, b.Name, HealthFinding{
				Severity:    HealthAttention,
				Title:       fmt.Sprintf("%s looks abandoned", b.Name),
				Explanation: fmt.Sprintf("No agent is working on it and it has been quiet for %s, but it holds %s that are not in %s. This is a guess from inactivity: it may be waiting on you.", ago(quiet), n(ahead, "commit", "commits"), target),
				Subject:     subj, Evidence: ex,
				Action: reviewBranch("Review what is on it", "Decide whether to merge it, resume it with a new run, or delete it.", b),
			})
			c.emitted[b.Name] = FindAbandonedBranch

		case b.Stale:
			c.add(FindStaleBranch, b.Name, HealthFinding{
				Severity: HealthAttention, Title: fmt.Sprintf("%s has gone stale", b.Name),
				Explanation: "It holds work that is not in the target, has not been touched for two weeks, and the target has moved on without it: the longer it waits, the harder it is to merge.",
				Subject:     subj,
				Evidence:    []HealthEvidence{ev("Detail", "%s", b.StaleWhy), ev("Commits not in target", "%d", ahead), taskEv(b)},
				Action:      reviewBranch("Review the branch", "Decide whether to merge it, rebase it in a terminal, or let it go.", b),
			})
			c.emitted[b.Name] = FindStaleBranch

		// Finished, and left unmerged for a while.
		case run != nil && run.State == RunCompleted && c.now.Sub(settled(run)) >= c.th.FinishedUnmergedAfter:
			c.add(FindFinishedUnmerged, b.Name, HealthFinding{
				Severity: HealthAttention,
				Title:    fmt.Sprintf("Finished work on %s has not been merged", b.Name),
				Explanation: fmt.Sprintf("The run finished %s ago and its %s are not in %s. They may simply be waiting for your review.",
					ago(c.now.Sub(settled(run))), n(ahead, "commit", "commits"), target),
				Subject:  subj,
				Evidence: []HealthEvidence{ev("Run finished", "%s ago", ago(c.now.Sub(settled(run)))), ev("Commits not in target", "%d", ahead), taskEv(b)},
				Action:   c.mergeActionOr(b, "Review and merge"),
			})
			c.emitted[b.Name] = FindFinishedUnmerged
		}

		// Far behind the target: whatever else is true, merging it will be harder.
		if k := c.emitted[b.Name]; k != FindAbandonedBranch && k != FindStaleBranch {
			if b.VsTarget.Behind >= c.th.FarBehind {
				sev := HealthAttention
				if b.VsTarget.Behind >= c.th.VeryFarBehind {
					sev = HealthRisk
				}
				c.add(FindBranchFarBehind, b.Name, HealthFinding{
					Severity: sev, Title: fmt.Sprintf("%s is %s behind %s", b.Name, n(b.VsTarget.Behind, "commit", "commits"), target),
					Explanation: "It was cut from an old target. The more the target has moved, the more of the branch's changes it may have made out of date, and the more likely a merge is to need hands. Whether it conflicts is what the merge check tells you.",
					Subject:     subj,
					Evidence:    []HealthEvidence{ev("Behind target", "%d", b.VsTarget.Behind), ev("Ahead of target", "%d", ahead)},
					Action:      reviewBranch("Review the branch", "Look at it, then update it from the target in a terminal or ask an agent to.", b),
				})
			}
		}
	}
}

func upstreamWord(b *GitBranch) string {
	switch b.Upstream.State {
	case UpstreamNone:
		return "none"
	case UpstreamGone:
		return b.Upstream.Name + " (deleted)"
	}
	return b.Upstream.Name + " (" + string(b.Upstream.State) + ")"
}

func wtLine(b *GitBranch) string {
	if b.Worktree == nil {
		return "none"
	}
	return b.Worktree.Path
}

// mergeActionOr is the merge action when Merge would be offered, else a
// review, so a finding always has a next step.
func (c *healthCtx) mergeActionOr(b *GitBranch, label string) HealthAction {
	m := c.mergeAction(b, label)
	if m.CanPerform {
		return m
	}
	return reviewBranch("Review the branch", "Merge is not available right now: "+m.Reason+".", b)
}

// emptyBranch: an owned branch with no commits of its own.
func (c *healthCtx) emptyBranch(b *GitBranch, subj HealthSubject, run *Run) {
	// An agent that finished without committing leaves its work in the worktree: the
	// uncommitted rule reports that. A branch with nothing anywhere is only noise.
	if b.Worktree != nil && b.Worktree.Dirty != nil && b.Worktree.Dirty.Dirty() {
		return
	}
	if b.DevBoard.Phase == PhaseReview || b.DevBoard.Phase == PhaseActive {
		return // someone still expects something from it
	}
	quiet := c.now.Sub(c.lastActivity(b))
	if quiet < c.th.AbandonedAfter {
		return
	}
	ex := []HealthEvidence{ev("Last activity", "%s ago", ago(quiet)), ev("Commits of its own", "0"), ev("Task", "%s", orNone(taskLine(b)))}
	if run != nil {
		ex = append(ex, ev("Last run", "%s", strings.ReplaceAll(string(run.State), "_", " ")))
	}
	c.add(FindAbandonedBranch, b.Name, HealthFinding{
		Severity:    HealthInfo,
		Title:       fmt.Sprintf("%s has nothing on it", b.Name),
		Explanation: fmt.Sprintf("It has no commits of its own and has been quiet for %s. Nothing is lost by deleting it.", ago(quiet)),
		Subject:     subj, Evidence: ex, Action: c.deleteAction(b, "Delete the branch"),
	})
	c.emitted[b.Name] = FindAbandonedBranch
}

func orNone(s string) string {
	if s == "" {
		return "none known"
	}
	return s
}

// ---- agent orchestration ----

func (c *healthCtx) orchestration() {
	// A worktree record whose branch is not in the repository, for work that is meant to be going on.
	for id, rec := range c.records {
		if rec.RemovingSince != nil {
			continue
		}
		if _, ok := c.local[rec.Branch]; ok {
			continue
		}
		active, taskTitle, taskID, runID := c.taskActivity(id)
		if !active {
			continue // a finished task's stray record is a worktree matter
		}
		w := c.wtByID[id]
		if w == nil || w.Missing || w.Detached || (w.Branch != "" && w.Branch != rec.Branch) {
			continue // the worktree itself disagrees with the record: reported as a metadata mismatch
		}
		c.add(FindMissingBranch, id, HealthFinding{
			Severity:    HealthRisk,
			Title:       fmt.Sprintf("The branch for %q is missing", taskTitle),
			Explanation: fmt.Sprintf("Dev Board created worktree %s for branch %s, but the repository has no such branch. Whatever the agent commits there belongs to no branch Dev Board can find, merge or push.", path.Base(rec.Path), rec.Branch),
			Subject:     HealthSubject{Branch: rec.Branch, WorktreeID: id, WorktreePath: rec.Path, TaskID: taskID, TaskTitle: taskTitle, RunID: runID},
			Evidence:    []HealthEvidence{ev("Recorded branch", "%s", rec.Branch), ev("Worktree", "%s", rec.Path), ev("Worktree HEAD", "%s", headWord(w))},
			Action: askAgent(fmt.Sprintf("Investigate the missing branch for %q", taskTitle),
				fmt.Sprintf("Dev Board created the worktree %s for branch %s, but the repository has no such branch, and the task is still active.\n\nFind out where the worktree's HEAD is and whether it holds commits. If it does, recreate the branch at that commit (git branch %s <commit>) so the work is not stranded. Do not delete or reset anything.", rec.Path, rec.Branch, rec.Branch)),
		})
	}

	// Two in-flight branches that change the same files.
	for _, ov := range c.in.Overlaps {
		a, b := c.local[ov.A], c.local[ov.B]
		if a == nil || b == nil || len(ov.Files) == 0 && !ov.Conflicts {
			continue
		}
		pair := []string{ov.A, ov.B}
		sort.Strings(pair)
		key := pair[0] + "|" + pair[1]
		subj := HealthSubject{Branch: pair[0], Related: []string{pair[1]}}
		if ov.Conflicts {
			files := ov.ConflictFiles
			if len(files) == 0 {
				files = ov.Files
			}
			note := " This is Git's own merge of their committed work, run in memory; uncommitted work is not part of it."
			c.add(FindBranchConflict, key, HealthFinding{
				Severity:    HealthRisk,
				Title:       fmt.Sprintf("%s and %s conflict", pair[0], pair[1]),
				Explanation: "Git merged the two branches in memory and reports conflicts. Whichever is merged second will need them resolved." + note,
				Subject:     subj,
				Evidence:    []HealthEvidence{ev("Conflicting files", "%s", listFiles(files)), ev("Method", "git merge-tree (nothing was changed)")},
				Action: HealthAction{Kind: ActCreateTask, Label: "Create a task to reconcile them", CanPerform: true,
					Detail:          "Adds a task to the board describing the conflict. Nothing is started.",
					TaskTitle:       fmt.Sprintf("Reconcile %s and %s", pair[0], pair[1]),
					TaskDescription: fmt.Sprintf("Git reports that merging %s and %s conflicts in: %s.\n\nDecide the order they should land in and resolve the conflict before the second is merged.", pair[0], pair[1], listFiles(files))},
			})
			continue
		}
		if ov.Simulated && !ov.Uncommitted {
			continue // Git merged their committed work cleanly: touching the same file is not a conflict
		}
		note := "Git cannot tell whether they conflict from the file lists alone"
		if ov.Uncommitted {
			note = "Part of it is uncommitted work, which Git cannot test"
		}
		c.add(FindBranchOverlap, key, HealthFinding{
			Severity:    HealthAttention,
			Title:       fmt.Sprintf("%s and %s change the same files", pair[0], pair[1]),
			Explanation: "Two agents are modifying overlapping files, so they may conflict when both are merged. " + note + ".",
			Subject:     subj,
			Evidence:    []HealthEvidence{ev("Shared files", "%s", listFiles(ov.Files)), ev("Method", "file lists compared (a guess, not a merge test)")},
			Action: HealthAction{Kind: ActCreateTask, Label: "Create a task to coordinate them", CanPerform: true,
				Detail:          "Adds a task to the board describing the overlap. Nothing is started.",
				TaskTitle:       fmt.Sprintf("Coordinate %s and %s", pair[0], pair[1]),
				TaskDescription: fmt.Sprintf("%s and %s both change: %s.\n\nCheck whether the changes are compatible and decide which should land first.", pair[0], pair[1], listFiles(ov.Files))},
		})
	}
}

func headWord(w *GitWorktree) string {
	if w.Detached {
		return "detached at " + short(w.Head)
	}
	return branchOr(w.Branch, "unknown")
}

func listFiles(files []string) string {
	const show = 5
	if len(files) <= show {
		return strings.Join(files, ", ")
	}
	return strings.Join(files[:show], ", ") + fmt.Sprintf(" and %d more", len(files)-show)
}

// taskActivity says whether the worktree's task is meant to be going on (Doing,
// or a run with a live session) and names the task and run.
func (c *healthCtx) taskActivity(wtID string) (active bool, title, taskID, runID string) {
	r := latestRun(c.in.RunsByWorktree[wtID])
	if r == nil {
		return false, "", "", ""
	}
	t, ok := c.in.Tasks[r.TaskID]
	if !ok {
		return false, "", "", ""
	}
	return r.State.Active() || t.State == TaskDoing, t.Title, t.ID, r.ID
}

// ---- worktree hygiene ----

func (c *healthCtx) worktrees() {
	// Git lists a worktree of Dev Board's that Dev Board has no record of.
	if c.in.WorktreeRoot != "" {
		for i := range c.o.Worktrees {
			w := &c.o.Worktrees[i]
			if w.Primary || w.Owned || !pathWithin(c.in.WorktreeRoot, w.Path) {
				continue
			}
			ex := []HealthEvidence{ev("Path", "%s", w.Path), ev("Branch", "%s", headWord(w))}
			c.add(FindOrphanedWorktree, w.Path, HealthFinding{
				Severity:    HealthAttention,
				Title:       fmt.Sprintf("A worktree in Dev Board's directory has no record: %s", path.Base(w.Path)),
				Explanation: "Git lists it and it lives where Dev Board puts its worktrees, but no active Dev Board record owns it, so Dev Board will not touch it: it may be left over from a removal that never finished. Anything in it is still on disk.",
				Subject:     HealthSubject{Branch: w.Branch, WorktreePath: w.Path},
				Evidence:    ex,
				Action: terminalOnly(ActInspect, "Check it, then remove it yourself",
					fmt.Sprintf("In a terminal, look at `git -C %s status`, and when nothing in it matters run `git worktree remove %s`.", w.Path, w.Path),
					"Dev Board only removes worktrees its own records vouch for"),
			})
		}
	}

	ids := make([]string, 0, len(c.records))
	for id := range c.records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rec := c.records[id]
		w := c.wtByID[id]
		runs := c.in.RunsByWorktree[id]
		b := c.local[rec.Branch]

		// Disagreements between the record and Git.
		var mism []HealthEvidence
		why := ""
		switch {
		case w == nil:
			mism = append(mism, ev("Git", "does not list %s as a worktree", rec.Path))
			why = "Dev Board's record says the worktree is active, but Git does not know it."
		default:
			if w.Missing {
				mism = append(mism, ev("Directory", "gone"))
				why = "The record says the worktree is active but its directory no longer exists."
			}
			if !w.Missing && w.Detached {
				mism = append(mism, ev("Worktree HEAD", "detached at %s, recorded on %s", short(w.Head), rec.Branch))
				why = "The worktree is on no branch, but the record says it belongs to " + rec.Branch + "."
			} else if !w.Missing && w.Branch != "" && w.Branch != rec.Branch {
				mism = append(mism, ev("Worktree branch", "%s, recorded on %s", w.Branch, rec.Branch))
				why = "Someone switched the worktree to another branch after Dev Board recorded it."
			}
		}
		if rec.RemovingSince != nil && c.now.Sub(*rec.RemovingSince) >= c.th.StuckRemovalAfter {
			mism = append(mism, ev("Removal", "began %s ago and never finished", ago(c.now.Sub(*rec.RemovingSince))))
			if why == "" {
				why = "A removal was begun and never finished."
			}
		}
		if len(mism) > 0 {
			mism = append([]HealthEvidence{ev("Record", "%s on %s", rec.Path, rec.Branch)}, mism...)
			// Dev Board removes a worktree only while its record and Git agree. A directory
			// that is simply gone is the one exception: only the record is retired.
			a := HealthAction{Kind: ActCleanWorktree, Label: "Clean worktree", WorktreeID: id, Branch: rec.Branch, Destructive: true}
			switch {
			case w == nil:
				a = askAgent(fmt.Sprintf("Investigate the worktree of %s", rec.Branch),
					fmt.Sprintf("Dev Board's record says the worktree %s belongs to branch %s, but Git does not list it as a worktree of this repository.\n\nFind out what happened, and report it. Dev Board will not delete a directory on the strength of its record alone; do not remove anything.", rec.Path, rec.Branch))
				a.WorktreeID, a.Branch = id, rec.Branch
			case w.Missing:
				a = c.cleanAction(w, "Retire the worktree record")
				a.Detail = "Only retires the record: the directory is already gone."
			default:
				// Dev Board will not touch a worktree that disagrees with its record. A person (or an
				// agent they choose to ask) has to look at what happened first.
				a = askAgent(fmt.Sprintf("Investigate the worktree of %s", rec.Branch),
					fmt.Sprintf("Dev Board's record says the worktree %s belongs to branch %s, but it no longer matches: %s\n\nFind out what happened, and report it. Do not remove or reset anything: Dev Board leaves the worktree alone until it matches its record again.", rec.Path, rec.Branch, why))
				a.WorktreeID, a.Branch = id, rec.Branch
			}
			subj := HealthSubject{Branch: rec.Branch, WorktreeID: id, WorktreePath: rec.Path}
			if len(runs) > 0 {
				subj.RunID, subj.TaskID = runs[len(runs)-1].ID, runs[len(runs)-1].TaskID
				subj.TaskTitle = c.in.Tasks[subj.TaskID].Title
			}
			if _, ok := c.local[rec.Branch]; !ok {
				mism = append(mism, ev("Branch", "%s is not in the repository", rec.Branch))
			}
			c.add(FindWorktreeMismatch, id, HealthFinding{
				Severity: HealthAttention, Title: fmt.Sprintf("The worktree of %s does not match Dev Board's record", rec.Branch),
				Explanation: why + " Dev Board acts on a worktree only while its record and Git agree.", Subject: subj, Evidence: mism, Action: a,
			})
			continue
		}
		if w == nil {
			continue
		}

		// Never used: the record is older than the grace period and no run ever had it.
		if len(runs) == 0 {
			if age := c.now.Sub(rec.CreatedAt); age >= c.th.WorktreeGrace {
				c.add(FindWorktreeNoRun, id, HealthFinding{
					Severity:    HealthAttention,
					Title:       fmt.Sprintf("A worktree has no run: %s", rec.Branch),
					Explanation: "Dev Board made it, and no run ever used it. It is probably left over from a run that failed to start, or whose task is gone. It takes disk space and holds a branch.",
					Subject:     HealthSubject{Branch: rec.Branch, WorktreeID: id, WorktreePath: rec.Path},
					Evidence:    []HealthEvidence{ev("Created", "%s ago", ago(age)), ev("Runs", "0"), ev("Worktree", "%s", rec.Path), ev("Uncommitted", "%s", dirtyWord(w.Dirty))},
					Action:      c.cleanAction(w, "Clean worktree"),
				})
			}
			continue
		}

		// A Done task keeping a worktree it does not need. (Dirty ones are reported as uncommitted work.)
		r := runs[len(runs)-1]
		if t, ok := c.in.Tasks[r.TaskID]; ok && t.State == TaskDone && !w.ActiveRun && w.Dirty != nil && !w.Dirty.Dirty() && w.Operation == "" {
			if b != nil {
				if _, said := c.emitted[b.Name]; said {
					continue // the branch's own finding already covers it, and says to clean the worktree first
				}
			}
			c.add(FindWorktreeAfterDone, id, HealthFinding{
				Severity:    HealthInfo,
				Title:       fmt.Sprintf("%q is Done and still has a worktree", t.Title),
				Explanation: "The worktree is clean, so nothing is lost by removing it. The branch is kept.",
				Subject:     HealthSubject{Branch: rec.Branch, WorktreeID: id, WorktreePath: rec.Path, TaskID: t.ID, TaskTitle: t.Title, RunID: r.ID},
				Evidence:    []HealthEvidence{ev("Task", "%q (done)", t.Title), ev("Worktree", "%s", rec.Path), ev("Uncommitted", "none")},
				Action:      c.cleanAction(w, "Clean worktree"),
			})
		}
	}
}

func dirtyWord(d *GitChangeCounts) string {
	switch {
	case d == nil:
		return "not checked"
	case !d.Dirty():
		return "none"
	}
	return countsPhrase(*d)
}

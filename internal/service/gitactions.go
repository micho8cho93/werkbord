package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"devboard/internal/domain"
	"devboard/internal/github"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
)

// Action names, as they appear in results and events.
const (
	ActionFetch       = "fetch"
	ActionPush        = "push"
	ActionMerge       = "merge"
	ActionDelete      = "delete_branch"
	ActionCleanTree   = "clean_worktree"
	ActionCreatePR    = "create_pull_request"
	defaultRemoteName = "origin"
)

func blocker(code, format string, args ...any) domain.GitBlocker {
	return domain.GitBlocker{Code: code, Message: fmt.Sprintf(format, args...)}
}

// refuse is the result of an action a safety check stopped. Nothing had changed.
func refuse(action, message string, blockers []domain.GitBlocker, warnings []string) *domain.GitActionResult {
	if blockers == nil {
		blockers = []domain.GitBlocker{}
	}
	return &domain.GitActionResult{Action: action, Outcome: domain.OutcomeRefused, Message: message, Blockers: blockers, Warnings: warnings}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// tip resolves a local branch to its commit, or says why it cannot.
func (s *GitControl) tip(ctx context.Context, root, branch string) (string, *domain.GitBlocker, error) {
	if err := branchNameProblem(branch); err != nil {
		b := blocker(domain.BlockInvalidBranch, "%q cannot be used: %v", branch, err)
		return "", &b, nil
	}
	sha, err := s.Git.ResolveCommit(ctx, root, "refs/heads/"+branch)
	if err != nil {
		return "", nil, err
	}
	if sha == "" {
		b := blocker(domain.BlockBranchMissing, "there is no local branch %s", branch)
		return "", &b, nil
	}
	return sha, nil, nil
}

// remoteOfRef finds which configured remote a remote-tracking ref belongs to:
// the longest remote name that prefixes it (remote names may contain a '/').
func remoteOfRef(ref string, remotes []domain.GitRemote) string {
	name := strings.TrimPrefix(ref, "refs/remotes/")
	best := ""
	for _, r := range remotes {
		if strings.HasPrefix(name, r.Name+"/") && len(r.Name) > len(best) {
			best = r.Name
		}
	}
	return best
}

// audit records something that happened, in the event log, once it has happened.
func (s *GitControl) audit(ctx context.Context, projectID string, typ domain.EventType, taskID, runID string, payload any) {
	// The action is already done: a failure to record it must not turn into a failure of the action.
	ctx = context.WithoutCancel(ctx)
	if err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		ev := newEvent(typ, payload)
		ev.ProjectID, ev.TaskID, ev.RunID = projectID, taskID, runID
		return em.emit(ev)
	}); err != nil {
		s.log().Warn("could not record a git event", "type", typ, "project", projectID, "err", err)
	}
}

// ---- fetch ----

// Fetch updates what this repository knows about its remotes: the remote-tracking
// branches, and nothing else. No branch of yours, no checkout and no worktree is
// touched, and nothing is merged.
func (s *GitControl) Fetch(ctx context.Context, projectID string) (*domain.GitActionResult, error) {
	unlock, err := s.lock(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(t.repo.Remotes) == 0 {
		return &domain.GitActionResult{Action: ActionFetch, Outcome: domain.OutcomeNoop, Message: "This repository has no remote to fetch from."}, nil
	}
	res := &domain.GitActionResult{Action: ActionFetch, Outcome: domain.OutcomeDone}
	var names, failures []string
	var failed *gitrepo.RemoteResult
	for _, r := range t.repo.Remotes {
		got, err := s.Git.Fetch(ctx, t.root, r.Name)
		if err != nil {
			return nil, err
		}
		if got.Outcome == domain.OutcomeDone {
			names = append(names, r.Name)
			continue
		}
		if failed == nil {
			f := got
			failed = &f
		}
		failures = append(failures, r.Name+": "+got.Message)
		if res.Git == "" {
			res.Git = got.Detail
		}
	}
	res.Local = &domain.GitLocalEffect{Note: "Remote-tracking branches were updated. Your own branches, your checkout and every worktree were left alone."}
	res.Remote = &domain.GitRemoteEffect{Remote: strings.Join(names, ", "), Verified: failed == nil, CheckedAt: s.now()}
	switch {
	case failed == nil:
		res.Message = "Fetched " + strings.Join(names, ", ") + "."
	case len(names) == 0:
		res.Outcome = failed.Outcome
		res.Message = strings.Join(failures, "; ")
		res.Local = nil
	default:
		res.Outcome = failed.Outcome
		res.Message = "Fetched " + strings.Join(names, ", ") + ", but " + strings.Join(failures, "; ")
	}
	res.OK = res.Outcome == domain.OutcomeDone
	if len(names) > 0 {
		s.audit(ctx, projectID, domain.EventGitFetched, "", "", res)
	}
	return res, nil
}

// ---- push ----

// PushInput says what to push. ExpectedSha is the branch tip the user was looking at.
type PushInput struct {
	Branch      string
	ExpectedSha string
	Remote      string // optional; chosen if the branch has an upstream, else origin, else the only remote
}

// Push publishes a local branch to a remote under the same name, with a plain
// fast-forward push: never forced. If the remote has commits the branch lacks,
// Git refuses and so does this, saying so. A success is checked by asking the
// remote itself where the branch now is.
func (s *GitControl) Push(ctx context.Context, projectID string, in PushInput) (*domain.GitActionResult, error) {
	unlock, err := s.lock(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	sha, bl, err := s.tip(ctx, t.root, in.Branch)
	if err != nil {
		return nil, err
	}
	if bl != nil {
		return refuse(ActionPush, "Nothing was pushed.", []domain.GitBlocker{*bl}, nil), nil
	}
	if in.ExpectedSha == "" {
		return refuse(ActionPush, "Nothing was pushed.", []domain.GitBlocker{blocker(domain.BlockInvalidInput, "the commit you were looking at must be given, so a branch that moved is not pushed unseen")}, nil), nil
	}
	if in.ExpectedSha != sha {
		return refuse(ActionPush, "Nothing was pushed: the branch changed since you looked.", []domain.GitBlocker{
			blocker(domain.BlockBranchMoved, "%s is now at %s, not %s. Refresh and look again.", in.Branch, short(sha), short(in.ExpectedSha)),
		}, nil), nil
	}

	refs, err := s.Git.ListRefs(ctx, t.root)
	if err != nil {
		return nil, err
	}
	var self gitrepo.RefInfo
	byRef := map[string]gitrepo.RefInfo{}
	for _, r := range refs {
		byRef[r.Ref] = r
		if r.Ref == "refs/heads/"+in.Branch {
			self = r
		}
	}
	remote, bl := pickRemote(t.repo.Remotes, in.Remote, self.UpstreamRef, in.Branch)
	if bl != nil {
		return refuse(ActionPush, "Nothing was pushed.", []domain.GitBlocker{*bl}, nil), nil
	}
	trackRef := "refs/remotes/" + remote + "/" + in.Branch
	before := byRef[trackRef].Sha

	pushed, err := s.Git.Push(ctx, t.root, gitrepo.PushRequest{Remote: remote, Branch: in.Branch, Sha: sha})
	if err != nil {
		return nil, err
	}
	res := &domain.GitActionResult{Action: ActionPush, Outcome: pushed.Outcome, Git: pushed.Detail}
	if pushed.Outcome != domain.OutcomeDone {
		res.Message = pushed.Message
		res.Remote = &domain.GitRemoteEffect{Remote: remote, Ref: "refs/heads/" + in.Branch, Verified: false, CheckedAt: s.now(), Note: "the push did not complete, so the remote is unchanged as far as Dev Board can tell"}
		return res, nil
	}

	// Git said it worked. Ask the remote, rather than trusting that.
	res.OK = true
	after, _ := s.Git.ResolveCommit(ctx, t.root, trackRef)
	res.Local = &domain.GitLocalEffect{Ref: trackRef, Before: before, After: after, Note: "your remote-tracking branch " + remote + "/" + in.Branch + " was updated"}
	eff := &domain.GitRemoteEffect{Remote: remote, Ref: "refs/heads/" + in.Branch, CheckedAt: s.now()}
	res.Remote = eff
	got, found, aerr := s.Git.RemoteBranch(ctx, t.root, remote, in.Branch)
	switch {
	case aerr != nil:
		eff.Note = "git reported success, but " + remote + " could not be asked to confirm it"
		res.Warnings = append(res.Warnings, "The push was not confirmed by the remote.")
	case !found:
		eff.Note = "git reported success, but " + remote + " does not list the branch"
		res.Warnings = append(res.Warnings, "The remote does not show the branch: the push may not have taken effect.")
	case got != sha:
		eff.Sha = got
		eff.Note = fmt.Sprintf("%s reports %s at %s, not %s", remote, in.Branch, short(got), short(sha))
		res.Warnings = append(res.Warnings, "The remote has a different commit than the one pushed.")
	default:
		eff.Sha, eff.Verified = got, true
	}
	switch {
	case pushed.UpToDate:
		res.Message = fmt.Sprintf("Nothing to push: %s already has %s at %s.", remote, in.Branch, short(sha))
	case eff.Verified:
		res.Message = fmt.Sprintf("Pushed %s to %s, and %s confirms it is at %s.", in.Branch, remote, remote, short(sha))
	default:
		res.Message = fmt.Sprintf("Git pushed %s to %s, but the remote did not confirm it: %s.", in.Branch, remote, eff.Note)
		res.OK = false
		res.Outcome = domain.OutcomeUnverified
	}
	if self.UpstreamRef == "" && eff.Verified {
		if err := s.Git.SetUpstream(ctx, t.root, in.Branch, remote); err != nil {
			res.Warnings = append(res.Warnings, "The branch was pushed but its upstream could not be set: "+err.Error())
		} else {
			res.Local.Note += "; it now tracks " + remote + "/" + in.Branch
		}
	}
	if res.OK {
		a, _ := s.associations(ctx, projectID)
		o := domain.GitBranchOwnership{}
		if a != nil {
			o = a.ownership(in.Branch)
		}
		s.audit(ctx, projectID, domain.EventGitPushed, o.TaskID, o.RunID, res)
	}
	return res, nil
}

// pickRemote chooses the remote to push to, or says why it cannot.
func pickRemote(remotes []domain.GitRemote, requested, upstreamRef, branch string) (string, *domain.GitBlocker) {
	fail := func(code, format string, args ...any) (string, *domain.GitBlocker) {
		b := blocker(code, format, args...)
		return "", &b
	}
	if len(remotes) == 0 {
		return fail(domain.BlockNoRemote, "this repository has no remote to push to")
	}
	has := func(name string) bool {
		for _, r := range remotes {
			if r.Name == name {
				return true
			}
		}
		return false
	}
	if requested != "" {
		if !has(requested) {
			return fail(domain.BlockInvalidInput, "%q is not a remote of this repository", requested)
		}
		return requested, nil
	}
	if upstreamRef != "" {
		if r := remoteOfRef(upstreamRef, remotes); r != "" {
			if rest := strings.TrimPrefix(strings.TrimPrefix(upstreamRef, "refs/remotes/"), r+"/"); rest != branch {
				return fail(domain.BlockRemoteDiffers, "%s tracks %s/%s, a branch of a different name. Dev Board pushes a branch to the branch of the same name, so push it from your terminal.", branch, r, rest)
			}
			return r, nil
		}
	}
	if has(defaultRemoteName) {
		return defaultRemoteName, nil
	}
	if len(remotes) == 1 {
		return remotes[0].Name, nil
	}
	return fail(domain.BlockAmbiguousRemote, "there are several remotes and none is called origin: say which one")
}

// ---- merge ----

// MergeInput names a merge. BranchSha and TargetSha are the commits the user was looking at.
type MergeInput struct {
	Branch    string
	BranchSha string
	Target    string
	TargetSha string
	Strategy  string
}

// mergeFacts is what a plan found that executing it needs.
type mergeFacts struct {
	dir       string // the checkout the target is in
	taskID    string
	taskTitle string
	runID     string
}

// MergePlan refreshes distributed tracking refs and checks a merge. The same check runs again, under
// the project's lock, when the merge is asked for.
func (s *GitControl) MergePlan(ctx context.Context, projectID string, in MergeInput) (*domain.GitMergePlan, error) {
	unlock, err := s.refreshReview(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	plan, _, err := s.planMerge(ctx, t, a, in)
	return plan, err
}

// planMerge is the whole safety check of a merge.
//
// The merge goes into the project's target branch, which must be checked out in
// some worktree (Git can only merge into a checked-out branch, and Dev Board will
// not check anything out for you), with nothing uncommitted in that checkout that
// the merge could mix itself into. It never resolves conflicts: if Git's own
// simulation says the merge conflicts, that is a blocker.
func (s *GitControl) planMerge(ctx context.Context, t *gitCtx, a *gitAssoc, in MergeInput) (*domain.GitMergePlan, *mergeFacts, error) {
	strategy, err := domain.ValidateMergeStrategy(in.Strategy)
	if err != nil {
		return nil, nil, err
	}
	plan := &domain.GitMergePlan{
		Branch: in.Branch, Strategy: strategy, Blockers: []domain.GitBlocker{}, Warnings: []string{},
		Relation: domain.RelUnknown, CheckedAt: s.now(), Conflicts: domain.GitConflictCheck{Method: domain.CheckNone, Result: domain.ConflictUnknown},
	}
	facts := &mergeFacts{}
	block := func(code, format string, args ...any) {
		plan.Blockers = append(plan.Blockers, blocker(code, format, args...))
	}
	warn := func(format string, args ...any) { plan.Warnings = append(plan.Warnings, fmt.Sprintf(format, args...)) }
	finish := func() (*domain.GitMergePlan, *mergeFacts, error) {
		plan.CanMerge = len(plan.Blockers) == 0
		return plan, facts, nil
	}

	// The branch.
	branchSha, bl, err := s.tip(ctx, t.root, in.Branch)
	if err != nil {
		return nil, nil, err
	}
	if bl != nil {
		plan.Blockers = append(plan.Blockers, *bl)
		return finish()
	}
	plan.BranchSha = branchSha
	if in.BranchSha != "" && in.BranchSha != branchSha {
		block(domain.BlockBranchMoved, "%s is now at %s, not %s, where you looked. Refresh and review it again.", in.Branch, short(branchSha), short(in.BranchSha))
	}

	// The target.
	ti, err := s.Git.DetectTarget(ctx, t.root)
	if err != nil {
		return nil, nil, err
	}
	if ti.Name == "" {
		block(domain.BlockTargetMissing, "no target branch could be determined for this repository")
		return finish()
	}
	plan.Target = ti.Name
	if in.Target != "" && in.Target != ti.Name {
		block(domain.BlockTargetMismatch, "the target is %s, not %s. Dev Board only merges into the project's target branch.", ti.Name, in.Target)
	}
	if !ti.LocalExists {
		block(domain.BlockTargetMissing, "%s exists only on the remote on this computer. Check it out here first; Dev Board will not create it for you.", ti.Name)
		return finish()
	}
	if len(a.remoteRuns) > 0 {
		remoteSha, err := s.Git.ResolveCommit(ctx, t.root, "refs/remotes/origin/"+ti.Name)
		if err != nil {
			return nil, nil, err
		}
		if remoteSha != "" {
			current, err := s.Git.IsAncestor(ctx, t.root, remoteSha, ti.Sha)
			if err != nil {
				return nil, nil, err
			}
			if !current {
				block(domain.BlockTargetMoved, "The remote target advanced on another machine. Update %s locally, then review the merge again.", ti.Name)
			}
		}
	}
	plan.TargetSha = ti.Sha
	if in.TargetSha != "" && in.TargetSha != ti.Sha {
		block(domain.BlockTargetMoved, "%s is now at %s, not %s, where you looked. Refresh and review it again.", ti.Name, short(ti.Sha), short(in.TargetSha))
	}
	if in.Branch == ti.Name {
		block(domain.BlockIsTarget, "%s is the target branch: there is nothing to merge it into", in.Branch)
		return finish()
	}

	// Ancestry and divergence, measured now.
	behind, ahead, err := s.Git.Divergence(ctx, t.root, ti.Sha, branchSha)
	if err != nil {
		return nil, nil, err
	}
	plan.Ahead, plan.Behind = ahead, behind
	plan.Relation = domain.ClassifyRelation(ahead, behind)
	plan.FastForwardable = behind == 0 && ahead > 0
	mb, err := s.Git.MergeBase(ctx, t.root, ti.Sha, branchSha)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case ahead == 0:
		block(domain.BlockAlreadyMerged, "everything on %s is already in %s: there is nothing to merge", in.Branch, ti.Name)
		return finish()
	case mb == "":
		block(domain.BlockUnrelated, "%s and %s share no history, and Dev Board will not merge unrelated histories", in.Branch, ti.Name)
		return finish()
	}
	if strategy == domain.MergeFastForward && behind > 0 {
		block(domain.BlockNotFastForward, "%s has moved on by %d commit%s since the branch left it, so it cannot be fast-forwarded. Use a merge commit, or update the branch first.", ti.Name, behind, plural(behind))
	}
	if behind > 0 {
		warn("%s has %d commit%s the branch does not: a merge commit will combine them.", ti.Name, behind, plural(behind))
	}

	// What it would bring in.
	files, truncated, err := s.Git.DiffFiles(ctx, t.root, mb, branchSha, maxCompareFiles)
	if err != nil {
		return nil, nil, err
	}
	plan.FilesChanged = len(files)
	for _, f := range files {
		plan.Additions += f.Additions
		plan.Deletions += f.Deletions
	}

	// The branch's own state: a run that is still at work moves the branch.
	own := a.ownership(in.Branch)
	facts.taskID, facts.taskTitle, facts.runID = own.TaskID, own.TaskTitle, own.RunID
	for _, r := range a.runsOn(in.Branch) {
		switch {
		case r.State == domain.RunStarting || r.State == domain.RunRunning || (r.State == domain.RunWaitingForUser && r.Waiting == domain.WaitQuestion):
			block(domain.BlockRunActive, "an agent is still working on %s (run %s is %s). Wait for it, or finish or stop the run first.", in.Branch, r.ID, strings.ReplaceAll(string(r.State), "_", " "))
		case r.State.Active():
			warn("The agent's session on %s is still open (%s). Merging does not stop it, and it can keep committing to the branch.", in.Branch, strings.ReplaceAll(string(r.State), "_", " "))
		}
	}

	// Where the merge would happen, and whether anything there could be lost or mixed up.
	entries, err := s.Git.ListWorktrees(ctx, t.root)
	if err != nil {
		return nil, nil, err
	}
	var targetEntry, branchEntry *gitrepo.WorktreeEntry
	for i := range entries {
		switch entries[i].Branch {
		case ti.Name:
			targetEntry = &entries[i]
		case in.Branch:
			branchEntry = &entries[i]
		}
	}
	if branchEntry != nil {
		if _, err := os.Stat(branchEntry.Path); err == nil {
			if tree, err := s.Git.Status(ctx, branchEntry.Path); err == nil && tree.Counts.Dirty() {
				warn("The worktree of %s has %d uncommitted change%s. They are not on the branch, so they will not be part of this merge.", in.Branch, tree.Counts.Total(), plural(tree.Counts.Total()))
			}
		}
	}
	if targetEntry == nil {
		block(domain.BlockNotCheckedOut, "%s is not checked out in any worktree. Git can only merge into a checked-out branch, and Dev Board will not switch your checkout. Check %s out, then try again.", ti.Name, ti.Name)
	} else {
		facts.dir = targetEntry.Path
		s.checkMergeCheckout(ctx, a, targetEntry, files, truncated, block)
	}

	// Conflicts.
	s.checkConflicts(ctx, t, plan, mb, files, block, warn)

	// The target against its own remote.
	if up := upstreamFromRefs(ctx, s, t.root, ti.Name); up.State == domain.UpstreamBehind || up.State == domain.UpstreamDiverged {
		warn("Your %s is %s %s (as of the last fetch). A merge made now will not be on the remote until you push it, and the remote has commits you do not.", ti.Name, up.State, up.Name)
	}
	return finish()
}

// upstreamFromRefs reads a local branch's upstream state.
func upstreamFromRefs(ctx context.Context, s *GitControl, root, branch string) domain.GitUpstream {
	refs, err := s.Git.ListRefs(ctx, root)
	if err != nil {
		return domain.GitUpstream{State: domain.UpstreamNone}
	}
	byRef := map[string]gitrepo.RefInfo{}
	for _, r := range refs {
		byRef[r.Ref] = r
	}
	return upstreamOf(byRef["refs/heads/"+branch], byRef)
}

// checkMergeCheckout blocks a merge into a checkout that has something in it a
// merge could destroy or confuse.
func (s *GitControl) checkMergeCheckout(ctx context.Context, a *gitAssoc, e *gitrepo.WorktreeEntry, files []domain.GitDiffFile, filesTruncated bool, block func(code, format string, args ...any)) {
	if _, err := os.Stat(e.Path); err != nil || e.Prunable {
		block(domain.BlockNotCheckedOut, "the worktree of the target at %s is gone", e.Path)
		return
	}
	if rec, ok := a.byPath[canonPath(e.Path)]; ok {
		for _, r := range a.runsByWT[rec.ID] {
			if r.State.Active() {
				block(domain.BlockRunActive, "the target is checked out in a Dev Board worktree where run %s is active", r.ID)
				return
			}
		}
	}
	tree, err := s.Git.Status(ctx, e.Path)
	if err != nil {
		block(domain.BlockUnverifiable, "the checkout of the target at %s could not be read, so nothing can be said about what a merge would touch: %v", e.Path, err)
		return
	}
	if tree.Operation != "" {
		block(domain.BlockOperation, "a %s is unfinished in %s. Finish or abort it there first.", tree.Operation, e.Path)
	}
	if tree.Counts.Conflicted > 0 {
		block(domain.BlockDirty, "%s has %d file%s in conflict", e.Path, tree.Counts.Conflicted, plural(tree.Counts.Conflicted))
	}
	if n := tree.Counts.Staged + tree.Counts.Unstaged; n > 0 {
		block(domain.BlockDirty, "%s has %d uncommitted change%s to tracked files. Commit or stash them first, so the merge cannot mix with them or discard them.", e.Path, n, plural(n))
	}
	if tree.Counts.Untracked == 0 {
		return
	}
	// Untracked files are not touched by a merge, unless the merge brings a file of the same name.
	if filesTruncated {
		block(domain.BlockUnverifiable, "the branch changes too many files to check them against the untracked files in %s", e.Path)
		return
	}
	for _, f := range files {
		if f.Status != domain.FileAdded && f.Status != domain.FileRenamed && f.Status != domain.FileCopied {
			continue
		}
		if _, err := os.Lstat(filepath.Join(e.Path, filepath.FromSlash(f.Path))); err != nil {
			continue
		}
		clash := false
		for _, u := range tree.Untracked {
			if u.Path == f.Path || strings.HasSuffix(u.Path, "/") && strings.HasPrefix(f.Path, u.Path) {
				clash = true
			}
		}
		if clash || tree.Truncated {
			block(domain.BlockUntrackedClash, "the merge adds %s, which already exists as an untracked file in %s and would be overwritten", f.Path, e.Path)
		}
	}
}

// checkConflicts asks Git whether the merge would conflict. Git's own simulation
// is used when this Git has it (2.38+): that is a real merge, run in memory,
// and it is as certain as an answer about a moving target can be. Without it only
// an overlap heuristic is possible, and it is reported as a heuristic: it can say
// "possible", never "none".
func (s *GitControl) checkConflicts(ctx context.Context, t *gitCtx, plan *domain.GitMergePlan, mergeBase string, branchFiles []domain.GitDiffFile, block func(code, format string, args ...any), warn func(format string, args ...any)) {
	sim, err := s.Git.MergeSimulation(ctx, t.root, plan.TargetSha, plan.BranchSha)
	switch {
	case err != nil:
		plan.Conflicts = domain.GitConflictCheck{Method: domain.CheckNone, Result: domain.ConflictUnknown, Note: "the merge could not be simulated: " + err.Error()}
		warn("Whether this merge conflicts could not be checked.")
	case sim.Unrelated:
		block(domain.BlockUnrelated, "the histories are unrelated")
	case sim.Supported && sim.Conflicts:
		plan.Conflicts = domain.GitConflictCheck{Method: domain.CheckSimulation, Result: domain.ConflictConflicts, Files: sim.Files,
			Note: "Git merged the two commits in memory and found conflicts. Dev Board never resolves conflicts: merge it yourself, or have the agent update the branch."}
		block(domain.BlockConflicts, "merging %s into %s conflicts in %d file%s: %s", plan.Branch, plan.Target, len(sim.Files), plural(len(sim.Files)), strings.Join(firstN(sim.Files, 5), ", "))
	case sim.Supported:
		plan.Conflicts = domain.GitConflictCheck{Method: domain.CheckSimulation, Result: domain.ConflictClean,
			Note: "Git merged the two commits in memory without conflicts. That is true of the commits as they are now."}
	default:
		onTarget, _, err := s.Git.DiffFiles(ctx, t.root, mergeBase, plan.TargetSha, maxCompareFiles)
		if err != nil {
			plan.Conflicts = domain.GitConflictCheck{Method: domain.CheckNone, Result: domain.ConflictUnknown, Note: "conflicts could not be checked"}
			return
		}
		touched := map[string]bool{}
		for _, f := range onTarget {
			touched[f.Path] = true
			if f.OldPath != "" {
				touched[f.OldPath] = true
			}
		}
		var both []string
		for _, f := range branchFiles {
			if touched[f.Path] || f.OldPath != "" && touched[f.OldPath] {
				both = append(both, f.Path)
			}
		}
		sort.Strings(both)
		if len(both) > 0 {
			plan.Conflicts = domain.GitConflictCheck{Method: domain.CheckOverlap, Result: domain.ConflictPossible, Files: both,
				Note: "This Git cannot simulate a merge. These files were changed on both sides, which is where conflicts happen; Git may still combine them without one."}
			warn("%d file%s changed on both sides: this merge may conflict.", len(both), plural(len(both)))
		} else {
			plan.Conflicts = domain.GitConflictCheck{Method: domain.CheckOverlap, Result: domain.ConflictUnknown,
				Note: "This Git cannot simulate a merge. No file was changed on both sides, which usually means no conflict, but that is not a guarantee."}
		}
	}
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], fmt.Sprintf("and %d more", len(s)-n))
	}
	return s
}

// Merge merges a branch into the project's target branch, locally. It needs the
// commits the user reviewed (BranchSha, TargetSha) and refuses if either has moved.
// Only a person calls it: a finished run never does, and nothing here is reachable
// from the agent runner.
//
// The merge exists only on this computer afterwards. The result says so: pushing
// the target is a separate action, and a GitHub pull request is not affected by it.
func (s *GitControl) Merge(ctx context.Context, projectID string, in MergeInput) (*domain.GitActionResult, error) {
	unlock, err := s.lock(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.refreshDistributed(ctx, projectID); err != nil {
		return nil, err
	}
	if in.BranchSha == "" || in.TargetSha == "" {
		return refuse(ActionMerge, "Nothing was merged.", []domain.GitBlocker{blocker(domain.BlockInvalidInput, "the commits you reviewed (branchSha and targetSha) must be given, so a merge is only made of what you looked at")}, nil), nil
	}
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// Look again, now, under the lock. Nothing from the screen is trusted.
	plan, facts, err := s.planMerge(ctx, t, a, in)
	if err != nil {
		return nil, err
	}
	if !plan.CanMerge {
		r := refuse(ActionMerge, "Nothing was merged: it is not safe to.", plan.Blockers, plan.Warnings)
		return r, nil
	}

	msg := mergeMessage(plan.Branch, plan.Target, facts.taskTitle)
	got, err := s.Git.Merge(ctx, facts.dir, gitrepo.MergeRequest{Sha: plan.BranchSha, Message: msg, Strategy: plan.Strategy})
	if err != nil {
		return nil, err
	}
	res := &domain.GitActionResult{Action: ActionMerge, Outcome: got.Outcome, Warnings: plan.Warnings, Git: got.Detail}
	switch got.Outcome {
	case domain.OutcomeDone:
		// Verify, rather than trust the exit status: the target moved and contains the branch.
		merged, verr := s.Git.IsAncestor(ctx, t.root, plan.BranchSha, got.After)
		head, _ := s.Git.ResolveCommit(ctx, t.root, "refs/heads/"+plan.Target)
		if verr != nil || !merged || head != got.After {
			res.Outcome = domain.OutcomeFailed
			res.Message = fmt.Sprintf("Git reported a merge, but %s does not now contain %s as it should. Check the repository.", plan.Target, plan.Branch)
			return res, nil
		}
		res.OK = true
		res.Local = &domain.GitLocalEffect{Ref: "refs/heads/" + plan.Target, Before: got.Before, After: got.After,
			Note: fmt.Sprintf("%s now contains %s (%d commit%s)", plan.Target, plan.Branch, plan.Ahead, plural(plan.Ahead))}
		res.Message = fmt.Sprintf("Merged %s into %s on this computer. Nothing was pushed: the remote and any pull request are unchanged.", plan.Branch, plan.Target)
		s.audit(ctx, projectID, domain.EventGitMerged, facts.taskID, facts.runID, struct {
			*domain.GitActionResult
			Branch    string `json:"branch"`
			BranchSha string `json:"branchSha"`
			Target    string `json:"target"`
		}{res, plan.Branch, plan.BranchSha, plan.Target})
	case domain.OutcomeNoop:
		res.Message = fmt.Sprintf("%s already contains %s: nothing changed.", plan.Target, plan.Branch)
	case domain.OutcomeConflict:
		res.Message = fmt.Sprintf("The merge conflicted in %d file%s and was undone: %s.", len(got.Conflicts), plural(len(got.Conflicts)), strings.Join(firstN(got.Conflicts, 5), ", "))
		if !got.Undone {
			res.Message += " The checkout may not have been restored completely: check it."
		}
		res.Blockers = []domain.GitBlocker{blocker(domain.BlockConflicts, "conflicts in %s", strings.Join(got.Conflicts, ", "))}
	default:
		res.Outcome = domain.OutcomeFailed
		res.Message = "Git could not complete the merge."
		if got.Undone {
			res.Message += " Nothing was changed."
		} else {
			res.Message += " The checkout may have been left changed: check it."
		}
	}
	return res, nil
}

// mergeMessage is the merge commit's message. The task title is data from the
// user and is cleaned of anything that is not plain text.
func mergeMessage(branch, target, taskTitle string) string {
	msg := fmt.Sprintf("Merge branch '%s' into %s", branch, target)
	title := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, taskTitle)
	if title = strings.TrimSpace(title); title != "" {
		if len(title) > 200 {
			title = title[:200]
		}
		msg += "\n\nTask: " + title
	}
	return msg
}

// ---- delete ----

// DeleteInput names a branch to delete. BranchSha is the commit the user was looking at.
type DeleteInput struct {
	Branch       string
	BranchSha    string
	DeleteRemote bool
}

// DeletePlan checks a deletion and changes nothing.
func (s *GitControl) DeletePlan(ctx context.Context, projectID string, in DeleteInput) (*domain.GitDeletePlan, error) {
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.planDelete(ctx, t, a, in)
}

// planDelete is the whole safety check of deleting a branch. A branch is deleted
// only if all of these hold: it is Dev Board's own (created by it, per its
// records), it is not protected, it is checked out nowhere, nothing unmerged would
// be lost, and it is still at the commit the user saw.
func (s *GitControl) planDelete(ctx context.Context, t *gitCtx, a *gitAssoc, in DeleteInput) (*domain.GitDeletePlan, error) {
	plan := &domain.GitDeletePlan{Branch: in.Branch, Blockers: []domain.GitBlocker{}, Warnings: []string{}, CheckedAt: s.now()}
	block := func(code, format string, args ...any) {
		plan.Blockers = append(plan.Blockers, blocker(code, format, args...))
	}
	finish := func() (*domain.GitDeletePlan, error) {
		plan.CanDelete = len(plan.Blockers) == 0
		return plan, nil
	}
	sha, bl, err := s.tip(ctx, t.root, in.Branch)
	if err != nil {
		return nil, err
	}
	if bl != nil {
		plan.Blockers = append(plan.Blockers, *bl)
		return finish()
	}
	plan.BranchSha = sha
	if in.BranchSha != "" && in.BranchSha != sha {
		block(domain.BlockBranchMoved, "%s is now at %s, not %s, where you looked. Refresh and look again.", in.Branch, short(sha), short(in.BranchSha))
	}
	ti, err := s.Git.DetectTarget(ctx, t.root)
	if err != nil {
		return nil, err
	}
	plan.Target = ti.Name
	head := t.repo.CurrentBranch

	if domain.ProtectedBranch(in.Branch, ti.Name, head) {
		block(domain.BlockProtected, "%s is a protected branch (the target, the branch checked out here, or a long-lived name). Dev Board never deletes those.", in.Branch)
	}
	// Ownership: only what Dev Board made.
	own := a.ownership(in.Branch)
	if !own.Created {
		if own.Namespace {
			block(domain.BlockNotOwned, "%s has Dev Board's name but no record that Dev Board created it, so it is treated as yours. Delete it from your terminal if you mean to.", in.Branch)
		} else {
			block(domain.BlockNotOwned, "Dev Board did not create %s, so it will not delete it. Delete it from your terminal if you mean to.", in.Branch)
		}
	}
	if own.ActiveRun {
		block(domain.BlockRunActive, "a run on %s still has a session open. Finish or stop it first.", in.Branch)
	}
	// Checked out anywhere.
	entries, err := s.Git.ListWorktrees(ctx, t.root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Branch == in.Branch {
			if rec, ok := a.byPath[canonPath(e.Path)]; ok {
				block(domain.BlockCheckedOut, "%s is checked out in a Dev Board worktree (%s). Clean up the worktree first; the branch stays when it is removed.", in.Branch, rec.ID)
			} else {
				block(domain.BlockCheckedOut, "%s is checked out at %s", in.Branch, e.Path)
			}
		}
	}

	// Merged: by ancestry, or by a merged pull request with exactly this tip.
	targetTip := ti.Sha
	switch {
	case targetTip == "":
		block(domain.BlockTargetMissing, "there is no target branch to check whether %s was merged into", in.Branch)
	default:
		behind, ahead, err := s.Git.Divergence(ctx, t.root, targetTip, sha)
		if err != nil {
			return nil, err
		}
		_ = behind
		if ahead == 0 {
			plan.Merged, plan.MergedVia = true, "ancestry"
		} else if s.mergedByPullRequest(ctx, t, in.Branch, sha, plan) {
			plan.Merged, plan.MergedVia = true, "pull_request"
		} else {
			are := "are"
			if ahead == 1 {
				are = "is"
			}
			block(domain.BlockNotMerged, "%d commit%s on %s %s not in %s, and no merged pull request has exactly this tip. Deleting it would lose that work.", ahead, plural(ahead), in.Branch, are, ti.Name)
		}
	}

	// The remote branch, as of the last fetch.
	refs, err := s.Git.ListRefs(ctx, t.root)
	if err != nil {
		return nil, err
	}
	var self gitrepo.RefInfo
	byRef := map[string]gitrepo.RefInfo{}
	for _, r := range refs {
		byRef[r.Ref] = r
		if r.Ref == "refs/heads/"+in.Branch {
			self = r
		}
	}
	remote, _ := pickRemote(t.repo.Remotes, "", self.UpstreamRef, in.Branch)
	if remote != "" {
		ref := "refs/remotes/" + remote + "/" + in.Branch
		if r, ok := byRef[ref]; ok {
			plan.RemoteExist, plan.RemoteRef, plan.RemoteSha = true, remote+"/"+in.Branch, r.Sha
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s still exists on %s. Deleting the local branch does not delete it there.", in.Branch, remote))
		}
	}
	plan.CanDelete = len(plan.Blockers) == 0
	if in.DeleteRemote {
		s.checkRemoteDeletion(ctx, t, plan, ti.Sha, block)
	}
	plan.CanDeleteRemote = plan.CanDelete && plan.RemoteExist && s.remoteDeletionEvidence(ctx, t, plan, ti.Sha) == ""
	plan.CanDelete = len(plan.Blockers) == 0
	return plan, nil
}

// remoteDeletionEvidence is empty if the remote branch may be deleted: its tip
// is the branch's own tip, or is already in the target.
func (s *GitControl) remoteDeletionEvidence(ctx context.Context, t *gitCtx, plan *domain.GitDeletePlan, targetSha string) string {
	if !plan.RemoteExist {
		return "there is no remote branch"
	}
	if plan.RemoteSha == plan.BranchSha {
		return ""
	}
	if targetSha != "" && gitrepo.IsCommitID(plan.RemoteSha) {
		if ok, err := s.Git.IsAncestor(ctx, t.root, plan.RemoteSha, targetSha); err == nil && ok {
			return ""
		}
	}
	return fmt.Sprintf("%s is at %s, which is neither the local branch's commit nor in %s", plan.RemoteRef, short(plan.RemoteSha), plan.Target)
}

func (s *GitControl) checkRemoteDeletion(ctx context.Context, t *gitCtx, plan *domain.GitDeletePlan, targetSha string, block func(code, format string, args ...any)) {
	if !plan.RemoteExist {
		block(domain.BlockInvalidInput, "there is no remote branch to delete (as of the last fetch)")
		return
	}
	if why := s.remoteDeletionEvidence(ctx, t, plan, targetSha); why != "" {
		block(domain.BlockRemoteUnmerged, "%s", why)
	}
}

// mergedByPullRequest is true if GitHub says a pull request of this repository,
// from this branch, was merged with exactly this tip. It is the only evidence
// that a squash- or rebase-merged branch is safe to delete, since its commits are
// not in the target. If GitHub cannot be asked the answer is no.
func (s *GitControl) mergedByPullRequest(ctx context.Context, t *gitCtx, branch, sha string, plan *domain.GitDeletePlan) bool {
	st := &domain.GitHubState{}
	repo, _, err := s.githubRepo(ctx, t, st)
	if err != nil {
		return false
	}
	prs, err := s.GitHub.PullRequests(ctx, repo, 50)
	if err != nil {
		plan.Warnings = append(plan.Warnings, "GitHub could not be asked whether a pull request merged this branch.")
		return false
	}
	for _, pr := range prs {
		if pr.State == "merged" && !pr.CrossRepo && pr.HeadBranch == branch && pr.HeadSha == sha {
			return true
		}
	}
	return false
}

// DeleteBranch deletes a branch Dev Board created, if it is safe to: see planDelete.
// The deletion is conditional on the branch still being at the commit the user
// saw, and the result says how to bring it back.
func (s *GitControl) DeleteBranch(ctx context.Context, projectID string, in DeleteInput) (*domain.GitActionResult, error) {
	unlock, err := s.lock(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if in.BranchSha == "" {
		return refuse(ActionDelete, "Nothing was deleted.", []domain.GitBlocker{blocker(domain.BlockInvalidInput, "the commit you were looking at (branchSha) must be given, so a branch that moved is not deleted unseen")}, nil), nil
	}
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	plan, err := s.planDelete(ctx, t, a, in)
	if err != nil {
		return nil, err
	}
	if !plan.CanDelete {
		return refuse(ActionDelete, "Nothing was deleted: it is not safe to.", plan.Blockers, plan.Warnings), nil
	}
	if err := s.Git.DeleteBranchAt(ctx, t.root, in.Branch, plan.BranchSha); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrInvalid) {
			return refuse(ActionDelete, "Nothing was deleted.", []domain.GitBlocker{blocker(domain.BlockBranchMoved, "%v", err)}, plan.Warnings), nil
		}
		return nil, err
	}
	own := a.ownership(in.Branch)
	res := &domain.GitActionResult{
		Action: ActionDelete, Outcome: domain.OutcomeDone, OK: true, Warnings: plan.Warnings,
		Local: &domain.GitLocalEffect{Ref: "refs/heads/" + in.Branch, Before: plan.BranchSha,
			Note: fmt.Sprintf("deleted. It was merged (%s). To bring it back: git branch %s %s", strings.ReplaceAll(plan.MergedVia, "_", " "), in.Branch, plan.BranchSha)},
		Message: fmt.Sprintf("Deleted the local branch %s (it was at %s).", in.Branch, short(plan.BranchSha)),
	}
	if in.DeleteRemote && plan.CanDeleteRemote {
		s.deleteRemote(ctx, t, plan, res)
	} else if in.DeleteRemote {
		res.Outcome, res.OK = domain.OutcomeRefused, false
		res.Message += " The remote branch was NOT deleted: it does not meet the conditions."
		res.Blockers = plan.Blockers
	}
	s.audit(ctx, projectID, domain.EventGitBranchDeleted, own.TaskID, own.RunID, res)
	return res, nil
}

func (s *GitControl) deleteRemote(ctx context.Context, t *gitCtx, plan *domain.GitDeletePlan, res *domain.GitActionResult) {
	remote, _, _ := strings.Cut(plan.RemoteRef, "/")
	for _, r := range t.repo.Remotes {
		if strings.HasPrefix(plan.RemoteRef, r.Name+"/") && len(r.Name) > len(remote) {
			remote = r.Name
		}
	}
	eff := &domain.GitRemoteEffect{Remote: remote, Ref: "refs/heads/" + plan.Branch, CheckedAt: s.now()}
	res.Remote = eff
	fail := func(outcome, why string) {
		res.Outcome, res.OK = outcome, false
		eff.Note = why
		res.Message = fmt.Sprintf("Deleted the local branch %s, but NOT the branch on %s: %s", plan.Branch, remote, why)
	}
	// Ask the remote itself, not the tracking ref, and delete only what we looked at.
	live, found, err := s.Git.RemoteBranch(ctx, t.root, remote, plan.Branch)
	switch {
	case err != nil:
		fail(domain.OutcomeUnavailable, "the remote could not be asked")
		return
	case !found:
		eff.Verified, eff.Note = true, "it was already gone from "+remote
		res.Message = fmt.Sprintf("Deleted the local branch %s. It was already gone from %s.", plan.Branch, remote)
		return
	case live != plan.RemoteSha:
		fail(domain.OutcomeRefused, fmt.Sprintf("it is now at %s, not %s where you looked", short(live), short(plan.RemoteSha)))
		return
	}
	got, err := s.Git.DeleteRemoteBranch(ctx, t.root, remote, plan.Branch, live)
	if err != nil {
		fail(domain.OutcomeFailed, err.Error())
		return
	}
	if got.Outcome != domain.OutcomeDone {
		fail(got.Outcome, got.Message)
		res.Git = got.Detail
		return
	}
	if _, still, err := s.Git.RemoteBranch(ctx, t.root, remote, plan.Branch); err != nil || still {
		fail(domain.OutcomeUnverified, "git reported success, but the remote could not be confirmed to have deleted it")
		return
	}
	eff.Verified, eff.Note = true, "confirmed gone from "+remote
	res.Message = fmt.Sprintf("Deleted %s locally and on %s (confirmed).", plan.Branch, remote)
}

// ---- worktrees ----

// CleanInput names a Dev Board worktree to remove. HeadSha is the commit it was at when looked at.
type CleanInput struct {
	WorktreeID string
	HeadSha    string
}

// CleanPlan checks removing a worktree and changes nothing.
func (s *GitControl) CleanPlan(ctx context.Context, projectID string, in CleanInput) (*domain.GitCleanPlan, error) {
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	plan, _, err := s.planClean(ctx, t, a, in)
	return plan, err
}

// planClean is the whole safety check of removing a worktree. Only a directory
// Dev Board's own records vouch for, that lies inside Dev Board's worktree
// directory, that Git agrees is a worktree of this repository, with nothing
// uncommitted in it and no run using it, is ever removed. Removing it never
// touches its branch.
func (s *GitControl) planClean(ctx context.Context, t *gitCtx, a *gitAssoc, in CleanInput) (*domain.GitCleanPlan, *domain.Worktree, error) {
	rec, ok := a.byID[in.WorktreeID]
	if !ok {
		return nil, nil, fmt.Errorf("worktree %s: %w", in.WorktreeID, domain.ErrNotFound)
	}
	plan := &domain.GitCleanPlan{WorktreeID: rec.ID, Path: rec.Path, Branch: rec.Branch, Blockers: []domain.GitBlocker{}, Warnings: []string{}, CheckedAt: s.now()}
	block := func(code, format string, args ...any) {
		plan.Blockers = append(plan.Blockers, blocker(code, format, args...))
	}
	finish := func() (*domain.GitCleanPlan, *domain.Worktree, error) {
		plan.CanClean = len(plan.Blockers) == 0
		return plan, &rec, nil
	}
	if rec.State != domain.WorktreeActive {
		block(domain.BlockWorktreeUnknown, "this worktree was already removed")
		return finish()
	}
	// Inside Dev Board's own worktree directory, and not through a symlink.
	if s.Worktrees == nil || s.Worktrees.Root == "" || !pathInside(s.Worktrees.Root, rec.Path) {
		block(domain.BlockWorktreeOutside, "%s is not inside Dev Board's worktree directory, so Dev Board will not delete it", rec.Path)
		return finish()
	}
	if err := requireCanonical(rec.Path); err != nil {
		block(domain.BlockWorktreeOutside, "%v", err)
		return finish()
	}
	for _, r := range a.runsByWT[rec.ID] {
		if r.State.Active() {
			block(domain.BlockRunActive, "run %s still has a session on this worktree. Finish or stop it first.", r.ID)
		}
	}

	entries, err := s.Git.ListWorktrees(ctx, t.root)
	if err != nil {
		return nil, nil, err
	}
	var entry *gitrepo.WorktreeEntry
	for i := range entries {
		if canonPath(entries[i].Path) == canonPath(rec.Path) {
			entry = &entries[i]
		}
	}
	_, statErr := os.Lstat(rec.Path)
	gone := errors.Is(statErr, os.ErrNotExist)
	switch {
	case entry == nil && !gone:
		block(domain.BlockWorktreeUnknown, "Git does not list %s as a worktree of this repository, so Dev Board will not delete the directory", rec.Path)
		return finish()
	case entry == nil && gone:
		plan.Missing = true
		plan.Warnings = append(plan.Warnings, "The directory is already gone; this only forgets the record of it.")
		return finish()
	case gone:
		plan.Missing = true
		plan.Warnings = append(plan.Warnings, "The directory is already gone; this only forgets the record of it.")
		return finish()
	}
	plan.Head = entry.Head
	if in.HeadSha != "" && in.HeadSha != entry.Head {
		block(domain.BlockWorktreeChanged, "the worktree is now at %s, not %s where you looked. Refresh and look again.", short(entry.Head), short(in.HeadSha))
	}
	if entry.Locked {
		block(domain.BlockWorktreeLocked, "the worktree is locked%s", lockReason(entry.LockReason))
	}
	if entry.Detached || entry.Branch != rec.Branch {
		block(domain.BlockWorktreeChanged, "the worktree was recorded on %s but is not on it any more. Someone changed it outside Dev Board, so it is left alone.", rec.Branch)
	}
	tree, err := s.Git.Status(ctx, rec.Path)
	if err != nil {
		block(domain.BlockUnverifiable, "the worktree could not be read, so it cannot be shown to be clean: %v", err)
		return finish()
	}
	c := tree.Counts
	plan.Dirty = &c
	if tree.Operation != "" {
		block(domain.BlockOperation, "a %s is unfinished in the worktree", tree.Operation)
	}
	if c.Dirty() {
		block(domain.BlockWorktreeDirty, "the worktree has uncommitted work (%d staged, %d modified, %d untracked, %d in conflict). Removing it would destroy that. Dev Board never does; commit it or discard it yourself.", c.Staged, c.Unstaged, c.Untracked, c.Conflicted)
	}
	if tree.Truncated {
		block(domain.BlockUnverifiable, "there are too many changes to check")
	}
	// Informational: the branch survives, and so does its work.
	if ti, err := s.Git.DetectTarget(ctx, t.root); err == nil && ti.Sha != "" && gitrepo.IsCommitID(entry.Head) {
		if _, ahead, err := s.Git.Divergence(ctx, t.root, ti.Sha, entry.Head); err == nil && ahead > 0 {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("The branch %s keeps %d commit%s that are not in %s. They stay on the branch; only the directory is removed.", rec.Branch, ahead, plural(ahead), ti.Name))
		}
	}
	return finish()
}

func lockReason(r string) string {
	if r == "" {
		return ""
	}
	return " (" + r + ")"
}

// pathInside reports whether child is strictly below parent, by whole components.
func pathInside(parent, child string) bool {
	parent, child = filepath.Clean(parent), filepath.Clean(child)
	return child != parent && strings.HasPrefix(child, strings.TrimSuffix(parent, string(filepath.Separator))+string(filepath.Separator))
}

// CleanWorktree removes a Dev Board worktree directory, if it is safe to: see
// planClean. The branch is kept. The steps follow the worktree records' own
// order (begin removal, delete, finish), so a crash leaves a record that can be
// retried and never a directory nobody knows about.
func (s *GitControl) CleanWorktree(ctx context.Context, projectID string, in CleanInput) (*domain.GitActionResult, error) {
	unlock, err := s.lock(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	plan, rec, err := s.planClean(ctx, t, a, in)
	if err != nil {
		return nil, err
	}
	if !plan.CanClean {
		return refuse(ActionCleanTree, "Nothing was removed: it is not safe to.", plan.Blockers, plan.Warnings), nil
	}
	cur := *rec
	if !cur.Removing() {
		got, err := s.Worktrees.BeginRemoval(ctx, cur.ID, cur.Version)
		if err != nil {
			if errors.Is(err, domain.ErrConflict) {
				return refuse(ActionCleanTree, "Nothing was removed.", []domain.GitBlocker{blocker(domain.BlockRunActive, "%v", err)}, plan.Warnings), nil
			}
			return nil, err
		}
		cur = *got
	}
	if err := s.Git.RemoveCleanWorktree(ctx, t.root, cur.Path); err != nil {
		return &domain.GitActionResult{
			Action: ActionCleanTree, Outcome: domain.OutcomeFailed, Warnings: plan.Warnings,
			Message: "Git refused to remove the worktree, so it was left as it is: " + err.Error(),
			Git:     err.Error(),
		}, nil
	}
	if _, err := s.Worktrees.FinishRemoval(ctx, cur.ID, cur.Version); err != nil && !errors.Is(err, domain.ErrConflict) {
		return nil, err
	}
	own := a.ownership(cur.Branch)
	res := &domain.GitActionResult{
		Action: ActionCleanTree, Outcome: domain.OutcomeDone, OK: true, Warnings: plan.Warnings,
		Local:   &domain.GitLocalEffect{Ref: cur.Path, Before: plan.Head, Note: "the directory was removed; the branch " + cur.Branch + " was kept"},
		Message: fmt.Sprintf("Removed the worktree of %s. The branch was kept.", cur.Branch),
	}
	s.audit(ctx, projectID, domain.EventGitTreeCleaned, own.TaskID, own.RunID, res)
	return res, nil
}

// ---- pull requests ----

// PRInput describes a pull request to open. ExpectedSha is the branch tip the user was looking at.
type PRInput struct {
	Branch      string
	ExpectedSha string
	Title       string
	Body        string
	Draft       bool
}

const (
	maxPRTitle = 256
	maxPRBody  = 60000
)

// CreatePullRequest opens a GitHub pull request from a branch that is already on
// the remote, into the project's target branch, using the user's own GitHub CLI.
// It never pushes: a branch that is not on the remote, or is at a different commit
// there, is refused, because the pull request would not show what the user reviewed.
// Success is only reported once the pull request has been read back from GitHub.
func (s *GitControl) CreatePullRequest(ctx context.Context, projectID string, in PRInput) (*domain.GitActionResult, error) {
	unlock, err := s.lock(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.refreshDistributed(ctx, projectID); err != nil {
		return nil, err
	}
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	no := func(bs ...domain.GitBlocker) (*domain.GitActionResult, error) {
		return refuse(ActionCreatePR, "No pull request was opened.", bs, nil), nil
	}
	title := strings.TrimSpace(in.Title)
	switch {
	case title == "":
		return no(blocker(domain.BlockInvalidInput, "a pull request needs a title"))
	case len([]rune(title)) > maxPRTitle:
		return no(blocker(domain.BlockInvalidInput, "the title is longer than %d characters", maxPRTitle))
	case len(in.Body) > maxPRBody:
		return no(blocker(domain.BlockInvalidInput, "the description is longer than %d characters", maxPRBody))
	case in.ExpectedSha == "":
		return no(blocker(domain.BlockInvalidInput, "the commit you were looking at must be given"))
	}
	sha, bl, err := s.tip(ctx, t.root, in.Branch)
	if err != nil {
		return nil, err
	}
	if bl != nil {
		return no(*bl)
	}
	if in.ExpectedSha != sha {
		return no(blocker(domain.BlockBranchMoved, "%s is now at %s, not %s. Refresh and look again.", in.Branch, short(sha), short(in.ExpectedSha)))
	}
	ti, err := s.Git.DetectTarget(ctx, t.root)
	if err != nil {
		return nil, err
	}
	if ti.Name == "" {
		return no(blocker(domain.BlockTargetMissing, "no target branch could be determined"))
	}
	if in.Branch == ti.Name {
		return no(blocker(domain.BlockIsTarget, "%s is the target branch: a pull request needs another branch", in.Branch))
	}
	if ti.Sha != "" {
		if _, ahead, err := s.Git.Divergence(ctx, t.root, ti.Sha, sha); err == nil && ahead == 0 {
			return no(blocker(domain.BlockAlreadyMerged, "%s has no commits that %s lacks, so there is nothing to propose", in.Branch, ti.Name))
		}
	}

	st := &domain.GitHubState{}
	repo, remote, err := s.githubRepo(ctx, t, st)
	if err != nil {
		return no(blocker(domain.BlockNoGitHub, "%s", st.Message))
	}
	// The pull request is made from the remote's copy of the branch: it has to be there, and at this commit.
	live, found, err := s.Git.RemoteBranch(ctx, t.root, remote, in.Branch)
	if err != nil {
		return &domain.GitActionResult{Action: ActionCreatePR, Outcome: domain.OutcomeUnavailable, Message: "No pull request was opened: " + remote + " could not be asked whether the branch is there.", Git: err.Error()}, nil
	}
	if !found {
		return no(blocker(domain.BlockNotPushed, "%s is not on %s yet. Push it first.", in.Branch, remote))
	}
	if live != sha {
		return no(blocker(domain.BlockRemoteDiffers, "%s has %s at %s but your branch is at %s. A pull request would show %s's version. Push first.", remote, in.Branch, short(live), short(sha), remote))
	}
	prs, err := s.GitHub.PullRequests(ctx, repo, 50)
	if err != nil {
		return githubFailed(ActionCreatePR, err), nil
	}
	for _, pr := range prs {
		if pr.State == "open" && !pr.CrossRepo && pr.HeadBranch == in.Branch && pr.BaseBranch == ti.Name {
			pr := pr
			r := refuse(ActionCreatePR, fmt.Sprintf("Pull request #%d is already open for this branch.", pr.Number), []domain.GitBlocker{blocker(domain.BlockPRExists, "#%d %s is already open", pr.Number, pr.Title)}, nil)
			r.PullRequest = &pr
			return r, nil
		}
	}

	pr, err := s.GitHub.CreatePullRequest(ctx, repo, github.CreateRequest{Head: in.Branch, Base: ti.Name, Title: title, Body: in.Body, Draft: in.Draft})
	if err != nil {
		return githubFailed(ActionCreatePR, err), nil
	}
	res := &domain.GitActionResult{Action: ActionCreatePR, PullRequest: &pr}
	res.Remote = &domain.GitRemoteEffect{Remote: remote, Ref: "refs/heads/" + in.Branch, Sha: pr.HeadSha, CheckedAt: s.now()}
	if pr.State == "open" && pr.HeadBranch == in.Branch && pr.BaseBranch == ti.Name && pr.URL != "" {
		res.Outcome, res.OK = domain.OutcomeDone, true
		res.Remote.Verified = true
		res.Message = fmt.Sprintf("Opened pull request #%d on GitHub (confirmed).", pr.Number)
	} else {
		res.Outcome = domain.OutcomeUnverified
		res.Message = "The GitHub CLI reported success, but the pull request could not be confirmed as open for this branch."
	}
	if res.OK {
		a, _ := s.associations(ctx, projectID)
		o := domain.GitBranchOwnership{}
		if a != nil {
			o = a.ownership(in.Branch)
		}
		s.audit(ctx, projectID, domain.EventGitPRCreated, o.TaskID, o.RunID, res)
	}
	return res, nil
}

// githubFailed words a failure to use GitHub as an action result.
func githubFailed(action string, err error) *domain.GitActionResult {
	st := &domain.GitHubState{}
	ghFailure(st, err)
	out := domain.OutcomeFailed
	switch st.Reason {
	case domain.GHUnauthenticated:
		out = domain.OutcomeAuth
	case domain.GHMissing:
		out = domain.OutcomeUnavailable
	}
	return &domain.GitActionResult{Action: action, Outcome: out, Message: "GitHub: " + st.Message}
}

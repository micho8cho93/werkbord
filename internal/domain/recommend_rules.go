package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Recommend is the whole decision. It runs in three passes, in this order, and a later pass never undoes an earlier one:
//
//  1. deriveRules reads tiers 1 to 3 (what this viewer and application can do, what policies and the repository require,
//     what the person decided) and writes down, for every intent and variant, what forbids it. Nothing else is consulted.
//  2. The candidate builders read tier 4 (verified Git, task, run and pull request state). A candidate is built only for
//     an intent or variant that pass 1 left standing; one that pass 1 removed is recorded as withheld, with its reason.
//  3. Selection reads tier 5 (habits, saved preferences, earlier feedback) and uses it to ORDER the candidates and to pick
//     between variants that are both allowed. It cannot add a candidate, cannot bring back a withheld one, and cannot
//     lower the standing of a required one below the evidence-ranked ones.
//
// The function has no clock, no I/O and no randomness: facts in, set out.
func Recommend(f RecFacts) RecommendationSet {
	// The caller supplies the time: the rules read no clock.
	f.Now = f.Now.UTC()
	s := &recState{f: f, rules: deriveRules(f)}
	s.build()
	return s.set()
}

// ---- pass 1: what forbids what ----

const (
	keyDirect   = IntentPrepareIntegration + "/" + VariantDirectMerge
	keyRefactor = IntentRefactor
)

var (
	repoIntents     = []string{IntentPrepareIntegration, IntentReviewIssues, IntentFindBugs, IntentRefactor}
	workflowIntents = []string{IntentFindBugs, IntentRefactor}
)

// recRules is the outcome of pass 1.
type recRules struct {
	standing []RecConstraint
	forbid   map[string][]RecConstraint
	// governed: changes reach the main line through review. It is set by anything in tiers 2 and 3 that asks for it, and
	// by nothing else.
	governed bool
	// reasons for the governed route, in tier order, for the words of a suggestion.
	governedBy []RecConstraint
	needReview bool
	needChecks bool
	frozen     *RecConstraint
	// decisionDirect: the person decided, in so many words, that changes go straight to the main line. It orders
	// variants that are already allowed; it allows nothing.
	decisionDirect *RecConstraint
}

func (r *recRules) add(c RecConstraint, keys ...string) {
	has := func(list []RecConstraint) bool {
		for _, e := range list {
			if e.Code == c.Code && e.Ref == c.Ref {
				return true
			}
		}
		return false
	}
	if !has(r.standing) {
		r.standing = append(r.standing, c)
	}
	for _, k := range keys {
		if !has(r.forbid[k]) {
			r.forbid[k] = append(r.forbid[k], c)
		}
	}
}

// blockers lists what forbids the intent or, if variant is set, that variant, strongest tier first.
func (r *recRules) blockers(intent, variant string) []RecConstraint {
	var out []RecConstraint
	out = append(out, r.forbid[intent]...)
	if variant != "" && variant != VariantDefault {
		out = append(out, r.forbid[intent+"/"+variant]...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tier < out[j].Tier })
	return out
}

// memQuote shows saved text inside a sentence, once, in double quotes. Quotation marks inside it become single quotes, so that
// the quoted part is always one span: a connection that is not given saved text can then take exactly that span out.
func memQuote(text string) string {
	return `"` + strings.ReplaceAll(ClipRecText(text, 120), `"`, `'`) + `"`
}

func deriveRules(f RecFacts) *recRules {
	r := &recRules{forbid: map[string][]RecConstraint{}}
	app := func(code, msg string) RecConstraint {
		return RecConstraint{Tier: TierRestriction, Source: RecSrcApplication, Code: code, Message: msg}
	}

	// Tier 1: what this viewer and this application can do at all.
	repoOK := false
	switch {
	case !f.Project.HasRepository:
		r.add(app("no_repository", "This is a work project with no Git repository, so there are no branches or repository workflows."), repoIntents...)
	case !f.Viewer.Repo:
		r.add(app("repository_not_permitted", "This session may not read repository state, so repository suggestions are not made."), repoIntents...)
	case f.Repo.Status != SourceStatusRead:
		why := ClipRecText(f.Repo.Problem, 160)
		if why == "" {
			why = "it did not answer"
		}
		r.add(app("repository_unreadable", "The repository could not be read: "+why+"."), repoIntents...)
	default:
		repoOK = true
	}
	if repoOK {
		switch {
		case f.Repo.Unborn:
			r.add(app("unborn_repository", "The repository has no commits yet."), repoIntents...)
		default:
			if f.Repo.Detached {
				r.add(app("detached_head", "The checkout is not on a branch (detached HEAD), so Werkbord does not suggest merging or refactoring from it."), keyDirect, keyRefactor)
			}
			if f.Repo.Operation != "" || f.Repo.Conflicted > 0 {
				op := f.Repo.Operation
				if op == "" {
					op = "merge"
				}
				r.add(app("operation_in_progress", fmt.Sprintf("A %s is unfinished in the project's checkout. Git work waits until it is done.", op)), IntentPrepareIntegration, IntentRefactor)
			}
			if f.Repo.Target == "" || !f.Repo.TargetLocal {
				r.add(app("target_missing", "There is no local main branch to merge into."), keyDirect)
			}
		}
	}
	if !f.Viewer.RunnerReady {
		r.add(app("no_runner", "No runner is online to take a workflow."), workflowIntents...)
	}
	if !f.Viewer.Tasks {
		r.add(app("tasks_not_permitted", "This session may not read tasks, so task suggestions are not made."), IntentNextTasks)
	}
	for name, wf := range f.Workflows {
		if !wf.Active {
			continue
		}
		intent := map[string]string{WorkflowFindBugs: IntentFindBugs, WorkflowRefactor: IntentRefactor}[name]
		if intent != "" {
			r.add(app("workflow_running", "That workflow is already running on this project."), intent)
		}
	}

	// Tier 2: what the team and the repository require.
	if f.Project.TeamLinked {
		c := RecConstraint{Tier: TierPolicy, Source: RecSrcTeam, Code: "team_pull_request",
			Message: "This repository belongs to a Team workspace, so Werkbord suggests only the pull request route here, never a direct merge."}
		r.add(c, keyDirect)
		r.governed, r.governedBy = true, append(r.governedBy, c)
		switch f.Memory.Team {
		case SourceStatusRead, SourceStatusEmpty:
		default:
			r.add(RecConstraint{Tier: TierPolicy, Source: RecSrcTeam, Code: "team_policy_unread",
				Message: "Team policies were not read here, so the pull request route is assumed. Ask in the assistant on the desktop to read them."})
		}
	}
	for _, it := range f.Memory.Items {
		if it.Kind != "policy" && it.Type != "policy" {
			continue
		}
		fl := classifyMemory(it.Summary)
		tier := TierPolicy
		quote := memQuote(it.Summary)
		if it.Freshness == "uncertain" {
			quote += " (freshness uncertain)"
		}
		if fl.topic || fl.noDirect {
			c := RecConstraint{Tier: tier, Source: it.Source, Code: "policy_integration", Ref: it.ID,
				Message: "Saved policy " + quote + " concerns how changes reach main, so the direct-merge shortcut is not suggested."}
			r.add(c, keyDirect)
			r.governed, r.governedBy = true, append(r.governedBy, c)
		}
		r.needReview = r.needReview || fl.requiresRev
		r.needChecks = r.needChecks || fl.requiresCheck
		if fl.freeze {
			c := RecConstraint{Tier: tier, Source: it.Source, Code: "policy_freeze", Ref: it.ID, Message: "Saved policy " + quote + " freezes changes, so merging and refactoring are not suggested."}
			r.add(c, keyDirect, keyRefactor)
			r.frozen = &c
		}
		if fl.noRefactor {
			r.add(RecConstraint{Tier: tier, Source: it.Source, Code: "policy_no_refactor", Ref: it.ID, Message: "Saved policy " + quote + " rules out refactoring."}, keyRefactor)
		}
		if fl.noAgents {
			r.add(RecConstraint{Tier: tier, Source: it.Source, Code: "policy_no_agents", Ref: it.ID, Message: "Saved policy " + quote + " rules out agent workflows."}, workflowIntents...)
		}
	}
	for _, pr := range f.Repo.PullRequests.Items {
		if pr.Review == "" {
			continue
		}
		ref := fmt.Sprintf("PR #%d", pr.Number)
		c := RecConstraint{Tier: TierPolicy, Source: RecSrcGitHub, Code: "repo_requires_review", Ref: ref,
			Message: fmt.Sprintf("GitHub asks for review on this repository (%s is %s), so the direct-merge shortcut is not suggested.", ref, strings.ReplaceAll(pr.Review, "_", " "))}
		r.add(c, keyDirect)
		r.governed, r.governedBy, r.needReview = true, append(r.governedBy, c), true
		break
	}

	// Tier 3: what the person decided. A decision can add a restriction. It can never lift one from tier 1 or 2.
	for _, it := range f.Memory.Items {
		if it.Kind != "decision" && it.Type != "decision" {
			continue
		}
		fl := classifyMemory(it.Summary)
		quote := memQuote(it.Summary)
		switch {
		case fl.noDirect || fl.requiresPR:
			c := RecConstraint{Tier: TierDecision, Source: it.Source, Code: "decision_pull_request", Ref: it.ID,
				Message: "Your decision " + quote + " asks for review, so the direct-merge shortcut is not suggested."}
			r.add(c, keyDirect)
			r.governed, r.governedBy = true, append(r.governedBy, c)
		case fl.prefersDirect && r.decisionDirect == nil:
			c := RecConstraint{Tier: TierDecision, Source: it.Source, Code: "decision_direct", Ref: it.ID, Message: "Your decision " + quote + " says changes go straight to main."}
			r.decisionDirect = &c
		}
		if fl.freeze {
			c := RecConstraint{Tier: TierDecision, Source: it.Source, Code: "decision_freeze", Ref: it.ID, Message: "Your decision " + quote + " freezes changes, so merging and refactoring are not suggested."}
			r.add(c, keyDirect, keyRefactor)
			r.frozen = &c
		}
		if fl.noRefactor {
			r.add(RecConstraint{Tier: TierDecision, Source: it.Source, Code: "decision_no_refactor", Ref: it.ID, Message: "Your decision " + quote + " rules out refactoring."}, keyRefactor)
		}
	}
	sort.SliceStable(r.standing, func(i, j int) bool { return r.standing[i].Tier < r.standing[j].Tier })
	return r
}

// ---- pass 2 and 3: candidates, then order ----

type candidate struct {
	intent, variant string
	title, detail   string
	action          RecAction
	evidence        []RecEvidence
	applied         []RecConstraint
	// score is tier 4: how strongly the current facts point at it. pref is tier 5: a bounded nudge. required is a state
	// the facts make unavoidable (a half-finished merge, an agent waiting for you).
	score    int
	pref     int
	required bool
	// chosenByHistory: tier 5 decided between two allowed variants.
	chosenByHistory bool
	// subject is the task or branch it is about, so that two suggestions about one thing are not both shown.
	subject string
	key     []string
}

type recState struct {
	f        RecFacts
	rules    *recRules
	cands    []candidate
	withheld []RecWithheld
}

func (s *recState) withhold(intent, variant string, c RecConstraint) {
	for _, w := range s.withheld {
		if w.Intent == intent && w.Variant == variant && w.Code == c.Code {
			return
		}
	}
	s.withheld = append(s.withheld, RecWithheld{Intent: intent, Variant: variant, Tier: c.Tier, Code: c.Code, Reason: c.Message})
}

// allowed reports whether pass 1 left the intent (or the variant) standing, and records why not.
func (s *recState) allowed(intent, variant string) bool {
	bl := s.rules.blockers(intent, variant)
	if len(bl) == 0 {
		return true
	}
	s.withhold(intent, variant, bl[0])
	return false
}

func recEv(tier RecTier, source, kind, ref, label string, verified bool) RecEvidence {
	return RecEvidence{Tier: tier, Source: source, Kind: kind, Ref: ref, Label: ClipRecText(label, 200), Verified: verified}
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (s *recState) build() {
	s.continueWork()
	s.prepareIntegration()
	s.reviewIssues()
	s.findBugs()
	s.refactor()
	s.nextTasks()
}

func (s *recState) feedback(intent, variant string) RecFeedback {
	for _, fb := range s.f.Memory.Feedback {
		if fb.Intent == intent && fb.Variant == variant {
			return fb
		}
	}
	return RecFeedback{Intent: intent, Variant: variant}
}

// historyNudge is the bounded tier 5 effect of earlier feedback on one intent and variant.
func (s *recState) historyNudge(intent, variant string) (int, *RecEvidence) {
	fb := s.feedback(intent, variant)
	net := fb.Net()
	if net == 0 {
		return 0, nil
	}
	n := net * 5
	if n > feedbackScoreCeiling {
		n = feedbackScoreCeiling
	}
	if n < -feedbackScoreCeiling {
		n = -feedbackScoreCeiling
	}
	label := fmt.Sprintf("You marked this kind of suggestion useful %s and not useful %s.", countOf(fb.Useful, "time", "times"), countOf(fb.NotUseful, "time", "times"))
	e := recEv(TierHistory, RecSrcFeedback, "feedback", "", label, false)
	e.Note = "your feedback"
	return n, &e
}

func (s *recState) hasTasks() bool { return s.f.Viewer.Tasks && s.f.Tasks.Status == SourceStatusRead }

// memoryEvidence returns the saved items (not policies or decisions) that say something about a topic, as tier 5 evidence.
func (s *recState) memoryEvidence(match func(memFlags) bool, max int) []RecMemoryItem {
	var out []RecMemoryItem
	for _, it := range s.f.Memory.Items {
		if it.Kind == "policy" || it.Kind == "decision" || it.Type == "policy" || it.Type == "decision" {
			continue
		}
		if match(classifyMemory(it.Summary)) {
			out = append(out, it)
			if len(out) == max {
				break
			}
		}
	}
	return out
}

func memEv(it RecMemoryItem) RecEvidence {
	e := recEv(TierHistory, it.Source, "memory", it.ID, it.Summary, it.Verification == "verified" || it.Verification == "human_confirmed")
	switch {
	case it.Kind == "suggestion":
		e.Note = "unverified suggestion"
	case it.Freshness == "uncertain":
		e.Note = "freshness uncertain"
	}
	return e
}

// ---- continue work ----

func (s *recState) continueWork() {
	f := s.f
	// A half-finished Git operation outranks everything the repository could suggest: the rest waits for it.
	if f.Repo.Status == SourceStatusRead && (f.Repo.Operation != "" || f.Repo.Conflicted > 0) {
		op := f.Repo.Operation
		if op == "" {
			op = "merge"
		}
		detail := fmt.Sprintf("The project's checkout has an unfinished %s.", op)
		if f.Repo.Conflicted > 0 {
			detail = fmt.Sprintf("The project's checkout has an unfinished %s with %s.", op, countOf(f.Repo.Conflicted, "conflicted file", "conflicted files"))
		}
		s.cands = append(s.cands, candidate{intent: IntentContinueWork, variant: VariantDefault, title: "Finish the " + op + " in progress", detail: detail + " Other Git work waits until it is done.",
			action: RecAction{Kind: ActionOpenChanges, Label: "Open changes", Confirmation: "none"}, score: 100, required: true, subject: "operation",
			evidence: []RecEvidence{recEv(TierEvidence, RecSrcGit, "operation", op, detail, true)}, key: []string{"operation", op}})
	}
	if s.hasTasks() {
		var waiting, failed, review, doing []RecTask
		for _, t := range f.Tasks.Items {
			switch {
			case t.Active && t.Waiting == WaitQuestion:
				waiting = append(waiting, t)
			case t.Active:
			case (t.State == TaskDoing || t.State == TaskReview) && (t.RunState == RunFailed || t.RunState == RunBlocked):
				failed = append(failed, t)
			case t.State == TaskReview:
				review = append(review, t)
			case t.State == TaskDoing:
				doing = append(doing, t)
			}
		}
		byAge := func(ts []RecTask) {
			sort.SliceStable(ts, func(i, j int) bool { return ts[i].UpdatedAt.Before(ts[j].UpdatedAt) })
		}
		for _, ts := range [][]RecTask{waiting, failed, review, doing} {
			byAge(ts)
		}
		add := func(t RecTask, score int, required bool, title, detail, kind string) {
			s.cands = append(s.cands, candidate{intent: IntentContinueWork, variant: VariantDefault, title: title, detail: detail, score: score, required: required, subject: t.ID,
				action:   RecAction{Kind: ActionOpenTask, Label: "Open task", TaskID: t.ID, Confirmation: "none"},
				evidence: []RecEvidence{recEv(TierEvidence, RecSrcTask, kind, t.ID, fmt.Sprintf("%q is in %s.", ClipRecText(t.Title, 80), t.State), true)}, key: []string{kind, t.ID, string(t.RunState)}})
		}
		for _, t := range waiting {
			add(t, 90, true, fmt.Sprintf("%q is waiting for you", ClipRecText(t.Title, 60)), "An agent stopped to ask something. It cannot go on until you answer.", "waiting")
			break
		}
		for _, t := range failed {
			add(t, 70, false, fmt.Sprintf("Pick up %q", ClipRecText(t.Title, 60)), fmt.Sprintf("Its last run %s. The task is still in %s.", strings.ReplaceAll(string(t.RunState), "_", " "), t.State), "stopped")
			break
		}
		for _, t := range review {
			add(t, 60, false, fmt.Sprintf("Review %q", ClipRecText(t.Title, 60)), "The work is finished and waits for your decision.", "review")
			break
		}
		for _, t := range doing {
			add(t, 45, false, fmt.Sprintf("Continue %q", ClipRecText(t.Title, 60)), "It is in Doing and nothing is running on it.", "doing")
			break
		}
	}
	// Uncommitted work in the project's own checkout.
	if f.Repo.Status == SourceStatusRead && f.Repo.Dirty > 0 && f.Repo.Operation == "" && f.Repo.Conflicted == 0 && !f.Repo.Detached && !f.Repo.Unborn {
		onTarget := f.Repo.Head != "" && f.Repo.Head == f.Repo.Target
		label := fmt.Sprintf("%s uncommitted in the checkout on %s.", countOf(f.Repo.Dirty, "file is", "files are"), ClipRecText(f.Repo.Head, 60))
		c := candidate{intent: IntentContinueWork, variant: VariantDefault, score: 55, subject: "checkout", key: []string{"dirty", f.Repo.Head, fmt.Sprint(f.Repo.Dirty)},
			action:   RecAction{Kind: ActionOpenChanges, Label: "Open changes", Confirmation: "none"},
			evidence: []RecEvidence{recEv(TierEvidence, RecSrcGit, "uncommitted", f.Repo.Head, label, true)}}
		switch {
		case onTarget && s.rules.governed:
			c.title = fmt.Sprintf("Move your changes off %s", f.Repo.Target)
			c.detail = fmt.Sprintf("%s This project asks for review, so changes belong on a branch, not on %s.", sentenceCase(label), f.Repo.Target)
			c.required, c.score = true, 80
			c.applied = append(c.applied, s.rules.governedBy...)
		case onTarget:
			c.title = fmt.Sprintf("You have uncommitted changes on %s", f.Repo.Target)
			c.detail = sentenceCase(label)
		default:
			c.title = fmt.Sprintf("Uncommitted changes on %s", ClipRecText(f.Repo.Head, 40))
			c.detail = sentenceCase(label)
		}
		s.cands = append(s.cands, c)
	}
}

func sentenceCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---- prepare a branch or a pull request ----

var viaPRSubject = regexp.MustCompile(`(\(#\d+\)\s*$)|(^Merge pull request)`)

// practice counts how the latest commits on the target got there: through a pull request, or without one. A merge made on
// this computer, with or without a merge commit, is "without": it is the direct route, not a review.
func (f RecFacts) practice() (direct, viaPR int) {
	for i, c := range f.Repo.TargetCommits {
		if i == 30 {
			break
		}
		if viaPRSubject.MatchString(c.Subject) {
			viaPR++
		} else {
			direct++
		}
	}
	return
}

func (f RecFacts) mergedPullRequests() int {
	n := 0
	for _, pr := range f.Repo.PullRequests.Items {
		if pr.State == "merged" {
			n++
		}
	}
	return n
}

func (s *recState) prepareIntegration() {
	f := s.f
	if !s.allowed(IntentPrepareIntegration, "") {
		return
	}
	// Pull requests that already exist come first: they are the state of the work, not a choice.
	branchPR := map[string]RecPullRequest{}
	if f.Repo.PullRequests.Available {
		for _, pr := range f.Repo.PullRequests.Items {
			if pr.State == "open" {
				if _, ok := branchPR[pr.Branch]; !ok {
					branchPR[pr.Branch] = pr
				}
			}
		}
		opened := 0
		for _, pr := range f.Repo.PullRequests.Items {
			if pr.State != "open" || pr.Draft || opened == 3 {
				continue
			}
			ref := fmt.Sprintf("PR #%d", pr.Number)
			base := RecEvidence{Tier: TierEvidence, Source: RecSrcGitHub, Kind: "pull_request", Ref: ref, Verified: true}
			c := candidate{intent: IntentPrepareIntegration, variant: VariantPullRequest, subject: pr.TaskID, key: []string{"pr", fmt.Sprint(pr.Number), pr.Review, pr.Checks, pr.Mergeable},
				action: RecAction{Kind: ActionOpenBranch, Label: "Open branch", Branch: pr.Branch, PR: pr.Number, Confirmation: "at_destination"}, applied: s.rules.governedBy}
			if c.subject == "" {
				c.subject = pr.Branch
			}
			switch {
			case pr.Review == "changes_requested":
				base.Label = fmt.Sprintf("%s has changes requested.", ref)
				c.title, c.detail, c.score, c.required = fmt.Sprintf("%s needs changes", ref), "A reviewer asked for changes before it can merge.", 85, true
			case pr.Checks == "failing":
				base.Label = fmt.Sprintf("%s has failing checks.", ref)
				c.title, c.detail, c.score = fmt.Sprintf("%s has failing checks", ref), "Its checks must pass before it can merge.", 80
			case pr.Review == "approved" && (pr.Checks == "passing" || pr.Checks == "none") && pr.Mergeable != "conflicting":
				base.Label = fmt.Sprintf("%s is approved and its checks pass.", ref)
				c.title, c.detail, c.score = fmt.Sprintf("%s is ready to merge", ref), "It is approved and ready. Merging it is your decision on its page.", 75
			case pr.Mergeable == "conflicting":
				base.Label = fmt.Sprintf("%s conflicts with %s.", ref, pr.Base)
				c.title, c.detail, c.score = fmt.Sprintf("%s has conflicts", ref), fmt.Sprintf("It no longer merges cleanly into %s.", pr.Base), 78
			default:
				continue
			}
			opened++
			c.evidence = []RecEvidence{base}
			s.cands = append(s.cands, c)
		}
	}
	// Finished work on a branch with no pull request yet: how should it go in?
	ready := []RecBranch{}
	for _, b := range f.Repo.Branches {
		if b.Target || b.Merged || b.Ahead == 0 || b.ActiveRun || b.Dirty > 0 {
			continue
		}
		if _, has := branchPR[b.Name]; has {
			continue
		}
		if !b.Owned && b.Phase != PhaseReview && b.Phase != PhaseCompleted {
			continue
		}
		ready = append(ready, b)
	}
	sort.SliceStable(ready, func(i, j int) bool {
		rank := func(b RecBranch) int {
			switch b.Phase {
			case PhaseReview:
				return 0
			case PhaseCompleted:
				return 1
			}
			return 2
		}
		if rank(ready[i]) != rank(ready[j]) {
			return rank(ready[i]) < rank(ready[j])
		}
		return ready[i].LastCommit.After(ready[j].LastCommit)
	})
	for i, b := range ready {
		if i == 2 {
			break
		}
		s.integrationFor(b)
	}
}

// integrationFor chooses how one finished branch should go in. Which ways are allowed is settled by tiers 1 to 3; the
// choice among them is tier 5's alone.
func (s *recState) integrationFor(b RecBranch) {
	f, r := s.f, s.rules
	name := b.Name
	task := b.TaskTitle
	label := name
	if task != "" {
		label = fmt.Sprintf("%q", ClipRecText(task, 60))
	}
	prPossible := f.Repo.HasRemote
	directOK := s.allowed(IntentPrepareIntegration, VariantDirectMerge)
	if !prPossible && r.governed {
		s.withhold(IntentPrepareIntegration, VariantPullRequest, RecConstraint{Tier: TierRestriction, Source: RecSrcApplication, Code: "no_remote", Message: "The repository has no remote to push to, so a pull request cannot be prepared."})
	}
	prOK := prPossible
	if !prOK && !directOK {
		return
	}

	phaseScore := 40
	switch b.Phase {
	case PhaseReview:
		phaseScore = 65
	case PhaseCompleted:
		phaseScore = 60
	}
	base := []RecEvidence{recEv(TierEvidence, RecSrcGit, "branch", name, fmt.Sprintf("%s has %s that %s lacks and nothing is running on it.", ClipRecText(name, 60), countOf(b.Ahead, "commit", "commits"), f.Repo.Target), true)}
	if b.TaskID != "" {
		base = append(base, recEv(TierEvidence, RecSrcTask, "task", b.TaskID, fmt.Sprintf("Its task %q is in %s.", ClipRecText(task, 60), phaseWord(b.Phase)), true))
	}

	// Scores for each allowed variant. Both start level; only decisions and history move them.
	type option struct {
		variant string
		score   int
		why     []RecEvidence
	}
	var opts []option
	direct, viaPR := f.practice()
	total := direct + viaPR
	practiceDirect := direct >= 5 && direct*3 >= total*2 && f.mergedPullRequests() < 3
	practicePR := viaPR >= 3 && direct*2 < total || f.mergedPullRequests() >= 3
	var mem []RecMemoryItem
	for _, it := range f.Memory.Items {
		if it.Kind == "policy" || it.Kind == "decision" || it.Type == "policy" || it.Type == "decision" {
			continue
		}
		mem = append(mem, it)
	}
	if prOK {
		o := option{variant: VariantPullRequest, score: 10}
		if practicePR {
			o.score += 15
			o.why = append(o.why, recEv(TierHistory, RecSrcGit, "practice", "", fmt.Sprintf("%d of the last %d changes on %s arrived through a pull request or merge.", viaPR, total, f.Repo.Target), true))
		}
		for _, it := range mem {
			if fl := classifyMemory(it.Summary); fl.requiresPR && !fl.prefersDirect && !fl.noDirect && len(o.why) < 3 {
				o.score += 10
				o.why = append(o.why, memEv(it))
			}
		}
		if n, e := s.historyNudge(IntentPrepareIntegration, VariantPullRequest); e != nil {
			o.score += n
			o.why = append(o.why, *e)
		}
		opts = append(opts, o)
	}
	if directOK {
		o := option{variant: VariantDirectMerge}
		if r.decisionDirect != nil {
			// Tier 3 decides before any habit, but only among the ways that survived tiers 1 and 2.
			o.score += 100
			o.why = append(o.why, recEv(TierDecision, r.decisionDirect.Source, "decision", r.decisionDirect.Ref, r.decisionDirect.Message, true))
		}
		if practiceDirect {
			o.score += 25
			o.why = append(o.why, recEv(TierHistory, RecSrcGit, "practice", "", fmt.Sprintf("%d of the last %d changes on %s were committed directly.", direct, total, f.Repo.Target), true))
		}
		for _, it := range mem {
			if fl := classifyMemory(it.Summary); fl.prefersDirect && len(o.why) < 3 {
				o.score += 15
				o.why = append(o.why, memEv(it))
			}
		}
		if n, e := s.historyNudge(IntentPrepareIntegration, VariantDirectMerge); e != nil {
			o.score += n
			o.why = append(o.why, *e)
		}
		opts = append(opts, o)
	}
	sort.SliceStable(opts, func(i, j int) bool { return opts[i].score > opts[j].score })
	pick := opts[0]
	// Basis "preference" is reserved for a choice that a decision or a habit made; the plain default is just evidence.
	chosenByHistory := len(opts) > 1 && opts[0].score > opts[1].score && len(pick.why) > 0
	c := candidate{intent: IntentPrepareIntegration, variant: pick.variant, score: phaseScore, subject: firstNonEmpty(b.TaskID, name), key: []string{"branch", name, fmt.Sprint(b.Ahead), pick.variant},
		action: RecAction{Kind: ActionOpenBranch, Label: "Open branch", Branch: name, Confirmation: "at_destination"}, evidence: append(base, pick.why...), chosenByHistory: chosenByHistory}
	// What removed the other way is part of the explanation.
	for _, other := range []string{VariantDirectMerge, VariantPullRequest} {
		if other != pick.variant {
			c.applied = append(c.applied, r.blockers(IntentPrepareIntegration, other)...)
		}
	}
	c.applied = append(c.applied, r.governedBy...)
	switch pick.variant {
	case VariantPullRequest:
		if f.Repo.PullRequests.Available {
			c.title = "Open a pull request for " + label
			c.action.Label = "Prepare pull request"
		} else {
			c.title = "Push " + label + " for review"
			c.action.Label = "Open branch"
		}
		c.detail = s.prWhy(b)
	case VariantDirectMerge:
		c.title = fmt.Sprintf("Merge %s into %s", label, f.Repo.Target)
		c.action.Label = "Review merge"
		c.detail = fmt.Sprintf("%s ready and nothing blocks merging it. The merge is checked again, and asks you, before anything changes.", countOf(b.Ahead, "commit is", "commits are"))
		if chosenByHistory && practiceDirect {
			c.detail = fmt.Sprintf("%s ready, and you usually merge straight to %s here. The merge is checked again, and asks you, before anything changes.", countOf(b.Ahead, "commit is", "commits are"), f.Repo.Target)
		}
	}
	if c.score < 0 {
		c.score = 0
	}
	s.cands = append(s.cands, c)
}

func (s *recState) prWhy(b RecBranch) string {
	r := s.rules
	var why string
	switch {
	case len(r.governedBy) > 0:
		why = reasonWords(r.governedBy[0])
	default:
		why = "A pull request keeps the change reviewable."
	}
	extra := ""
	switch {
	case r.needReview && r.needChecks:
		extra = " Review and passing checks are required."
	case r.needReview:
		extra = " Review is required."
	case r.needChecks:
		extra = " Checks must pass."
	}
	if r.frozen != nil {
		extra += " Merging is frozen, so prepare it for review only."
	}
	return fmt.Sprintf("%s ahead of %s. %s%s", sentenceCase(countOf(b.Ahead, "commit", "commits")), s.f.Repo.Target, why, extra)
}

// reasonWords says in one short sentence why the governed route applies.
func reasonWords(c RecConstraint) string {
	switch c.Code {
	case "team_pull_request":
		return "This project's repository belongs to a Team workspace, so changes go in through a pull request."
	case "repo_requires_review":
		return "GitHub asks for review on this repository."
	case "decision_pull_request":
		return "You decided this project uses pull requests."
	case "policy_integration":
		return "A saved policy covers how changes reach main."
	}
	return "Review comes first on this project."
}

func phaseWord(p TaskPhase) string {
	switch p {
	case PhaseReview:
		return "Review"
	case PhaseCompleted:
		return "Done"
	case PhaseActive:
		return "Doing"
	}
	return "Backlog"
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---- review repository issues ----

func (s *recState) reviewIssues() {
	f := s.f
	if !s.allowed(IntentReviewIssues, "") || f.Repo.Health.Status != SourceStatusRead {
		return
	}
	h := f.Repo.Health
	if h.Risk == 0 && h.Attention == 0 {
		return
	}
	score := 45
	title := fmt.Sprintf("%s need a look", sentenceCase(countOf(h.Attention, "repository finding", "repository findings")))
	if h.Risk > 0 {
		score = 75
		title = fmt.Sprintf("%s at risk in the repository", sentenceCase(countOf(h.Risk, "finding", "findings")))
	}
	var evs []RecEvidence
	for i, fd := range h.Top {
		if i == 2 {
			break
		}
		evs = append(evs, recEv(TierEvidence, RecSrcHealth, "finding", fd.ID, fd.Title, true))
	}
	if len(evs) == 0 {
		evs = append(evs, recEv(TierEvidence, RecSrcHealth, "summary", "", fmt.Sprintf("%s open.", countOf(h.Attention, "finding", "findings")), true))
	}
	detail := "Werkbord noticed these in the repository's state. Each one says what to check."
	if len(h.Top) > 0 {
		detail = ClipRecText(h.Top[0].Title, 120) + "."
	}
	s.cands = append(s.cands, candidate{intent: IntentReviewIssues, variant: VariantDefault, title: title, detail: detail, score: score, subject: "health", evidence: evs,
		action: RecAction{Kind: ActionOpenHealth, Label: "Open findings", Confirmation: "none"}, key: []string{"health", fmt.Sprint(h.Risk), fmt.Sprint(h.Attention), topID(h.Top)}})
}

func topID(fs []RecFinding) string {
	if len(fs) == 0 {
		return ""
	}
	return fs[0].ID
}

// ---- workflows ----

func (s *recState) commitsSince(t time.Time) int {
	n := 0
	for _, c := range s.f.Repo.TargetCommits {
		if t.IsZero() || c.Date.After(t) {
			n++
		}
	}
	return n
}

func (s *recState) findBugs() {
	f := s.f
	if !s.allowed(IntentFindBugs, "") {
		return
	}
	wf := f.Workflows[WorkflowFindBugs]
	since := s.commitsSince(wf.LastAt)
	score := 0
	var title, detail, kind string
	var evs []RecEvidence
	switch {
	case wf.LastAt.IsZero() && len(f.Repo.TargetCommits) >= 5:
		score, kind = 55, "never_scanned"
		title, detail = "Scan for bugs", "Find Bugs has not been run on this project. It reads the code and changes nothing."
		evs = append(evs, recEv(TierEvidence, RecSrcGit, "history", "", fmt.Sprintf("The project has %s and no earlier scan.", countOf(len(f.Repo.TargetCommits), "recent commit", "recent commits")), true))
	case wf.LastFailed:
		score, kind = 50, "retry"
		title, detail = "Scan for bugs again", "The last scan did not finish. It reads the code and changes nothing."
		evs = append(evs, recEv(TierEvidence, RecSrcRun, "workflow", wf.TaskID, "The last Find Bugs scan ended without a result.", true))
	case since >= 8:
		score, kind = 60, "new_commits"
		title, detail = "Scan the new changes for bugs", fmt.Sprintf("%s landed since the last scan. It reads the code and changes nothing.", sentenceCase(countOf(since, "commit", "commits")))
		evs = append(evs, recEv(TierEvidence, RecSrcGit, "history", "", fmt.Sprintf("%s on %s since %s.", countOf(since, "commit", "commits"), f.Repo.Target, wf.LastAt.Format("2 Jan")), true))
	case since >= 3 && f.Now.Sub(wf.LastAt) > 21*24*time.Hour:
		score, kind = 50, "stale_scan"
		title, detail = "Scan for bugs", fmt.Sprintf("The last scan was %d days ago and %s landed since. It reads the code and changes nothing.", int(f.Now.Sub(wf.LastAt).Hours()/24), countOf(since, "commit", "commits"))
		evs = append(evs, recEv(TierEvidence, RecSrcGit, "history", "", fmt.Sprintf("%s since the scan on %s.", countOf(since, "commit", "commits"), wf.LastAt.Format("2 Jan")), true))
	default:
		return
	}
	c := candidate{intent: IntentFindBugs, variant: VariantDefault, title: title, detail: detail, score: score, subject: "wf:" + WorkflowFindBugs, evidence: evs,
		action: RecAction{Kind: ActionOpenWorkflow, Label: "Show on Deck", Workflow: WorkflowFindBugs, Confirmation: "at_destination"}, key: []string{kind, fmt.Sprint(since)}}
	for _, it := range s.memoryEvidence(func(m memFlags) bool { return m.bugs }, 1) {
		c.pref += 8
		c.evidence = append(c.evidence, memEv(it))
	}
	s.cands = append(s.cands, c)
}

func (s *recState) refactor() {
	f := s.f
	if !s.allowed(IntentRefactor, "") {
		return
	}
	// Tier 4 safety: a refactor touches many files, so it waits until the repository is quiet.
	quiet := func(code, msg string) bool {
		s.withhold(IntentRefactor, "", RecConstraint{Tier: TierEvidence, Source: RecSrcGit, Code: code, Message: msg})
		return false
	}
	if f.Repo.Dirty > 0 {
		quiet("dirty_checkout", "The checkout has uncommitted changes, which a refactor could collide with.")
		return
	}
	openWork := 0
	for _, b := range f.Repo.Branches {
		if !b.Target && !b.Merged && b.Ahead > 0 {
			openWork++
		}
	}
	for _, pr := range f.Repo.PullRequests.Items {
		if pr.State == "open" {
			openWork++
		}
	}
	if openWork > 2 {
		quiet("open_work", fmt.Sprintf("%s are open, and a refactor would collide with them.", sentenceCase(countOf(openWork, "branch or pull request", "branches and pull requests"))))
		return
	}
	for _, fd := range f.Repo.Health.Top {
		if fd.Type == string(FindBranchOverlap) || fd.Type == string(FindBranchConflict) {
			quiet("overlapping_branches", "Open branches already overlap, and a refactor would add to it.")
			return
		}
	}
	wf := f.Workflows[WorkflowRefactor]
	var evs []RecEvidence
	score := 0
	if s.hasTasks() {
		for _, t := range f.Tasks.Items {
			if t.State == TaskBacklog && refactorTitle(t.Title) {
				evs = append(evs, recEv(TierEvidence, RecSrcTask, "task", t.ID, fmt.Sprintf("Your backlog has %q.", ClipRecText(t.Title, 80)), true))
				score = 50
				break
			}
		}
	}
	debt := s.memoryEvidence(func(m memFlags) bool { return m.debt }, 2)
	for _, it := range debt {
		evs = append(evs, memEv(it))
	}
	if score == 0 && len(debt) > 0 {
		score = 35
	}
	if score == 0 {
		return
	}
	if !wf.LastAt.IsZero() && f.Now.Sub(wf.LastAt) < 30*24*time.Hour && score < 50 {
		return
	}
	if wf.LastFailed {
		score -= 10
	}
	detail := "It proposes one small cleanup and waits for your approval before changing a file."
	if len(debt) > 0 && score == 35 {
		detail = "Saved notes mention code that could be simpler. It proposes one small cleanup and waits for your approval."
	}
	c := candidate{intent: IntentRefactor, variant: VariantDefault, title: "Tidy the code", detail: detail, score: score, subject: "wf:" + WorkflowRefactor, evidence: evs,
		action: RecAction{Kind: ActionOpenWorkflow, Label: "Show on Deck", Workflow: WorkflowRefactor, Confirmation: "at_destination"}, key: []string{fmt.Sprint(score), fmt.Sprint(len(evs))}}
	if len(debt) > 0 {
		c.pref += 5
	}
	s.cands = append(s.cands, c)
}

// ---- next tasks ----

func (s *recState) nextTasks() {
	f := s.f
	if !s.allowed(IntentNextTasks, "") || !s.hasTasks() {
		return
	}
	doing, ready, blocked := 0, []RecTask{}, 0
	for _, t := range f.Tasks.Items {
		switch {
		case t.State == TaskDoing:
			doing++
		case t.State == TaskBacklog && t.Ready:
			ready = append(ready, t)
		case t.State == TaskBacklog && t.WaitingOn > 0:
			blocked++
		}
	}
	if len(ready) == 0 {
		return
	}
	if doing >= 3 {
		s.withhold(IntentNextTasks, "", RecConstraint{Tier: TierEvidence, Source: RecSrcTask, Code: "work_in_progress", Message: fmt.Sprintf("%s already in Doing. Finish some before starting more.", sentenceCase(countOf(doing, "task is", "tasks are")))})
		return
	}
	today := f.Now.Format("2006-01-02")
	sort.SliceStable(ready, func(i, j int) bool {
		a, b := ready[i], ready[j]
		if (a.PlannedStart != "") != (b.PlannedStart != "") {
			return a.PlannedStart != ""
		}
		if a.PlannedStart != b.PlannedStart {
			return a.PlannedStart < b.PlannedStart
		}
		return a.Position < b.Position
	})
	t := ready[0]
	score := 40
	detail := "Nothing it depends on is unfinished."
	evs := []RecEvidence{recEv(TierEvidence, RecSrcTask, "task", t.ID, fmt.Sprintf("%q is first in Backlog with no unfinished dependency.", ClipRecText(t.Title, 80)), true)}
	if t.PlannedStart != "" && t.PlannedStart <= today {
		score = 55
		detail = "It was planned to start by now, and nothing it depends on is unfinished."
		evs = append(evs, recEv(TierEvidence, RecSrcTask, "plan", t.ID, "Planned start "+t.PlannedStart+".", true))
	}
	if t.Team {
		detail = "It came from your Team workspace. " + detail
	}
	if blocked > 0 {
		evs = append(evs, recEv(TierEvidence, RecSrcTask, "dependencies", "", countOf(blocked, "other task is", "other tasks are")+" waiting on unfinished work.", true))
	}
	s.cands = append(s.cands, candidate{intent: IntentNextTasks, variant: VariantDefault, title: fmt.Sprintf("Next: %q", ClipRecText(t.Title, 60)), detail: detail, score: score, subject: t.ID, evidence: evs,
		action: RecAction{Kind: ActionOpenTask, Label: "Open task", TaskID: t.ID, Confirmation: "none"}, key: []string{t.ID, fmt.Sprint(score)}})
}

// ---- selection ----

func (s *recState) set() RecommendationSet {
	f := s.f
	out := RecommendationSet{ProjectID: f.ProjectID, ProjectName: f.ProjectName, GeneratedAt: f.Now, Items: []Recommendation{}, Standing: []RecConstraint{}, Withheld: []RecWithheld{}, Notes: []string{}, Boundary: RecommendationBoundary}
	out.Sources = RecSources{Individual: orStatus(f.Memory.Individual, SourceStatusNotChecked), Team: orStatus(f.Memory.Team, SourceStatusNotChecked), Git: orStatus(f.Repo.Status, SourceStatusNotChecked),
		PullRequests: orStatus(f.Repo.PullRequests.Status, SourceStatusNotChecked), Tasks: orStatus(f.Tasks.Status, SourceStatusNotChecked), Health: orStatus(f.Repo.Health.Status, SourceStatusNotChecked)}

	// Tier 5, applied to what is left: feedback and history reorder, hide and quiet. They never add.
	var live []candidate
	for _, c := range s.cands {
		if f.Focus != "" && c.intent != f.Focus {
			continue
		}
		if n, e := s.historyNudge(c.intent, c.variant); e != nil && c.intent != IntentPrepareIntegration {
			c.pref += n
			c.evidence = append(c.evidence, *e)
		}
		id := RecommendationID(append([]string{f.ProjectID, c.intent, c.variant}, c.key...)...)
		fb := s.feedback(c.intent, c.variant)
		switch {
		case fb.HidesAt(id, f.Now):
			s.withhold(c.intent, c.variant, RecConstraint{Tier: TierHistory, Source: RecSrcFeedback, Code: "dismissed", Message: "You dismissed this suggestion. It returns if something changes."})
			continue
		case !c.required && fb.Net() <= -FeedbackSuppressNet:
			s.withhold(c.intent, c.variant, RecConstraint{Tier: TierHistory, Source: RecSrcFeedback, Code: "found_not_useful", Message: "You found this kind of suggestion not useful several times, so it is quiet for now."})
			continue
		}
		c.key = append([]string{id}, c.key...)
		live = append(live, c)
	}
	order := map[string]int{}
	for i, in := range RecIntents {
		order[in] = i
	}
	rank := func(c candidate) int {
		if c.required {
			return 1000 + c.score
		}
		return c.score + c.pref
	}
	sort.SliceStable(live, func(i, j int) bool {
		if rank(live[i]) != rank(live[j]) {
			return rank(live[i]) > rank(live[j])
		}
		return order[live[i].intent] < order[live[j].intent]
	})
	seenIntent, seenSubject, workflows := map[string]bool{}, map[string]bool{}, 0
	for _, c := range live {
		if len(out.Items) == MaxRecommendations {
			break
		}
		isWorkflow := c.action.Kind == ActionOpenWorkflow
		if seenIntent[c.intent] || (c.subject != "" && seenSubject[c.subject]) || (isWorkflow && workflows == 1) {
			continue
		}
		seenIntent[c.intent] = true
		if c.subject != "" {
			seenSubject[c.subject] = true
		}
		if isWorkflow {
			workflows++
		}
		out.Items = append(out.Items, s.finish(c))
	}

	out.Standing = append(out.Standing, s.rules.standing...)
	if len(out.Standing) > 6 {
		out.Standing = out.Standing[:6]
	}
	sort.SliceStable(s.withheld, func(i, j int) bool { return s.withheld[i].Tier < s.withheld[j].Tier })
	out.Withheld = append(out.Withheld, s.withheld...)
	if len(out.Withheld) > 8 {
		out.Withheld = out.Withheld[:8]
	}
	if f.Memory.Individual == SourceStatusUnavailable {
		out.Notes = append(out.Notes, "Saved memory could not be read just now. These suggestions follow your repository and tasks only.")
	} else if len(f.Memory.Items) == 0 && f.Memory.Individual != SourceStatusDenied && f.Memory.Team != SourceStatusRead {
		out.Notes = append(out.Notes, "Nothing is saved for this project yet, so these suggestions follow your repository and tasks.")
	}
	if f.Repo.PullRequests.Status == SourceStatusUnavailable && f.Repo.PullRequests.Reason != "" {
		out.Notes = append(out.Notes, "Pull requests could not be read: "+ClipRecText(f.Repo.PullRequests.Reason, 120)+".")
	}
	return out
}

func orStatus(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (s *recState) finish(c candidate) Recommendation {
	basis := BasisEvidence
	switch {
	case c.required:
		basis = BasisRequired
	case c.chosenByHistory || (c.pref > 0 && c.score <= 0):
		basis = BasisPreference
	}
	confidence := "high"
	for _, e := range c.evidence {
		if !e.Verified {
			confidence = "medium"
		}
	}
	if basis == BasisPreference {
		confidence = "medium"
	}
	if len(c.evidence) == 0 {
		confidence = "low"
	}
	if len(c.evidence) > 5 {
		c.evidence = c.evidence[:5]
	}
	if len(c.applied) > 4 {
		c.applied = c.applied[:4]
	}
	if c.applied == nil {
		c.applied = []RecConstraint{}
	}
	return Recommendation{ID: c.key[0], ProjectID: s.f.ProjectID, Intent: c.intent, Variant: c.variant, Title: ClipRecText(c.title, 120), Detail: ClipRecText(c.detail, 280),
		Basis: basis, Confidence: confidence, Action: c.action, Evidence: c.evidence, Applied: c.applied}
}

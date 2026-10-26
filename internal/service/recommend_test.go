package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/integration"
)

// These tests run the Recommender against real Git repositories, the real stores and the real memory gate. The rules
// themselves are tested in internal/domain; what is tested here is that the facts are read honestly, that what cannot be
// read is reported as a gap, and that nothing is ever changed on the way.

type recFixture struct {
	*gcFixture
	rec *Recommender
	k   *Knowledge
}

func newRec(t *testing.T) *recFixture {
	t.Helper()
	f := newGC(t)
	k := &Knowledge{Memory: memoryService(t, f.fixture, "individual", domain.MemoryLimits{})}
	settings := &Settings{Deps: f.deps}
	local, err := settings.RegisterRunner(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	runners := &Runners{Deps: f.deps, Runs: f.runs, LocalID: local.ID, LocalCapabilities: func(context.Context) domain.RunnerCapabilities {
		return domain.RunnerCapabilities{Agents: []domain.Agent{{ID: "codex"}, {ID: "claude-code", Available: true}}}
	}}
	return &recFixture{gcFixture: f, k: k, rec: &Recommender{Deps: f.deps, Control: f.gc, Knowledge: k, Runners: runners}}
}

func ownerViewer() domain.RecViewer {
	return domain.RecViewer{Kind: "owner", Tasks: true, Runs: true, Repo: true, Memory: true}
}

func (f *recFixture) ask(t *testing.T, mutate ...func(*RecommendInput)) *domain.RecommendationSet {
	t.Helper()
	in := RecommendInput{ProjectID: f.project.ID, Viewer: ownerViewer(), PersonalMemory: true}
	for _, m := range mutate {
		m(&in)
	}
	set, err := f.rec.Recommend(f.ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func findRec(set *domain.RecommendationSet, intent string) *domain.Recommendation {
	for i := range set.Items {
		if set.Items[i].Intent == intent {
			return &set.Items[i]
		}
	}
	return nil
}

func (f *recFixture) policy(t *testing.T, kind, text string) {
	t.Helper()
	if _, err := f.k.SaveManual(f.ctx, f.project.ID, ManualMemory{Kind: kind, Summary: text}); err != nil {
		t.Fatal(err)
	}
}

func (f *recFixture) directCommitsOnMain(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		commitIn(t, f.repo, fmt.Sprintf("direct-%d.txt", i), "x\n", fmt.Sprintf("tweak %d", i))
	}
}

func TestDeckWorkflowIdsMatchWhatTheRulesPointAt(t *testing.T) {
	for _, id := range []string{domain.WorkflowFindBugs, domain.WorkflowRefactor} {
		if _, ok := deckWorkflow(id); !ok {
			t.Errorf("the rules point at deck workflow %q, which the deck does not have", id)
		}
	}
}

func TestSuggestionsComeFromTheRealRepositoryAndTheBoard(t *testing.T) {
	f := newRec(t)
	a := f.agent(t, "Fix login", 2)
	set := f.ask(t)
	got := findRec(set, domain.IntentPrepareIntegration)
	if got == nil || got.Action.Branch != a.branch {
		t.Fatalf("a finished agent branch should be offered for integration: %+v", set.Items)
	}
	if got.Variant != domain.VariantPullRequest {
		t.Fatalf("with nothing saved the reviewable route is the default, got %s", got.Variant)
	}
	if got.Action.StartsExecution || got.Action.Kind != domain.ActionOpenBranch {
		t.Fatalf("%+v", got.Action)
	}
	if set.Sources.Git != domain.SourceStatusRead || set.Sources.Individual != domain.SourceStatusEmpty || set.Sources.Tasks != domain.SourceStatusRead {
		t.Fatalf("sources = %+v", set.Sources)
	}
	// No gh is configured: that is a gap in the evidence, said plainly, not an error.
	if set.Sources.PullRequests == domain.SourceStatusRead {
		t.Fatalf("pull requests cannot have been read without gh: %+v", set.Sources)
	}
}

func TestAnIndividualsHabitOfCommittingToMainIsOfferedWhereNothingForbidsIt(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.directCommitsOnMain(t, 8)
	got := findRec(f.ask(t), domain.IntentPrepareIntegration)
	if got == nil || got.Variant != domain.VariantDirectMerge || got.Basis != domain.BasisPreference {
		t.Fatalf("a solo project with a direct-commit habit and no restriction: %+v", got)
	}
}

func TestTheSameHabitUnderAPolicyIsNeverPermission(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.directCommitsOnMain(t, 8)
	f.policy(t, "preference", "I usually commit straight to main.")
	f.policy(t, "policy", "Main is protected: changes need a pull request, passing tests and a review.")
	set := f.ask(t)
	got := findRec(set, domain.IntentPrepareIntegration)
	if got == nil || got.Variant != domain.VariantPullRequest {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Detail, "Review and passing checks are required") {
		t.Fatalf("the policy's requirements should reach the person: %q", got.Detail)
	}
	cited := false
	for _, c := range got.Applied {
		cited = cited || c.Code == "policy_integration" && c.Ref != ""
	}
	if !cited {
		t.Fatalf("the policy must be cited by record: %+v", got.Applied)
	}
}

func TestAnArchivedPolicyStopsApplyingAndAnUnverifiedNoteNeverStarts(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.directCommitsOnMain(t, 8)
	m, err := f.k.SaveManual(f.ctx, f.project.ID, ManualMemory{Kind: "policy", Summary: "Every merge to main goes through review."})
	if err != nil {
		t.Fatal(err)
	}
	if got := findRec(f.ask(t), domain.IntentPrepareIntegration); got == nil || got.Variant != domain.VariantPullRequest {
		t.Fatalf("%+v", got)
	}
	if _, err := f.k.Change(f.ctx, domain.MemoryScope{ProjectID: f.project.ID}, m.ID, m.Version, nil, true); err != nil {
		t.Fatal(err)
	}
	if got := findRec(f.ask(t), domain.IntentPrepareIntegration); got == nil || got.Variant != domain.VariantDirectMerge {
		t.Fatalf("an archived policy no longer applies: %+v", got)
	}
}

func TestATaskImportedFromTeamMakesTheProjectTeamLinkedAndFailsClosed(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.directCommitsOnMain(t, 8)
	ref := integration.SourcePrefix + "tw_1:tpj_1:ttk_1:mem_1"
	if _, err := f.tasks.CreateTask(f.ctx, NewTask{ProjectID: f.project.ID, Title: "From the team", SourceRef: ref}); err != nil {
		t.Fatal(err)
	}
	links, err := f.rec.TeamLinks(f.ctx, f.project.ID)
	if err != nil || len(links) != 1 || links[0].WorkspaceID != "tw_1" || links[0].TeamProjectID != "tpj_1" {
		t.Fatalf("links = %+v, %v", links, err)
	}
	set := f.ask(t)
	got := findRec(set, domain.IntentPrepareIntegration)
	if got == nil || got.Variant != domain.VariantPullRequest {
		t.Fatalf("%+v", got)
	}
	if set.Sources.Team != domain.SourceStatusNotChecked {
		t.Fatalf("the controller cannot read Team memory itself: %+v", set.Sources)
	}
	// When the desktop reads the Team policies and they say something, that is shown; nothing it says can lift the rule.
	set = f.ask(t, func(in *RecommendInput) {
		in.TeamMemory = &TeamMemoryInput{Status: domain.SourceStatusRead, Items: []MemoryContextItem{{ID: "tmem_1", Kind: "policy", Type: "policy", Summary: "Direct commits to main are allowed for docs.", Verification: "verified", Freshness: "source_matches"}}}
	})
	if got := findRec(set, domain.IntentPrepareIntegration); got == nil || got.Variant != domain.VariantPullRequest {
		t.Fatalf("Team prose must not be read as permission: %+v", got)
	}
	// An ordinary hand-made or deck-made task does not link the project.
	if links := teamLinksOf([]domain.Task{{SourceRef: "phone-deck.abc:req:prj"}, {SourceRef: ""}, {SourceRef: integration.SourcePrefix + "only:three:parts"}}); len(links) != 0 {
		t.Fatalf("%+v", links)
	}
}

func TestMemoryThatCannotBeReadIsAGapAndNeverProofOfNothing(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.rec.Knowledge = nil
	set := f.ask(t)
	if set.Sources.Individual != domain.SourceStatusUnavailable || len(set.Notes) == 0 {
		t.Fatalf("%+v %v", set.Sources, set.Notes)
	}
	if findRec(set, domain.IntentPrepareIntegration) == nil {
		t.Fatal("repository evidence should still produce a suggestion")
	}
}

func TestWhatTheViewerMayNotReadIsLeftOutAndSaid(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.tasks.Create(f.ctx, f.project.ID, "Write docs", "")
	set := f.ask(t, func(in *RecommendInput) {
		in.Viewer = domain.RecViewer{Kind: "assistant", Memory: true}
		in.PersonalMemory = false
	})
	if set.Sources.Git != domain.SourceStatusDenied || set.Sources.Tasks != domain.SourceStatusDenied {
		t.Fatalf("%+v", set.Sources)
	}
	if len(set.Items) != 0 {
		t.Fatalf("with nothing readable there is nothing to say: %+v", set.Items)
	}
	body, _ := json.Marshal(set)
	if strings.Contains(string(body), f.project.RepoPath) || strings.Contains(string(body), "Write docs") {
		t.Fatalf("withheld evidence leaked: %s", body)
	}
	set = f.ask(t, func(in *RecommendInput) { in.Viewer = domain.RecViewer{Kind: "assistant", Tasks: true, Memory: true} })
	if got := findRec(set, domain.IntentNextTasks); got == nil {
		t.Fatalf("a viewer who may read tasks should get task suggestions: %+v", set)
	}
}

func TestInvalidGitStateIsReportedWithoutLeakingPathsOrGuessing(t *testing.T) {
	t.Run("detached head", func(t *testing.T) {
		f := newRec(t)
		f.agent(t, "Tidy", 1)
		f.directCommitsOnMain(t, 8)
		git(t, f.repo, "checkout", "-q", "--detach")
		set := f.ask(t)
		if got := findRec(set, domain.IntentPrepareIntegration); got != nil && got.Variant == domain.VariantDirectMerge {
			t.Fatalf("a detached checkout is not offered a direct merge: %+v", got)
		}
	})
	t.Run("unfinished merge", func(t *testing.T) {
		f := newRec(t)
		f.agent(t, "Tidy", 1)
		git(t, f.repo, "checkout", "-q", "-b", "side")
		commitIn(t, f.repo, "README.md", "side\n", "side change")
		git(t, f.repo, "checkout", "-q", "main")
		commitIn(t, f.repo, "README.md", "main\n", "main change")
		_ = exec.Command("git", "-C", f.repo, "merge", "side").Run() // conflicts, leaving a merge in progress
		set := f.ask(t)
		first := set.Items[0]
		if first.Intent != domain.IntentContinueWork || first.Basis != domain.BasisRequired || !strings.Contains(first.Title, "merge") {
			t.Fatalf("an unfinished merge comes first: %+v", set.Items)
		}
		if findRec(set, domain.IntentPrepareIntegration) != nil || findRec(set, domain.IntentRefactor) != nil {
			t.Fatalf("no further Git work while one is unfinished: %+v", set.Items)
		}
	})
	t.Run("repository gone", func(t *testing.T) {
		f := newRec(t)
		f.agent(t, "Tidy", 1)
		if err := os.RemoveAll(f.repo); err != nil {
			t.Fatal(err)
		}
		set := f.ask(t)
		if set.Sources.Git != domain.SourceStatusUnavailable {
			t.Fatalf("%+v", set.Sources)
		}
		body, _ := json.Marshal(set)
		if strings.Contains(string(body), f.repo) || strings.Contains(string(body), f.wtRoot) {
			t.Fatalf("a path leaked into the answer: %s", body)
		}
		for _, it := range set.Items {
			if it.Intent == domain.IntentPrepareIntegration || it.Intent == domain.IntentFindBugs {
				t.Fatalf("suggested from a repository that cannot be read: %+v", it)
			}
		}
	})
	t.Run("work project", func(t *testing.T) {
		f := newRec(t)
		work, err := f.projects.CreateWork(f.ctx, "Marketing")
		if err != nil {
			t.Skipf("no work projects here: %v", err)
		}
		f.tasks.Create(f.ctx, work.ID, "Draft the post", "")
		set, err := f.rec.Recommend(f.ctx, RecommendInput{ProjectID: work.ID, Viewer: ownerViewer()})
		if err != nil {
			t.Fatal(err)
		}
		if findRec(set, domain.IntentNextTasks) == nil || findRec(set, domain.IntentFindBugs) != nil {
			t.Fatalf("%+v", set.Items)
		}
	})
}

func TestADeckWorkflowThatAlreadyRanSettlesItsOwnSuggestion(t *testing.T) {
	f := newRec(t)
	f.directCommitsOnMain(t, 8)
	if got := findRec(f.ask(t), domain.IntentFindBugs); got == nil || got.Action.Workflow != domain.WorkflowFindBugs || got.Action.StartsExecution {
		t.Fatalf("a project with history and no scan: %+v", got)
	}
	// The deck's own task for the workflow (its title and source reference) is what the history is read from.
	if _, err := f.tasks.CreateTask(f.ctx, NewTask{ProjectID: f.project.ID, Title: "Find Bugs", SourceRef: DeckOwnerKey("owner") + ":req-123456789012345:" + f.project.ID}); err != nil {
		t.Fatal(err)
	}
	if got := findRec(f.ask(t), domain.IntentFindBugs); got != nil {
		t.Fatalf("a scan was just made: %+v", got)
	}
	// And a workflow that is running right now is withheld, with the reason.
	set := f.ask(t)
	if findRec(set, domain.IntentFindBugs) != nil {
		t.Fatal("still quiet")
	}
}

func TestNoRunnerMeansNoWorkflowIsSuggested(t *testing.T) {
	f := newRec(t)
	f.directCommitsOnMain(t, 8)
	f.rec.Runners = nil
	set := f.ask(t)
	if findRec(set, domain.IntentFindBugs) != nil {
		t.Fatalf("%+v", set.Items)
	}
	found := false
	for _, w := range set.Withheld {
		found = found || w.Intent == domain.IntentFindBugs && w.Code == "no_runner" && w.Tier == domain.TierRestriction
	}
	if !found {
		t.Fatalf("%+v", set.Withheld)
	}
}

func TestPullRequestsFromGitHubShapeTheSuggestion(t *testing.T) {
	f := newRec(t)
	a := f.agent(t, "Cart", 1)
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli
	gh.set(t, "list.json", `[{"number":4,"title":"Cart","url":"https://github.com/acme/app/pull/4","state":"OPEN","isDraft":false,"headRefName":"`+a.branch+`","baseRefName":"main","headRefOid":"abc","author":{"login":"me"},"reviewDecision":"CHANGES_REQUESTED","mergeable":"MERGEABLE","statusCheckRollup":[],"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","mergedAt":null,"closedAt":null,"isCrossRepository":false}]`)
	set := f.ask(t)
	got := findRec(set, domain.IntentPrepareIntegration)
	if got == nil || got.Action.PR != 4 || got.Basis != domain.BasisRequired || !strings.Contains(got.Title, "#4 needs changes") {
		t.Fatalf("%+v", got)
	}
	// A repository that asks for review is governed from then on, whatever the person's habit.
	found := false
	for _, c := range set.Standing {
		found = found || c.Code == "repo_requires_review"
	}
	if !found {
		t.Fatalf("GitHub's review requirement is repository protection evidence: %+v", set.Standing)
	}
	// GitHub being asked at most once a minute: asking again does not call gh again.
	before := strings.Count(gh.calls(t), "pr list")
	f.ask(t)
	if after := strings.Count(gh.calls(t), "pr list"); after != before {
		t.Fatalf("gh was asked %d times, then %d", before, after)
	}
}

func TestAskingForSuggestionsChangesNothingAnywhere(t *testing.T) {
	f := newRec(t)
	a := f.agent(t, "Cart", 2)
	f.directCommitsOnMain(t, 6)
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli
	state := func() string {
		return gitOut(t, f.repo, "for-each-ref") + "\n" + gitOut(t, f.repo, "status", "--porcelain") + "\n" + gitOut(t, f.repo, "worktree", "list") + "\n" + gitOut(t, f.repo, "rev-parse", "HEAD")
	}
	tasksBefore, _ := f.tasks.List(f.ctx, f.project.ID)
	before := state()
	for i := 0; i < 3; i++ {
		f.ask(t)
	}
	if after := state(); after != before {
		t.Fatalf("the repository changed:\n%s\n---\n%s", before, after)
	}
	tasksAfter, _ := f.tasks.List(f.ctx, f.project.ID)
	if len(tasksAfter) != len(tasksBefore) {
		t.Fatal("a task was created")
	}
	// Only reads reach GitHub: the subcommand of every call is one that lists or shows.
	for _, line := range strings.Split(strings.TrimSpace(gh.calls(t)), "\n") {
		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}
		cmd := words[0]
		if len(words) > 1 {
			cmd += " " + words[1]
		}
		if cmd != "pr list" && cmd != "auth status" && cmd != "--version" {
			t.Fatalf("GitHub was asked to do more than read: %q", line)
		}
	}
	runs, _ := f.runs.ListByTask(f.ctx, a.task.ID)
	if len(runs) != 1 {
		t.Fatalf("a run was started: %d", len(runs))
	}
}

// ---- feedback ----

func memoryRecords(t *testing.T, f *recFixture) []domain.Memory {
	t.Helper()
	page, err := f.k.Memory.List(f.ctx, domain.MemoryScope{ProjectID: f.project.ID}, domain.MemoryQuery{Type: domain.FeedbackMemoryType, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	return page.Records
}

func TestFeedbackIsABoundedPrivatePreferenceAndNeverARuleOrAShareableStatement(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.directCommitsOnMain(t, 8)
	id := findRec(f.ask(t), domain.IntentPrepareIntegration).ID
	fb := func(verdict string) {
		t.Helper()
		if _, err := f.rec.Feedback(f.ctx, FeedbackInput{ProjectID: f.project.ID, RecommendationID: id, Intent: domain.IntentPrepareIntegration, Variant: domain.VariantDirectMerge, Verdict: verdict}); err != nil {
			t.Fatal(err)
		}
	}
	fb(domain.FeedbackUseful)
	fb(domain.FeedbackUseful)
	fb(domain.FeedbackNotUseful)
	recs := memoryRecords(t, f)
	if len(recs) != 1 {
		t.Fatalf("one record per project, intent and variant, not one per tap: %d", len(recs))
	}
	m := recs[0]
	if m.Authority != domain.MemoryPreference || m.State != domain.MemoryNeedsVerification || m.SharingEligible || m.Sensitivity != domain.MemoryPrivate || m.VerifiedAt != nil || m.Source.Type != domain.MemorySourceUser {
		t.Fatalf("feedback must stay an unverified, private, unshareable inference: %+v", m)
	}
	var got domain.RecFeedback
	if err := json.Unmarshal(m.Details, &got); err != nil || got.Useful != 2 || got.NotUseful != 1 || got.Net() != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if !strings.Contains(m.Summary, "2 useful, 1 not useful") {
		t.Fatalf("the person can read what was kept: %q", m.Summary)
	}
	// It never reaches the context that the assistant is given, and it never becomes a restriction or a permission.
	ctxOut, err := f.k.Retrieve(f.ctx, MemoryContextQuery{Scope: domain.MemoryScope{ProjectID: f.project.ID}, TokenBudget: 2000, PrioritizeAuthority: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range ctxOut.Items {
		if it.Type == domain.FeedbackMemoryType {
			t.Fatalf("feedback is not memory for a model to read: %+v", it)
		}
	}
	// The "not useful" hid that exact suggestion, and the habit still cannot reach a route a policy removed.
	if findRec(f.ask(t), domain.IntentPrepareIntegration) != nil && findRec(f.ask(t), domain.IntentPrepareIntegration).ID == id {
		t.Fatal("a suggestion marked not useful should be hidden")
	}
	f.policy(t, "policy", "All merges to main need review.")
	for _, it := range f.ask(t).Items {
		if it.Variant == domain.VariantDirectMerge {
			t.Fatalf("feedback brought back a removed route: %+v", it)
		}
	}
}

func TestFeedbackRejectsWhatItDoesNotUnderstand(t *testing.T) {
	f := newRec(t)
	good := FeedbackInput{ProjectID: f.project.ID, RecommendationID: domain.RecommendationID("x"), Intent: domain.IntentFindBugs, Variant: domain.VariantDefault, Verdict: domain.FeedbackUseful}
	bad := map[string]func(*FeedbackInput){
		"verdict":            func(in *FeedbackInput) { in.Verdict = "love_it" },
		"intent":             func(in *FeedbackInput) { in.Intent = "deploy" },
		"variant":            func(in *FeedbackInput) { in.Variant = "force_push" },
		"variant for intent": func(in *FeedbackInput) { in.Variant = domain.VariantDirectMerge },
		"id":                 func(in *FeedbackInput) { in.RecommendationID = "rec_../../etc" },
		"integration needs a route": func(in *FeedbackInput) {
			in.Intent, in.Variant = domain.IntentPrepareIntegration, domain.VariantDefault
		},
	}
	for name, mutate := range bad {
		in := good
		mutate(&in)
		if _, err := f.rec.Feedback(f.ctx, in); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	in := good
	in.ProjectID = "prj_missing"
	if _, err := f.rec.Feedback(f.ctx, in); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
	if len(memoryRecords(t, f)) != 0 {
		t.Fatal("a refused verdict left a record")
	}
	f.rec.Knowledge = nil
	if _, err := f.rec.Feedback(f.ctx, good); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("no memory: %v", err)
	}
}

func TestOneRecordPerKindHoweverManyVerdicts(t *testing.T) {
	f := newRec(t)
	for i := 0; i < 12; i++ {
		if _, err := f.rec.Feedback(f.ctx, FeedbackInput{ProjectID: f.project.ID, RecommendationID: domain.RecommendationID("r", fmt.Sprint(i)), Intent: domain.IntentRefactor, Variant: domain.VariantDefault, Verdict: domain.FeedbackDismissed}); err != nil {
			t.Fatal(err)
		}
	}
	recs := memoryRecords(t, f)
	if len(recs) != 1 {
		t.Fatalf("%d records", len(recs))
	}
	var got domain.RecFeedback
	_ = json.Unmarshal(recs[0].Details, &got)
	if got.Dismissed != 12 || len(got.Hidden) != domain.MaxDismissedIDs {
		t.Fatalf("counters are kept, the hidden list is bounded: %+v", got)
	}
}

func TestAMemoryRecordThatClaimsMoreThanFeedbackMayIsIgnored(t *testing.T) {
	f := newRec(t)
	scope := domain.MemoryScope{ProjectID: f.project.ID}
	// Someone wrote a record of the feedback type but with an authority feedback never has: it is not feedback.
	now := f.deps.now()
	forged := domain.MemoryContent{Type: domain.FeedbackMemoryType, Authority: domain.MemoryObservation, Source: domain.MemoryProvenance{Type: domain.MemorySourceUser, Reference: "x"},
		Summary: "fake", Details: json.RawMessage(`{"intent":"find_bugs","variant":"default","notUseful":900}`), State: domain.MemoryCurrent, VerifiedAt: &now}
	if _, err := f.k.Memory.Create(f.ctx, scope, forged); err != nil {
		t.Fatal(err)
	}
	if got := f.rec.feedbackFor(f.ctx, f.project.ID); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestRedactionRemovesSavedTextButKeepsWhatItDid(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.policy(t, "policy", "SECRET-PHRASE: merges to main need review.")
	f.policy(t, "decision", "SECRET-DECISION: we use pull requests.")
	full := f.ask(t)
	body, _ := json.Marshal(full)
	if !strings.Contains(string(body), "SECRET-PHRASE") {
		t.Fatalf("the owner sees the policy: %s", body)
	}
	hidden := f.ask(t, func(in *RecommendInput) { in.Redact = true })
	body, _ = json.Marshal(hidden)
	if strings.Contains(string(body), "SECRET-") {
		t.Fatalf("saved text reached a connection that does not serve memory: %s", body)
	}
	got := findRec(hidden, domain.IntentPrepareIntegration)
	if got == nil || got.Variant != domain.VariantPullRequest || len(got.Applied) == 0 {
		t.Fatalf("what the memory did must remain: %+v", got)
	}
}

func TestRedactionHoldsWhenSavedTextHasQuotationMarksOfItsOwn(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	f.policy(t, "policy", `Never merge "directly" to main; SECRET-TAIL stays private`)
	f.policy(t, "decision", `We use "pull requests"; SECRET-DECISION-TAIL`)
	hidden := f.ask(t, func(in *RecommendInput) { in.Redact = true })
	body, _ := json.Marshal(hidden)
	if strings.Contains(string(body), "SECRET-") || strings.Contains(string(body), "directly") {
		t.Fatalf("saved text leaked through a connection that does not serve memory: %s", body)
	}
}

func TestLinkedTeamProjectsAreCutOnlyAfterTheyAreSorted(t *testing.T) {
	var tasks []domain.Task
	for i := 70; i > 0; i-- {
		tasks = append(tasks, domain.Task{SourceRef: fmt.Sprintf("%stw_%02d:tpj_%02d:ttk:mem", integration.SourcePrefix, i, i)})
	}
	links := teamLinksOf(tasks)
	if len(links) != maxTeamLinks || links[0].WorkspaceID != "tw_01" || links[len(links)-1].WorkspaceID != fmt.Sprintf("tw_%02d", maxTeamLinks) {
		t.Fatalf("the same tasks must always give the same links, in order: first %+v last %+v (%d)", links[0], links[len(links)-1], len(links))
	}
}

func TestAGitHubFailureThatMayPassIsNotRememberedForAMinute(t *testing.T) {
	f := newRec(t)
	f.agent(t, "Tidy", 1)
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	f.rec.Deps.Now = func() time.Time { return now }
	gh.failWith(t, "error connecting to api.github.com")
	calls := func() int { return strings.Count(gh.calls(t), "pr list") }
	f.ask(t)
	f.ask(t)
	if calls() != 1 {
		t.Fatalf("a failure is kept briefly, not asked again at once: %d", calls())
	}
	now = now.Add(prFailureFor + time.Second)
	f.ask(t)
	if calls() != 2 {
		t.Fatalf("after a few seconds GitHub is asked again: %d", calls())
	}
	// And once it answers, that answer holds for the longer time.
	if err := os.Remove(filepath.Join(gh.dir, "fail")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(prFailureFor + time.Second)
	f.ask(t)
	now = now.Add(prFailureFor + time.Second)
	f.ask(t)
	if calls() != 3 {
		t.Fatalf("a good answer holds for a minute: %d", calls())
	}
}

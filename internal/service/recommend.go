package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/integration"
	"werkbord/internal/store"
)

// Recommender answers "what would be a good next step here?" for one project.
//
// It READS. It gathers the facts that domain.Recommend needs from places that already exist (the board, the repository
// as the Git Control Center reads it, GitHub as it reads it, the stored health findings, saved memory) and returns the
// rules' answer. It holds no runner, no deck and no way to push, merge, branch or start anything: a source-level test
// (internal/archtest) keeps it that way. What it returns are ways to OPEN existing screens, each of which still asks.
//
// It is also the only place memory is turned into rule inputs, and it does that through Knowledge.Retrieve, the same gate
// the assistant's retrieve_memory uses: only current, private, non-duplicate records whose repository and run evidence
// still check out come through, policies and decisions first. A record that gate drops is not seen here either.
//
// Deterministic first: the rules decide what is allowed and what is suggested before any model is involved. A model that
// is shown the answer (the assistant) can explain it. It cannot extend it.
type Recommender struct {
	Deps
	// Control reads Git and GitHub. Without it the repository is reported unavailable and nothing repository-shaped is suggested.
	Control *GitControl
	// Knowledge reads saved memory and keeps the feedback. Without it memory is reported unavailable.
	Knowledge *Knowledge
	// Runners says which runners are online and what they can run. Without it no workflow is suggested: that a workflow
	// could start is a fact about the application, and it is not guessed.
	Runners *Runners

	mu    sync.Mutex
	prs   map[string]cachedPRs
	locks map[string]*sync.Mutex
}

type cachedPRs struct {
	at    time.Time
	state *domain.GitHubState
	// ttl is how long this answer holds. A failure that may pass (a rate limit, a sign-in being fixed) is kept briefly.
	ttl time.Duration
}

const (
	// recommendTimeout bounds the slow parts (Git, GitHub). A source that is late is reported unavailable, never waited for.
	recommendTimeout = 8 * time.Second
	// prCacheFor spares GitHub: the suggestions are asked for whenever a screen opens.
	prCacheFor    = 60 * time.Second
	prFailureFor  = 10 * time.Second
	memoryBudget  = 4000
	maxFactTasks  = 200
	maxFactBranch = 40
	maxFactPRs    = 30
	// maxTeamLinks bounds the Team projects one project is linked to. The assistant reads at most MaxTeamReads of them.
	maxTeamLinks = 64
	// MaxTeamReads is how many linked Team projects the desktop bridge is asked about in one answer; more than this is reported as a gap.
	MaxTeamReads = 8
)

// RecommendInput says who is asking and about what.
type RecommendInput struct {
	ProjectID string
	// Viewer says who asks and what they may read. RunnerReady is worked out here and any value given is ignored.
	Viewer domain.RecViewer
	// PersonalMemory includes memory that belongs to the person rather than to the project. Sessions limited to some
	// projects must leave it false.
	PersonalMemory bool
	// TeamMemory is what the desktop read of the linked Team workspace's memory, if it could. The controller has no Team
	// credential and cannot read it itself.
	TeamMemory *TeamMemoryInput
	// Focus limits the answer to one intent. Anything that is not an intent is ignored.
	Focus string
	// Redact removes the text of saved memory from the answer, for a connection on which memory is not served.
	Redact bool
}

// TeamMemoryInput is Team memory as the desktop bridge returned it, after Team's own access and freshness checks.
type TeamMemoryInput struct {
	Status string
	Items  []MemoryContextItem
}

// TeamLink is a Team project this project's tasks were imported from.
type TeamLink struct {
	WorkspaceID   string `json:"workspaceId"`
	TeamProjectID string `json:"teamProjectId"`
}

// Recommend gathers the facts and applies the rules.
func (r *Recommender) Recommend(ctx context.Context, in RecommendInput) (*domain.RecommendationSet, error) {
	facts, err := r.Facts(ctx, in)
	if err != nil {
		return nil, err
	}
	set := domain.Recommend(facts)
	if in.Redact {
		redactSet(&set)
	}
	return &set, nil
}

// TeamLinks lists the Team projects the project's tasks came from, so that a caller that can reach those workspaces
// (the desktop assistant) knows which ones to read policies from.
func (r *Recommender) TeamLinks(ctx context.Context, projectID string) ([]TeamLink, error) {
	var tasks []domain.Task
	if err := r.Store.View(ctx, func(tx store.Tx) error {
		var e error
		tasks, e = tx.Tasks().ListByProject(ctx, projectID)
		return e
	}); err != nil {
		return nil, err
	}
	return teamLinksOf(tasks), nil
}

func teamLinksOf(tasks []domain.Task) []TeamLink {
	seen := map[TeamLink]bool{}
	var out []TeamLink
	for _, t := range tasks {
		if t.ArchivedAt != nil {
			continue
		}
		w, p, _, _, ok := integration.ParseSource(t.SourceRef)
		if !ok {
			continue
		}
		if l := (TeamLink{WorkspaceID: w, TeamProjectID: p}); !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	// Everything is collected and sorted before anything is cut, so the same tasks always give the same links.
	sort.Slice(out, func(i, j int) bool {
		if out[i].WorkspaceID != out[j].WorkspaceID {
			return out[i].WorkspaceID < out[j].WorkspaceID
		}
		return out[i].TeamProjectID < out[j].TeamProjectID
	})
	if len(out) > maxTeamLinks {
		out = out[:maxTeamLinks]
	}
	return out
}

// Facts reads everything the rules look at. A source that fails is marked unavailable; the call itself fails only when
// the project does not exist.
func (r *Recommender) Facts(ctx context.Context, in RecommendInput) (domain.RecFacts, error) {
	now := r.now()
	f := domain.RecFacts{Now: now, ProjectID: in.ProjectID, Viewer: in.Viewer, Workflows: map[string]domain.RecWorkflow{}}
	if domain.ValidRecIntent(in.Focus) {
		f.Focus = in.Focus
	}
	f.Memory = domain.RecMemory{Individual: domain.SourceStatusNotChecked, Team: domain.SourceStatusNotLinked}
	f.Repo = domain.RecRepo{Status: domain.SourceStatusNotChecked}
	f.Tasks.Status = domain.SourceStatusNotChecked
	f.Viewer.RunnerReady = false

	var (
		project domain.Project
		tasks   []domain.Task
		latest  []domain.Run
		active  []domain.Run
		health  []domain.HealthFinding
	)
	err := r.Store.View(ctx, func(tx store.Tx) error {
		p, e := tx.Projects().Get(ctx, in.ProjectID)
		if e != nil {
			return e
		}
		project = *p
		if tasks, e = tx.Tasks().ListByProject(ctx, in.ProjectID); e != nil {
			return e
		}
		if latest, e = tx.Runs().ListLatestByProject(ctx, in.ProjectID); e != nil {
			return e
		}
		all, e := tx.Runs().ListActive(ctx)
		if e != nil {
			return e
		}
		for _, run := range all {
			if run.ProjectID == in.ProjectID {
				active = append(active, run)
			}
		}
		health, e = tx.Health().ListByProject(ctx, in.ProjectID, domain.HealthOpen)
		return e
	})
	if err != nil {
		return f, err
	}
	f.ProjectName = project.Name
	f.Project = domain.RecProject{HasRepository: project.HasRepository(), TeamLinked: len(teamLinksOf(tasks)) > 0}
	if r.Runners != nil {
		if runners, e := r.Runners.List(ctx); e == nil {
			f.Viewer.RunnerReady = runnerReady(runners, project.ID)
		}
	}

	if in.Viewer.Tasks {
		f.Tasks = taskFacts(tasks, latest, active)
	} else {
		f.Tasks.Status = domain.SourceStatusDenied
	}
	if in.Viewer.Runs || in.Viewer.Tasks {
		f.Workflows = workflowFacts(tasks, latest, active)
	}
	if project.HasRepository() && in.Viewer.Repo {
		r.repoFacts(ctx, &f, project.ID, health)
	} else if project.HasRepository() {
		f.Repo.Status = domain.SourceStatusDenied
	}
	r.memoryFacts(ctx, &f, in)
	return f, nil
}

// runnerReady says whether some runner is online, enabled and may take this project's work. An agent that can run must be available on it.
func runnerReady(runners []domain.Runner, projectID string) bool {
	for _, rn := range runners {
		if !rn.Online || rn.Disabled || rn.Removed || !anyAgentAvailable(rn.Capabilities.Agents) {
			continue
		}
		if len(rn.Projects) == 0 {
			return true
		}
		for _, p := range rn.Projects {
			if p == projectID {
				return true
			}
		}
	}
	return false
}

// anyAgentAvailable: an agent that is listed but not installed or not signed in cannot take a workflow.
func anyAgentAvailable(agents []domain.Agent) bool {
	for _, a := range agents {
		if a.Available {
			return true
		}
	}
	return false
}

func taskFacts(tasks []domain.Task, latest, active []domain.Run) domain.RecTasks {
	out := domain.RecTasks{Status: domain.SourceStatusRead}
	byID := map[string]domain.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	last := map[string]domain.Run{}
	for _, r := range latest {
		last[r.TaskID] = r
	}
	live := map[string]domain.Run{}
	for _, r := range active {
		live[r.TaskID] = r
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Position < tasks[j].Position })
	for _, t := range tasks {
		if t.ArchivedAt != nil || len(out.Items) >= maxFactTasks {
			continue
		}
		item := domain.RecTask{ID: t.ID, Title: t.Title, State: t.State, Position: t.Position, UpdatedAt: t.UpdatedAt, PlannedStart: t.Plan.Start}
		_, _, _, _, item.Team = integration.ParseSource(t.SourceRef)
		for _, dep := range t.Orchestration.Dependencies {
			if d, ok := byID[dep]; !ok || d.State != domain.TaskDone {
				item.WaitingOn++
			}
		}
		if run, ok := live[t.ID]; ok {
			item.Active, item.RunState, item.Waiting = true, run.State, run.Waiting
		} else if run, ok := last[t.ID]; ok {
			item.RunState, item.EndedAt = run.State, run.EndedAt
		}
		// A human's task is still the human's to start; only an agent-capable one is "ready" for the board's own purposes,
		// but a person decides what they do next either way, so readiness here means only that nothing is in the way.
		item.Ready = t.State == domain.TaskBacklog && item.WaitingOn == 0 && !item.Active
		out.Items = append(out.Items, item)
	}
	return out
}

// workflowFacts reads the deck workflows' history from the tasks the deck made. A task the deck made has the deck's source
// reference and the workflow's name as its title; nothing else is assumed.
func workflowFacts(tasks []domain.Task, latest, active []domain.Run) map[string]domain.RecWorkflow {
	out := map[string]domain.RecWorkflow{}
	last := map[string]domain.Run{}
	for _, r := range latest {
		last[r.TaskID] = r
	}
	live := map[string]bool{}
	for _, r := range active {
		live[r.TaskID] = true
	}
	ids := map[string]string{}
	for _, w := range DeckWorkflows {
		ids[w.Name] = w.ID
	}
	for _, t := range tasks {
		id, ok := ids[t.Title]
		if !ok || !strings.HasPrefix(t.SourceRef, "phone-deck.") {
			continue
		}
		wf := out[id]
		if live[t.ID] {
			wf.Active = true
		}
		if t.CreatedAt.After(wf.LastAt) {
			wf.LastAt, wf.TaskID = t.CreatedAt, t.ID
			wf.LastFailed = false
			if run, ok := last[t.ID]; ok && (run.State == domain.RunFailed || run.State == domain.RunStopped) {
				wf.LastFailed = true
			}
		}
		out[id] = wf
	}
	return out
}

// ---- the repository ----

func (r *Recommender) repoFacts(ctx context.Context, f *domain.RecFacts, projectID string, health []domain.HealthFinding) {
	f.Repo.Health = healthFacts(health)
	if r.Control == nil {
		f.Repo.Status, f.Repo.Problem = domain.SourceStatusUnavailable, "Git is not available here"
		return
	}
	gctx, cancel := context.WithTimeout(ctx, recommendTimeout)
	defer cancel()
	o, err := r.Control.Overview(gctx, projectID)
	if err != nil {
		f.Repo.Status, f.Repo.Problem = domain.SourceStatusUnavailable, describeRepoError(ctx, err)
		return
	}
	f.Repo.Status = domain.SourceStatusRead
	f.Repo.Head, f.Repo.Detached, f.Repo.Unborn = o.Local.Head.Branch, o.Local.Head.Detached, o.Local.Head.Unborn
	f.Repo.Target, f.Repo.TargetLocal = o.Local.Target.Name, o.Local.Target.LocalExists
	wt := o.Local.WorkingTree
	f.Repo.Dirty, f.Repo.Conflicted, f.Repo.Operation = wt.Counts.Total(), wt.Counts.Conflicted, wt.Operation
	f.Repo.HasRemote = len(o.Remote.Remotes) > 0
	for _, b := range o.Branches {
		if b.Scope != domain.ScopeLocal || len(f.Repo.Branches) >= maxFactBranch {
			continue
		}
		rb := domain.RecBranch{Name: b.Name, Owned: b.DevBoard.Created, Target: b.Target, Head: b.Head, Ahead: b.VsTarget.Ahead, Behind: b.VsTarget.Behind,
			Merged: b.Merged, NotPushed: b.NotPushed, HasUpstream: b.Upstream.State != domain.UpstreamNone, ActiveRun: b.DevBoard.ActiveRun,
			TaskID: b.DevBoard.TaskID, TaskTitle: b.DevBoard.TaskTitle, Phase: b.DevBoard.Phase, LastCommit: b.CommitDate}
		if b.Worktree != nil && b.Worktree.Dirty != nil {
			rb.Dirty = b.Worktree.Dirty.Total()
		}
		f.Repo.Branches = append(f.Repo.Branches, rb)
	}
	if f.Repo.TargetLocal && f.Repo.Target != "" {
		if page, e := r.Control.Commits(gctx, projectID, domain.ScopeLocal, f.Repo.Target, 0, 50); e == nil {
			for _, c := range page.Items {
				f.Repo.TargetCommits = append(f.Repo.TargetCommits, domain.RecCommit{Date: c.Date, Merge: c.Merge, Subject: c.Subject})
			}
		}
	}
	f.Repo.PullRequests = r.pullRequestFacts(gctx, projectID)
}

// describeRepoError says why the repository could not be read, in words that carry no path and no raw error text.
func describeRepoError(ctx context.Context, err error) string {
	switch {
	case ctx.Err() != nil, errors.Is(err, context.DeadlineExceeded):
		return "it took too long to answer"
	case errors.Is(err, domain.ErrConflict):
		return "the folder no longer holds the repository that was registered"
	case errors.Is(err, domain.ErrNotFound):
		return "the folder is missing"
	case errors.Is(err, domain.ErrInvalid):
		return "it is not a usable Git repository"
	}
	return "Git did not answer"
}

func (r *Recommender) pullRequestFacts(ctx context.Context, projectID string) domain.RecPullRequests {
	out := domain.RecPullRequests{Status: domain.SourceStatusUnavailable}
	st := r.cachedPullRequests(projectID)
	if st == nil {
		var err error
		st, err = r.Control.PullRequests(ctx, projectID)
		if err != nil || st == nil {
			out.Reason = "GitHub could not be asked"
			return out
		}
		r.storePullRequests(projectID, st)
	}
	if !st.Available {
		switch st.Reason {
		case domain.GHNoRemote, domain.GHNotGitHub:
			out.Status = domain.SourceStatusNotLinked
		default:
			out.Detected = true
			out.Reason = ClipMessage(st.Message)
		}
		return out
	}
	out.Status, out.Detected, out.Available = domain.SourceStatusRead, true, true
	for _, pr := range st.PullRequests {
		if len(out.Items) >= maxFactPRs {
			break
		}
		item := domain.RecPullRequest{Number: pr.Number, Title: pr.Title, Branch: pr.HeadBranch, Base: pr.BaseBranch, State: pr.State, Draft: pr.Draft,
			Review: pr.Review, Checks: pr.Checks.State, Mergeable: pr.Mergeable, TaskID: pr.TaskID}
		if pr.CrossRepo {
			// A fork's branch name says nothing about a branch of this repository.
			item.Branch = ""
		}
		if pr.UpdatedAt != nil {
			item.UpdatedAt = *pr.UpdatedAt
		}
		out.Items = append(out.Items, item)
	}
	return out
}

// ClipMessage shortens a third party's message to something that fits a note.
func ClipMessage(s string) string { return domain.ClipRecText(s, 120) }

func (r *Recommender) cachedPullRequests(projectID string) *domain.GitHubState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.prs[projectID]; ok && r.now().Sub(c.at) < c.ttl {
		return c.state
	}
	return nil
}

func (r *Recommender) storePullRequests(projectID string, st *domain.GitHubState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.prs == nil || len(r.prs) > 64 {
		r.prs = map[string]cachedPRs{}
	}
	ttl := prCacheFor
	switch {
	case st.Available:
	case st.Reason == domain.GHNoRemote || st.Reason == domain.GHNotGitHub:
		// Not a failure: the repository simply has no GitHub to ask.
	default:
		ttl = prFailureFor
	}
	r.prs[projectID] = cachedPRs{at: r.now(), state: st, ttl: ttl}
}

func healthFacts(findings []domain.HealthFinding) domain.RecHealth {
	out := domain.RecHealth{Status: domain.SourceStatusRead}
	for _, f := range findings {
		if f.State != domain.HealthOpen {
			continue
		}
		if f.Severity.Rank() >= domain.HealthAttention.Rank() {
			out.Attention++
		}
		if f.Severity.Rank() >= domain.HealthRisk.Rank() {
			out.Risk++
		}
		if f.Severity.Rank() >= domain.HealthAttention.Rank() && len(out.Top) < 3 {
			out.Top = append(out.Top, domain.RecFinding{ID: f.ID, Type: string(f.Type), Title: f.Title, Severity: string(f.Severity), Subject: f.Subject.Branch})
		}
	}
	return out
}

// ---- memory ----

func (r *Recommender) memoryFacts(ctx context.Context, f *domain.RecFacts, in RecommendInput) {
	if f.Project.TeamLinked {
		f.Memory.Team = domain.SourceStatusNotChecked
		if !in.Viewer.Memory {
			f.Memory.Team = domain.SourceStatusDenied
		} else if in.TeamMemory != nil {
			f.Memory.Team = in.TeamMemory.Status
			for _, it := range in.TeamMemory.Items {
				f.Memory.Items = append(f.Memory.Items, memoryItem(it, domain.RecSrcTeam))
			}
		}
	}
	switch {
	case !in.Viewer.Memory:
		f.Memory.Individual = domain.SourceStatusDenied
		return
	case r.Knowledge == nil:
		f.Memory.Individual = domain.SourceStatusUnavailable
		return
	}
	scopes := []domain.MemoryScope{{ProjectID: in.ProjectID}}
	if in.PersonalMemory {
		scopes = append(scopes, domain.MemoryScope{})
	}
	found := false
	for _, scope := range scopes {
		got, err := r.Knowledge.Retrieve(ctx, MemoryContextQuery{Scope: scope, TokenBudget: memoryBudget, PrioritizeAuthority: true})
		if err != nil {
			if ctx.Err() == nil {
				f.Memory.Individual = domain.SourceStatusUnavailable
			}
			continue
		}
		for _, it := range got.Items {
			if it.Type == domain.FeedbackMemoryType {
				continue
			}
			found = true
			f.Memory.Items = append(f.Memory.Items, memoryItem(it, domain.RecSrcIndividual))
		}
	}
	if f.Memory.Individual != domain.SourceStatusUnavailable {
		f.Memory.Individual = domain.SourceStatusEmpty
		if found {
			f.Memory.Individual = domain.SourceStatusRead
		}
	}
	f.Memory.Feedback = r.feedbackFor(ctx, in.ProjectID)
}

func memoryItem(it MemoryContextItem, source string) domain.RecMemoryItem {
	return domain.RecMemoryItem{ID: it.ID, Kind: it.Kind, Type: it.Type, Summary: domain.ClipRecText(it.Summary, 400), Source: source,
		Verification: it.Verification, Freshness: it.Freshness, Reference: domain.ClipRecText(it.Source.Reference, 200)}
}

// ---- feedback ----

var recID = regexp.MustCompile(`^rec_[0-9a-f]{20}$`)

// FeedbackInput is a person's verdict on one suggestion.
type FeedbackInput struct {
	ProjectID        string `json:"-"`
	RecommendationID string `json:"recommendationId"`
	Intent           string `json:"intent"`
	Variant          string `json:"variant"`
	Verdict          string `json:"verdict"`
}

func (r *Recommender) feedbackFor(ctx context.Context, projectID string) []domain.RecFeedback {
	recs, err := r.feedbackRecords(ctx, projectID)
	if err != nil {
		return nil
	}
	out := make([]domain.RecFeedback, 0, len(recs))
	for _, m := range recs {
		if fb, ok := decodeFeedback(m); ok {
			out = append(out, fb)
		}
	}
	return out
}

func (r *Recommender) feedbackRecords(ctx context.Context, projectID string) ([]domain.Memory, error) {
	page, err := r.Knowledge.Memory.List(ctx, domain.MemoryScope{ProjectID: projectID}, domain.MemoryQuery{Type: domain.FeedbackMemoryType, Limit: 50})
	return page.Records, err
}

func decodeFeedback(m domain.Memory) (domain.RecFeedback, bool) {
	var fb domain.RecFeedback
	if m.Type != domain.FeedbackMemoryType || m.Authority != domain.MemoryPreference || json.Unmarshal(m.Details, &fb) != nil {
		return fb, false
	}
	if !domain.ValidRecIntent(fb.Intent) || !domain.ValidRecVariant(fb.Variant) || fb.Useful < 0 || fb.NotUseful < 0 || fb.Dismissed < 0 {
		return fb, false
	}
	// Whatever the stored record claims, it stays within the bounds feedback keeps.
	fb.Useful, fb.NotUseful, fb.Dismissed = min(fb.Useful, domain.MaxFeedbackCount), min(fb.NotUseful, domain.MaxFeedbackCount), min(fb.Dismissed, domain.MaxFeedbackCount)
	if len(fb.Hidden) > domain.MaxDismissedIDs {
		fb.Hidden = fb.Hidden[len(fb.Hidden)-domain.MaxDismissedIDs:]
	}
	return fb, true
}

func validFeedbackKind(intent, variant string) bool {
	if !domain.ValidRecIntent(intent) || !domain.ValidRecVariant(variant) {
		return false
	}
	if intent == domain.IntentPrepareIntegration {
		return variant == domain.VariantDirectMerge || variant == domain.VariantPullRequest
	}
	return variant == domain.VariantDefault
}

var intentWords = map[string]string{
	domain.IntentContinueWork: "continuing work", domain.IntentPrepareIntegration: "preparing a branch", domain.IntentFindBugs: "Find Bugs",
	domain.IntentRefactor: "Refactor Codebase", domain.IntentReviewIssues: "reviewing repository issues", domain.IntentNextTasks: "choosing the next task",
}

func feedbackSummary(fb domain.RecFeedback) string {
	s := fmt.Sprintf("Suggestions for %s", intentWords[fb.Intent])
	switch fb.Variant {
	case domain.VariantDirectMerge:
		s += " (merging directly)"
	case domain.VariantPullRequest:
		s += " (pull request)"
	}
	return fmt.Sprintf("%s: %d useful, %d not useful, %d dismissed.", s, fb.Useful, fb.NotUseful, fb.Dismissed)
}

// Feedback records a verdict on a suggestion as an inferred preference in this person's own memory. It is a hint to the
// ordering of later suggestions and nothing more:
//
//   - it is an inferred_preference, never a decision or a policy, so it can neither restrict nor permit anything;
//   - it is private, unverified and not eligible for sharing, so no part of Team can receive it unless the person goes
//     through the ordinary review to publish a statement of their own wording;
//   - it holds counters and at most a few hidden suggestion IDs, bounded, one record per project, intent and variant.
func (r *Recommender) Feedback(ctx context.Context, in FeedbackInput) (*domain.RecFeedback, error) {
	invalid := func(why string) error { return fmt.Errorf("%w: %s", domain.ErrInvalid, why) }
	switch {
	case r.Knowledge == nil:
		return nil, invalid("memory is not available")
	case !validFeedbackKind(in.Intent, in.Variant):
		return nil, invalid("unknown suggestion kind")
	case in.Verdict != domain.FeedbackUseful && in.Verdict != domain.FeedbackNotUseful && in.Verdict != domain.FeedbackDismissed:
		return nil, invalid("unknown verdict")
	case in.RecommendationID != "" && !recID.MatchString(in.RecommendationID):
		return nil, invalid("unknown suggestion")
	}
	if err := r.Store.View(ctx, func(tx store.Tx) error { _, e := tx.Projects().Get(ctx, in.ProjectID); return e }); err != nil {
		return nil, err
	}
	// One writer per project, so two quick taps count twice instead of racing on the same version.
	unlock := r.lockProject(in.ProjectID)
	defer unlock()

	scope := domain.MemoryScope{ProjectID: in.ProjectID}
	for attempt := 0; attempt < 3; attempt++ {
		records, err := r.feedbackRecords(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		var existing *domain.Memory
		var fb domain.RecFeedback
		for i := range records {
			if got, ok := decodeFeedback(records[i]); ok && got.Intent == in.Intent && got.Variant == in.Variant {
				existing, fb = &records[i], got
			}
		}
		if existing == nil {
			fb = domain.RecFeedback{Intent: in.Intent, Variant: in.Variant}
		}
		fb = fb.Apply(in.Verdict, in.RecommendationID, r.now())
		details, _ := json.Marshal(fb)
		content := domain.MemoryContent{Type: domain.FeedbackMemoryType, Authority: domain.MemoryPreference,
			Source:  domain.MemoryProvenance{Type: domain.MemorySourceUser, Reference: "recommendation-feedback:" + in.Intent + "/" + in.Variant},
			Summary: feedbackSummary(fb), Details: details, State: domain.MemoryNeedsVerification, Sensitivity: domain.MemoryPrivate}
		if existing == nil {
			_, err = r.Knowledge.Memory.Create(ctx, scope, content)
		} else {
			_, err = r.Knowledge.Memory.Update(ctx, scope, existing.ID, existing.Version, content)
		}
		switch {
		case err == nil:
			return &fb, nil
		case errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrDuplicate):
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("%w: feedback changed while it was being saved; try again", domain.ErrConflict)
}

func (r *Recommender) lockProject(projectID string) func() {
	r.mu.Lock()
	if r.locks == nil {
		r.locks = map[string]*sync.Mutex{}
	}
	l, ok := r.locks[projectID]
	if !ok {
		l = &sync.Mutex{}
		r.locks[projectID] = l
	}
	r.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// ---- redaction ----

var quoted = regexp.MustCompile(`"[^"]*"( \(freshness uncertain\))?`)

// redactSet takes the words of saved memory out of a set, for a connection that is not allowed to read memory. What
// remains says that something saved applied, which record, and what it did to the suggestion; not what it said.
func redactSet(s *domain.RecommendationSet) {
	const hidden = "(saved text not shown on this connection)"
	scrub := func(text string) string { return quoted.ReplaceAllString(text, hidden) }
	isMemory := func(source string) bool { return source == domain.RecSrcIndividual || source == domain.RecSrcTeam }
	constraints := func(cs []domain.RecConstraint) {
		for i := range cs {
			if isMemory(cs[i].Source) {
				cs[i].Message = scrub(cs[i].Message)
			}
		}
	}
	constraints(s.Standing)
	for i := range s.Items {
		constraints(s.Items[i].Applied)
		for j := range s.Items[i].Evidence {
			if e := &s.Items[i].Evidence[j]; isMemory(e.Source) {
				e.Label = "Saved note " + e.Ref
				if e.Tier == domain.TierDecision {
					e.Label = "A saved decision applies."
				}
			}
		}
	}
	for i := range s.Withheld {
		s.Withheld[i].Reason = scrub(s.Withheld[i].Reason)
	}
}

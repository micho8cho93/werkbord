package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Workflow recommendations: what Werkbord may suggest doing next, and why.
//
// A recommendation is advice with its evidence. It is never a permission and never an instruction to anything that
// executes: every action it carries is a way to OPEN a screen that already exists, where the existing confirmation still
// applies (RecAction.StartsExecution is always false, and the tests hold it there). Nothing in this file, or in
// recommend_rules.go, reads a table, runs Git or calls a model: the service gathers the facts (internal/service/recommend.go)
// and the rules here decide, so the same facts always give the same answer and the answer can be explained.
//
// The rules obey one strict order. A suggestion is first checked against what outranks it, and only what survives is
// ranked by what ranks lower:
//
//	1. security and application-enforced restrictions   (what this viewer and this application can do at all)
//	2. explicit Team policies and repository protection  (what the team and the repository require)
//	3. explicit user decisions                           (what the person said they want)
//	4. current, verified Git and task evidence           (what is true right now)
//	5. historical preferences and inferred suggestions   (what they usually did, what they found useful)
//
// Tiers 1 to 3 can only forbid, require or explain. Tiers 4 and 5 can only order what tiers 1 to 3 left standing. That
// asymmetry is the guarantee: a remembered habit can pick between two allowed ways of working and can never add a way
// that a restriction removed.

// RecTier is a rung of the hierarchy above. A lower number outranks a higher one.
type RecTier int

const (
	TierRestriction RecTier = 1
	TierPolicy      RecTier = 2
	TierDecision    RecTier = 3
	TierEvidence    RecTier = 4
	TierHistory     RecTier = 5
)

func (t RecTier) String() string {
	switch t {
	case TierRestriction:
		return "restriction"
	case TierPolicy:
		return "policy"
	case TierDecision:
		return "decision"
	case TierEvidence:
		return "evidence"
	case TierHistory:
		return "history"
	}
	return "unknown"
}

// What the suggestions are about. The first six are the workflows Werkbord starts with.
const (
	IntentContinueWork       = "continue_work"
	IntentPrepareIntegration = "prepare_integration"
	IntentFindBugs           = "find_bugs"
	IntentRefactor           = "refactor_codebase"
	IntentReviewIssues       = "review_issues"
	IntentNextTasks          = "next_tasks"
)

// RecIntents lists every intent, in the order they are considered.
var RecIntents = []string{IntentContinueWork, IntentPrepareIntegration, IntentReviewIssues, IntentFindBugs, IntentRefactor, IntentNextTasks}

// ValidRecIntent reports whether s names an intent.
func ValidRecIntent(s string) bool {
	for _, i := range RecIntents {
		if i == s {
			return true
		}
	}
	return false
}

// The ways of working a suggestion can take. Most intents have only the default.
const (
	VariantDefault     = "default"
	VariantDirectMerge = "direct_merge"
	VariantPullRequest = "pull_request"
)

// ValidRecVariant reports whether s names a variant.
func ValidRecVariant(s string) bool {
	return s == VariantDefault || s == VariantDirectMerge || s == VariantPullRequest
}

// The deck workflows a suggestion can point at. They are the ids of service.DeckWorkflows; a service test keeps them equal.
const (
	WorkflowFindBugs = "find-bugs"
	WorkflowRefactor = "refactor-codebase"
)

// What a suggestion's action opens. Each is a screen or a flow that exists and asks the person before anything happens.
const (
	ActionOpenWorkflow = "open_workflow" // the Control Deck, at the workflow's button
	ActionOpenTask     = "open_task"
	ActionOpenBranch   = "open_branch" // the Git branch screen, where merge and pull request are confirmed
	ActionOpenChanges  = "open_changes"
	ActionOpenGit      = "open_git"
	ActionOpenHealth   = "open_health"
	ActionOpenBoard    = "open_board"
)

// Where the evidence behind a suggestion comes from.
const (
	RecSrcApplication = "application"
	RecSrcGit         = "git"
	RecSrcGitHub      = "github"
	RecSrcTask        = "task"
	RecSrcRun         = "run"
	RecSrcHealth      = "health"
	RecSrcIndividual  = "individual_memory"
	RecSrcTeam        = "team_memory"
	RecSrcFeedback    = "feedback"
)

// Why a suggestion was made, in one word.
const (
	BasisRequired   = "required"   // a restriction, a policy or a half-finished operation makes this the right next step
	BasisEvidence   = "evidence"   // current Git, task or run state points at it
	BasisPreference = "preference" // a remembered habit or earlier feedback chose it among the allowed ways
)

// RecEvidence is one fact a suggestion stands on. Ref is something a person can find again (a memory id, a task id, a
// branch, a pull request number); Label is a short sentence written by Werkbord, or a clipped quotation of saved text.
type RecEvidence struct {
	Tier     RecTier `json:"tier"`
	Source   string  `json:"source"`
	Kind     string  `json:"kind"`
	Ref      string  `json:"ref,omitempty"`
	Label    string  `json:"label"`
	Verified bool    `json:"verified"`
	// Note says what limits it: "freshness uncertain", "historical" and the like.
	Note string `json:"note,omitempty"`
}

// RecConstraint is a rule that shaped, or removed, a suggestion. Code is stable so that a screen can branch on it.
type RecConstraint struct {
	Tier    RecTier `json:"tier"`
	Source  string  `json:"source"`
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Ref     string  `json:"ref,omitempty"`
}

// RecAction is how a suggestion is taken up: by opening something. It never carries a command, a path or a credential,
// and it never starts anything by itself.
type RecAction struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Workflow string `json:"workflow,omitempty"`
	TaskID   string `json:"taskId,omitempty"`
	RunID    string `json:"runId,omitempty"`
	Branch   string `json:"branch,omitempty"`
	PR       int    `json:"pullRequest,omitempty"`
	// StartsExecution is false for every action there is. It is in the data so that nobody has to assume it.
	StartsExecution bool `json:"startsExecution"`
	// Confirmation says where the person decides: "at_destination" for a screen with its own confirmation, "none" for
	// one that only shows something.
	Confirmation string `json:"confirmation"`
}

// Recommendation is one suggestion with the reasons to believe it.
type Recommendation struct {
	// ID is a fingerprint of the intent, the variant and the evidence: the same suggestion on the same facts has the same
	// ID, and a changed fact makes it a different one, so dismissing it does not hide what comes later.
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Intent    string `json:"intent"`
	Variant   string `json:"variant"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	Basis     string `json:"basis"`
	// Confidence is high where the facts are verified and current, medium where a habit chose between allowed ways, low
	// where the support is thin.
	Confidence string        `json:"confidence"`
	Action     RecAction     `json:"action"`
	Evidence   []RecEvidence `json:"evidence"`
	// Applied are the restrictions, policies and decisions that shaped this suggestion, strongest first.
	Applied []RecConstraint `json:"applied"`
}

// RecWithheld is a suggestion that was considered and not made, and what stopped it.
type RecWithheld struct {
	Intent  string  `json:"intent"`
	Variant string  `json:"variant,omitempty"`
	Tier    RecTier `json:"tier"`
	Code    string  `json:"code"`
	Reason  string  `json:"reason"`
}

// What a source of evidence could say.
const (
	SourceStatusRead        = "read"          // read now
	SourceStatusEmpty       = "empty"         // read now, and there is nothing
	SourceStatusUnavailable = "unavailable"   // could not be read; this is a gap, not proof of nothing
	SourceStatusNotChecked  = "not_checked"   // not read in this place
	SourceStatusDenied      = "not_permitted" // the viewer may not read it
	SourceStatusNotLinked   = "not_linked"
)

// RecSources says what was looked at, so that "no suggestion" can be told apart from "could not look".
type RecSources struct {
	Individual   string `json:"individualMemory"`
	Team         string `json:"teamMemory"`
	Git          string `json:"git"`
	PullRequests string `json:"pullRequests"`
	Tasks        string `json:"tasks"`
	Health       string `json:"health"`
}

// RecommendationSet is the answer for one project: at most MaxRecommendations suggestions, and the account of the rest.
type RecommendationSet struct {
	ProjectID   string           `json:"projectId"`
	ProjectName string           `json:"projectName,omitempty"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Items       []Recommendation `json:"items"`
	// Standing are the restrictions in force for the project, whether or not they changed a suggestion: they are what a
	// person needs to know before they decide anything about integration.
	Standing []RecConstraint `json:"standing"`
	Withheld []RecWithheld   `json:"withheld"`
	Sources  RecSources      `json:"sources"`
	Notes    []string        `json:"notes"`
	// Boundary repeats what a suggestion is, for a reader that is a model.
	Boundary string `json:"boundary"`
}

// MaxRecommendations is how many suggestions are shown at once. Two is a choice, not a limit of the data.
const MaxRecommendations = 2

// RecommendationBoundary is the statement that travels with every set.
const RecommendationBoundary = "Suggestions are advice with evidence. They grant no permission, start nothing and approve nothing. Saved habits and feedback only choose between ways of working that policy, permissions and the repository already allow."

// ---- feedback ----

// The verdicts a person can give a suggestion.
const (
	FeedbackUseful    = "useful"
	FeedbackNotUseful = "not_useful"
	FeedbackDismissed = "dismissed"
)

// Bounds on what feedback keeps. They are small on purpose: feedback is a hint, never a dossier.
const (
	MaxFeedbackCount     = 1000
	MaxDismissedIDs      = 8
	DismissedFor         = 14 * 24 * time.Hour
	FeedbackMemoryType   = "recommendation_feedback"
	FeedbackSuppressNet  = 3 // this many more "not useful" than "useful" quiets optional suggestions of that kind
	feedbackScoreCeiling = 15
)

// RecFeedback is the feedback recorded for one (intent, variant) of a project.
type RecFeedback struct {
	Intent    string               `json:"intent"`
	Variant   string               `json:"variant"`
	Useful    int                  `json:"useful"`
	NotUseful int                  `json:"notUseful"`
	Dismissed int                  `json:"dismissed"`
	LastAt    time.Time            `json:"lastAt"`
	Hidden    []RecDismissedMarker `json:"hidden,omitempty"`
}

// RecDismissedMarker hides one specific suggestion, by ID, until Until.
type RecDismissedMarker struct {
	ID    string    `json:"id"`
	Until time.Time `json:"until"`
}

// Net is how much more useful than not useful the kind has been found.
func (f RecFeedback) Net() int { return f.Useful - f.NotUseful }

// Apply records a verdict. It is pure and bounded: counters stop at MaxFeedbackCount, at most MaxDismissedIDs markers
// are kept (the newest), and expired markers are dropped.
func (f RecFeedback) Apply(verdict, id string, now time.Time) RecFeedback {
	bump := func(n int) int {
		if n < MaxFeedbackCount {
			return n + 1
		}
		return n
	}
	switch verdict {
	case FeedbackUseful:
		f.Useful = bump(f.Useful)
	case FeedbackNotUseful:
		f.NotUseful = bump(f.NotUseful)
	case FeedbackDismissed:
		f.Dismissed = bump(f.Dismissed)
	}
	f.LastAt = now.UTC()
	kept := make([]RecDismissedMarker, 0, MaxDismissedIDs)
	for _, h := range f.Hidden {
		if h.Until.After(now) && h.ID != id {
			kept = append(kept, h)
		}
	}
	// "Not useful" hides the exact suggestion as well as a dismissal does: nobody wants to be shown it again unchanged.
	if (verdict == FeedbackDismissed || verdict == FeedbackNotUseful) && id != "" {
		kept = append(kept, RecDismissedMarker{ID: id, Until: now.Add(DismissedFor).UTC()})
	}
	if len(kept) > MaxDismissedIDs {
		kept = kept[len(kept)-MaxDismissedIDs:]
	}
	f.Hidden = kept
	return f
}

// HidesAt reports whether the suggestion with this ID is hidden at the time.
func (f RecFeedback) HidesAt(id string, now time.Time) bool {
	for _, h := range f.Hidden {
		if h.ID == id && h.Until.After(now) {
			return true
		}
	}
	return false
}

// ---- small helpers shared by the rules and the service ----

// RecommendationID fingerprints a suggestion from the parts that identify it. Parts are joined with a separator that cannot
// occur in them, so ("a","bc") and ("ab","c") differ.
func RecommendationID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return "rec_" + hex.EncodeToString(sum[:])[:20]
}

// ClipRecText makes saved or third-party text fit a label: control characters become spaces, runs of space collapse, and
// the result is cut to max runes at a rune boundary. Memory summaries, task titles and pull request titles were written
// by other people and programs; this is the one place they are shortened.
func ClipRecText(s string, max int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 {
		return ""
	}
	if r := []rune(s); len(r) > max {
		return strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return s
}

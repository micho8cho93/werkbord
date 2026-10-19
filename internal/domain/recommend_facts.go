package domain

import "time"

// RecFacts is everything the rules may look at, already gathered and already bounded. The rules never reach for more:
// they take these facts, and the same facts always give the same answer. The service builds them from the board, the
// repository and memory (internal/service/recommend.go); the tests build them by hand.
//
// A fact that could not be read is not absent: its source says Unavailable, and the rules treat that as a gap in the
// evidence, never as a reason to allow something.
type RecFacts struct {
	Now         time.Time
	ProjectID   string
	ProjectName string
	Viewer      RecViewer
	Project     RecProject
	Repo        RecRepo
	Tasks       RecTasks
	// Workflows is what is known of the deck workflows already run on the project, by workflow id.
	Workflows map[string]RecWorkflow
	Memory    RecMemory
	// Focus limits the answer to one intent when the person asked about one thing ("what should I scan?"). It narrows the
	// choice among suggestions; it does not relax a restriction or lift a limit.
	Focus string
}

// RecViewer is who asks, and what they may read. Whatever they may not read is left out of the facts, and the answer
// says so.
type RecViewer struct {
	// Kind is "owner", "phone" or "assistant".
	Kind   string
	Tasks  bool
	Runs   bool
	Repo   bool
	Memory bool
	// RunnerReady is whether some runner is online that could take a workflow. It is a fact about the application.
	RunnerReady bool
}

// RecProject is what the project is.
type RecProject struct {
	// HasRepository is false for a work project, which has no Git repository.
	HasRepository bool
	// TeamLinked: tasks of this project were imported from a Team workspace, so Team's rules may apply to its repository.
	TeamLinked bool
}

// RecRepo is the repository as it was read just now.
type RecRepo struct {
	Status  string // one of the SourceStatus constants
	Problem string // in plain words, when Status is unavailable

	Head        string
	Detached    bool
	Unborn      bool
	Target      string
	TargetLocal bool
	// Dirty is how many files are changed in the project's own checkout.
	Dirty      int
	Conflicted int
	// Operation is a merge, rebase or the like left unfinished in the checkout.
	Operation string
	HasRemote bool

	Branches []RecBranch
	// TargetCommits are the latest commits on the target branch, newest first, at most 50.
	TargetCommits []RecCommit

	PullRequests RecPullRequests
	Health       RecHealth
}

// RecBranch is a local branch that matters to a suggestion.
type RecBranch struct {
	Name        string
	Owned       bool // created by Werkbord for a task
	Target      bool
	Head        bool
	Ahead       int // commits the target lacks
	Behind      int // commits the branch lacks
	Merged      bool
	NotPushed   int
	HasUpstream bool
	Dirty       int
	ActiveRun   bool
	TaskID      string
	TaskTitle   string
	Phase       TaskPhase
	LastCommit  time.Time
}

// RecCommit is a commit on the target, reduced to what tells how work reached it.
type RecCommit struct {
	Date    time.Time
	Merge   bool
	Subject string
}

// RecPullRequests is what GitHub says, asked for separately and allowed to fail.
type RecPullRequests struct {
	Status string // one of the SourceStatus constants
	Reason string // why unavailable, in words
	// Detected: the remote looks like GitHub. Available: and it could be asked just now.
	Detected  bool
	Available bool
	Items     []RecPullRequest
}

// RecPullRequest is a pull request, open or recently closed.
type RecPullRequest struct {
	Number    int
	Title     string
	Branch    string
	Base      string
	State     string // open, closed, merged
	Draft     bool
	Review    string // approved, changes_requested, review_required, or empty
	Checks    string // passing, failing, pending, none
	Mergeable string
	TaskID    string
	UpdatedAt time.Time
}

// RecHealth is the repository's stored health findings.
type RecHealth struct {
	Status    string
	Attention int
	Risk      int
	Top       []RecFinding
}

// RecFinding is one open health finding.
type RecFinding struct {
	ID       string
	Type     string
	Title    string
	Severity string // action, risk, warn, info, as the health rules name them
	Subject  string
}

// RecTasks is the board, as far as the viewer may read it.
type RecTasks struct {
	Status string
	Items  []RecTask
}

// RecTask is one unarchived task.
type RecTask struct {
	ID    string
	Title string
	State TaskState
	// Ready: it is in Backlog, nothing it depends on is unfinished, and nothing is running on it.
	Ready bool
	// WaitingOn counts unfinished dependencies.
	WaitingOn int
	// Run says what the latest run did; empty when it never ran.
	RunState RunState
	// Waiting is what an active run waits for: a question or the next message.
	Waiting WaitingKind
	Active  bool
	Team    bool // imported from a Team workspace
	// PlannedStart is the planned start day, if any (YYYY-MM-DD), for ordering.
	PlannedStart string
	Position     float64
	UpdatedAt    time.Time
	EndedAt      *time.Time
}

// RecWorkflow is the history of one deck workflow on this project.
type RecWorkflow struct {
	LastAt     time.Time // when the latest invocation was created; zero if never
	Active     bool      // an invocation is still running or waiting
	LastFailed bool      // the latest invocation ended without finishing
	TaskID     string
}

// RecMemoryItem is one saved statement the rules may consult. The service has already applied every access, freshness and
// verification check that memory retrieval applies; the rules apply none of their own, and trust none of the text.
type RecMemoryItem struct {
	ID           string
	Kind         string // policy, decision, preference, verified_fact, suggestion
	Type         string // the record's own type: "policy" for a Team policy even when its freshness is uncertain
	Summary      string
	Source       string // RecSrcIndividual or RecSrcTeam
	Verification string
	Freshness    string
	Reference    string
}

// RecMemory is what was saved, and how completely it could be read.
type RecMemory struct {
	// Individual and Team are SourceStatus values. Team is "not_linked" for a project with no Team link and "not_checked"
	// where this place has no way to read Team's memory, which is the case for everything but the desktop assistant.
	Individual string
	Team       string
	Items      []RecMemoryItem
	Feedback   []RecFeedback
}

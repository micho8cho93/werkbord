package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// More ID prefixes.
const (
	PrefixTicket = "ttk"
	PrefixInvite = "tiv"
)

// TicketStatus is where a ticket is on the project board.
type TicketStatus string

const (
	TicketBacklog    TicketStatus = "backlog"     // an idea; not ready to be picked up
	TicketAvailable  TicketStatus = "available"   // ready: any member of the project may claim it
	TicketInProgress TicketStatus = "in_progress" // one member holds it and is working on it
	TicketReview     TicketStatus = "review"      // the work is submitted and waits for a reviewer
	TicketDone       TicketStatus = "done"
)

// TicketStatuses lists the board's columns in order.
func TicketStatuses() []TicketStatus {
	return []TicketStatus{TicketBacklog, TicketAvailable, TicketInProgress, TicketReview, TicketDone}
}

// Valid reports whether s is a board column.
func (s TicketStatus) Valid() bool {
	for _, v := range TicketStatuses() {
		if s == v {
			return true
		}
	}
	return false
}

// Label is the column's name for people.
func (s TicketStatus) Label() string {
	switch s {
	case TicketBacklog:
		return "Backlog"
	case TicketAvailable:
		return "Available"
	case TicketInProgress:
		return "In Progress"
	case TicketReview:
		return "Review"
	case TicketDone:
		return "Done"
	}
	return string(s)
}

// ParseTicketStatus validates a status received from outside.
func ParseTicketStatus(s string) (TicketStatus, error) {
	st := TicketStatus(s)
	if !st.Valid() {
		return "", invalid("unknown ticket status %q (statuses: backlog, available, in_progress, review, done)", s)
	}
	return st, nil
}

// ticketTransitions is every move a ticket may make, and the only place it is
// written down. How each move happens (claim, release, submit, ...) is the
// service's; whether it may happen at all is decided here.
var ticketTransitions = map[TicketStatus][]TicketStatus{
	TicketBacklog:    {TicketAvailable, TicketInProgress},             // promote it; or the project owner assigns it straight away
	TicketAvailable:  {TicketBacklog, TicketInProgress},               // put it back; claim or assign it
	TicketInProgress: {TicketAvailable, TicketReview},                 // release it; submit the work
	TicketReview:     {TicketInProgress, TicketAvailable, TicketDone}, // send it back (to the board, if its owner left); complete it
	TicketDone:       {TicketAvailable},                               // reopen it
}

// CanTransitionTo reports whether a ticket may move from s to next.
func (s TicketStatus) CanTransitionTo(next TicketStatus) bool {
	for _, to := range ticketTransitions[s] {
		if to == next {
			return true
		}
	}
	return false
}

// Held reports whether a ticket in this status has an owner who is working on it.
func (s TicketStatus) Held() bool { return s == TicketInProgress || s == TicketReview }

// PullRequest is what the team knows about a ticket's pull request. Team never
// asks GitHub: the developer's own Werkbord, signed in to GitHub as them, reports it.
type PullRequest struct {
	Fields     map[string]bool `json:"-"` // optional patch presence; never persisted
	Number     int             `json:"number,omitempty"`
	URL        string          `json:"url"`
	State      PRState         `json:"state"`
	Draft      bool            `json:"draft,omitempty"`
	Mergeable  Mergeable       `json:"mergeable"`
	BaseBranch string          `json:"baseBranch,omitempty"`
	// Behind is how many commits the branch is behind the base branch; -1 when unknown.
	Behind     int       `json:"behind"`
	Ahead      int       `json:"ahead"`
	ReportedBy string    `json:"reportedBy,omitempty"`
	ReportedAt time.Time `json:"reportedAt"`
	CreatedAt  time.Time `json:"createdAt"`
}

// PRState is a pull request's state.
type PRState string

const (
	PROpen   PRState = "open"
	PRMerged PRState = "merged"
	PRClosed PRState = "closed"
)

// Mergeable is whether GitHub (or Git) says a pull request can merge cleanly.
type Mergeable string

const (
	MergeUnknown     Mergeable = "unknown"
	MergeClean       Mergeable = "mergeable"
	MergeConflicting Mergeable = "conflicting"
)

// Commit is a commit a developer reports as part of a ticket's work: metadata only.
type Commit struct {
	SHA         string    `json:"sha"`
	Subject     string    `json:"subject"`
	Author      string    `json:"author,omitempty"`
	CommittedAt time.Time `json:"committedAt"`
}

// Ticket is a unit of work on a project's board.
//
// Version is incremented on every write. The claim is a single guarded UPDATE, so
// two members cannot both get a ticket; Version also lets a client notice that the
// ticket changed under it.
type Ticket struct {
	Assignment   int64        `json:"assignment"` // monotonic ownership interval, independent of text/report version
	ArchivedAt   *time.Time   `json:"archivedAt,omitempty"`
	ID           string       `json:"id"`
	ProjectID    string       `json:"projectId"`
	Number       int          `json:"number"`
	Key          string       `json:"key"` // "WB-142"
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Requirements string       `json:"requirements"`
	Status       TicketStatus `json:"status"`
	AssigneeID   string       `json:"assigneeId,omitempty"` // the active owner, while the ticket is held
	CreatorID    string       `json:"creatorId"`
	ReviewerID   string       `json:"reviewerId,omitempty"` // who was asked to review it, if anyone in particular
	Branch       string       `json:"branch,omitempty"`
	Commits      []Commit     `json:"commits"`
	PullRequest  *PullRequest `json:"pullRequest,omitempty"`
	Version      int64        `json:"version"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	ClaimedAt    *time.Time   `json:"claimedAt,omitempty"`
	SubmittedAt  *time.Time   `json:"submittedAt,omitempty"`
	CompletedAt  *time.Time   `json:"completedAt,omitempty"`
}

// TicketKey renders a ticket number the way people say it.
func TicketKey(number int) string { return fmt.Sprintf("WB-%d", number) }

// ---- branch names ----

const maxBranchSlug = 40

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// BranchName derives a ticket's branch name from its number and title, the same
// way every time: wb-142-authentication-error. Only ASCII letters and digits
// survive, so it is valid in every Git host; a title with none of them (for
// example one in another script) yields wb-142-ticket.
func BranchName(number int, title string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(asciiFold(title)), "-"), "-")
	if len(slug) > maxBranchSlug {
		slug = slug[:maxBranchSlug]
		if i := strings.LastIndexByte(slug, '-'); i > 8 { // cut at a word boundary unless that loses too much
			slug = slug[:i]
		}
		slug = strings.Trim(slug, "-")
	}
	if slug == "" {
		slug = "ticket"
	}
	return fmt.Sprintf("wb-%d-%s", number, slug)
}

// asciiFold maps the common accented Latin letters to their base letter, so
// "Café" becomes "cafe" rather than "caf".
func asciiFold(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case strings.ContainsRune("àáâãäåāăą", r):
			b.WriteByte('a')
		case strings.ContainsRune("çćĉċč", r):
			b.WriteByte('c')
		case strings.ContainsRune("èéêëēĕėęě", r):
			b.WriteByte('e')
		case strings.ContainsRune("ìíîïĩīĭįı", r):
			b.WriteByte('i')
		case strings.ContainsRune("ñńņň", r):
			b.WriteByte('n')
		case strings.ContainsRune("òóôõöøōŏő", r):
			b.WriteByte('o')
		case strings.ContainsRune("ùúûüũūŭůűų", r):
			b.WriteByte('u')
		case strings.ContainsRune("ýÿ", r):
			b.WriteByte('y')
		case r == 'ß':
			b.WriteString("ss")
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// TicketNumberFromBranch finds the ticket a branch belongs to from its name
// (wb-142-anything, optionally under a prefix such as feature/), or 0.
func TicketNumberFromBranch(name string) int {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	m := branchTicket.FindStringSubmatch(strings.ToLower(name))
	if m == nil {
		return 0
	}
	n := 0
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0
		}
	}
	return n
}

var branchTicket = regexp.MustCompile(`^wb-([0-9]{1,9})(?:-|$)`)

// ---- validation ----

// Limits on ticket text and reported Git metadata.
const (
	MaxTicketTitleLen = 200
	MaxTicketTextLen  = 20000
	MaxBranchLen      = 200
	MaxCommitSubject  = 200
	MaxCommitsPerTick = 200
	MaxBranchFiles    = 500
	MaxFilePathLen    = 500
	MaxPRURLLen       = 2048
)

// CleanTicketTitle trims a one-line title.
func CleanTicketTitle(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", invalid("ticket title is required")
	case utf8.RuneCountInString(s) > MaxTicketTitleLen:
		return "", invalid("ticket title is longer than %d characters", MaxTicketTitleLen)
	case hasControl(s) || strings.ContainsAny(s, "\n\t"):
		return "", invalid("ticket title contains a control character")
	}
	return s, nil
}

// CleanTicketText trims a long free-text field (description, requirements).
func CleanTicketText(what, s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxTicketTextLen {
		return "", invalid("%s is longer than %d characters", what, MaxTicketTextLen)
	}
	if hasControl(s) {
		return "", invalid("%s contains a control character", what)
	}
	return s, nil
}

// CleanBranch checks a branch name by the rules of git-check-ref-format (the ones
// that matter here), so a reported name can be shown, compared and handed to a
// developer's own Git without surprising it.
func CleanBranch(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", invalid("branch name is required")
	case len(s) > MaxBranchLen:
		return "", invalid("branch name is longer than %d characters", MaxBranchLen)
	case strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.HasSuffix(s, ".lock"):
		return "", invalid("%q is not a valid branch name", s)
	case strings.Contains(s, "..") || strings.Contains(s, "//") || strings.Contains(s, "@{") || s == "@":
		return "", invalid("%q is not a valid branch name", s)
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return "", invalid("%q is not a valid branch name", s)
		}
	}
	for _, part := range strings.Split(s, "/") {
		if strings.HasPrefix(part, ".") {
			return "", invalid("%q is not a valid branch name", s)
		}
	}
	return s, nil
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// CleanSHA checks an abbreviated or full commit hash and returns it lower-cased.
func CleanSHA(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !shaRE.MatchString(s) {
		return "", invalid("%q is not a commit hash", s)
	}
	return s, nil
}

// CleanCommit checks a reported commit.
func CleanCommit(c Commit) (Commit, error) {
	sha, err := CleanSHA(c.SHA)
	if err != nil {
		return Commit{}, err
	}
	subject := strings.TrimSpace(strings.SplitN(strings.TrimSpace(c.Subject), "\n", 2)[0])
	if utf8.RuneCountInString(subject) > MaxCommitSubject {
		subject = string([]rune(subject)[:MaxCommitSubject])
	}
	if hasControl(subject) {
		return Commit{}, invalid("commit subject contains a control character")
	}
	author := strings.TrimSpace(c.Author)
	if utf8.RuneCountInString(author) > MaxNameLen || hasControl(author) || strings.ContainsAny(author, "\n\t") {
		return Commit{}, invalid("commit author is not usable")
	}
	return Commit{SHA: sha, Subject: subject, Author: author, CommittedAt: c.CommittedAt.UTC().Truncate(time.Millisecond)}, nil
}

// CleanPullRequestURL checks a pull request's address: https, with no credentials,
// query or fragment, because it is shown to the whole project.
func CleanPullRequestURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > MaxPRURLLen || hasControl(s) || strings.ContainsAny(s, " \t\r\n") {
		return "", invalid("pull request address is not usable")
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", invalid("pull request address must be an https address")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", invalid("pull request address must not contain credentials, a query or a fragment")
	}
	return s, nil
}

// CleanPullRequest validates a reported pull request, filling unknowns with safe defaults.
func CleanPullRequest(p PullRequest) (PullRequest, error) {
	u, err := CleanPullRequestURL(p.URL)
	if err != nil {
		return PullRequest{}, err
	}
	p.URL = u
	if p.Number < 0 {
		return PullRequest{}, invalid("pull request number is negative")
	}
	switch p.State {
	case "":
		p.State = PROpen
	case PROpen, PRMerged, PRClosed:
	default:
		return PullRequest{}, invalid("pull request state %q is not open, merged or closed", p.State)
	}
	switch p.Mergeable {
	case "":
		p.Mergeable = MergeUnknown
	case MergeUnknown, MergeClean, MergeConflicting:
	default:
		return PullRequest{}, invalid("pull request mergeable %q is not unknown, mergeable or conflicting", p.Mergeable)
	}
	if p.BaseBranch != "" {
		if p.BaseBranch, err = CleanBranch(p.BaseBranch); err != nil {
			return PullRequest{}, err
		}
	}
	if p.Behind < -1 || p.Ahead < 0 || p.Behind > 1_000_000 || p.Ahead > 1_000_000 {
		return PullRequest{}, invalid("pull request ahead/behind counts are out of range")
	}
	return p, nil
}

// CleanFiles checks a list of changed file paths: relative, no traversal.
func CleanFiles(files []string) ([]string, error) {
	if len(files) > MaxBranchFiles {
		return nil, invalid("more than %d changed files reported for one branch", MaxBranchFiles)
	}
	out := make([]string, 0, len(files))
	seen := map[string]bool{}
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" || len(f) > MaxFilePathLen || hasControl(f) || strings.HasPrefix(f, "/") || strings.Contains(f, "\\") {
			return nil, invalid("%q is not a repository-relative path", f)
		}
		for _, part := range strings.Split(f, "/") {
			if part == ".." {
				return nil, invalid("%q is not a repository-relative path", f)
			}
		}
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out, nil
}

// ---- repository state ----

// ProjectBranch is a branch a developer reports having. Team never lists a
// repository itself: each member's Werkbord reports what it sees.
type ProjectBranch struct {
	ProjectID    string    `json:"projectId"`
	Name         string    `json:"name"`
	HeadSHA      string    `json:"headSha,omitempty"`
	BaseBranch   string    `json:"baseBranch,omitempty"`
	Ahead        int       `json:"ahead"`
	Behind       int       `json:"behind"` // -1: not known
	LastCommitAt time.Time `json:"lastCommitAt"`
	Files        []string  `json:"files,omitempty"` // paths changed on the branch relative to its base
	ReportedBy   string    `json:"reportedBy"`
	ReportedAt   time.Time `json:"reportedAt"`
}

// ---- activity ----

// ActivityKind names a coordination event.
type ActivityKind string

const (
	ActTicketCreated     ActivityKind = "ticket.created"
	ActTicketArchived    ActivityKind = "ticket.archived"
	ActTicketRestored    ActivityKind = "ticket.restored"
	ActTicketClaimed     ActivityKind = "ticket.claimed"
	ActTicketReleased    ActivityKind = "ticket.released"
	ActTicketReassigned  ActivityKind = "ticket.reassigned"
	ActWorkSubmitted     ActivityKind = "ticket.work_submitted"
	ActPullRequestMade   ActivityKind = "ticket.pull_request_created"
	ActReviewRequested   ActivityKind = "ticket.review_requested"
	ActChangesRequested  ActivityKind = "ticket.changes_requested"
	ActTicketCompleted   ActivityKind = "ticket.completed"
	ActTicketReopened    ActivityKind = "ticket.reopened"
	ActTicketMoved       ActivityKind = "ticket.moved"
	ActMemberJoined      ActivityKind = "project.member_joined"
	ActHandedOff         ActivityKind = "ticket.handed_off"
	ActPullRequestMerged ActivityKind = "ticket.pull_request_merged"
)

// Activity is one entry in a project's history: who did what to which ticket.
// It carries coordination metadata only.
type Activity struct {
	ID        int64        `json:"id"`
	ProjectID string       `json:"projectId"`
	TicketID  string       `json:"ticketId,omitempty"`
	TicketKey string       `json:"ticketKey,omitempty"`
	ActorID   string       `json:"actorId"`
	Kind      ActivityKind `json:"kind"`
	// Detail is a short human sentence fragment, such as "to Grace" or the PR address.
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ---- invites ----

// InviteCodePrefix marks a project invite code.
const InviteCodePrefix = "wbi_"

// Invite lets someone join one project, once or a few times, until it expires.
// The code is shown when the invite is created and never again: only its hash is
// kept.
type Invite struct {
	ID        string      `json:"id"`
	ProjectID string      `json:"projectId"`
	Role      ProjectRole `json:"role"` // what a person who joins with it becomes on the project
	CreatedBy string      `json:"createdBy"`
	CreatedAt time.Time   `json:"createdAt"`
	ExpiresAt time.Time   `json:"expiresAt"`
	MaxUses   int         `json:"maxUses"`
	Uses      int         `json:"uses"`
	RevokedAt *time.Time  `json:"revokedAt,omitempty"`
}

// Active reports whether the invite can still be used at now.
func (i Invite) Active(now time.Time) bool {
	return i.RevokedAt == nil && now.Before(i.ExpiresAt) && i.Uses < i.MaxUses
}

// NewInviteCode returns a new random invite code (128 bits) and the hash to store.
func NewInviteCode() (code, hash string) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("team: crypto/rand failed: " + err.Error())
	}
	code = InviteCodePrefix + hex.EncodeToString(b[:])
	return code, HashInviteCode(code)
}

// HashInviteCode is what is stored in place of an invite code.
func HashInviteCode(code string) string {
	h := sha256.Sum256([]byte("invite:" + strings.TrimSpace(code)))
	return hex.EncodeToString(h[:])
}

// Invite limits.
const (
	DefaultInviteTTL  = 7 * 24 * time.Hour
	MaxInviteTTL      = 30 * 24 * time.Hour
	DefaultInviteUses = 1
	MaxInviteUses     = 100
)

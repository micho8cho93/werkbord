// Package github is the GitHub boundary. It never talks to GitHub itself: it
// runs the user's own GitHub CLI (gh), so it uses whatever account the user has
// already signed in with, and Dev Board holds no GitHub credential, token or
// account of its own. Nothing here is required: with no gh, or no network, or
// no sign-in, local Git carries on and the pull request section says why it is
// empty.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"devboard/internal/domain"
)

// Repo names a GitHub repository.
type Repo struct {
	Host  string // github.com, or an Enterprise host
	Owner string
	Name  string
}

// String is owner/name.
func (r Repo) String() string { return r.Owner + "/" + r.Name }

// Selector is what gh's -R flag takes: owner/name for github.com, otherwise host/owner/name.
func (r Repo) Selector() string {
	if r.Host == "" || r.Host == "github.com" {
		return r.String()
	}
	return r.Host + "/" + r.String()
}

// Error is a failure to use GitHub, with a stable reason the UI words itself.
type Error struct {
	Reason  string // domain.GHMissing, GHUnauthenticated or GHError
	Message string
}

func (e *Error) Error() string { return e.Message }

// Client is what the Git Control Center needs from GitHub.
type Client interface {
	// HostKnown reports whether gh is signed in to host: how an Enterprise host,
	// which cannot be told from any other server by its name, is recognised.
	HostKnown(ctx context.Context, host string) bool
	// PullRequests lists up to limit pull requests of every state, most recently updated first.
	PullRequests(ctx context.Context, repo Repo, limit int) ([]domain.GitHubPR, error)
	// CreatePullRequest opens a pull request from an existing remote branch and
	// returns it as GitHub then reports it.
	CreatePullRequest(ctx context.Context, repo Repo, req CreateRequest) (domain.GitHubPR, error)
}

// CreateRequest describes a pull request to open.
type CreateRequest struct {
	Head  string
	Base  string
	Title string
	Body  string
	Draft bool
}

// CLI implements Client by running gh.
type CLI struct {
	Binary  string        // defaults to "gh" on PATH
	Timeout time.Duration // per command; defaults to 30s
}

var _ Client = (*CLI)(nil)

// prFields are what is asked of GitHub for each pull request.
const prFields = "number,title,url,state,isDraft,headRefName,baseRefName,headRefOid,author,reviewDecision,mergeable,statusCheckRollup,createdAt,updatedAt,mergedAt,closedAt,isCrossRepository"

// ghEnv is the environment gh runs in: the user's own, so their sign-in is
// used, with prompts, colour, pagers and update checks off, and GH_REPO removed
// (the repository is always named explicitly).
func ghEnv(base []string) []string {
	env := make([]string, 0, len(base)+6)
	for _, kv := range base {
		if !strings.HasPrefix(kv, "GH_REPO=") && !domain.IsControllerSecret(kv) {
			env = append(env, kv)
		}
	}
	return append(env, "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "CLICOLOR=0", "GH_NO_UPDATE_NOTIFIER=1", "GH_SPINNER_DISABLED=1", "GH_PAGER=cat", "GH_FORCE_TTY=0")
}

func (c *CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := c.Binary
	if bin == "" {
		bin = "gh"
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = ghEnv(os.Environ())
	cmd.WaitDelay = time.Second
	detach(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limitWriter{w: &stdout, max: 8 << 20}, &limitWriter{w: &stderr, max: 64 << 10}
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	switch {
	case errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist):
		return nil, &Error{Reason: domain.GHMissing, Message: "the GitHub CLI (gh) is not installed"}
	case ctx.Err() != nil:
		return nil, &Error{Reason: domain.GHError, Message: "GitHub did not answer in time"}
	}
	return nil, classify(stderr.String(), err)
}

// limitWriter drops what is past max rather than failing the command.
type limitWriter struct {
	w   *bytes.Buffer
	max int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if room := l.max - l.w.Len(); room > 0 {
		l.w.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

var credentialsInURL = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]*@`)

// classify turns gh's error output into a reason.
func classify(stderr string, err error) *Error {
	msg := strings.TrimSpace(credentialsInURL.ReplaceAllString(stderr, "${1}"))
	if msg == "" {
		msg = err.Error()
	}
	l := strings.ToLower(msg)
	for _, s := range []string{"gh auth login", "not logged in", "authentication required", "http 401", "bad credentials", "requires authentication", "no oauth token", "token has expired"} {
		if strings.Contains(l, s) {
			return &Error{Reason: domain.GHUnauthenticated, Message: "the GitHub CLI is not signed in (run `gh auth login`)"}
		}
	}
	for _, s := range []string{"error connecting to", "could not resolve host", "dial tcp", "timeout", "network is unreachable", "no such host"} {
		if strings.Contains(l, s) {
			return &Error{Reason: domain.GHError, Message: "GitHub could not be reached"}
		}
	}
	return &Error{Reason: domain.GHError, Message: clip(msg, 600)}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// HostKnown implements Client.
func (c *CLI) HostKnown(ctx context.Context, host string) bool {
	if host == "" {
		return false
	}
	_, err := c.run(ctx, "auth", "status", "--hostname", host)
	return err == nil
}

// PullRequests implements Client.
func (c *CLI) PullRequests(ctx context.Context, repo Repo, limit int) ([]domain.GitHubPR, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	out, err := c.run(ctx, "pr", "list", "-R", repo.Selector(), "--state", "all", "--limit", fmt.Sprint(limit), "--json", prFields)
	if err != nil {
		return nil, err
	}
	var raw []rawPR
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
	}
	prs := make([]domain.GitHubPR, 0, len(raw))
	for _, r := range raw {
		prs = append(prs, r.convert())
	}
	return prs, nil
}

// CreatePullRequest implements Client.
func (c *CLI) CreatePullRequest(ctx context.Context, repo Repo, req CreateRequest) (domain.GitHubPR, error) {
	args := []string{"pr", "create", "-R", repo.Selector(), "--head", req.Head, "--base", req.Base, "--title", req.Title, "--body", req.Body}
	if req.Draft {
		args = append(args, "--draft")
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return domain.GitHubPR{}, err
	}
	// gh prints the new pull request's URL. Success is not taken from the exit
	// status alone: the pull request is read back from GitHub.
	created := lastLine(string(out))
	number := prNumber(created)
	if number == "" {
		return domain.GitHubPR{}, &Error{Reason: domain.GHError, Message: "gh did not say which pull request it opened: " + clip(strings.TrimSpace(string(out)), 300)}
	}
	view, err := c.run(ctx, "pr", "view", number, "-R", repo.Selector(), "--json", prFields)
	if err != nil {
		return domain.GitHubPR{}, err
	}
	var raw rawPR
	if err := json.Unmarshal(view, &raw); err != nil {
		return domain.GitHubPR{}, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
	}
	return raw.convert(), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

var prURL = regexp.MustCompile(`/pull/(\d+)/?$`)

func prNumber(u string) string {
	if m := prURL.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}

// ---- JSON ----

type rawPR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	State       string `json:"state"`
	IsDraft     bool   `json:"isDraft"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	HeadRefOid  string `json:"headRefOid"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	ReviewDecision    string     `json:"reviewDecision"`
	Mergeable         string     `json:"mergeable"`
	StatusCheckRollup []rawCheck `json:"statusCheckRollup"`
	CreatedAt         string     `json:"createdAt"`
	UpdatedAt         string     `json:"updatedAt"`
	MergedAt          string     `json:"mergedAt"`
	ClosedAt          string     `json:"closedAt"`
	IsCrossRepository bool       `json:"isCrossRepository"`
}

type rawCheck struct {
	Type       string `json:"__typename"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

func (r rawPR) convert() domain.GitHubPR {
	pr := domain.GitHubPR{
		Number: r.Number, Title: r.Title, URL: r.URL, Draft: r.IsDraft,
		HeadBranch: r.HeadRefName, BaseBranch: r.BaseRefName, HeadSha: r.HeadRefOid,
		Author: r.Author.Login, CrossRepo: r.IsCrossRepository,
		CreatedAt: parseTime(r.CreatedAt), UpdatedAt: parseTime(r.UpdatedAt),
		MergedAt: parseTime(r.MergedAt), ClosedAt: parseTime(r.ClosedAt),
		Checks: summarizeChecks(r.StatusCheckRollup),
	}
	switch strings.ToUpper(r.State) {
	case "OPEN":
		pr.State = "open"
	case "MERGED":
		pr.State = "merged"
	default:
		pr.State = "closed"
	}
	switch strings.ToUpper(r.ReviewDecision) {
	case "APPROVED":
		pr.Review = "approved"
	case "CHANGES_REQUESTED":
		pr.Review = "changes_requested"
	case "REVIEW_REQUIRED":
		pr.Review = "review_required"
	}
	// Mergeability is only meaningful for an open pull request, and GitHub often
	// has not computed it yet: UNKNOWN stays empty and is never shown as a yes.
	if pr.State == "open" {
		switch strings.ToUpper(r.Mergeable) {
		case "MERGEABLE":
			pr.Mergeable = "mergeable"
		case "CONFLICTING":
			pr.Mergeable = "conflicting"
		}
	}
	return pr
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil || t.Year() < 2000 {
		return nil
	}
	t = t.UTC()
	return &t
}

// summarizeChecks counts a pull request's checks. A check run is pending until
// it completes; a status context carries its state directly.
func summarizeChecks(checks []rawCheck) domain.GitHubChecks {
	var c domain.GitHubChecks
	for _, k := range checks {
		c.Total++
		switch {
		case k.Type == "StatusContext" || (k.Status == "" && k.State != ""):
			switch strings.ToUpper(k.State) {
			case "SUCCESS":
				c.Passed++
			case "PENDING", "EXPECTED":
				c.Pending++
			default:
				c.Failed++
			}
		case !strings.EqualFold(k.Status, "COMPLETED"):
			c.Pending++
		default:
			switch strings.ToUpper(k.Conclusion) {
			case "SUCCESS", "NEUTRAL", "SKIPPED":
				c.Passed++
			default:
				c.Failed++
			}
		}
	}
	switch {
	case c.Failed > 0:
		c.State = "failing"
	case c.Pending > 0:
		c.State = "pending"
	case c.Total > 0:
		c.State = "passing"
	default:
		c.State = "none"
	}
	return c
}

// ---- remotes ----

// ParseRemote reads a GitHub repository out of a remote URL: https, ssh://, git://
// and scp-style (git@host:owner/name) forms, with or without .git. It says nothing
// about whether the host is GitHub.
func ParseRemote(raw string) (Repo, bool) {
	raw = strings.TrimSpace(raw)
	var host, path string
	switch {
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return Repo{}, false
		}
		host, path = u.Hostname(), strings.TrimPrefix(u.Path, "/")
	default:
		// scp-like: [user@]host:owner/name
		at := strings.LastIndex(raw, "@")
		rest := raw[at+1:]
		h, p, ok := strings.Cut(rest, ":")
		if !ok || h == "" || strings.Contains(h, "/") {
			return Repo{}, false
		}
		host, path = h, p
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Repo{}, false
	}
	host = strings.ToLower(host)
	if host == "ssh.github.com" || host == "www.github.com" {
		host = "github.com"
	}
	return Repo{Host: host, Owner: parts[0], Name: parts[1]}, true
}

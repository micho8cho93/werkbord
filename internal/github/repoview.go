package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"werkbord/internal/domain"
)

// RepoView is a repository as GitHub reports it to the person who is signed in: what they may do, its branches, its latest
// commits and its pull requests. It needs no checkout on this computer. It is read-only.
type RepoView struct {
	FullName      string            `json:"fullName"`
	Description   string            `json:"description,omitempty"`
	URL           string            `json:"url"`
	Private       bool              `json:"private"`
	DefaultBranch string            `json:"defaultBranch"`
	PushedAt      *time.Time        `json:"pushedAt,omitempty"`
	Permissions   RepoPermissions   `json:"permissions"`
	Branches      []RepoBranch      `json:"branches"`
	Commits       []RepoCommit      `json:"commits"`
	PullRequests  []domain.GitHubPR `json:"pullRequests"`
}

// RepoPermissions are what GitHub says the signed-in person may do in the repository. They are a hint for what to offer;
// GitHub decides when an action is taken.
type RepoPermissions struct {
	Admin    bool `json:"admin"`
	Maintain bool `json:"maintain"`
	Push     bool `json:"push"`
	Triage   bool `json:"triage"`
	Pull     bool `json:"pull"`
}

// Role is the highest of the permissions, in GitHub's words.
func (p RepoPermissions) Role() string {
	switch {
	case p.Admin:
		return "admin"
	case p.Maintain:
		return "maintain"
	case p.Push:
		return "write"
	case p.Triage:
		return "triage"
	case p.Pull:
		return "read"
	}
	return "none"
}

// RepoBranch is a branch and the commit it is at.
type RepoBranch struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"`
	Protected bool   `json:"protected"`
}

// RepoCommit is a commit on the default branch.
type RepoCommit struct {
	SHA     string     `json:"sha"`
	Message string     `json:"message"`
	Author  string     `json:"author"`
	At      *time.Time `json:"at,omitempty"`
	URL     string     `json:"url,omitempty"`
}

// NotFound reports whether GitHub said the repository does not exist for this person: it is private and they have no access,
// or it is not there. GitHub does not tell the two apart, and neither does this.
func NotFound(err error) bool {
	e, ok := err.(*Error)
	if !ok || e.Reason != domain.GHError {
		return false
	}
	l := strings.ToLower(e.Message)
	return strings.Contains(l, "http 404") || strings.Contains(l, "not found") || strings.Contains(l, "http 403")
}

// Heads is where each branch of the repository is now: one call, cheap enough to ask every minute. A change in it is how a push is noticed.
// When etag is the one GitHub gave for the last answer, GitHub answers "not modified" and that costs nothing against the rate limit:
// unchanged is then true and heads is nil. The returned etag is the one to ask with next time.
func (c *CLI) Heads(ctx context.Context, repo Repo, etag string) (heads []RepoBranch, newTag string, unchanged bool, err error) {
	args := []string{"api", "-i", "repos/" + repo.Owner + "/" + repo.Name + "/branches?per_page=100"}
	if repo.Host != "" && repo.Host != "github.com" {
		args = append(args, "--hostname", repo.Host)
	}
	if etag != "" && validETag(etag) {
		args = append(args, "-H", "If-None-Match: "+etag)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		if e, ok := err.(*Error); ok && etag != "" && strings.Contains(e.Message, "304") {
			return nil, etag, true, nil
		}
		return nil, "", false, err
	}
	head, body := splitResponse(string(out))
	if strings.Contains(strings.SplitN(head, "\n", 2)[0], " 304") {
		return nil, etag, true, nil
	}
	for _, line := range strings.Split(head, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "etag") && validETag(strings.TrimSpace(v)) {
			newTag = strings.TrimSpace(v)
		}
	}
	var raw []struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
		Commit    struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, "", false, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
	}
	heads = make([]RepoBranch, 0, len(raw))
	for _, b := range raw {
		heads = append(heads, RepoBranch{Name: b.Name, SHA: b.Commit.SHA, Protected: b.Protected})
	}
	return heads, newTag, false, nil
}

// validETag keeps a value that came from a header, and goes into one, to the characters an entity tag has.
func validETag(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for _, c := range []byte(s) {
		if c < 0x21 || c == 0x7f || c == '\\' {
			return false
		}
	}
	return true
}

// RepoView reads the repository from GitHub with the person's own gh. The repository is named once, in a validated form.
func (c *CLI) RepoView(ctx context.Context, repo Repo) (*RepoView, error) {
	base := "repos/" + repo.Owner + "/" + repo.Name
	get := func(path string, into any) error {
		args := []string{"api", path}
		if repo.Host != "" && repo.Host != "github.com" {
			args = append(args, "--hostname", repo.Host)
		}
		out, err := c.run(ctx, args...)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(out, into); err != nil {
			return &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
		}
		return nil
	}

	var r struct {
		FullName      string          `json:"full_name"`
		Description   string          `json:"description"`
		HTMLURL       string          `json:"html_url"`
		Private       bool            `json:"private"`
		DefaultBranch string          `json:"default_branch"`
		PushedAt      string          `json:"pushed_at"`
		Permissions   RepoPermissions `json:"permissions"`
	}
	if err := get(base, &r); err != nil {
		return nil, err
	}
	v := &RepoView{FullName: r.FullName, Description: r.Description, URL: r.HTMLURL, Private: r.Private, DefaultBranch: r.DefaultBranch,
		PushedAt: parseTime(r.PushedAt), Permissions: r.Permissions, Branches: []RepoBranch{}, Commits: []RepoCommit{}, PullRequests: []domain.GitHubPR{}}

	// Everything past the repository itself is best effort: a repository with no commits yet has no branches, and one failing
	// part must not hide the rest.
	var branches []struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
		Commit    struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if get(base+"/branches?per_page=50", &branches) == nil {
		for _, b := range branches {
			v.Branches = append(v.Branches, RepoBranch{Name: b.Name, SHA: b.Commit.SHA, Protected: b.Protected})
		}
	}
	var commits []struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Author *struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if get(fmt.Sprintf("%s/commits?per_page=20&sha=%s", base, urlEscape(r.DefaultBranch)), &commits) == nil {
		for _, k := range commits {
			who := k.Commit.Author.Name
			if k.Author != nil && k.Author.Login != "" {
				who = k.Author.Login
			}
			msg, _, _ := strings.Cut(k.Commit.Message, "\n")
			v.Commits = append(v.Commits, RepoCommit{SHA: k.SHA, Message: msg, Author: who, At: parseTime(k.Commit.Author.Date), URL: k.HTMLURL})
		}
	}
	if prs, err := c.PullRequests(ctx, repo, 30); err == nil {
		v.PullRequests = prs
	}
	return v, nil
}

// urlEscape escapes a branch name for a query string.
func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

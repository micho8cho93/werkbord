package domain

import (
	"net/url"
	"strings"
)

// Links are addresses a person can follow from a ticket to the place where the
// work lives: the repository, its branch, a comparison with the base branch, the
// commits and the pull request. Team builds them from the project's repository
// address and the branch names members report; it never contacts the host.
type Links struct {
	Repository  string `json:"repository,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Compare     string `json:"compare,omitempty"`
	PullRequest string `json:"pullRequest,omitempty"`
	// CommitPrefix plus a commit hash is that commit's page.
	CommitPrefix string `json:"commitPrefix,omitempty"`
}

// RepositoryWebURL turns a repository address (https, ssh or scp-style) into the
// address of its web page, for the hosts whose page layout is known; "" for any
// other host, so a link is never guessed.
func RepositoryWebURL(addr string) string {
	key := RepositoryKey(addr)
	host, path, ok := strings.Cut(key, "/")
	if !ok {
		return ""
	}
	switch host {
	case "github.com", "gitlab.com":
		return "https://" + host + "/" + escapePath(path)
	}
	return ""
}

// TicketLinks builds the links for a ticket in a project whose repository is repo.
func TicketLinks(repo string, k Ticket) Links {
	l := Links{}
	if k.PullRequest != nil && isWebURL(k.PullRequest.URL) {
		l.PullRequest = k.PullRequest.URL
	}
	web := RepositoryWebURL(repo)
	if web == "" {
		return l
	}
	l.Repository = web
	gitlab := strings.HasPrefix(web, "https://gitlab.com/")
	dash := ""
	if gitlab {
		dash = "/-"
	}
	l.CommitPrefix = web + dash + "/commit/"
	if k.Branch != "" {
		l.Branch = web + dash + "/tree/" + escapePath(k.Branch)
		base := ""
		if k.PullRequest != nil {
			base = k.PullRequest.BaseBranch
		}
		if base != "" {
			l.Compare = web + dash + "/compare/" + escapePath(base) + "..." + escapePath(k.Branch)
		}
	}
	return l
}

func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// escapePath escapes each segment of a slash-separated path.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

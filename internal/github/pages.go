package github

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"werkbord/internal/domain"
)

// Creating a repository and publishing it with GitHub Pages, both through the
// user's own GitHub CLI. Pages is set to build from a GitHub Actions workflow, so
// the repository publishes whatever its workflow deploys.

// ErrExists is returned when the user already has a repository of that name.
var ErrExists = errors.New("a repository with that name already exists on this account")

// ErrPagesUnavailable is returned when GitHub will not host Pages for the
// repository, for instance a private repository on a plan without it.
var ErrPagesUnavailable = errors.New("GitHub Pages is not available for this repository on your plan")

var newRepoNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// ValidNewRepoName reports whether name can name a new repository.
func ValidNewRepoName(name string) bool {
	return newRepoNameRE.MatchString(name) && name != "." && name != ".." && !strings.HasSuffix(name, ".git")
}

// CreateRepository creates a repository under the signed-in user, with a first
// commit (a README) so it has a default branch to clone and to publish from.
func (c *CLI) CreateRepository(ctx context.Context, name, description string, private bool) (Repository, error) {
	if !ValidNewRepoName(name) {
		return Repository{}, &Error{Reason: domain.GHError, Message: "that is not a valid repository name"}
	}
	priv := "false"
	if private {
		priv = "true"
	}
	out, err := c.run(ctx, "api", "--hostname", "github.com", "-X", "POST", "user/repos",
		"-f", "name="+name, "-f", "description="+description, "-F", "private="+priv, "-F", "auto_init=true")
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && strings.Contains(strings.ToLower(ge.Message), "name already exists") {
			return Repository{}, ErrExists
		}
		return Repository{}, err
	}
	var r struct {
		FullName      string `json:"full_name"`
		Name          string `json:"name"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
		HTMLURL       string `json:"html_url"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(out, &r); err != nil || !ValidRepoName(r.FullName) {
		return Repository{}, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read"}
	}
	return Repository{FullName: r.FullName, Owner: r.Owner.Login, Name: r.Name, Private: r.Private, DefaultBranch: r.DefaultBranch, URL: r.HTMLURL, CanPush: true}, nil
}

// Pages is how a repository's GitHub Pages site stands.
type Pages struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
	// BuildType is "workflow" when a GitHub Actions workflow deploys the site.
	BuildType string `json:"buildType,omitempty"`
}

// EnablePages turns on Pages for the repository, deployed by a workflow. Turning
// it on when it already is, is not an error.
func (c *CLI) EnablePages(ctx context.Context, repo Repo) (Pages, error) {
	out, err := c.run(ctx, "api", "--hostname", "github.com", "-X", "POST", "repos/"+repo.String()+"/pages", "-f", "build_type=workflow")
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) {
			l := strings.ToLower(ge.Message)
			switch {
			case strings.Contains(l, "already enabled") || strings.Contains(l, "already exists"):
				return c.PagesInfo(ctx, repo)
			case strings.Contains(l, "does not support github pages") || strings.Contains(l, "upgrade to github pro"):
				return Pages{}, ErrPagesUnavailable
			}
		}
		return Pages{}, err
	}
	return parsePages(out)
}

// PagesInfo reads the repository's Pages settings. A repository without Pages is
// reported as not enabled, not as an error.
func (c *CLI) PagesInfo(ctx context.Context, repo Repo) (Pages, error) {
	out, err := c.run(ctx, "api", "--hostname", "github.com", "repos/"+repo.String()+"/pages")
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && strings.Contains(strings.ToLower(ge.Message), "not found") {
			return Pages{}, nil
		}
		return Pages{}, err
	}
	return parsePages(out)
}

func parsePages(out []byte) (Pages, error) {
	var p struct {
		HTMLURL   string `json:"html_url"`
		BuildType string `json:"build_type"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return Pages{}, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read"}
	}
	return Pages{Enabled: true, URL: p.HTMLURL, BuildType: p.BuildType}, nil
}

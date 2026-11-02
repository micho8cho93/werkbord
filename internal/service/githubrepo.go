package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"werkbord/internal/domain"
	"werkbord/internal/github"
)

// RepoState is a team's repository as this person's own GitHub sign-in sees it. State says which of three things is true, so the
// page can show GitHub, or fall back to what the team has reported and say why:
//
//	live           signed in, and the repository answers
//	no_access      signed in, but GitHub does not show this repository to this person
//	not_connected  the GitHub CLI is missing or signed out
//	error          GitHub could not be reached or answered something unreadable
type RepoState struct {
	State   string           `json:"state"`
	Message string           `json:"message,omitempty"`
	Repo    *github.RepoView `json:"repo,omitempty"`
	// Viewer is who the person is signed in to GitHub as, so their own pushes can be told apart from others'.
	Viewer string `json:"viewer,omitempty"`
}

// Repository reads fullName (owner/name) from GitHub with the person's own gh. The name may have come from a teammate, so it is
// checked before it is passed to any command, and nothing is written.
func (g *GitHubSetup) Repository(ctx context.Context, fullName string) (*RepoState, error) {
	if g.CLI == nil {
		return &RepoState{State: "not_connected", Message: "The GitHub integration is turned off."}, nil
	}
	repo, ok := parseRepoName(fullName)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a GitHub repository name (owner/name)", domain.ErrInvalid, fullName)
	}
	// Another GitHub host is shown only to someone whose gh is signed in to it.
	if repo.Host != "github.com" && !g.CLI.HostKnown(ctx, repo.Host) {
		return &RepoState{State: "not_connected", Message: "The GitHub CLI is not signed in to " + repo.Host + "."}, nil
	}
	v, err := g.CLI.RepoView(ctx, repo)
	if err != nil {
		var ge *github.Error
		switch {
		case errors.As(err, &ge) && (ge.Reason == domain.GHMissing || ge.Reason == domain.GHUnauthenticated):
			return &RepoState{State: "not_connected", Message: ge.Message}, nil
		case github.NotFound(err):
			return &RepoState{State: "no_access", Message: "GitHub does not show this repository to your account."}, nil
		case errors.As(err, &ge):
			return &RepoState{State: "error", Message: ge.Message}, nil
		}
		return nil, g.asDomain(err)
	}
	viewer := ""
	if acct, err := g.CLI.Account(ctx); err == nil && acct != nil {
		viewer = acct.Login
	}
	return &RepoState{State: "live", Repo: v, Viewer: viewer}, nil
}

// RepoHeads is where each branch of a repository is now, for noticing a push. A repository that cannot be read answers with an
// empty list, not an error: the watcher simply sees nothing change. Each repository is asked with the entity tag GitHub gave last
// time, so a look at an unchanged repository costs nothing against GitHub's limit.
func (g *GitHubSetup) RepoHeads(ctx context.Context, fullName string) ([]github.RepoBranch, error) {
	if g.CLI == nil {
		return []github.RepoBranch{}, nil
	}
	repo, ok := parseRepoName(fullName)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a GitHub repository name (owner/name)", domain.ErrInvalid, fullName)
	}
	g.headsMu.Lock()
	last := g.headsSeen[fullName]
	g.headsMu.Unlock()
	heads, tag, unchanged, err := g.CLI.Heads(ctx, repo, last.etag)
	if unchanged && last.heads != nil {
		return last.heads, nil
	}
	if err != nil || heads == nil {
		return []github.RepoBranch{}, nil
	}
	g.headsMu.Lock()
	if g.headsSeen == nil {
		g.headsSeen = map[string]cachedHeads{}
	}
	if len(g.headsSeen) > 100 {
		g.headsSeen = map[string]cachedHeads{}
	}
	g.headsSeen[fullName] = cachedHeads{etag: tag, heads: heads}
	g.headsMu.Unlock()
	return heads, nil
}

type cachedHeads struct {
	etag  string
	heads []github.RepoBranch
}

// parseRepoName reads owner/name (github.com) or host/owner/name (another GitHub host, such as a company's), and nothing else.
func parseRepoName(s string) (github.Repo, bool) {
	parts := strings.Split(s, "/")
	switch len(parts) {
	case 2:
		if !github.ValidRepoName(s) {
			return github.Repo{}, false
		}
		return github.Repo{Host: "github.com", Owner: parts[0], Name: parts[1]}, true
	case 3:
		if !validHost(parts[0]) || !github.ValidRepoName(parts[1]+"/"+parts[2]) {
			return github.Repo{}, false
		}
		return github.Repo{Host: parts[0], Owner: parts[1], Name: parts[2]}, true
	}
	return github.Repo{}, false
}

var hostRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$`)

func validHost(h string) bool { return len(h) <= 253 && hostRE.MatchString(h) }

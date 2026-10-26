package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/github"
)

// CreateRepoRequest is what the user confirmed for a new repository.
type CreateRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Private keeps the repository private. GitHub Pages for a private repository
	// needs a paid plan, so a private one may come back without a site.
	Private bool `json:"private"`
}

// CreateRepoResult says what was made.
type CreateRepoResult struct {
	Repository github.Repository `json:"repository"`
	Project    *ProjectDetail    `json:"project"`
	// PagesURL is where the site will be published, once something is deployed.
	PagesURL string `json:"pagesUrl,omitempty"`
	// PagesError says why Pages could not be turned on. The repository and its
	// clone exist regardless, so the person can carry on without a site.
	PagesError string `json:"pagesError,omitempty"`
}

// CreateRepository makes a repository on the user's GitHub account, turns on
// Pages for it, clones it onto this computer and registers the clone as a
// project. Each step that succeeds stays: if the clone fails the repository
// still exists on GitHub, and the error says so. It writes to GitHub, so it only
// runs for a request the person confirmed, and it never deletes anything.
func (g *GitHubSetup) CreateRepository(ctx context.Context, req CreateRepoRequest) (*CreateRepoResult, error) {
	if g.CLI == nil {
		return nil, fmt.Errorf("%w: the GitHub integration is turned off", domain.ErrConflict)
	}
	if !github.ValidNewRepoName(req.Name) {
		return nil, fmt.Errorf("%w: %q is not a valid repository name (letters, numbers, - _ . only)", domain.ErrInvalid, req.Name)
	}
	acct, err := g.cachedAccount(ctx)
	if err != nil {
		return nil, g.asDomain(err)
	}
	if missing := acct.MissingScopes(); len(missing) > 0 {
		return nil, fmt.Errorf("%w: GitHub has not allowed %s yet", domain.ErrConflict, strings.Join(missing, " and "))
	}

	// Creating and cloning must finish even if the phone that asked disconnects.
	ctx = context.WithoutCancel(ctx)
	repo, err := g.CLI.CreateRepository(ctx, req.Name, req.Description, req.Private)
	if err != nil {
		if errors.Is(err, github.ErrExists) {
			return nil, fmt.Errorf("%w: you already have a repository called %s; choose another name", domain.ErrConflict, req.Name)
		}
		return nil, g.asDomain(err)
	}
	res := &CreateRepoResult{Repository: repo}

	pages, perr := g.CLI.EnablePages(ctx, github.Repo{Host: "github.com", Owner: repo.Owner, Name: repo.Name})
	switch {
	case perr == nil:
		res.PagesURL = pages.URL
	case errors.Is(perr, github.ErrPagesUnavailable):
		res.PagesError = "GitHub Pages is not available for this repository on your plan. You can still use the repository."
	default:
		res.PagesError = "GitHub Pages could not be turned on: " + g.asDomain(perr).Error()
	}

	added, err := g.Add(ctx, repo.FullName, "")
	if err != nil {
		return res, fmt.Errorf("%w: %s was created on GitHub but could not be cloned here: %v", domain.ErrConflict, repo.FullName, err)
	}
	res.Project = added.Project
	return res, nil
}

// PagesState is where a site stands.
type PagesState struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
	// State is "disabled", "waiting" (enabled, nothing published yet) or "live"
	// (the address answers).
	State string `json:"state"`
}

// Pages reports whether the repository's site is enabled and answering.
func (g *GitHubSetup) Pages(ctx context.Context, fullName string) (*PagesState, error) {
	if g.CLI == nil {
		return nil, fmt.Errorf("%w: the GitHub integration is turned off", domain.ErrConflict)
	}
	repo, ok := github.ParseRemote("https://github.com/" + fullName + ".git")
	if !ok || !github.ValidRepoName(fullName) {
		return nil, fmt.Errorf("%w: %q is not a GitHub repository name (owner/name)", domain.ErrInvalid, fullName)
	}
	p, err := g.CLI.PagesInfo(ctx, repo)
	if err != nil {
		return nil, g.asDomain(err)
	}
	if !p.Enabled {
		return &PagesState{State: "disabled"}, nil
	}
	st := &PagesState{Enabled: true, URL: p.URL, State: "waiting"}
	if g.live(ctx, p.URL) {
		st.State = "live"
	}
	return st, nil
}

// live is whether the address answers with a page.
func (g *GitHubSetup) live(ctx context.Context, url string) bool {
	if g.Probe != nil {
		return g.Probe(ctx, url)
	}
	if url == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

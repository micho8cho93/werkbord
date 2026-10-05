package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"devboard/internal/domain"
	"devboard/internal/github"
)

// GitHubSetup is the optional GitHub side of onboarding: connecting the user's
// own GitHub account, choosing repositories to add as projects, and telling
// apart what is already on this computer from what exists only on GitHub.
//
// It goes through the user's GitHub CLI (see package github), so Werkbord has no
// GitHub credential of its own and stores nothing in GitHub. Everything here is
// optional: without it, projects are added from a path, as before.
type GitHubSetup struct {
	Deps
	// CLI is nil when the integration is turned off in config.json.
	CLI      *github.CLI
	Login    *github.LoginSession
	Projects *Projects
	// Home is the user's home directory, where local clones are looked for and new
	// ones made. Default: os.UserHomeDir().
	Home string
	// ScanRoots overrides where local clones are looked for (tests).
	ScanRoots []string
	// CloneRoot overrides where new clones are made.
	CloneRoot string

	mu        sync.Mutex
	account   *github.Account
	accountAt time.Time
}

// GitHub connection states.
const (
	GitHubDisabled  = "disabled"   // turned off in config.json
	GitHubMissing   = "missing"    // the GitHub CLI is not installed
	GitHubSignedOut = "signed_out" // installed, nobody signed in
	GitHubSigningIn = "signing_in" // a sign-in is waiting for the user on GitHub
	GitHubSignedIn  = "signed_in"
	GitHubError     = "error" // GitHub could not be reached, or said something unexpected
)

// GitHubStatus is the state of the connection, for the app to show.
type GitHubStatus struct {
	State string `json:"state"`
	// Account is who is signed in, when State is signed_in.
	Account *github.Account `json:"account,omitempty"`
	Version string          `json:"version,omitempty"`
	// Message says what is wrong, and Guidance what to do about it.
	Message  string       `json:"message,omitempty"`
	Guidance string       `json:"guidance,omitempty"`
	Login    github.Login `json:"login"`
}

const accountTTL = 20 * time.Second

func (g *GitHubSetup) home() string {
	if g.Home != "" {
		return g.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

// Status reports the connection. While a sign-in is pending it does not ask
// GitHub who is signed in, which keeps the app's polling cheap.
func (g *GitHubSetup) Status(ctx context.Context) GitHubStatus {
	if g.CLI == nil {
		return GitHubStatus{State: GitHubDisabled, Message: "The GitHub integration is turned off in config.json (github.disabled).", Login: github.Login{State: github.LoginIdle}}
	}
	login := github.Login{State: github.LoginIdle}
	if g.Login != nil {
		login = g.Login.Status()
	}
	if login.State == github.LoginPending {
		return GitHubStatus{State: GitHubSigningIn, Login: login}
	}
	st := GitHubStatus{Login: login}
	if login.State == github.LoginDone {
		g.forgetAccount() // a sign-in just finished: look again
	}
	ver, err := g.CLI.Version(ctx)
	if err != nil {
		return g.failed(st, err)
	}
	st.Version = ver
	acct, err := g.cachedAccount(ctx)
	if err != nil {
		return g.failed(st, err)
	}
	st.State, st.Account = GitHubSignedIn, acct
	return st
}

func (g *GitHubSetup) failed(st GitHubStatus, err error) GitHubStatus {
	var ge *github.Error
	if !errors.As(err, &ge) {
		st.State, st.Message = GitHubError, err.Error()
		return st
	}
	switch ge.Reason {
	case domain.GHMissing:
		st.State, st.Message = GitHubMissing, "The GitHub CLI (gh) is not installed."
		st.Guidance = "Install it from https://cli.github.com (on macOS: brew install gh), then connect. Werkbord does not install it for you, and you can skip GitHub for now."
	case domain.GHUnauthenticated:
		st.State = GitHubSignedOut
	default:
		st.State, st.Message = GitHubError, ge.Message
	}
	return st
}

func (g *GitHubSetup) cachedAccount(ctx context.Context) (*github.Account, error) {
	g.mu.Lock()
	if g.account != nil && time.Since(g.accountAt) < accountTTL {
		a := g.account
		g.mu.Unlock()
		return a, nil
	}
	g.mu.Unlock()
	a, err := g.CLI.Account(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err != nil {
		g.account = nil
		return nil, err
	}
	g.account, g.accountAt = a, time.Now()
	return a, nil
}

func (g *GitHubSetup) forgetAccount() {
	g.mu.Lock()
	g.account = nil
	g.mu.Unlock()
}

// StartLogin begins signing in to GitHub with the browser: the user is shown a
// one-time code to enter at github.com/login/device.
func (g *GitHubSetup) StartLogin(ctx context.Context) (github.Login, error) {
	if g.CLI == nil || g.Login == nil {
		return github.Login{}, fmt.Errorf("%w: the GitHub integration is turned off", domain.ErrConflict)
	}
	l, err := g.Login.Start(ctx)
	var ge *github.Error
	if errors.As(err, &ge) {
		return l, fmt.Errorf("%w: %s", domain.ErrConflict, ge.Message)
	}
	g.forgetAccount()
	return l, err
}

// CancelLogin abandons a sign-in that is waiting.
func (g *GitHubSetup) CancelLogin() {
	if g.Login != nil {
		g.Login.Cancel()
	}
}

// RepoChoice is a repository the user can reach on GitHub, with what this
// computer knows about it. The two are kept apart on purpose: LocalPaths is
// access to a clone on this computer, which is what agents work in; everything
// else is only GitHub's metadata about it.
type RepoChoice struct {
	github.Repository
	// LocalPaths are clones of it found on this computer. Empty means GitHub only:
	// it would have to be cloned before an agent could work on it.
	LocalPaths []string `json:"localPaths"`
	// Project is set when it is already a Werkbord project.
	Project *ProjectRef `json:"project,omitempty"`
}

// ProjectRef names a project.
type ProjectRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// RepoList is the user's repositories.
type RepoList struct {
	Account   *github.Account `json:"account"`
	Repos     []RepoChoice    `json:"repos"`
	Truncated bool            `json:"truncated"`
	// CloneDir is where a repository that is only on GitHub would be cloned.
	CloneDir string `json:"cloneDir"`
}

// scanBudget bounds the search for local clones.
const scanBudget = 6 * time.Second

// Repositories lists the repositories the user can reach and marks which are
// already on this computer (found by looking for clones whose remote is that
// repository) and which are already projects.
func (g *GitHubSetup) Repositories(ctx context.Context) (*RepoList, error) {
	if g.CLI == nil {
		return nil, fmt.Errorf("%w: the GitHub integration is turned off", domain.ErrConflict)
	}
	acct, err := g.cachedAccount(ctx)
	if err != nil {
		return nil, g.asDomain(err)
	}
	repos, truncated, err := g.CLI.Repositories(ctx)
	if err != nil {
		return nil, g.asDomain(err)
	}

	scanCtx, cancel := context.WithTimeout(ctx, scanBudget)
	defer cancel()
	roots := g.ScanRoots
	if roots == nil {
		roots = github.DefaultScanRoots(g.home())
	}
	local := map[string][]string{} // lower-cased owner/name -> clone paths
	for _, c := range github.ScanLocalClones(scanCtx, github.ScanOptions{Roots: roots}) {
		k := strings.ToLower(c.Repo.String())
		local[k] = appendUnique(local[k], c.Path)
	}

	projects, err := g.Projects.List(ctx)
	if err != nil {
		return nil, err
	}
	byRepo := map[string]ProjectRef{}
	for _, p := range projects {
		if p.Repository == nil {
			continue
		}
		for _, rm := range p.Repository.Remotes {
			if r, ok := github.ParseRemote(rm.URL); ok && r.Host == "github.com" {
				byRepo[strings.ToLower(r.String())] = ProjectRef{ID: p.ID, Name: p.Name, Path: p.RepoPath}
				local[strings.ToLower(r.String())] = appendUnique(local[strings.ToLower(r.String())], p.RepoPath)
			}
		}
	}

	out := &RepoList{Account: acct, Truncated: truncated, CloneDir: g.cloneRoot(), Repos: make([]RepoChoice, 0, len(repos))}
	for _, r := range repos {
		k := strings.ToLower(r.FullName)
		c := RepoChoice{Repository: r, LocalPaths: local[k]}
		if c.LocalPaths == nil {
			c.LocalPaths = []string{}
		}
		if p, ok := byRepo[k]; ok {
			c.Project = &p
		}
		out.Repos = append(out.Repos, c)
	}
	// Ones the user can work on now come first, then by how recently they were pushed.
	sort.SliceStable(out.Repos, func(i, j int) bool {
		a, b := out.Repos[i], out.Repos[j]
		if (len(a.LocalPaths) > 0) != (len(b.LocalPaths) > 0) {
			return len(a.LocalPaths) > 0
		}
		return a.PushedAt.After(b.PushedAt)
	})
	return out, nil
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func (g *GitHubSetup) asDomain(err error) error {
	var ge *github.Error
	if errors.As(err, &ge) {
		if ge.Reason == domain.GHError {
			return fmt.Errorf("%w: %s", domain.ErrConflict, ge.Message)
		}
		return fmt.Errorf("%w: %s", domain.ErrConflict, ge.Message)
	}
	return err
}

// cloneRoot is where new clones go: a directory the user already keeps code in,
// else ~/Code.
func (g *GitHubSetup) cloneRoot() string {
	if g.CloneRoot != "" {
		return g.CloneRoot
	}
	home := g.home()
	for _, d := range []string{"code", "Code", "dev", "Dev", "src", "projects", "Projects", "Developer", "repos", "Repos", "workspace", "GitHub", "github"} {
		p := filepath.Join(home, d)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return filepath.Join(home, "Code")
}

// AddResult says what adding a repository did.
type AddResult struct {
	Project *ProjectDetail `json:"project"`
	// Cloned is true when it was cloned to this computer first.
	Cloned bool `json:"cloned"`
}

// cloneTimeout bounds a clone: large repositories take minutes, not hours.
const cloneTimeout = 15 * time.Minute

// Add makes a GitHub repository a Werkbord project. With path, it is the clone
// the user chose among those found on this computer; without, the repository is
// cloned first, into the clone directory, using the user's GitHub sign-in.
// Cloning is the only thing here that writes to disk or talks to GitHub beyond
// reading, and it only happens when the user chose a repository that is not on
// this computer.
func (g *GitHubSetup) Add(ctx context.Context, fullName, path string) (*AddResult, error) {
	if !github.ValidRepoName(fullName) {
		return nil, fmt.Errorf("%w: %q is not a GitHub repository name (owner/name)", domain.ErrInvalid, fullName)
	}
	if path != "" {
		if err := g.checkClone(path, fullName); err != nil {
			return nil, err
		}
		p, err := g.Projects.Register(ctx, path, "")
		if err != nil {
			return nil, err
		}
		return &AddResult{Project: p}, nil
	}
	if g.CLI == nil {
		return nil, fmt.Errorf("%w: the GitHub integration is turned off", domain.ErrConflict)
	}
	dest, err := g.clone(ctx, fullName)
	if err != nil {
		return nil, err
	}
	p, err := g.Projects.Register(ctx, dest, "")
	if err != nil {
		return nil, err
	}
	return &AddResult{Project: p, Cloned: true}, nil
}

// checkClone refuses a path that is not a clone of the repository.
func (g *GitHubSetup) checkClone(path, fullName string) error {
	for _, r := range github.ClonesAt(path) {
		if strings.EqualFold(r.String(), fullName) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is not a clone of %s", domain.ErrInvalid, path, fullName)
}

// clone copies the repository from GitHub to a new directory. Git is given the
// user's GitHub CLI as a credential helper for this one command, so a private
// repository clones with the user's own sign-in and nothing is written to their
// Git configuration; afterwards the same helper is set on this repository only, so
// that pushing from it works too.
func (g *GitHubSetup) clone(ctx context.Context, fullName string) (string, error) {
	owner, name, _ := strings.Cut(fullName, "/")
	root := g.cloneRoot()
	dest := filepath.Join(root, name)
	if _, err := os.Lstat(dest); err == nil {
		dest = filepath.Join(root, owner+"-"+name)
		if _, err := os.Lstat(dest); err == nil {
			return "", fmt.Errorf("%w: %s already exists; if it is a clone of %s, choose it from the list instead", domain.ErrConflict, dest, fullName)
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", root, err)
	}
	gh := g.CLI.Binary
	if gh == "" {
		gh = "gh"
	}
	ghPath, err := exec.LookPath(gh)
	if err != nil {
		return "", fmt.Errorf("%w: the GitHub CLI (gh) is not installed", domain.ErrConflict)
	}
	helper := "!" + shellQuote(ghPath) + " auth git-credential"

	// Cloning must finish even if the phone that asked disconnects: a half-made
	// clone helps nobody.
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cloneTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "-c", "credential.helper=", "-c", "credential.helper="+helper,
		"clone", "--", "https://github.com/"+fullName+".git", dest)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_ASKPASS=", "SSH_ASKPASS=")
	cmd.WaitDelay = 2 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dest) // only ever a directory this call has just made
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return "", fmt.Errorf("%w: could not clone %s: %s", domain.ErrGit, fullName, truncate(msg, 300))
	}
	_ = exec.Command("git", "-C", dest, "config", "credential.helper", helper).Run()
	return dest, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

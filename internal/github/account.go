package github

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"devboard/internal/domain"
)

// The pieces of the GitHub integration that onboarding needs: who is signed
// in, signing in, and the repositories the user can reach. All of it goes
// through the user's own GitHub CLI, so Werkbord never holds a GitHub
// credential, never creates an account, and never stores anything in GitHub.

// Account is who gh is signed in as.
type Account struct {
	Host  string `json:"host"`
	Login string `json:"login"`
	Name  string `json:"name,omitempty"`
}

// Version returns the installed gh's version, or a *Error saying it is missing.
func (c *CLI) Version(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "--version")
	if err != nil {
		return "", err
	}
	return versionRE.FindString(string(out)), nil
}

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// Account returns who gh is signed in as on github.com. It asks GitHub, so it
// also finds out that a stored token has expired or been revoked, which `gh auth
// status` alone does not.
func (c *CLI) Account(ctx context.Context) (*Account, error) {
	out, err := c.run(ctx, "api", "--hostname", "github.com", "user")
	if err != nil {
		return nil, err
	}
	var u struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(out, &u); err != nil || u.Login == "" {
		return nil, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read"}
	}
	return &Account{Host: "github.com", Login: u.Login, Name: u.Name}, nil
}

// Repository is a repository the signed-in user can reach, as GitHub describes it.
type Repository struct {
	FullName      string    `json:"fullName"` // owner/name
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Private       bool      `json:"private"`
	Fork          bool      `json:"fork"`
	Archived      bool      `json:"archived"`
	DefaultBranch string    `json:"defaultBranch,omitempty"`
	PushedAt      time.Time `json:"pushedAt,omitempty"`
	URL           string    `json:"url"`
	// CanPush is whether the user may push to it: a repository they can only read
	// is of less use as a place for agents to work.
	CanPush bool `json:"canPush"`
}

const (
	reposPerPage = 100
	maxRepoPages = 5 // 500 repositories: enough to choose from, bounded in time
)

// Repositories lists the repositories the signed-in user owns, collaborates on or
// reaches through an organization, most recently pushed first. truncated says
// there are more than were fetched.
func (c *CLI) Repositories(ctx context.Context) (repos []Repository, truncated bool, err error) {
	for page := 1; page <= maxRepoPages; page++ {
		out, err := c.run(ctx, "api", "--hostname", "github.com", "-X", "GET", "user/repos",
			"-f", fmt.Sprintf("per_page=%d", reposPerPage), "-f", fmt.Sprintf("page=%d", page),
			"-f", "sort=pushed", "-f", "affiliation=owner,collaborator,organization_member")
		if err != nil {
			return nil, false, err
		}
		var raw []struct {
			FullName      string `json:"full_name"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			Private       bool   `json:"private"`
			Fork          bool   `json:"fork"`
			Archived      bool   `json:"archived"`
			DefaultBranch string `json:"default_branch"`
			PushedAt      string `json:"pushed_at"`
			HTMLURL       string `json:"html_url"`
			Owner         struct {
				Login string `json:"login"`
			} `json:"owner"`
			Permissions struct {
				Push bool `json:"push"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal(out, &raw); err != nil {
			return nil, false, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
		}
		for _, r := range raw {
			if !ValidRepoName(r.FullName) {
				continue
			}
			repo := Repository{
				FullName: r.FullName, Owner: r.Owner.Login, Name: r.Name, Description: r.Description, Private: r.Private,
				Fork: r.Fork, Archived: r.Archived, DefaultBranch: r.DefaultBranch, URL: r.HTMLURL, CanPush: r.Permissions.Push,
			}
			if t := parseTime(r.PushedAt); t != nil {
				repo.PushedAt = *t
			}
			repos = append(repos, repo)
		}
		if len(raw) < reposPerPage {
			return repos, false, nil
		}
	}
	return repos, true, nil
}

var repoNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._][A-Za-z0-9._-]*$`)

// ValidRepoName reports whether s is an owner/name that is safe to pass to a
// command: no flags, no path tricks.
func ValidRepoName(s string) bool {
	if !repoNameRE.MatchString(s) || strings.Contains(s, "..") {
		return false
	}
	_, name, _ := strings.Cut(s, "/")
	return name != "." // a directory name, so it must name one
}

// ---- signing in ----

// LoginState is where a sign-in attempt has got to.
type LoginState string

const (
	LoginIdle    LoginState = "idle"
	LoginPending LoginState = "pending" // waiting for the user to enter the code on GitHub
	LoginDone    LoginState = "done"
	LoginFailed  LoginState = "failed"
)

// Login is a sign-in attempt, for the app to show: the user opens URL, types Code,
// and approves. Nothing here is a credential: the code is a one-time code that is
// useless without the user's approval on GitHub, and the token that results goes
// straight into gh's own store.
type Login struct {
	State LoginState `json:"state"`
	Code  string     `json:"code,omitempty"`
	URL   string     `json:"url,omitempty"`
	Error string     `json:"error,omitempty"`
}

var (
	codeRE     = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}\b`)
	loginURLRE = regexp.MustCompile(`https://[^\s]+/login/device\S*`)
)

// loginTimeout is how long a sign-in may take: GitHub's one-time code expires in
// 15 minutes.
const loginTimeout = 15 * time.Minute

// LoginSession runs `gh auth login` for the app. There is at most one at a time.
type LoginSession struct {
	CLI *CLI

	mu     sync.Mutex
	state  Login
	cancel context.CancelFunc
	id     int // the attempt that owns state; an attempt that was replaced must not write to it
}

// Status returns the current attempt.
func (l *LoginSession) Status() Login {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state.State == "" {
		return Login{State: LoginIdle}
	}
	return l.state
}

// Start begins signing in with the browser flow and returns once the one-time
// code is known (or it has failed). Starting while an attempt is pending returns
// that attempt.
func (l *LoginSession) Start(ctx context.Context) (Login, error) {
	l.mu.Lock()
	if l.state.State == LoginPending {
		st := l.state
		l.mu.Unlock()
		return st, nil
	}
	l.mu.Unlock()

	bin := l.CLI.Binary
	if bin == "" {
		bin = "gh"
	}
	runCtx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	cmd := exec.CommandContext(runCtx, bin, "auth", "login", "--hostname", "github.com", "--web", "--git-protocol", "https", "--skip-ssh-key")
	cmd.Env = ghEnv(os.Environ())
	cmd.WaitDelay = time.Second
	detach(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw // gh prints the code on stderr; both are read
	if err := cmd.Start(); err != nil {
		cancel()
		_ = pw.Close()
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return Login{}, &Error{Reason: domain.GHMissing, Message: "the GitHub CLI (gh) is not installed"}
		}
		return Login{}, &Error{Reason: domain.GHError, Message: "could not start the GitHub CLI: " + err.Error()}
	}

	l.mu.Lock()
	l.id++
	id := l.id
	l.state, l.cancel = Login{State: LoginPending}, cancel
	l.mu.Unlock()

	ready := make(chan struct{})
	var once sync.Once
	go func() {
		sc := bufio.NewScanner(pr)
		var tail []string
		for sc.Scan() {
			line := sc.Text()
			tail = append(tail, line)
			if len(tail) > 20 {
				tail = tail[1:]
			}
			l.mu.Lock()
			haveCode := false
			if l.id == id {
				if m := codeRE.FindString(line); m != "" && l.state.Code == "" {
					l.state.Code = m
					if l.state.URL == "" {
						l.state.URL = "https://github.com/login/device"
					}
				}
				if m := loginURLRE.FindString(line); m != "" {
					l.state.URL = m
				}
				haveCode = l.state.Code != ""
			}
			l.mu.Unlock()
			if haveCode {
				once.Do(func() { close(ready) })
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		cancel()
		l.mu.Lock()
		switch {
		case l.id != id:
		case err == nil:
			l.state.State = LoginDone
		case l.state.State == LoginIdle: // cancelled by the user
		case runCtx.Err() != nil:
			l.state.State, l.state.Error = LoginFailed, "the sign-in code expired before it was used; start again"
		default:
			l.state.State, l.state.Error = LoginFailed, "GitHub sign-in did not complete"
		}
		l.mu.Unlock()
		once.Do(func() { close(ready) })
	}()

	select {
	case <-ready:
	case <-time.After(15 * time.Second):
	case <-ctx.Done():
	}
	st := l.Status()
	if st.State == LoginPending && st.Code == "" {
		l.Cancel()
		return Login{}, &Error{Reason: domain.GHError, Message: "the GitHub CLI did not give a sign-in code"}
	}
	if st.State == LoginFailed {
		return st, &Error{Reason: domain.GHError, Message: st.Error}
	}
	return st, nil
}

// Cancel abandons a pending attempt.
func (l *LoginSession) Cancel() {
	l.mu.Lock()
	cancel := l.cancel
	if l.state.State == LoginPending {
		l.state = Login{State: LoginIdle}
	}
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

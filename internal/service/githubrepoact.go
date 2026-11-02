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

// RepoAction is one thing the person asks to do to a team's repository on GitHub. Which fields matter depends on Action. ConfirmRepo
// must be the repository's name exactly: the confirmation that named it is what the person agreed to, and a request that does not
// carry it is refused before anything is asked of GitHub.
type RepoAction struct {
	Repo          string `json:"repo"`
	ConfirmRepo   string `json:"confirmRepo"`
	Action        string `json:"action"` // create_branch, delete_branch, create_pr, comment, review, merge, update_branch
	Branch        string `json:"branch,omitempty"`
	From          string `json:"from,omitempty"` // create_branch: the branch to start from (default: the default branch)
	Base          string `json:"base,omitempty"`
	Title         string `json:"title,omitempty"`
	Body          string `json:"body,omitempty"`
	Draft         bool   `json:"draft,omitempty"`
	Number        int    `json:"number,omitempty"`
	Event         string `json:"event,omitempty"`         // review: approve, request_changes, comment
	Method        string `json:"method,omitempty"`        // merge: merge, squash, rebase
	SHA           string `json:"sha,omitempty"`           // merge, update_branch: the head the person looked at
	User          string `json:"user,omitempty"`          // set_role, remove_collaborator: a GitHub user name
	ConfirmTarget string `json:"confirmTarget,omitempty"` // set_role, remove_collaborator: the user name again, as typed by the person
	Role          string `json:"role,omitempty"`          // set_role: read, triage, write, maintain or admin
	Reviews       int    `json:"reviews,omitempty"`       // protect_branch: approving reviews required (1 to 6)
}

// RepoActionResult says what happened, in the words of GitHub Control Center's results: done, refused (a check stopped it before
// anything was asked of GitHub), rejected (GitHub refused), unverified (GitHub said yes but reading it back did not agree) or failed.
type RepoActionResult struct {
	Action      string           `json:"action"`
	Outcome     string           `json:"outcome"`
	OK          bool             `json:"ok"`
	Message     string           `json:"message"`
	PullRequest *domain.GitHubPR `json:"pullRequest,omitempty"`
	URL         string           `json:"url,omitempty"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// validBranch accepts the names Git does, and refuses the rest, as the Team server does for the branches it records.
func validBranch(s string) bool {
	if s == "" || len(s) > 200 || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.HasSuffix(s, ".lock") ||
		strings.Contains(s, "..") || strings.Contains(s, "//") || strings.Contains(s, "@{") || s == "@" {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	for _, p := range strings.Split(s, "/") {
		if strings.HasPrefix(p, ".") {
			return false
		}
	}
	return true
}

func refused(a RepoAction, format string, args ...any) *RepoActionResult {
	return &RepoActionResult{Action: a.Action, Outcome: "refused", Message: fmt.Sprintf(format, args...)}
}

// ActOnRepository does one write to a team's repository. It takes nothing from the page on trust: the request must repeat the
// repository's name, the name must be valid, GitHub is asked what the person may do before anything is changed, and a result is
// read back from GitHub before it is called done.
func (g *GitHubSetup) ActOnRepository(ctx context.Context, a RepoAction) (*RepoActionResult, error) {
	if g.CLI == nil {
		return refused(a, "The GitHub integration is turned off."), nil
	}
	repo, ok := parseRepoName(a.Repo)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a GitHub repository name (owner/name)", domain.ErrInvalid, a.Repo)
	}
	if a.ConfirmRepo != a.Repo {
		return refused(a, "Confirm the repository by name first."), nil
	}
	view, err := g.CLI.RepoView(ctx, repo)
	if err != nil {
		var ge *github.Error
		if errors.As(err, &ge) && github.NotFound(err) {
			return refused(a, "GitHub does not show this repository to your account."), nil
		}
		return &RepoActionResult{Action: a.Action, Outcome: "failed", Message: err.Error()}, nil
	}
	perms := view.Permissions
	needWrite := func() *RepoActionResult {
		if !perms.Push {
			return refused(a, "Your role on this repository (%s) does not allow this.", perms.Role())
		}
		return nil
	}
	fail := func(err error) *RepoActionResult {
		out := &RepoActionResult{Action: a.Action, Outcome: "rejected", Message: err.Error()}
		var ge *github.Error
		if errors.As(err, &ge) && ge.Reason != domain.GHError {
			out.Outcome = "failed"
		}
		return out
	}
	done := func(msg string) *RepoActionResult {
		return &RepoActionResult{Action: a.Action, Outcome: "done", OK: true, Message: msg}
	}
	unverified := func(msg string) *RepoActionResult {
		return &RepoActionResult{Action: a.Action, Outcome: "unverified", Message: msg}
	}

	needAdmin := func() *RepoActionResult {
		if !perms.Admin {
			return refused(a, "This needs the admin role on the repository; yours is %s.", perms.Role())
		}
		return nil
	}
	viewer := func() string {
		if acct, err := g.CLI.Account(ctx); err == nil && acct != nil {
			return acct.Login
		}
		return ""
	}
	// Changing who has access: the person types the user's name again, and cannot change or remove themselves from here.
	targetOK := func() *RepoActionResult {
		if !github.ValidLogin(a.User) || a.ConfirmTarget != a.User {
			return refused(a, "Type the user name of the person to confirm.")
		}
		if v := viewer(); v == "" || strings.EqualFold(v, a.User) {
			return refused(a, "You cannot change or remove your own access from here.")
		}
		return nil
	}
	roleOf := func(login string) (string, bool, error) {
		list, err := g.CLI.Collaborators(ctx, repo)
		if err != nil {
			return "", false, err
		}
		for _, c := range list {
			if strings.EqualFold(c.Login, login) {
				return c.Role, true, nil
			}
		}
		return "", false, nil
	}

	switch a.Action {
	case "set_role":
		if r := needAdmin(); r != nil {
			return r, nil
		}
		if r := targetOK(); r != nil {
			return r, nil
		}
		perm := map[string]string{"read": "pull", "triage": "triage", "write": "push", "maintain": "maintain", "admin": "admin"}[a.Role]
		if perm == "" {
			return refused(a, "Choose read, triage, write, maintain or admin."), nil
		}
		if err := g.CLI.SetCollaboratorRole(ctx, repo, a.User, perm); err != nil {
			return fail(err), nil
		}
		role, found, err := roleOf(a.User)
		switch {
		case err != nil:
			return unverified("GitHub accepted the change, but who has access could not be read back. Refresh to see."), nil
		case !found:
			return done("Invited " + a.User + " as " + a.Role + ". They have access once they accept."), nil
		case role != a.Role:
			return unverified(fmt.Sprintf("GitHub accepted the change, but %s reads back as %s. Refresh to see.", a.User, role)), nil
		}
		return done(fmt.Sprintf("%s is now %s.", a.User, a.Role)), nil

	case "remove_collaborator":
		if r := needAdmin(); r != nil {
			return r, nil
		}
		if r := targetOK(); r != nil {
			return r, nil
		}
		if err := g.CLI.RemoveCollaborator(ctx, repo, a.User); err != nil {
			return fail(err), nil
		}
		if _, found, err := roleOf(a.User); err != nil || found {
			return unverified("GitHub accepted the removal, but " + a.User + " still reads back as having access. Refresh to see."), nil
		}
		return done("Removed " + a.User + "."), nil

	case "protect_branch":
		if r := needAdmin(); r != nil {
			return r, nil
		}
		if !validBranch(a.Branch) || a.Reviews < 1 || a.Reviews > 6 {
			return refused(a, "Choose a branch and 1 to 6 approving reviews."), nil
		}
		if _, found, err := g.CLI.RefSHA(ctx, repo, a.Branch); err != nil {
			return fail(err), nil
		} else if !found {
			return refused(a, "There is no branch %s.", a.Branch), nil
		}
		if err := g.CLI.ProtectBranch(ctx, repo, a.Branch, a.Reviews); err != nil {
			return fail(err), nil
		}
		if p, err := g.CLI.BranchProtection(ctx, repo, a.Branch); err != nil || !p.Protected || p.RequiredReviews != a.Reviews {
			return unverified("GitHub accepted the rule, but it does not read back as set. Refresh to see."), nil
		}
		return done(fmt.Sprintf("%s now needs %d approving %s before a pull request can be merged.", a.Branch, a.Reviews, map[bool]string{true: "review", false: "reviews"}[a.Reviews == 1])), nil

	case "unprotect_branch":
		if r := needAdmin(); r != nil {
			return r, nil
		}
		if !validBranch(a.Branch) {
			return refused(a, "%q is not a valid branch name.", a.Branch), nil
		}
		if err := g.CLI.UnprotectBranch(ctx, repo, a.Branch); err != nil {
			return fail(err), nil
		}
		if p, err := g.CLI.BranchProtection(ctx, repo, a.Branch); err != nil || p.Protected {
			return unverified("GitHub accepted the change, but the branch still reads back as protected. Refresh to see."), nil
		}
		return done("Removed the rule on " + a.Branch + "."), nil

	case "enable_pages":
		if r := needAdmin(); r != nil {
			return r, nil
		}
		if repo.Host != "github.com" {
			return refused(a, "GitHub Pages is only turned on from here for repositories on github.com."), nil
		}
		p, err := g.CLI.EnablePages(ctx, repo)
		if err != nil {
			return fail(err), nil
		}
		if !p.Enabled {
			return unverified("GitHub accepted the request, but Pages does not read back as on. Refresh to see."), nil
		}
		r := done("GitHub Pages is on.")
		r.URL = p.URL
		return r, nil

	case "create_branch":
		if r := needWrite(); r != nil {
			return r, nil
		}
		if !validBranch(a.Branch) {
			return refused(a, "%q is not a valid branch name.", a.Branch), nil
		}
		from := a.From
		if from == "" {
			from = view.DefaultBranch
		}
		if !validBranch(from) {
			return refused(a, "%q is not a valid branch name.", from), nil
		}
		if _, exists, err := g.CLI.RefSHA(ctx, repo, a.Branch); err != nil {
			return fail(err), nil
		} else if exists {
			return refused(a, "A branch named %s already exists.", a.Branch), nil
		}
		sha, found, err := g.CLI.RefSHA(ctx, repo, from)
		if err != nil {
			return fail(err), nil
		}
		if !found {
			return refused(a, "There is no branch %s to start from.", from), nil
		}
		if err := g.CLI.CreateBranch(ctx, repo, a.Branch, sha); err != nil {
			return fail(err), nil
		}
		if got, found, err := g.CLI.RefSHA(ctx, repo, a.Branch); err != nil || !found || got != sha {
			return unverified("GitHub accepted the branch, but it could not be read back as expected. Refresh to see."), nil
		}
		return done(fmt.Sprintf("Created %s from %s.", a.Branch, from)), nil

	case "delete_branch":
		if r := needWrite(); r != nil {
			return r, nil
		}
		if !validBranch(a.Branch) {
			return refused(a, "%q is not a valid branch name.", a.Branch), nil
		}
		if a.Branch == view.DefaultBranch {
			return refused(a, "%s is the default branch of the repository, and is not deleted here.", a.Branch), nil
		}
		for _, b := range view.Branches {
			if b.Name == a.Branch && b.Protected {
				return refused(a, "%s is protected on GitHub.", a.Branch), nil
			}
		}
		if _, found, err := g.CLI.RefSHA(ctx, repo, a.Branch); err != nil {
			return fail(err), nil
		} else if !found {
			return refused(a, "There is no branch %s.", a.Branch), nil
		}
		if err := g.CLI.DeleteBranch(ctx, repo, a.Branch); err != nil {
			return fail(err), nil
		}
		if _, found, err := g.CLI.RefSHA(ctx, repo, a.Branch); err != nil || found {
			return unverified("GitHub accepted the deletion, but the branch could still be read. Refresh to see."), nil
		}
		return done("Deleted " + a.Branch + "."), nil

	case "create_pr":
		if !validBranch(a.Branch) || !validBranch(a.Base) || a.Branch == a.Base {
			return refused(a, "Choose two different branches."), nil
		}
		title := strings.TrimSpace(a.Title)
		if title == "" || len(title) > 256 || len(a.Body) > 20000 {
			return refused(a, "A pull request needs a title (up to 256 characters) and a description of up to 20000."), nil
		}
		pr, err := g.CLI.CreatePullRequest(ctx, repo, github.CreateRequest{Head: a.Branch, Base: a.Base, Title: title, Body: a.Body, Draft: a.Draft})
		if err != nil {
			return fail(err), nil
		}
		if pr.State != "open" || pr.HeadBranch != a.Branch {
			return unverified("GitHub opened a pull request, but it does not read back as expected. Refresh to see."), nil
		}
		r := done(fmt.Sprintf("Opened pull request #%d.", pr.Number))
		r.PullRequest, r.URL = &pr, pr.URL
		return r, nil

	case "comment":
		body := strings.TrimSpace(a.Body)
		if a.Number <= 0 || body == "" || len(body) > 20000 {
			return refused(a, "Write a comment of up to 20000 characters on a pull request."), nil
		}
		url, err := g.CLI.Comment(ctx, repo, a.Number, body)
		if err != nil {
			return fail(err), nil
		}
		r := done(fmt.Sprintf("Commented on #%d.", a.Number))
		r.URL = url
		return r, nil

	case "review":
		events := map[string]string{"approve": "APPROVE", "request_changes": "REQUEST_CHANGES", "comment": "COMMENT"}
		event, ok := events[a.Event]
		body := strings.TrimSpace(a.Body)
		if a.Number <= 0 || !ok || len(body) > 20000 || (event != "APPROVE" && body == "") {
			return refused(a, "Choose approve, request changes or comment; the last two need a message."), nil
		}
		state, err := g.CLI.Review(ctx, repo, a.Number, event, body)
		if err != nil {
			return fail(err), nil
		}
		return done(fmt.Sprintf("Review recorded on #%d (%s).", a.Number, strings.ToLower(strings.ReplaceAll(state, "_", " ")))), nil

	case "merge":
		if r := needWrite(); r != nil {
			return r, nil
		}
		if a.Number <= 0 || !shaPattern.MatchString(a.SHA) || (a.Method != "merge" && a.Method != "squash" && a.Method != "rebase") {
			return refused(a, "Choose a pull request, the commit you looked at, and how to merge it."), nil
		}
		pr, err := g.CLI.PullRequest(ctx, repo, a.Number)
		if err != nil {
			return fail(err), nil
		}
		switch {
		case pr.State != "open":
			return refused(a, "#%d is %s.", a.Number, pr.State), nil
		case pr.Draft:
			return refused(a, "#%d is a draft.", a.Number), nil
		case pr.HeadSha != "" && pr.HeadSha != a.SHA:
			return refused(a, "#%d changed since you looked at it. Refresh and look again.", a.Number), nil
		case pr.Mergeable == "conflicting":
			return refused(a, "#%d has conflicts with %s.", a.Number, pr.BaseBranch), nil
		}
		if err := g.CLI.Merge(ctx, repo, a.Number, a.Method, a.SHA); err != nil {
			return fail(err), nil
		}
		after, err := g.CLI.PullRequest(ctx, repo, a.Number)
		if err != nil || after.State != "merged" {
			return unverified("GitHub accepted the merge, but the pull request does not read back as merged. Refresh to see."), nil
		}
		r := done(fmt.Sprintf("Merged #%d into %s.", a.Number, after.BaseBranch))
		r.PullRequest = &after
		return r, nil

	case "update_branch":
		if r := needWrite(); r != nil {
			return r, nil
		}
		if a.Number <= 0 || !shaPattern.MatchString(a.SHA) {
			return refused(a, "Choose a pull request and the commit you looked at."), nil
		}
		if err := g.CLI.UpdateBranch(ctx, repo, a.Number, a.SHA); err != nil {
			return fail(err), nil
		}
		return done(fmt.Sprintf("GitHub is bringing #%d up to date with its base branch. Refresh in a moment.", a.Number)), nil
	}
	return nil, fmt.Errorf("%w: %q is not something that can be done to a repository here", domain.ErrInvalid, a.Action)
}

// RepoAdmin is who has access to a team's repository, the rule on its default branch and its Pages site, as far as this person's role
// lets GitHub show them. A part GitHub will not show is left out and says why.
type RepoAdmin struct {
	CanAdmin      bool                  `json:"canAdmin"`
	Collaborators []github.Collaborator `json:"collaborators"`
	CollabNote    string                `json:"collabNote,omitempty"`
	Branch        string                `json:"branch"`
	Protection    *github.Protection    `json:"protection,omitempty"`
	ProtectNote   string                `json:"protectNote,omitempty"`
	Pages         *github.Pages         `json:"pages,omitempty"`
}

// RepositoryAdmin reads the access and settings of a repository. It only reads.
func (g *GitHubSetup) RepositoryAdmin(ctx context.Context, fullName string) (*RepoAdmin, error) {
	if g.CLI == nil {
		return nil, fmt.Errorf("%w: the GitHub integration is turned off", domain.ErrConflict)
	}
	repo, ok := parseRepoName(fullName)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a GitHub repository name (owner/name)", domain.ErrInvalid, fullName)
	}
	view, err := g.CLI.RepoView(ctx, repo)
	if err != nil {
		return nil, g.asDomain(err)
	}
	out := &RepoAdmin{CanAdmin: view.Permissions.Admin, Collaborators: []github.Collaborator{}, Branch: view.DefaultBranch}
	if view.Permissions.Push {
		if list, err := g.CLI.Collaborators(ctx, repo); err == nil {
			out.Collaborators = list
		} else {
			out.CollabNote = "GitHub did not show who has access."
		}
	} else {
		out.CollabNote = "GitHub shows who has access to people with write access."
	}
	if view.Permissions.Admin {
		if p, err := g.CLI.BranchProtection(ctx, repo, view.DefaultBranch); err == nil {
			out.Protection = &p
		} else {
			out.ProtectNote = "GitHub did not show the rule on this branch."
		}
		if repo.Host == "github.com" {
			if p, err := g.CLI.PagesInfo(ctx, repo); err == nil {
				out.Pages = &p
			}
		}
	} else {
		out.ProtectNote = "GitHub shows branch rules to administrators."
	}
	return out, nil
}

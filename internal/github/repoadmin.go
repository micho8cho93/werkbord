package github

import (
	"context"
	"encoding/json"
	"os"
	"regexp"

	"werkbord/internal/domain"
)

// Who may do what in a repository, and the rules on its branches. Reads are best effort (GitHub shows some of this only to people
// with enough access); writes are for administrators and are checked by internal/service before they get here.

// Collaborator is a person with access to the repository and the role GitHub gives them.
type Collaborator struct {
	Login string `json:"login"`
	// Role is GitHub's name for it: read, triage, write, maintain or admin.
	Role string `json:"role"`
}

var loginRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

// ValidLogin reports whether s can be a GitHub user name: it is passed in a URL path, so nothing else is let through.
func ValidLogin(s string) bool { return loginRE.MatchString(s) && s[len(s)-1] != '-' }

// Collaborators lists who has access, up to 100.
func (c *CLI) Collaborators(ctx context.Context, repo Repo) ([]Collaborator, error) {
	out, err := c.apiCall(ctx, repo, "GET", "/collaborators?per_page=100&affiliation=all", nil, nil)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Login    string `json:"login"`
		RoleName string `json:"role_name"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
	}
	list := make([]Collaborator, 0, len(raw))
	for _, r := range raw {
		list = append(list, Collaborator{Login: r.Login, Role: r.RoleName})
	}
	return list, nil
}

// SetCollaboratorRole gives login the role (pull, triage, push, maintain or admin). Someone who is not in the repository is invited.
func (c *CLI) SetCollaboratorRole(ctx context.Context, repo Repo, login, permission string) error {
	_, err := c.apiCall(ctx, repo, "PUT", "/collaborators/"+login, map[string]string{"permission": permission}, nil)
	return err
}

// RemoveCollaborator takes login's access away (or withdraws their invitation).
func (c *CLI) RemoveCollaborator(ctx context.Context, repo Repo, login string) error {
	_, err := c.apiCall(ctx, repo, "DELETE", "/collaborators/"+login, nil, nil)
	return err
}

// Protection is the rule that keeps a branch from being changed carelessly. Only what Werkbord sets is read.
type Protection struct {
	Protected bool `json:"protected"`
	// RequiredReviews is how many approving reviews a pull request needs; 0 when none are required.
	RequiredReviews int `json:"requiredReviews"`
}

// BranchProtection reads the rule on a branch. A branch with none is reported as unprotected, not as an error.
func (c *CLI) BranchProtection(ctx context.Context, repo Repo, branch string) (Protection, error) {
	out, err := c.apiCall(ctx, repo, "GET", "/branches/"+escapePath(branch)+"/protection", nil, nil)
	if err != nil {
		if NotFound(err) {
			return Protection{}, nil
		}
		return Protection{}, err
	}
	var raw struct {
		Reviews *struct {
			Count int `json:"required_approving_review_count"`
		} `json:"required_pull_request_reviews"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Protection{}, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
	}
	p := Protection{Protected: true}
	if raw.Reviews != nil {
		p.RequiredReviews = raw.Reviews.Count
	}
	return p, nil
}

// ProtectBranch requires `reviews` approving reviews (1 to 6) before a pull request can be merged into the branch. Administrators are
// not bound by it, so this cannot lock the person who set it out of their own repository.
func (c *CLI) ProtectBranch(ctx context.Context, repo Repo, branch string, reviews int) error {
	body, _ := json.Marshal(map[string]any{
		"required_status_checks":        nil,
		"enforce_admins":                false,
		"required_pull_request_reviews": map[string]any{"required_approving_review_count": reviews},
		"restrictions":                  nil,
	})
	f, err := os.CreateTemp("", "wb-protect-*.json")
	if err != nil {
		return &Error{Reason: domain.GHError, Message: "could not prepare the request: " + err.Error()}
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
		f.Close()
		return &Error{Reason: domain.GHError, Message: "could not prepare the request: " + err.Error()}
	}
	if err := f.Close(); err != nil {
		return &Error{Reason: domain.GHError, Message: "could not prepare the request: " + err.Error()}
	}
	args := []string{"api", "-X", "PUT", "repos/" + repo.Owner + "/" + repo.Name + "/branches/" + escapePath(branch) + "/protection", "--input", f.Name()}
	if repo.Host != "" && repo.Host != "github.com" {
		args = append(args, "--hostname", repo.Host)
	}
	_, err = c.run(ctx, args...)
	return err
}

// UnprotectBranch removes the rule on a branch.
func (c *CLI) UnprotectBranch(ctx context.Context, repo Repo, branch string) error {
	_, err := c.apiCall(ctx, repo, "DELETE", "/branches/"+escapePath(branch)+"/protection", nil, nil)
	return err
}

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"werkbord/internal/domain"
)

// Writes to a repository on GitHub, with the person's own gh. Each is one request, whose answer is read: the caller reads the
// result back from GitHub again before saying it worked. Nothing here decides whether an action is wise; internal/service does.

// apiCall runs `gh api` against a repository. strs are string fields (-f), ints are numbers (-F). Values never reach a shell.
func (c *CLI) apiCall(ctx context.Context, repo Repo, method, path string, strs map[string]string, ints map[string]int) ([]byte, error) {
	args := []string{"api", "-X", method, "repos/" + repo.Owner + "/" + repo.Name + path}
	if repo.Host != "" && repo.Host != "github.com" {
		args = append(args, "--hostname", repo.Host)
	}
	for k, v := range strs {
		args = append(args, "-f", k+"="+v)
	}
	for k, v := range ints {
		args = append(args, "-F", k+"="+strconv.Itoa(v))
	}
	return c.run(ctx, args...)
}

// RefSHA is where a branch is on GitHub now; found is false when there is no such branch.
func (c *CLI) RefSHA(ctx context.Context, repo Repo, branch string) (sha string, found bool, err error) {
	args := []string{"api", "repos/" + repo.Owner + "/" + repo.Name + "/git/ref/heads/" + escapePath(branch)}
	if repo.Host != "" && repo.Host != "github.com" {
		args = append(args, "--hostname", repo.Host)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		if NotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	var r struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return "", false, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
	}
	return r.Object.SHA, true, nil
}

// CreateBranch makes a branch at sha.
func (c *CLI) CreateBranch(ctx context.Context, repo Repo, name, sha string) error {
	_, err := c.apiCall(ctx, repo, "POST", "/git/refs", map[string]string{"ref": "refs/heads/" + name, "sha": sha}, nil)
	return err
}

// DeleteBranch removes a branch.
func (c *CLI) DeleteBranch(ctx context.Context, repo Repo, name string) error {
	_, err := c.apiCall(ctx, repo, "DELETE", "/git/refs/heads/"+escapePath(name), nil, nil)
	return err
}

// Comment adds a comment to a pull request's conversation and returns the comment's address.
func (c *CLI) Comment(ctx context.Context, repo Repo, number int, body string) (string, error) {
	out, err := c.apiCall(ctx, repo, "POST", fmt.Sprintf("/issues/%d/comments", number), map[string]string{"body": body}, nil)
	if err != nil {
		return "", err
	}
	var r struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	}
	if json.Unmarshal(out, &r) != nil || r.ID == 0 {
		return "", &Error{Reason: domain.GHError, Message: "GitHub did not confirm the comment"}
	}
	return r.HTMLURL, nil
}

// Review submits a review: event is APPROVE, REQUEST_CHANGES or COMMENT. It returns the state GitHub recorded.
func (c *CLI) Review(ctx context.Context, repo Repo, number int, event, body string) (string, error) {
	strs := map[string]string{"event": event}
	if body != "" {
		strs["body"] = body
	}
	out, err := c.apiCall(ctx, repo, "POST", fmt.Sprintf("/pulls/%d/reviews", number), strs, nil)
	if err != nil {
		return "", err
	}
	var r struct {
		State string `json:"state"`
	}
	if json.Unmarshal(out, &r) != nil || r.State == "" {
		return "", &Error{Reason: domain.GHError, Message: "GitHub did not confirm the review"}
	}
	return r.State, nil
}

// Merge merges a pull request with method (merge, squash or rebase). sha is the head the person looked at: GitHub refuses if it moved.
func (c *CLI) Merge(ctx context.Context, repo Repo, number int, method, sha string) error {
	_, err := c.apiCall(ctx, repo, "PUT", fmt.Sprintf("/pulls/%d/merge", number), map[string]string{"merge_method": method, "sha": sha}, nil)
	return err
}

// UpdateBranch asks GitHub to bring a pull request's branch up to date with its base. GitHub does it in the background.
func (c *CLI) UpdateBranch(ctx context.Context, repo Repo, number int, sha string) error {
	_, err := c.apiCall(ctx, repo, "PUT", fmt.Sprintf("/pulls/%d/update-branch", number), map[string]string{"expected_head_sha": sha}, nil)
	return err
}

// PullRequest reads one pull request as GitHub reports it.
func (c *CLI) PullRequest(ctx context.Context, repo Repo, number int) (domain.GitHubPR, error) {
	view, err := c.run(ctx, "pr", "view", strconv.Itoa(number), "-R", repo.Selector(), "--json", prFields)
	if err != nil {
		return domain.GitHubPR{}, err
	}
	var raw rawPR
	if err := json.Unmarshal(view, &raw); err != nil {
		return domain.GitHubPR{}, &Error{Reason: domain.GHError, Message: "GitHub's answer could not be read: " + err.Error()}
	}
	return raw.convert(), nil
}

// escapePath escapes each part of a branch name for a URL path, keeping its slashes.
func escapePath(s string) string {
	parts := strings.Split(s, "/")
	for i, p := range parts {
		parts[i] = urlEscape(p)
	}
	return strings.Join(parts, "/")
}

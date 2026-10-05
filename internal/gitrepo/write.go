package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"devboard/internal/domain"
)

// Operator changes a repository. Each method is one narrow, guarded Git
// operation, and none of them decides whether it SHOULD happen: that is
// service.GitControl's job, which plans, checks and locks first. What the
// engine guarantees is that the operation cannot do more than it says:
//
//   - nothing here forces. A push is a plain fast-forward push of a named commit;
//     the one lease-guarded deletion (DeleteRemoteBranch) only removes a remote
//     branch that is still exactly where the caller saw it.
//   - commits are named by ID, never by branch name, so a branch that moves
//     between the caller's look and the operation cannot change what is done.
//   - the repository's hooks are not run, and a transport may not run a command.
//   - a merge that does not complete is undone, and the result says whether it was.
//   - there is no reset, no checkout, no clean and no rebase.
type Operator interface {
	// Fetch updates the remote-tracking refs of one remote, pruning ones the remote deleted.
	Fetch(ctx context.Context, root, remote string) (RemoteResult, error)
	// Push sends commit Sha to refs/heads/<Branch> on the remote, without force.
	Push(ctx context.Context, root string, req PushRequest) (RemoteResult, error)
	// RemoteBranch asks the remote itself (not the local tracking ref) where a branch is.
	RemoteBranch(ctx context.Context, root, remote, branch string) (sha string, found bool, err error)
	// DeleteRemoteBranch deletes a remote branch only if it is still at expectSha.
	DeleteRemoteBranch(ctx context.Context, root, remote, branch, expectSha string) (RemoteResult, error)
	// SetUpstream makes branch track remote/branch.
	SetUpstream(ctx context.Context, root, branch, remote string) error
	// Merge merges commit req.Sha into the branch checked out at dir.
	Merge(ctx context.Context, dir string, req MergeRequest) (MergeResult, error)
	// DeleteBranchAt deletes a local branch only if it is still at sha.
	DeleteBranchAt(ctx context.Context, root, branch, sha string) error
	// RemoveCleanWorktree removes a linked worktree, and only if it has nothing
	// uncommitted in it: Git itself refuses otherwise, since no --force is used.
	RemoveCleanWorktree(ctx context.Context, root, path string) error
}

var _ Operator = (*CLI)(nil)

// PushRequest names what to push.
type PushRequest struct {
	Remote      string
	Branch      string
	Sha         string // the commit to publish as refs/heads/<Branch>
	SetUpstream bool
}

// RemoteResult is how a command that talks to a remote ended.
type RemoteResult struct {
	Outcome string // domain.OutcomeDone, OutcomeRejected, OutcomeUnavailable, OutcomeAuth or OutcomeFailed
	// NewBranch: the remote did not have the branch before.
	NewBranch bool
	UpToDate  bool
	Message   string // a sentence for the user
	Detail    string // Git's own error output, with credentials removed
}

// MergeRequest describes a merge.
type MergeRequest struct {
	Sha      string
	Message  string
	Strategy string // domain.MergeCommit or domain.MergeFastForward
}

// MergeResult is how a merge ended. After anything but OutcomeDone the checkout
// is back where it started, unless Undone is false, which says it is not.
type MergeResult struct {
	Outcome   string // done, noop, conflict, failed
	Before    string
	After     string
	Conflicts []string
	Undone    bool // a merge that did not complete was rolled back to Before
	Detail    string
}

// Hooks are never run by Werkbord's own commands: they are the repository's
// code, and Werkbord would be running it on the strength of a button tap.
// (A push or merge that the user wants hooks for is run in their terminal.)
func (c *CLI) runWrite(ctx context.Context, dir string, network bool, args ...string) ([]byte, error) {
	timeout := c.WriteTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	out, _, err := c.run(ctx, dir, runOpts{timeout: timeout, network: network}, append(append([]string{}, writeConfig...), args...)...)
	return out, err
}

// requireRemote checks that name is one of the repository's configured remotes.
// A push or fetch is only ever given a remote by name: never a URL, and never
// something that could be read as an option.
func (c *CLI) requireRemote(ctx context.Context, root, name string) error {
	if err := domain.ValidateRefName(name); err != nil {
		return fmt.Errorf("remote: %w", err)
	}
	names, err := c.remoteNames(ctx, root)
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == name {
			return nil
		}
	}
	return fmt.Errorf("%w: %q is not a remote of this repository", domain.ErrInvalid, name)
}

// ---- fetch, push ----

// Fetch implements Operator.
func (c *CLI) Fetch(ctx context.Context, root, remote string) (RemoteResult, error) {
	if err := c.requireRemote(ctx, root, remote); err != nil {
		return RemoteResult{}, err
	}
	_, err := c.runWrite(ctx, root, true, "fetch", "--prune", "--no-recurse-submodules", remote)
	if err != nil {
		return failureResult(err, "fetch from "+remote), nil
	}
	return RemoteResult{Outcome: domain.OutcomeDone, Message: "fetched " + remote}, nil
}

// Push implements Operator.
func (c *CLI) Push(ctx context.Context, root string, req PushRequest) (RemoteResult, error) {
	if err := domain.ValidateRefName(req.Branch); err != nil {
		return RemoteResult{}, fmt.Errorf("branch: %w", err)
	}
	if !IsCommitID(req.Sha) {
		return RemoteResult{}, fmt.Errorf("%w: a push needs the commit ID", domain.ErrInvalid)
	}
	if err := c.requireRemote(ctx, root, req.Remote); err != nil {
		return RemoteResult{}, err
	}
	// No +, no --force, no --force-with-lease: a remote that has moved on makes Git
	// refuse, and the refusal is reported, never worked around.
	out, err := c.runWrite(ctx, root, true, "push", "--porcelain", "--no-follow-tags", "--recurse-submodules=no",
		req.Remote, req.Sha+":refs/heads/"+req.Branch)
	res := parsePush(string(out), err, "push "+req.Branch+" to "+req.Remote)
	return res, nil
}

// DeleteRemoteBranch implements Operator.
func (c *CLI) DeleteRemoteBranch(ctx context.Context, root, remote, branch, expectSha string) (RemoteResult, error) {
	if err := domain.ValidateRefName(branch); err != nil {
		return RemoteResult{}, fmt.Errorf("branch: %w", err)
	}
	if !IsCommitID(expectSha) {
		return RemoteResult{}, fmt.Errorf("%w: deleting a remote branch needs the commit ID it is expected at", domain.ErrInvalid)
	}
	if err := c.requireRemote(ctx, root, remote); err != nil {
		return RemoteResult{}, err
	}
	// The lease makes the deletion conditional: the remote refuses it if the branch is
	// no longer at expectSha, so a commit pushed in the meantime is never deleted unseen.
	ref := "refs/heads/" + branch
	out, err := c.runWrite(ctx, root, true, "push", "--porcelain", "--no-follow-tags", "--recurse-submodules=no",
		"--force-with-lease="+ref+":"+expectSha, remote, ":"+ref)
	return parsePush(string(out), err, "delete "+branch+" on "+remote), nil
}

// RemoteBranch implements Operator.
func (c *CLI) RemoteBranch(ctx context.Context, root, remote, branch string) (string, bool, error) {
	if err := domain.ValidateRefName(branch); err != nil {
		return "", false, fmt.Errorf("branch: %w", err)
	}
	if err := c.requireRemote(ctx, root, remote); err != nil {
		return "", false, err
	}
	ref := "refs/heads/" + branch
	out, _, err := c.run(ctx, root, runOpts{timeout: 30 * time.Second, network: true}, "ls-remote", "--refs", remote, ref)
	if err != nil {
		return "", false, fmt.Errorf("ask %s about %s: %s", remote, branch, redactText(err.Error()))
	}
	for _, line := range strings.Split(string(out), "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && name == ref && IsCommitID(sha) {
			return sha, true, nil
		}
	}
	return "", false, nil
}

// SetUpstream implements Operator.
func (c *CLI) SetUpstream(ctx context.Context, root, branch, remote string) error {
	if err := domain.ValidateRefName(branch); err != nil {
		return fmt.Errorf("branch: %w", err)
	}
	if err := c.requireRemote(ctx, root, remote); err != nil {
		return err
	}
	_, err := c.runWrite(ctx, root, false, "branch", "--set-upstream-to="+remote+"/"+branch, branch)
	if err != nil {
		return fmt.Errorf("set upstream: %w", err)
	}
	return nil
}

// parsePush reads `git push --porcelain`: a line per ref, "<flag>\t<from>:<to>\t<summary>".
func parsePush(stdout string, runErr error, what string) RemoteResult {
	res := RemoteResult{Outcome: domain.OutcomeDone}
	rejected := false
	for _, line := range strings.Split(stdout, "\n") {
		flag, rest, ok := strings.Cut(line, "\t")
		if !ok || len(flag) != 1 {
			continue
		}
		switch flag {
		case "*":
			res.NewBranch = true
		case "=":
			res.UpToDate = true
		case "!":
			rejected = true
			_, summary, _ := strings.Cut(rest, "\t")
			res.Message = "the remote rejected it: " + strings.TrimSpace(summary)
		}
	}
	if runErr == nil && !rejected {
		res.Message = what
		return res
	}
	if rejected {
		fail := failureResult(runErr, what)
		fail.Outcome = domain.OutcomeRejected
		fail.Message = res.Message
		if strings.Contains(strings.ToLower(fail.Detail), "non-fast-forward") || strings.Contains(strings.ToLower(res.Message), "non-fast-forward") || strings.Contains(strings.ToLower(res.Message), "fetch first") {
			fail.Message = "the remote has commits you do not have, so a plain push is refused. Werkbord never forces a push: fetch, then bring those commits in first."
		}
		return fail
	}
	return failureResult(runErr, what)
}

var credentialsInURL = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]*@`)

// redactText removes credentials from URLs in text that came from Git's error
// output, which quotes remote URLs.
func redactText(s string) string { return credentialsInURL.ReplaceAllString(s, "${1}") }

// failureResult classifies a failed fetch or push for the user.
func failureResult(err error, what string) RemoteResult {
	res := RemoteResult{Outcome: domain.OutcomeFailed}
	var ge *gitError
	text := ""
	switch {
	case errors.As(err, &ge):
		text = ge.stderr
	case err != nil:
		text = err.Error()
	}
	res.Detail = clip(redactText(strings.TrimSpace(text)), 2000)
	l := strings.ToLower(text)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(l, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("[rejected]", "non-fast-forward", "[remote rejected]", "remote rejected", "pre-receive hook declined", "protected branch", "stale info", "fetch first"):
		res.Outcome = domain.OutcomeRejected
		res.Message = "the remote rejected it"
	case has("authentication failed", "permission denied", "could not read username", "could not read password", "invalid credentials", "access denied", "returned error: 401", "returned error: 403", "repository not found", "terminal prompts disabled"):
		res.Outcome = domain.OutcomeAuth
		res.Message = "could not " + what + ": the remote did not accept your credentials, or the repository is not visible to them"
	case has("could not resolve host", "unable to access", "connection refused", "connection timed out", "timed out", "deadline exceeded", "network is unreachable", "no route to host", "unable to connect", "early eof", "the remote end hung up", "could not read from remote repository", "does not appear to be a git repository", "couldn't connect"):
		res.Outcome = domain.OutcomeUnavailable
		res.Message = "could not " + what + ": the remote could not be reached"
	default:
		res.Message = "could not " + what
	}
	return res
}

// ---- merge ----

// Merge implements Operator.
func (c *CLI) Merge(ctx context.Context, dir string, req MergeRequest) (MergeResult, error) {
	var res MergeResult
	if !IsCommitID(req.Sha) {
		return res, fmt.Errorf("%w: a merge needs the commit ID", domain.ErrInvalid)
	}
	before, err := c.git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return res, fmt.Errorf("read HEAD: %w", err)
	}
	res.Before = before

	args := []string{"-c", "merge.autoStash=false", "merge", "--no-edit", "--no-verify", "--no-stat"}
	switch req.Strategy {
	case domain.MergeFastForward:
		args = append(args, "--ff-only", req.Sha)
	default:
		msg := strings.TrimSpace(req.Message)
		if msg == "" {
			msg = "Merge commit " + req.Sha
		}
		args = append(args, "--no-ff", "-m", msg, req.Sha)
	}
	_, err = c.runWrite(ctx, dir, false, args...)
	if err == nil {
		after, herr := c.git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
		if herr != nil {
			return res, fmt.Errorf("merge finished but HEAD cannot be read: %w", herr)
		}
		res.After = after
		res.Outcome = domain.OutcomeDone
		if after == before {
			res.Outcome = domain.OutcomeNoop
		}
		return res, nil
	}

	// It did not complete. Say why, then put the checkout back as it was.
	var ge *gitError
	if errors.As(err, &ge) {
		res.Detail = clip(redactText(ge.stderr), 2000)
	} else {
		res.Detail = clip(err.Error(), 2000)
	}
	res.Outcome = domain.OutcomeFailed
	op, _ := c.operation(ctx, dir)
	if op == "merge" {
		res.Outcome = domain.OutcomeConflict
		if out, _, derr := c.run(ctx, dir, runOpts{}, "diff", "--name-only", "--diff-filter=U", "-z"); derr == nil {
			for _, f := range strings.Split(string(out), "\x00") {
				if f != "" {
					res.Conflicts = append(res.Conflicts, f)
				}
			}
		}
		if _, aerr := c.runWrite(ctx, dir, false, "merge", "--abort"); aerr != nil {
			res.Detail += "\nmerge --abort failed: " + redactText(aerr.Error())
		}
	}
	after, herr := c.git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	opAfter, _ := c.operation(ctx, dir)
	res.After = after
	res.Undone = herr == nil && after == before && opAfter == ""
	return res, nil
}

// ---- deletion ----

// DeleteBranchAt implements Operator.
func (c *CLI) DeleteBranchAt(ctx context.Context, root, branch, sha string) error {
	if err := domain.ValidateRefName(branch); err != nil {
		return fmt.Errorf("branch: %w", err)
	}
	if !IsCommitID(sha) {
		return fmt.Errorf("%w: deleting a branch needs the commit ID it is expected at", domain.ErrInvalid)
	}
	// update-ref refuses to delete a ref that is not at the given value, which
	// makes the deletion conditional on the branch being exactly what was looked
	// at. It does not mind a branch being checked out, so that is checked, in every
	// worktree, first. (`git branch -D` would check that itself but would delete
	// whatever the branch had become.)
	wts, err := c.ListWorktrees(ctx, root)
	if err != nil {
		return err
	}
	for _, w := range wts {
		if w.Branch == branch {
			return fmt.Errorf("%w: %s is checked out at %s", domain.ErrConflict, branch, w.Path)
		}
	}
	if _, err := c.runWrite(ctx, root, false, "update-ref", "-d", "refs/heads/"+branch, sha); err != nil {
		var ge *gitError
		if errors.As(err, &ge) {
			return fmt.Errorf("%w: %s is no longer at %s, or cannot be deleted: %s", domain.ErrConflict, branch, sha[:12], ge.stderr)
		}
		return fmt.Errorf("delete branch: %w", err)
	}
	// Drop the branch's own settings (its upstream); a missing section is not an error.
	_, _ = c.runWrite(ctx, root, false, "config", "--remove-section", "branch."+branch)
	return nil
}

// RemoveCleanWorktree implements Operator.
func (c *CLI) RemoveCleanWorktree(ctx context.Context, root, path string) error {
	if err := domain.ValidateWorktreePath(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		_, err := c.runWrite(ctx, root, false, "worktree", "prune")
		return err
	}
	// Without --force Git refuses a worktree with modified or untracked files,
	// a locked one, and one it does not know. This is the last line of defence
	// after the service's own checks, and it is checked by Git at the moment of removal.
	if _, err := c.runWrite(ctx, root, false, "worktree", "remove", filepath.Clean(path)); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	_, err := c.runWrite(ctx, root, false, "worktree", "prune")
	return err
}

// Control is the whole Git boundary the Git Control Center uses. The agent
// runner is deliberately given only Worktrees, which has no merge, no push and
// no branch deletion beyond undoing its own, so a finished run has no way to
// merge anything (see TestRunnerCannotMerge).
type Control interface {
	Inspector
	Reader
	Operator
}

var _ Control = (*CLI)(nil)

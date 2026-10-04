package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"devboard/internal/domain"
)

// Worktrees creates and removes the linked worktrees agents run in.
//
// It is the engine behind service.Worktrees: that service records, before
// anything exists, which directories the controller owns and refuses paths it
// could not safely delete later; this engine only does what Git does. Callers
// must go through the service first.
type Worktrees interface {
	// AddWorktree creates a linked worktree of the repository at repoRoot, at
	// path, with branch checked out. If the branch does not exist it is created
	// at startPoint (a commit); if it does exist it is checked out as it is, and
	// startPoint is ignored. path must not exist or be an empty directory.
	AddWorktree(ctx context.Context, repoRoot, path, branch, startPoint string) (AddedWorktree, error)
	// RemoveWorktree removes the worktree at path, with whatever uncommitted
	// work is in it. A worktree whose directory is already gone is just
	// forgotten. It never touches the branch.
	RemoveWorktree(ctx context.Context, repoRoot, path string) error
	// DeleteBranch force-deletes a local branch. It exists to undo AddWorktree
	// when the branch was created by it and nothing was committed on it.
	DeleteBranch(ctx context.Context, repoRoot, branch string) error
}

// AddedWorktree reports what AddWorktree did.
type AddedWorktree struct {
	BranchCreated bool   // false if the branch already existed
	Head          string // the commit checked out
}

// ErrBranchCheckedOut is returned when the branch is already checked out in
// another worktree (including the main one), which Git does not allow.
var ErrBranchCheckedOut = fmt.Errorf("%w: branch is already checked out in another worktree", domain.ErrConflict)

var _ Worktrees = (*CLI)(nil)

// Repository-supplied hooks are not run: `git worktree add` would otherwise run
// the repository's post-checkout hook before the user has seen anything. The
// agent will run the repository's code soon enough, but at the user's request
// and in the open.
var writeConfig = []string{"-c", "core.hooksPath=/dev/null"}

func (c *CLI) write(ctx context.Context, repoRoot string, args ...string) (string, error) {
	timeout := c.WriteTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return c.gitTimeout(ctx, repoRoot, timeout, append(append([]string{}, writeConfig...), args...)...)
}

// AddWorktree implements Worktrees.
func (c *CLI) AddWorktree(ctx context.Context, repoRoot, path, branch, startPoint string) (AddedWorktree, error) {
	var out AddedWorktree
	if err := domain.ValidateWorktreePath(path); err != nil {
		return out, err
	}
	if err := domain.ValidateRefName(branch); err != nil {
		return out, fmt.Errorf("branch: %w", err)
	}
	if err := domain.ValidateRefName(startPoint); err != nil {
		return out, fmt.Errorf("start point: %w", err)
	}
	if err := requireEmptyOrMissing(path); err != nil {
		return out, err
	}

	exists, err := c.branchExists(ctx, repoRoot, branch)
	if err != nil {
		return out, err
	}
	args := []string{"worktree", "add", "--quiet"}
	if exists {
		args = append(args, path, branch)
	} else {
		args = append(args, "-b", branch, path, startPoint)
		out.BranchCreated = true
	}
	if _, err := c.write(ctx, repoRoot, args...); err != nil {
		var ge *gitError
		if errors.As(err, &ge) && (strings.Contains(ge.stderr, "already used by worktree") || strings.Contains(ge.stderr, "is already checked out")) {
			return AddedWorktree{}, fmt.Errorf("%s: %w", branch, ErrBranchCheckedOut)
		}
		return AddedWorktree{}, fmt.Errorf("create worktree: %w", err)
	}
	head, err := c.git(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return AddedWorktree{}, fmt.Errorf("worktree created, but its HEAD cannot be read: %w", err)
	}
	out.Head = head
	return out, nil
}

// RemoveWorktree implements Worktrees.
func (c *CLI) RemoveWorktree(ctx context.Context, repoRoot, path string) error {
	if err := domain.ValidateWorktreePath(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		_, err := c.write(ctx, repoRoot, "worktree", "prune")
		return err
	}
	if _, err := c.write(ctx, repoRoot, "worktree", "remove", "--force", path); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	_, err := c.write(ctx, repoRoot, "worktree", "prune")
	return err
}

// DeleteBranch implements Worktrees.
func (c *CLI) DeleteBranch(ctx context.Context, repoRoot, branch string) error {
	if err := domain.ValidateRefName(branch); err != nil {
		return fmt.Errorf("branch: %w", err)
	}
	if _, err := c.write(ctx, repoRoot, "branch", "-D", branch); err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}
	return nil
}

func (c *CLI) branchExists(ctx context.Context, repoRoot, branch string) (bool, error) {
	_, err := c.git(ctx, repoRoot, "rev-parse", "-q", "--verify", "refs/heads/"+branch)
	var ge *gitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &ge) && ge.code == 1 && ge.stderr == "":
		return false, nil
	}
	return false, fmt.Errorf("look up branch %s: %w", branch, err)
}

// requireEmptyOrMissing mirrors what Git demands, so the refusal names the
// real problem instead of surfacing as a half-made directory.
func requireEmptyOrMissing(path string) error {
	entries, err := os.ReadDir(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("%w: %s: %v", domain.ErrInvalid, path, err)
	case len(entries) > 0:
		return fmt.Errorf("%w: %s already exists and is not empty", domain.ErrConflict, path)
	}
	return nil
}

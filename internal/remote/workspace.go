package remote

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/runnerwire"
)

func git(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// Disable hooks, interactive credential prompts, remote helpers and unsafe
	// local/exec transports. Tests can inject a local bare origin in their Git CLI.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	data, e := cmd.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("git %s failed", args[0])
	}
	return strings.TrimSpace(string(data)), nil
}

// prepare never changes the user's checkout. Fetches remote refs, verifies
// repository identity, and creates a unique branch/worktree on this machine.
func (w *Worker) prepare(ctx context.Context, j runnerwire.Job) (string, string, string, error) {
	gitCommand := git
	if w.Git != nil {
		gitCommand = w.Git
	}
	if domain.ValidateRefName(j.TargetBranch) != nil {
		return "", "", "", domain.ErrInvalid
	}
	w.mu.Lock()
	root := w.Bindings[j.Run.ProjectID]
	w.mu.Unlock()
	if root == "" {
		if !w.AllowClone || !j.AllowClone || !runnerwire.SafeRemote(j.RemoteURL) {
			return "", "", "", fmt.Errorf("repository not bound; use devboard runner repo %s <clone>", j.Run.ProjectID)
		}
		root = filepath.Join(w.Dir, "repositories", j.Run.ProjectID)
		if e := os.MkdirAll(filepath.Dir(root), 0700); e != nil {
			return "", "", "", e
		}
		if _, e := os.Stat(root); os.IsNotExist(e) {
			if _, e := gitCommand(ctx, "", "clone", "--no-recurse-submodules", "--", j.RemoteURL, root); e != nil {
				return "", "", "", e
			}
		}
	}
	repo, e := (&gitrepo.CLI{}).Inspect(ctx, root)
	if e != nil {
		return "", "", "", e
	}
	root = repo.RootPath
	if j.RemoteURL != "" {
		actual := ""
		for _, r := range repo.Remotes {
			if r.Name == "origin" {
				actual = r.URL
			}
		}
		if actual != j.RemoteURL {
			return "", "", "", fmt.Errorf("bound clone origin differs from the controller's authorized origin")
		}
		if _, e := gitCommand(ctx, root, "fetch", "--prune", "--no-recurse-submodules", "origin"); e != nil {
			return "", "", "", e
		}
	}
	ref := "refs/heads/" + j.TargetBranch
	if j.RemoteURL != "" {
		ref = "refs/remotes/origin/" + j.TargetBranch
	}
	base, e := gitCommand(ctx, root, "rev-parse", "--verify", ref+"^{commit}")
	if e != nil {
		return "", "", "", fmt.Errorf("target branch unavailable")
	}
	if j.ExpectedCommit != "" && j.ExpectedCommit != base {
		return "", "", "", fmt.Errorf("stale target: expected %s, remote now %s", j.ExpectedCommit, base)
	}
	start := base
	if j.PreviousBranch != "" {
		if domain.ValidateRefName(j.PreviousBranch) != nil {
			return "", "", "", domain.ErrInvalid
		}
		if !j.PreviousPublished {
			start, e = gitCommand(ctx, root, "rev-parse", "--verify", "refs/heads/"+j.PreviousBranch+"^{commit}")
		} else {
			e = domain.ErrNotFound
		}
		if e != nil {
			start, e = gitCommand(ctx, root, "rev-parse", "--verify", "refs/remotes/origin/"+j.PreviousBranch+"^{commit}")
		}
		if e != nil {
			return "", "", "", fmt.Errorf("previous work is not available here; commit and push its branch on the owning runner first")
		}
		if j.PreviousCommit != "" {
			if _, e := gitCommand(ctx, root, "merge-base", "--is-ancestor", j.PreviousCommit, start); e != nil {
				return "", "", "", fmt.Errorf("published branch does not contain the prior runner commit; push its latest work first")
			}
		}
		if _, e := gitCommand(ctx, root, "merge-base", "--is-ancestor", base, start); e != nil {
			return "", "", "", fmt.Errorf("previous branch is stale or diverged; reconcile with the fresh target before retry")
		}
		// Uncommitted work cannot move into a new unique worktree. Refuse loss.
		worktrees, e := (&gitrepo.CLI{}).ListWorktrees(ctx, root)
		if e != nil {
			return "", "", "", e
		}
		for _, wt := range worktrees {
			if wt.Branch == j.PreviousBranch {
				st, e := (&gitrepo.CLI{}).Status(ctx, wt.Path)
				if e != nil {
					return "", "", "", e
				}
				if st.Counts.Staged > 0 || st.Counts.Unstaged > 0 || st.Counts.Untracked > 0 || st.Counts.Conflicted > 0 {
					return "", "", "", fmt.Errorf("previous worktree has uncommitted work; commit it before continuing")
				}
			}
		}
	}
	branch := "devboard/" + j.Run.RunnerID + "/" + j.Run.ID
	path := filepath.Join(w.Dir, "worktrees", j.Run.ProjectID, j.Run.ID)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", "", "", e
	}
	// Do not reuse a branch after a partial launch or after a restart.
	if _, e := gitCommand(ctx, root, "rev-parse", "--verify", "refs/heads/"+branch); e == nil {
		return "", "", "", fmt.Errorf("run branch already exists; refusing duplicate launch")
	}
	if _, e := (&gitrepo.CLI{}).AddWorktree(ctx, root, path, branch, start); e != nil {
		return "", "", "", e
	}
	return path, branch, base, nil
}

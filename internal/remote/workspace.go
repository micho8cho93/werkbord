package remote

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/runnerwire"
)

func git(ctx context.Context, root string, args ...string) (string, error) {
	budget := 2 * time.Minute
	if len(args) > 0 && args[0] == "clone" {
		budget = 8 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	// Disable hooks, interactive credential prompts, remote helpers and unsafe
	// local/exec transports. Tests can inject a local bare origin in their Git CLI.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(agent.SanitizedEnv(os.Environ()), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	data, e := cmd.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("git %s failed", args[0])
	}
	return strings.TrimSpace(string(data)), nil
}

// prepare never changes the user's checkout. Fetches remote refs, verifies
// repository identity, and creates a unique branch/worktree on this machine.
func (w *Worker) prepare(ctx context.Context, j runnerwire.Job) (string, string, string, error) {
	w.progress(j.Run.ID, "Preparing repository on runner")
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
			w.progress(j.Run.ID, "Cloning authorized repository; allowing up to eight minutes")
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
		w.progress(j.Run.ID, "Fetching origin and verifying target branch")
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
	branch := j.WorkBranch
	if branch == "" {
		branch = "devboard/" + j.Run.RunnerID + "/" + j.Run.ID
	}
	if domain.ValidateRefName(branch) != nil {
		return "", "", "", domain.ErrInvalid
	}
	path := filepath.Join(w.Dir, "worktrees", j.Run.ProjectID, j.Run.ID)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", "", "", e
	}
	// Do not reuse a branch after a partial launch or after a restart.
	if _, e := gitCommand(ctx, root, "rev-parse", "--verify", "refs/heads/"+branch); e == nil && j.WorkBranch == "" {
		return "", "", "", fmt.Errorf("run branch already exists; refusing duplicate launch")
	}
	if j.WorkBranch != "" {
		if head, e := gitCommand(ctx, root, "rev-parse", "--verify", "refs/heads/"+branch); e == nil {
			if _, e = gitCommand(ctx, root, "merge-base", "--is-ancestor", base, head); e != nil {
				return "", "", "", fmt.Errorf("intended branch is stale or diverged; reconcile it with the target first")
			}
		}
	}
	if j.WorkBranch != "" {
		worktrees, e := (&gitrepo.CLI{}).ListWorktrees(ctx, root)
		if e != nil {
			return "", "", "", e
		}
		for _, wt := range worktrees {
			if wt.Branch != branch {
				continue
			}
			w.mu.Lock()
			owned := false
			for _, record := range w.records {
				if sameWorktreePath(record.Path, wt.Path) && sameWorktreePath(record.Path, filepath.Join(w.Dir, "worktrees", record.Job.Run.ProjectID, record.Job.Run.ID)) && record.Job.Run.ProjectID == j.Run.ProjectID && record.Phase == "ended" {
					owned = true
					break
				}
			}
			w.mu.Unlock()
			if !owned {
				return "", "", "", fmt.Errorf("intended branch is checked out in a worktree not owned by a completed journal; inspect it locally")
			}
			if _, e := os.Stat(wt.Path); !os.IsNotExist(e) {
				st, e := (&gitrepo.CLI{}).Status(ctx, wt.Path)
				if e != nil {
					return "", "", "", e
				}
				if st.Counts.Staged+st.Counts.Unstaged+st.Counts.Untracked+st.Counts.Conflicted > 0 {
					return "", "", "", fmt.Errorf("commit or preserve the prior worktree changes before continuing")
				}
			}
			if e := (&gitrepo.CLI{}).RemoveWorktree(ctx, root, wt.Path); e != nil {
				return "", "", "", e
			}
		}
	}
	w.progress(j.Run.ID, "Creating worktree for "+branch)
	if _, e := (&gitrepo.CLI{}).AddWorktree(ctx, root, path, branch, start); e != nil {
		return "", "", "", e
	}
	return path, branch, base, nil
}

func (w *Worker) progress(id, message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.records[id]
	if r == nil {
		return
	}
	ev := agent.Event{Kind: agent.KindOutput, Stream: domain.StreamSystem, Text: message + "\n"}
	_ = w.appendLocked(r, runnerwire.Observation{Kind: "event", Event: &ev})
}

// Compare canonical parents as well as existing directories so a deleted
// worktree can be recovered on systems with /tmp or /var symlink aliases.
func sameWorktreePath(a, b string) bool {
	canonical := func(path string) string {
		if full, e := filepath.EvalSymlinks(path); e == nil {
			return full
		}
		if parent, e := filepath.EvalSymlinks(filepath.Dir(path)); e == nil {
			return filepath.Join(parent, filepath.Base(path))
		}
		return filepath.Clean(path)
	}
	return a != "" && b != "" && canonical(a) == canonical(b)
}

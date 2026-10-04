package runner

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
)

// workspace is the worktree a run will work in.
type workspace struct {
	wt *domain.Worktree
	// created is set when this call made the worktree (rather than reusing the
	// task's earlier one), so a failure further on can take it away again.
	created       bool
	branchCreated bool
	repoRoot      string
}

// prepareWorkspace finds or makes the Git worktree for a task.
//
// A task keeps one worktree across its runs, so a retry, a follow-up or a
// resumed session continues from the files the agent already changed. If the
// directory has gone, the record of it is retired and a fresh worktree is made
// on the same branch, which still holds whatever was committed.
//
// Making one follows the order the worktree records require: record first, then
// the directory, so a crash leaves a record with no directory (harmless) and
// never a directory nobody knows about.
func (m *Manager) prepareWorkspace(ctx context.Context, project *service.ProjectDetail, task *domain.Task, prior []domain.Run) (*workspace, error) {
	// A fresh look at the repository: it may have moved on, or gone.
	fresh, err := m.opt.Projects.Refresh(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	repo := fresh.Repository
	if repo == nil || repo.HeadCommit == "" {
		return nil, fmt.Errorf("%w: %s has no commits yet; make an initial commit before running an agent in it", domain.ErrInvalid, project.Name)
	}

	if last := latestWorkspaceRun(prior); last != nil && last.Remote {
		return m.workspaceFromRemote(ctx, project, task, *last, repo)
	}

	for i := len(prior) - 1; i >= 0; i-- {
		if prior[i].WorktreeID == "" {
			continue
		}
		w, err := m.opt.Worktrees.Get(ctx, prior[i].WorktreeID)
		if err != nil {
			return nil, err
		}
		if w.State != domain.WorktreeActive {
			continue
		}
		if !w.Removing() && dirExists(w.Path) {
			return &workspace{wt: w, repoRoot: repo.RootPath}, nil
		}
		// Gone from disk, or a removal that never finished: put the record to rest.
		m.retire(ctx, repo.RootPath, w, false)
		break
	}

	branch := branchName(task)
	path := filepath.Join(m.opt.WorktreeRoot, project.ID, worktreeDirName(task))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create the worktree directory: %w", err)
	}
	base := repo.CurrentBranch
	if base == "" {
		base = repo.HeadCommit
	}
	wt, err := m.opt.Worktrees.Create(ctx, service.NewWorktree{ProjectID: project.ID, Path: path, Branch: branch, BaseRef: base})
	if err != nil {
		return nil, err
	}
	added, err := m.opt.Git.AddWorktree(ctx, repo.RootPath, path, branch, repo.HeadCommit)
	if err != nil {
		m.retire(ctx, repo.RootPath, wt, false)
		return nil, err
	}
	m.log().Info("worktree created", "worktree", wt.ID, "task", task.ID, "branch", branch, "path", path, "newBranch", added.BranchCreated)
	return &workspace{wt: wt, created: true, branchCreated: added.BranchCreated, repoRoot: repo.RootPath}, nil
}

// workspaceFromRemote never falls back to an older local task checkout.
func (m *Manager) workspaceFromRemote(ctx context.Context, project *service.ProjectDetail, task *domain.Task, last domain.Run, repo *domain.GitRepository) (*workspace, error) {
	if last.Uncommitted == nil || *last.Uncommitted || last.HeadCommit == "" {
		return nil, fmt.Errorf("%w: previous runner work is uncommitted or unknown; reconcile it on its owning runner before changing machines", domain.ErrConflict)
	}
	if domain.ValidateRefName(last.Branch) != nil {
		return nil, domain.ErrInvalid
	}
	reader := &gitrepo.CLI{}
	start, e := reader.ResolveCommit(ctx, repo.RootPath, "refs/remotes/origin/"+last.Branch)
	if e != nil {
		return nil, e
	}
	if start == "" {
		return nil, fmt.Errorf("%w: previous runner branch unavailable; commit, push, and fetch it before changing machines", domain.ErrConflict)
	}
	preserved, e := reader.IsAncestor(ctx, repo.RootPath, last.HeadCommit, start)
	if e != nil || !preserved {
		return nil, fmt.Errorf("%w: published branch does not contain the prior runner commit; push its latest work first", domain.ErrConflict)
	}
	target, e := reader.DetectTarget(ctx, repo.RootPath)
	if e != nil {
		return nil, e
	}
	base, e := reader.ResolveCommit(ctx, repo.RootPath, "refs/remotes/origin/"+target.Name)
	if e != nil {
		return nil, e
	}
	if base == "" {
		return nil, fmt.Errorf("%w: fresh shared target unavailable", domain.ErrConflict)
	}
	current, e := reader.IsAncestor(ctx, repo.RootPath, base, start)
	if e != nil {
		return nil, e
	}
	if !current {
		return nil, fmt.Errorf("%w: previous runner branch is stale; reconcile with the fresh shared target", domain.ErrConflict)
	}
	branch := branchName(task) + "-from-" + idTail(last.ID)
	path := filepath.Join(m.opt.WorktreeRoot, project.ID, worktreeDirName(task))
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	wt, e := m.opt.Worktrees.Create(ctx, service.NewWorktree{ProjectID: project.ID, Path: path, Branch: branch, BaseRef: "origin/" + last.Branch})
	if e != nil {
		return nil, e
	}
	added, e := m.opt.Git.AddWorktree(ctx, repo.RootPath, path, branch, start)
	if e != nil {
		m.retire(ctx, repo.RootPath, wt, false)
		return nil, e
	}
	return &workspace{wt: wt, created: true, branchCreated: added.BranchCreated, repoRoot: repo.RootPath}, nil
}

// retire removes a worktree the controller made and no longer needs, in the
// order its records demand: begin removal (refused while a run uses it), delete
// the directory, finish. If a step fails the record is left as it is, with its
// path and branch still reserved, for a later attempt; nothing is deleted that
// the records do not vouch for. With deleteBranch the branch goes too, which
// is only right for one created a moment ago, with nothing committed on it.
func (m *Manager) retire(ctx context.Context, repoRoot string, w *domain.Worktree, deleteBranch bool) {
	ctx = context.WithoutCancel(ctx)
	if !w.Removing() {
		got, err := m.opt.Worktrees.BeginRemoval(ctx, w.ID, w.Version)
		if err != nil {
			m.log().Warn("cannot begin removing worktree", "worktree", w.ID, "err", err)
			return
		}
		w = got
	}
	if err := m.opt.Git.RemoveWorktree(ctx, repoRoot, w.Path); err != nil {
		m.log().Warn("cannot remove worktree directory", "worktree", w.ID, "path", w.Path, "err", err)
		return
	}
	if deleteBranch {
		if err := m.opt.Git.DeleteBranch(ctx, repoRoot, w.Branch); err != nil {
			m.log().Warn("cannot delete branch of discarded worktree", "branch", w.Branch, "err", err)
		}
	}
	if _, err := m.opt.Worktrees.FinishRemoval(ctx, w.ID, w.Version); err != nil && !errors.Is(err, domain.ErrConflict) {
		m.log().Warn("cannot record worktree removal", "worktree", w.ID, "err", err)
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// branchName is the branch a task's work happens on. It is the same every time
// for a task, so work committed in an earlier run is found again, and the task
// ID's tail keeps two tasks with similar titles apart.
func branchName(task *domain.Task) string {
	return "devboard/" + slug(task.Title) + "-" + idTail(task.ID)
}

// worktreeDirName is unique on every call: a worktree's path is never reused,
// even after its record is retired.
func worktreeDirName(task *domain.Task) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("runner: crypto/rand failed: " + err.Error())
	}
	return slug(task.Title) + "-" + idTail(task.ID) + "-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))[:6]
}

// idTail is the last few characters of an ID such as "tsk_k3j9x2m4q7p1a8z5".
func idTail(id string) string {
	if len(id) > 6 {
		return id[len(id)-6:]
	}
	return id
}

// slug reduces a title to a short, lowercase, filesystem- and ref-safe word run.
func slug(title string) string {
	var sb strings.Builder
	dash := true
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			dash = false
		case !dash:
			sb.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(sb.String(), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		return "task"
	}
	return s
}

// buildPrompt is the first message of a run: the task, then anything the user
// added when starting it.
func buildPrompt(task *domain.Task, instructions string, resuming bool) string {
	instructions = strings.TrimSpace(instructions)
	if resuming {
		if instructions == "" {
			return "Continue where you left off."
		}
		return instructions
	}
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(task.Title))
	if d := strings.TrimSpace(task.Description); d != "" {
		sb.WriteString("\n\n")
		sb.WriteString(d)
	}
	if instructions != "" {
		sb.WriteString("\n\n")
		sb.WriteString(instructions)
	}
	sb.WriteString(`

When ending implementation or review, provide a compact handoff report. Include only evidence you actually have. Use <devboard-handoff> followed by a JSON object and </devboard-handoff>, with keys summary (string), decisions (string array), tests (string array with commands and results), results (string), knownIssues (string array), blockers (string array), questions (string array), nextAction (string). Do not invent test results or answers. Git evidence is captured by the controller.`)
	return sb.String()
}

// A failed setup has no workspace and must not hide work from an earlier run.
func latestWorkspaceRun(prior []domain.Run) *domain.Run {
	for i := len(prior) - 1; i >= 0; i-- {
		if prior[i].Remote && prior[i].Branch != "" || !prior[i].Remote && prior[i].WorktreeID != "" {
			return &prior[i]
		}
	}
	return nil
}

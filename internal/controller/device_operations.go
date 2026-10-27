package controller

import (
	"context"
	"fmt"
	"path/filepath"

	"werkbord/internal/clientaccess"
	"werkbord/internal/deviceops"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/gitrepo"
	"werkbord/internal/service"
	"werkbord/internal/store"
)

func (c *Controller) newDeviceOperations(deps service.Deps, runners *service.Runners, projects *service.Projects, runs *service.Runs, worktrees *service.Worktrees, git *service.GitControl, clients *clientaccess.Store) *service.DeviceOperations {
	actorAllowed := func(id string) bool {
		for _, client := range clients.List() {
			if client.ID == id {
				return true
			}
		}
		return false
	}
	ops := &service.DeviceOperations{Deps: deps, Runners: runners, LocalID: runners.LocalID, ActorAllowed: actorAllowed}
	executor := &deviceops.Executor{Dir: filepath.Join(c.cfg.DataDir, "device-operations"), RunnerID: runners.LocalID, Terminals: c.terminals, Git: &gitrepo.CLI{}, Lock: git.LockExecution}
	executor.Authorize = func(ctx context.Context, op devicewire.Operation) error {
		return ops.AuthorizeOperation(ctx, op)
	}
	executor.Resolve = func(ctx context.Context, op devicewire.Operation) (string, error) {
		p, err := projects.Get(ctx, op.Request.ProjectID)
		if err != nil {
			return "", err
		}
		if p.Inactive() || p.Repository == nil {
			return "", domain.ErrForbidden
		}
		root := p.RepoPath
		if op.Request.RunID != "" {
			run, err := runs.Get(ctx, op.Request.RunID)
			if err != nil {
				return "", err
			}
			if run.ProjectID != p.ID || run.Remote || !run.State.Terminal() || run.WorktreeID == "" {
				return "", domain.ErrForbidden
			}
			wt, err := worktrees.Get(ctx, run.WorktreeID)
			if err != nil {
				return "", err
			}
			root = wt.Path
		}
		actual, err := (&gitrepo.CLI{}).Inspect(ctx, root)
		if err != nil {
			return "", err
		}
		if actual.RootPath != root || actual.CommonDir != p.Repository.CommonDir {
			return "", fmt.Errorf("%w: registered repository changed", domain.ErrConflict)
		}
		return root, nil
	}
	executor.Busy = func(ctx context.Context, project string) bool {
		busy := false
		err := deps.Store.View(ctx, func(tx store.Tx) error {
			active, err := tx.Runs().ListActive(ctx)
			if err != nil {
				return err
			}
			for _, run := range active {
				if run.ProjectID == project && !run.Remote {
					busy = true
				}
			}
			return nil
		})
		for _, s := range c.terminals.List() {
			if s.ProjectID == project && s.State == "running" {
				busy = true
			}
		}
		return err != nil || busy
	}
	ops.Execute = executor.Run
	runners.Operations = ops
	return ops
}

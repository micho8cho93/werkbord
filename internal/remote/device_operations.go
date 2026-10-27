package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"werkbord/internal/agentcatalog"
	"werkbord/internal/deviceops"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/gitrepo"
	"werkbord/internal/terminal"
)

func (w *Worker) LockRepository(ctx context.Context, project string) (func(), error) {
	w.repoMu.Lock()
	if w.repoLocks == nil {
		w.repoLocks = map[string]chan struct{}{}
	}
	lock := w.repoLocks[project]
	if lock == nil {
		lock = make(chan struct{}, 1)
		w.repoLocks[project] = lock
	}
	w.repoMu.Unlock()
	select {
	case lock <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-lock }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (w *Worker) operationAllowed(_ context.Context, op devicewire.Operation) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.paused || w.disabled || w.Identity.RequiresRecovery || !w.now().Before(w.lease) || !contains(w.Identity.Projects, op.Request.ProjectID) {
		return domain.ErrForbidden
	}
	return nil
}
func (w *Worker) operationRoot(ctx context.Context, op devicewire.Operation) (string, error) {
	w.mu.Lock()
	root := w.Bindings[op.Request.ProjectID]
	if op.Request.RunID != "" {
		r := w.records[op.Request.RunID]
		if r == nil || r.Job.Run.ProjectID != op.Request.ProjectID || r.Phase != "ended" || r.Path == "" {
			w.mu.Unlock()
			return "", domain.ErrForbidden
		}
		root = r.Path
	}
	w.mu.Unlock()
	if root == "" {
		return "", fmt.Errorf("%w: bind this project on its owning runner first", domain.ErrInvalid)
	}
	repo, err := (&gitrepo.CLI{}).Inspect(ctx, root)
	if err != nil {
		return "", err
	}
	if repo.RootPath != root {
		return "", domain.ErrForbidden
	}
	if op.RemoteURL != "" {
		actual := ""
		for _, r := range repo.Remotes {
			if r.Name == "origin" {
				actual = r.URL
			}
		}
		if actual != op.RemoteURL {
			return "", fmt.Errorf("%w: clone differs from the authorised repository", domain.ErrForbidden)
		}
	}
	return root, nil
}
func (w *Worker) terminalDirectory(ctx context.Context, project, agent string) (terminal.Directory, error) {
	op := devicewire.Operation{Request: devicewire.Request{ProjectID: project}}
	if err := w.operationAllowed(ctx, op); err != nil {
		return terminal.Directory{}, err
	}
	var prefs map[string]domain.AgentSelectionPreference
	raw, err := os.ReadFile(filepath.Join(w.Dir, "terminal-permissions.json"))
	if err != nil {
		return terminal.Directory{}, fmt.Errorf("%w: authorise native terminals locally with werkbord runner terminals", domain.ErrForbidden)
	}
	if json.Unmarshal(raw, &prefs) != nil || !prefs[agent].DirectCLI || !agentcatalog.NativeTerminal(agent) {
		return terminal.Directory{}, domain.ErrForbidden
	}
	a, err := w.Agents.Get(agent)
	if err != nil {
		return terminal.Directory{}, err
	}
	detected := a.Detect(ctx)
	if !detected.Available || !detected.Installed || detected.SignIn == domain.SignedOut || detected.SignIn != domain.SignedIn && !prefs[agent].CLISignedIn {
		return terminal.Directory{}, domain.ErrForbidden
	}
	root, err := w.operationRoot(ctx, op)
	if err != nil {
		return terminal.Directory{}, err
	}
	repo, err := (&gitrepo.CLI{}).Inspect(ctx, root)
	if err != nil {
		return terminal.Directory{}, err
	}
	return terminal.Directory{Root: root, CommonDir: repo.CommonDir, Name: project}, nil
}
func (w *Worker) initDeviceOperations() error {
	w.opResults = map[string]devicewire.Result{}
	w.activeOps = map[string]bool{}
	manager, err := terminal.New(terminal.Options{IDPrefix: w.Identity.RunnerID + "_", Device: w.Identity.RunnerID, StatePath: filepath.Join(w.Dir, "terminals.json"), Agents: w.Agents, Commands: w.TerminalCommands, Resolve: w.terminalDirectory, Authorized: func(context.Context) bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return !w.disabled && !w.Identity.RequiresRecovery && w.now().Before(w.lease)
	}})
	if err != nil {
		return err
	}
	w.Operations = &deviceops.Executor{Dir: w.Dir, RunnerID: w.Identity.RunnerID, Git: &gitrepo.CLI{}, Terminals: manager, Resolve: w.operationRoot, Authorize: w.operationAllowed, Lock: w.LockRepository, Now: w.now, Busy: func(ctx context.Context, project string) bool {
		w.mu.Lock()
		busy := false
		for _, r := range w.records {
			if r.Job.Run.ProjectID == project && r.Phase != "ended" && r.Phase != "resolved" {
				busy = true
			}
		}
		w.mu.Unlock()
		for _, session := range manager.List() {
			if session.ProjectID == project && session.State == "running" {
				busy = true
			}
		}
		return busy
	}}
	return nil
}
func (w *Worker) dispatchOperationsLocked(ops []devicewire.Operation) {
	for _, op := range ops {
		if w.activeOps[op.ID] {
			continue
		}
		if _, ok := w.opResults[op.ID]; ok {
			continue
		}
		if len(w.activeOps) >= 8 || len(w.opResults) >= 64 {
			break
		}
		if op.Request.RunnerID != w.Identity.RunnerID || !strings.HasPrefix(op.ID, "op_") {
			continue
		}
		w.activeOps[op.ID] = true
		w.wg.Add(1)
		go func(op devicewire.Operation) {
			defer w.wg.Done()
			result := w.Operations.Run(context.Background(), op)
			w.mu.Lock()
			defer w.mu.Unlock()
			delete(w.activeOps, op.ID)
			if result.State != "pending" {
				w.opResults[op.ID] = result
			}
		}(op)
	}
}

package remote

import (
	"context"
	"sort"
	"time"

	"devboard/internal/gitrepo"
	"devboard/internal/runnerwire"
)

// Retained worktrees can be committed after execution ends. Report their Git
// state through the same owned, signed journal so continuation can use it.
// A bounded round-robin scan avoids delaying heartbeats as history grows.
func (w *Worker) refreshWorkspaces(ctx context.Context) {
	w.mu.Lock()
	ids := []string{}
	for id, r := range w.records {
		if r.Phase == "ended" && r.Path != "" && contains(w.Identity.Projects, r.Job.Run.ProjectID) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	paths := map[string]string{}
	for n := 0; n < len(ids) && n < 8; n++ {
		id := ids[(w.workspaceCursor+n)%len(ids)]
		paths[id] = w.records[id].Path
	}
	if len(ids) > 0 {
		w.workspaceCursor = (w.workspaceCursor + len(paths)) % len(ids)
	}
	w.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for id, path := range paths {
		if ctx.Err() != nil {
			break
		}
		head, dirty := inspectWorkspace(ctx, path)
		w.mu.Lock()
		r := w.records[id]
		if r.Phase == "ended" && (head != r.WorkspaceHead || !sameDirty(dirty, r.WorkspaceDirty)) {
			r.WorkspaceHead, r.WorkspaceDirty = head, dirty
			_ = w.appendLocked(r, runnerwire.Observation{Kind: "workspace", HeadCommit: head, Uncommitted: dirty})
		}
		w.mu.Unlock()
	}
}

func inspectWorkspace(ctx context.Context, path string) (string, *bool) {
	reader := &gitrepo.CLI{}
	head, err := reader.HeadCommit(ctx, path)
	if err != nil || head == "" {
		return "", nil
	}
	state, err := reader.Status(ctx, path)
	if err != nil {
		return head, nil
	}
	dirty := state.Counts.Staged+state.Counts.Unstaged+state.Counts.Untracked+state.Counts.Conflicted > 0
	return head, &dirty
}

func sameDirty(a, b *bool) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

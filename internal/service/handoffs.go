package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
)

type Handoffs struct {
	Deps
	Git gitrepo.Reader
}

func basicHandoff(ctx context.Context, tx store.Tx, r *domain.Run, now time.Time) (*domain.Handoff, error) {
	task, err := tx.Tasks().Get(ctx, r.TaskID)
	if err != nil {
		return nil, err
	}
	h := &domain.Handoff{Objective: task.Title + "\n" + task.Description, Summary: "Implementation details have not been reported.", FilesChanged: []string{}, Decisions: []string{}, Tests: []string{}, Results: "Run state: " + string(r.State), GitState: "Repository evidence has not been captured.", KnownIssues: []string{}, Blockers: []string{}, Questions: []string{}, NextAction: "Inspect the work and record tests before continuing.", GeneratedAt: now}
	if r.Reason != "" {
		h.KnownIssues = append(h.KnownIssues, r.Reason)
	}
	if r.Blocker != nil {
		h.Blockers = append(h.Blockers, r.Blocker.Summary, r.Blocker.Detail)
	}
	questions, err := tx.Questions().ListByRun(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	for _, q := range questions {
		if q.Answer == "" {
			h.Questions = append(h.Questions, q.Prompt)
		}
	}
	evs, err := tx.Events().ListByRun(ctx, r.ID, 0, 200)
	if err != nil {
		return nil, err
	}
	// Retain one final assistant report, not the transcript. Structured output
	// may be split into multiple streaming events; reconstruct only a bounded
	// tail and use a tagged report only when it ends that tail.
	latest, tail := "", ""
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != domain.EventAgentOutput {
			continue
		}
		var out domain.AgentOutput
		if json.Unmarshal(evs[i].Payload, &out) != nil || out.Stream != domain.StreamAssistant {
			continue
		}
		if latest == "" && strings.TrimSpace(out.Text) != "" {
			latest = out.Text
		}
		tail = out.Text + tail
		if len(tail) > 32000 {
			tail = tail[len(tail)-32000:]
			break
		}
		if strings.Contains(tail, "<devboard-handoff>") {
			break
		}
	}
	if latest != "" {
		h.Summary = "Latest agent report (unverified):\n" + truncate(latest, 8000)
	}
	if start := strings.LastIndex(tail, "<devboard-handoff>"); start >= 0 {
		raw := tail[start+len("<devboard-handoff>"):]
		if end := strings.Index(raw, "</devboard-handoff>"); end >= 0 && strings.TrimSpace(raw[end+len("</devboard-handoff>"):]) == "" {
			var report domain.Handoff
			if json.Unmarshal([]byte(raw[:end]), &report) == nil {
				if report.Summary != "" {
					h.Summary = "Agent report (unverified):\n" + report.Summary
				}
				h.Decisions = report.Decisions
				h.Tests = report.Tests
				if report.Results != "" {
					h.Results += "\nAgent-reported results: " + report.Results
				}
				h.KnownIssues = append(h.KnownIssues, report.KnownIssues...)
				h.Blockers = append(h.Blockers, report.Blockers...)
				h.Questions = append(h.Questions, report.Questions...)
				if report.NextAction != "" {
					h.NextAction = report.NextAction
				}
			}
		}
	}
	if r.Handoff != nil && r.Handoff.Objective != "" {
		h.Objective = r.Handoff.Objective
	}
	h.Normalize()
	return h, nil
}

func (s *Handoffs) Generate(ctx context.Context, runID string) (*domain.Run, error) {
	var r *domain.Run
	var h *domain.Handoff
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		r, e = tx.Runs().Get(ctx, runID)
		if e != nil {
			return e
		}
		h, e = basicHandoff(ctx, tx, r, s.now())
		return e
	})
	if err != nil {
		return nil, err
	}
	if r.State.Active() {
		return nil, fmt.Errorf("%w: finish or stop the session before capturing its handoff", domain.ErrConflict)
	}
	if r.Handoff != nil {
		h = r.Handoff
	}
	if s.Git != nil && r.WorktreeID != "" {
		var wt *domain.Worktree
		var project *domain.Project
		err = s.Store.View(ctx, func(tx store.Tx) error {
			var e error
			wt, e = tx.Worktrees().Get(ctx, r.WorktreeID)
			if e != nil {
				return e
			}
			project, e = tx.Projects().Get(ctx, r.ProjectID)
			return e
		})
		if err != nil {
			return nil, err
		}
		if wt.State != domain.WorktreeActive || wt.Removing() {
			h.GitState = "Worktree was removed; only the recorded handoff is available."
		} else {
			st, e := s.Git.Status(ctx, wt.Path)
			if e != nil {
				h.GitState = "Cannot inspect worktree: " + e.Error()
			} else {
				h.GitState = fmt.Sprintf("Branch: %s; HEAD: %s; staged: %d; unstaged: %d; untracked: %d; conflicts: %d; operation: %s", wt.Branch, st.Head, st.Counts.Staged, st.Counts.Unstaged, st.Counts.Untracked, st.Counts.Conflicted, st.Operation)
				paths := map[string]bool{}
				for _, list := range [][]domain.GitFileChange{st.Staged, st.Unstaged, st.Untracked, st.Conflicted} {
					for _, f := range list {
						paths[f.Path] = true
						if f.OldPath != "" {
							paths[f.OldPath] = true
						}
					}
				}
				target, e := s.Git.DetectTarget(ctx, project.RepoPath)
				if e != nil {
					return nil, e
				}
				if target.Sha != "" && st.Head != "" {
					base, e := s.Git.MergeBase(ctx, project.RepoPath, target.Sha, st.Head)
					if e != nil {
						return nil, e
					}
					if base != "" {
						files, cut, e := s.Git.DiffFiles(ctx, project.RepoPath, base, st.Head, 1000)
						if e != nil {
							return nil, e
						}
						for _, f := range files {
							paths[f.Path] = true
							if f.OldPath != "" {
								paths[f.OldPath] = true
							}
						}
						if cut {
							h.GitState += "; changed-file listing truncated"
						}
					}
				}
				if st.Truncated {
					h.GitState += "; working-tree listing truncated"
				}
				h.FilesChanged = []string{}
				for p := range paths {
					h.FilesChanged = append(h.FilesChanged, p)
				}
				sort.Strings(h.FilesChanged)
			}
		}
	}
	// The handoff is derived from the finished run and its worktree, not from the run row, so a
	// version bump while the git work above was running (the runner's end-of-run bookkeeping)
	// must not discard it: save against the version now current, a few times at most.
	version := r.Version
	for attempt := 0; ; attempt++ {
		saved, err := s.Save(ctx, r.ID, version, *h)
		if err == nil || attempt >= 3 || !errors.Is(err, domain.ErrConflict) {
			return saved, err
		}
		var cur *domain.Run
		if e := s.Store.View(ctx, func(tx store.Tx) error {
			var e error
			cur, e = tx.Runs().Get(ctx, r.ID)
			return e
		}); e != nil {
			return nil, e
		}
		if cur.State.Active() || cur.Version == version {
			return saved, err
		}
		if cur.Handoff != nil {
			return cur, nil
		}
		version = cur.Version
	}
}
func (s *Handoffs) Save(ctx context.Context, id string, version int64, h domain.Handoff) (*domain.Run, error) {
	h.Normalize()
	b, e := json.Marshal(h)
	if e != nil {
		return nil, e
	}
	if len(b) > 32000 {
		return nil, fmt.Errorf("%w: handoff must fit in 32KB", domain.ErrInvalid)
	}
	var r *domain.Run
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var e error
		r, e = tx.Runs().Get(ctx, id)
		if e != nil {
			return e
		}
		if r.Version != version {
			return domain.ErrConflict
		}
		if r.State.Active() {
			return fmt.Errorf("%w: finish or stop the run before editing a handoff", domain.ErrConflict)
		}
		h.GeneratedAt = s.now()
		r.Handoff = &h
		r.UpdatedAt = s.now()
		if e := tx.Runs().Update(ctx, r); e != nil {
			return e
		}
		ev := newEvent(domain.EventRunStateChanged, map[string]any{"run": r, "from": r.State})
		runIDs(&ev, r)
		return em.emit(ev)
	})
	return r, err
}

// Context intentionally includes the artifact, bounded current diffs and only
// the context a person selected. No conversation replay or session resume.
func (s *Handoffs) Context(ctx context.Context, id, purpose, selected string) (string, error) {
	if purpose != "continue" && purpose != "review" && purpose != "fix" {
		return "", fmt.Errorf("%w: purpose must be continue, review or fix", domain.ErrInvalid)
	}
	if len(selected) > 16000 {
		return "", fmt.Errorf("%w: selected context exceeds 16KB", domain.ErrInvalid)
	}
	r, err := s.Generate(ctx, id)
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(r.Handoff, "", "  ")
	out := fmt.Sprintf("Handoff from run %s (%s, model %s). Purpose: %s. Treat prior reports and diffs as context to verify.\n%s\n\nIntentionally selected context:\n%s", r.ID, r.AgentID, r.Model, purpose, b, selected)
	if s.Git != nil && r.WorktreeID != "" {
		var wt *domain.Worktree
		var p *domain.Project
		err = s.Store.View(ctx, func(tx store.Tx) error {
			var e error
			wt, e = tx.Worktrees().Get(ctx, r.WorktreeID)
			if e != nil {
				return e
			}
			p, e = tx.Projects().Get(ctx, r.ProjectID)
			return e
		})
		if err != nil {
			return "", err
		}
		if wt.State != domain.WorktreeActive || wt.Removing() {
			return "", fmt.Errorf("%w: original worktree is unavailable; restore the work before continuing", domain.ErrConflict)
		}
		st, e := s.Git.Status(ctx, wt.Path)
		if e != nil {
			return "", e
		}
		target, e := s.Git.DetectTarget(ctx, p.RepoPath)
		if e != nil {
			return "", e
		}
		out += "\n\nUntracked file contents are excluded. Review them locally and include only selected, non-sensitive context explicitly."
		budget := 20000
		add := func(label string, d *domain.GitFileDiff) {
			if d == nil || budget <= 0 {
				return
			}
			raw, _ := json.Marshal(d)
			n := len(raw)
			if n > budget {
				n = budget
			}
			out += "\n\n" + label + " (bounded):\n" + truncate(string(raw), n)
			budget -= n
		}
		if target.Sha != "" && st.Head != "" {
			base, e := s.Git.MergeBase(ctx, p.RepoPath, target.Sha, st.Head)
			if e != nil {
				return "", e
			}
			if base != "" {
				d, e := s.Git.FileDiff(ctx, p.RepoPath, base, st.Head, nil, gitrepo.DiffWindow{Lines: 150})
				if e != nil {
					return "", e
				}
				add("Committed diff", d)
			}
		}
		for _, group := range []struct {
			kind  gitrepo.WorkingKind
			files []domain.GitFileChange
		}{{gitrepo.WorkingStaged, st.Staged}, {gitrepo.WorkingUnstaged, st.Unstaged}} {
			for i, f := range group.files {
				if i >= 5 || budget <= 0 {
					break
				}
				paths := []string{f.Path}
				if f.OldPath != "" {
					paths = append(paths, f.OldPath)
				}
				d, e := s.Git.WorkingDiff(ctx, wt.Path, paths, group.kind, gitrepo.DiffWindow{Lines: 100})
				if e != nil {
					return "", e
				}
				add(string(group.kind)+" diff: "+f.Path, d)
			}
		}
	}
	return out, nil
}

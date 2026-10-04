package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
)

func configureOrchestration(ctx context.Context, tx store.Tx, task *domain.Task, o domain.Orchestration) error {
	o.Normalize()
	if err := o.Validate(); err != nil {
		return err
	}
	tasks, err := tx.Tasks().ListByProject(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, t := range tasks {
		ids[t.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range o.Dependencies {
		if id == task.ID || !ids[id] || seen[id] {
			return fmt.Errorf("%w: dependencies must be distinct other tasks in this project", domain.ErrInvalid)
		}
		seen[id] = true
	}
	o.Key = ""
	o.RunID = ""
	o.DispatchedAt = nil
	o.Missed = false
	o.Error = ""
	if o.Enabled {
		o.Key = domain.NewID("schedule")
	}
	task.Orchestration = o
	replaced := false
	for i := range tasks {
		if tasks[i].ID == task.ID {
			tasks[i] = *task
			replaced = true
		}
	}
	if !replaced {
		tasks = append(tasks, *task)
	}
	if domain.DependencyCycle(tasks) {
		return fmt.Errorf("%w: dependency cycle", domain.ErrInvalid)
	}
	return nil
}

type OrchestrationSettings struct {
	ConcurrencyLimit int `json:"concurrencyLimit"`
}

type Scheduler struct {
	Runners *Runners
	Deps
	// Reader is optional in pure scheduling tests. The controller always wires it.
	Git gitrepo.Reader
}

func (s *Scheduler) Settings(ctx context.Context, projectID string) (OrchestrationSettings, error) {
	out := OrchestrationSettings{ConcurrencyLimit: 1}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, e := tx.Projects().Get(ctx, projectID); e != nil {
			return e
		}
		e := tx.Settings().Get(ctx, "orchestration:"+projectID, &out)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		return e
	})
	return out, err
}
func (s *Scheduler) SetSettings(ctx context.Context, projectID string, in OrchestrationSettings) error {
	if in.ConcurrencyLimit < 1 || in.ConcurrencyLimit > 16 {
		return fmt.Errorf("%w: concurrency limit must be 1–16", domain.ErrInvalid)
	}
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		if _, e := tx.Projects().Get(ctx, projectID); e != nil {
			return e
		}
		if e := tx.Settings().Set(ctx, "orchestration:"+projectID, in, s.now()); e != nil {
			return e
		}
		ev := newEvent(domain.EventSettingsUpdated, map[string]any{"orchestrationSettings": in})
		ev.ProjectID = projectID
		return em.emit(ev)
	})
}

type scheduleSnapshot struct {
	tasks     []domain.Task
	latest    map[string]domain.Run
	active    []domain.Run
	project   *domain.Project
	limit     int
	health    []domain.HealthFinding
	worktrees map[string]domain.Worktree
}

func scheduleRead(ctx context.Context, tx store.Tx, projectID string) (scheduleSnapshot, error) {
	x := scheduleSnapshot{latest: map[string]domain.Run{}, worktrees: map[string]domain.Worktree{}, limit: 1}
	var err error
	if x.project, err = tx.Projects().Get(ctx, projectID); err != nil {
		return x, err
	}
	if x.tasks, err = tx.Tasks().ListByProject(ctx, projectID); err != nil {
		return x, err
	}
	runs, err := tx.Runs().ListLatestByProject(ctx, projectID)
	if err != nil {
		return x, err
	}
	for _, r := range runs {
		x.latest[r.TaskID] = r
	}
	active, err := tx.Runs().ListActive(ctx)
	if err != nil {
		return x, err
	}
	for _, r := range active {
		if r.ProjectID == projectID {
			x.active = append(x.active, r)
		}
	}
	settings := OrchestrationSettings{ConcurrencyLimit: 1}
	if e := tx.Settings().Get(ctx, "orchestration:"+projectID, &settings); e != nil && !errors.Is(e, domain.ErrNotFound) {
		return x, e
	}
	x.limit = settings.ConcurrencyLimit
	if x.health, err = tx.Health().ListByProject(ctx, projectID, domain.HealthOpen); err != nil {
		return x, err
	}
	wt, err := tx.Worktrees().ListByProject(ctx, projectID)
	if err != nil {
		return x, err
	}
	for _, w := range wt {
		x.worktrees[w.ID] = w
	}
	return x, nil
}

func dependencyDecision(task domain.Task, x scheduleSnapshot) (string, string) {
	byID := map[string]domain.Task{}
	for _, t := range x.tasks {
		byID[t.ID] = t
	}
	waiting := ""
	for _, id := range task.Orchestration.Dependencies {
		dep, ok := byID[id]
		if !ok {
			return "blocked", "Dependency is missing: " + id
		}
		r, ran := x.latest[id]
		if ran && r.State.Active() {
			if r.State == domain.RunBlocked {
				return "blocked", "Dependency " + dep.Title + " is blocked"
			}
			if waiting == "" {
				waiting = "Waiting for " + dep.Title + " to finish"
			}
			continue
		}
		if dep.State == domain.TaskDone || ran && r.State == domain.RunCompleted {
			continue
		}
		if ran && (r.State == domain.RunFailed || r.State == domain.RunStopped) {
			return "blocked", "Dependency " + dep.Title + " is " + string(r.State) + ": " + r.Reason
		}
		if waiting == "" {
			waiting = "Waiting for " + dep.Title + " to complete successfully"
		}
	}
	if waiting != "" {
		return "waiting_dependency", waiting
	}
	return "", ""
}
func baseDecision(task domain.Task, x scheduleSnapshot, now time.Time, manual bool) domain.SchedulingDecision {
	d := domain.SchedulingDecision{TaskID: task.ID, State: "runnable", Reason: "Ready to execute"}
	o := task.Orchestration
	set := func(state, reason string) domain.SchedulingDecision { d.State = state; d.Reason = reason; return d }
	if r, ok := x.latest[task.ID]; ok && r.State.Active() {
		return set("queued", "An agent session is already active")
	}
	if task.State == domain.TaskDone {
		return set("blocked", "Task is in Done")
	}
	if !manual {
		if !o.Enabled {
			return set("queued", "Automatic execution is off")
		}
		if o.RunID != "" {
			return set("queued", "This one-shot schedule already dispatched run "+o.RunID)
		}
		if o.Missed {
			return set("blocked", "Schedule was marked missed; reschedule to rearm")
		}
		if o.Error != "" {
			return set("blocked", o.Error)
		}
	}
	if o.Deadline != nil && now.After(*o.Deadline) {
		return set("blocked", "Deadline passed; update the deadline to continue")
	}
	if !manual && o.ScheduledAt != nil && o.MissedPolicy == "skip" && now.After(o.ScheduledAt.Add(time.Duration(o.GraceSeconds)*time.Second)) {
		return set("blocked", "Schedule missed its allowed start window")
	}
	if state, reason := dependencyDecision(task, x); state != "" {
		return set(state, reason)
	}
	if !manual && o.ScheduledAt != nil && now.Before(*o.ScheduledAt) {
		return set("waiting_schedule", "Waiting for scheduled time")
	}
	if o.NotBefore != nil && now.Before(*o.NotBefore) {
		return set("waiting_schedule", "Waiting for not-before time")
	}

	for _, f := range x.health {
		if f.Severity == domain.HealthCritical {
			return set("blocked", "Repository health: "+f.Title)
		}
	}
	if len(x.active) >= x.limit {
		return set("waiting_capacity", fmt.Sprintf("Project concurrency limit (%d) is occupied", x.limit))
	}
	for _, r := range x.active {
		var other domain.Task
		for _, t := range x.tasks {
			if t.ID == r.TaskID {
				other = t
				break
			}
		}
		if len(o.ExpectedPaths) == 0 || len(other.Orchestration.ExpectedPaths) == 0 {
			return set("potentially_conflicting", "Active repository work has unknown file scope; execution is serialized")
		}
		if pathsOverlap(o.ExpectedPaths, other.Orchestration.ExpectedPaths) {
			return set("potentially_conflicting", "Potential file or subsystem overlap with "+other.Title)
		}
	}
	return d
}
func pathsOverlap(a, b []string) bool {
	for _, p := range a {
		for _, q := range b {
			p = strings.TrimSuffix(p, "/")
			q = strings.TrimSuffix(q, "/")
			if p == q || strings.HasPrefix(p, q+"/") || strings.HasPrefix(q, p+"/") {
				return true
			}
		}
	}
	return false
}

func (s *Scheduler) inspect(ctx context.Context, t domain.Task, x scheduleSnapshot) (string, string) {
	if s.Git == nil {
		return "", ""
	}
	status, err := s.Git.Status(ctx, x.project.RepoPath)
	if err != nil {
		return "blocked", "Cannot inspect repository: " + err.Error()
	}
	if status.Operation != "" || status.Counts.Conflicted > 0 {
		return "blocked", "Repository has an operation or unresolved conflicts"
	}
	if status.Truncated {
		return "blocked", "Repository inspection was truncated"
	}
	target, err := s.Git.DetectTarget(ctx, x.project.RepoPath)
	if err != nil {
		return "blocked", "Cannot inspect target: " + err.Error()
	}
	targetSHA := target.Sha
	if targetSHA == "" {
		targetSHA = status.Head
	}
	if targetSHA != "" && status.Head != "" {
		_, behind, e := s.Git.Divergence(ctx, x.project.RepoPath, status.Head, targetSHA)
		if e != nil {
			return "blocked", "Cannot inspect repository checkout ancestry: " + e.Error()
		}
		if behind > 0 {
			return "blocked", "Repository checkout is behind the target; reconcile it in Git before starting work"
		}
	}
	if t.Orchestration.TargetCommit != "" && t.Orchestration.TargetCommit != targetSHA {
		return "blocked", "Task is based on a stale target; inspect Git and update the expected target"
	}
	if old, ok := x.latest[t.ID]; ok && old.WorktreeID != "" {
		if w, ok := x.worktrees[old.WorktreeID]; ok && w.State == domain.WorktreeActive {
			st, e := s.Git.Status(ctx, w.Path)
			if e != nil {
				return "blocked", "Cannot inspect previous worktree: " + e.Error()
			}
			if st.Operation != "" || st.Counts.Conflicted > 0 {
				return "blocked", "Previous worktree has an operation or conflicts"
			}
			if targetSHA != "" {
				_, behind, e := s.Git.Divergence(ctx, x.project.RepoPath, "refs/heads/"+w.Branch, targetSHA)
				if e != nil {
					return "blocked", "Cannot inspect branch ancestry: " + e.Error()
				}
				if behind > 0 {
					return "blocked", "Previous task branch is behind the target; reconcile it in Git before continuing"
				}
			}
		}
	}
	for _, r := range x.active {
		w, ok := x.worktrees[r.WorktreeID]
		if !ok {
			return "potentially_conflicting", "Active run has no inspectable worktree"
		}
		st, e := s.Git.Status(ctx, w.Path)
		if e != nil || st.Truncated {
			return "potentially_conflicting", "Cannot determine active worktree changes"
		}
		if st.Operation != "" || st.Counts.Conflicted > 0 {
			return "potentially_conflicting", "Active worktree has a repository operation or conflicts"
		}
		paths := []string{}
		for _, list := range [][]domain.GitFileChange{st.Staged, st.Unstaged, st.Untracked, st.Conflicted} {
			for _, f := range list {
				paths = append(paths, f.Path)
				if f.OldPath != "" {
					paths = append(paths, f.OldPath)
				}
			}
		}
		if targetSHA != "" {
			base, e := s.Git.MergeBase(ctx, x.project.RepoPath, targetSHA, st.Head)
			if e != nil || base == "" {
				return "potentially_conflicting", "Cannot determine active branch base"
			}
			files, cut, e := s.Git.DiffFiles(ctx, x.project.RepoPath, base, st.Head, 1000)
			if e != nil || cut {
				return "potentially_conflicting", "Cannot determine active branch changes"
			}
			for _, f := range files {
				paths = append(paths, f.Path, f.OldPath)
			}
		}
		if pathsOverlap(t.Orchestration.ExpectedPaths, paths) {
			return "potentially_conflicting", "Observed active branch changes may overlap this task"
		}
	}
	return "", ""
}

// Plan is deterministic: due time, execution order, priority, creation, ID.
// Dependencies and capacity are gates, never synonyms for ordering or priority.
func (s *Scheduler) Plan(ctx context.Context, projectID string) ([]domain.SchedulingDecision, error) {
	var x scheduleSnapshot
	err := s.Store.View(ctx, func(tx store.Tx) error { var e error; x, e = scheduleRead(ctx, tx, projectID); return e })
	if err != nil {
		return nil, err
	}
	var global domain.ExecutionConfig
	err = s.Store.View(ctx, func(tx store.Tx) error {
		e := tx.Settings().Get(ctx, domain.SettingExecution, &global)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		return e
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(x.tasks, func(i, j int) bool {
		a, b := x.tasks[i], x.tasks[j]
		ao, bo := a.Orchestration, b.Orchestration
		at, bt := time.Time{}, time.Time{}
		if ao.ScheduledAt != nil {
			at = *ao.ScheduledAt
		}
		if bo.ScheduledAt != nil {
			bt = *bo.ScheduledAt
		}
		if !at.Equal(bt) {
			return at.Before(bt)
		}
		ai, bi := 1000001, 1000001
		if ao.ExecutionOrder != nil {
			ai = *ao.ExecutionOrder
		}
		if bo.ExecutionOrder != nil {
			bi = *bo.ExecutionOrder
		}
		if ai != bi {
			return ai < bi
		}
		rank := func(t domain.Task) int {
			p := domain.ResolveExecution(domain.Level{Config: t.Execution}, domain.Level{Config: x.project.Execution}, domain.Level{Config: global}).Priority
			if p == domain.PriorityHigh {
				return 0
			}
			if p == domain.PriorityLow {
				return 2
			}
			return 1
		}
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	out := []domain.SchedulingDecision{}
	slots := x.limit - len(x.active)
	selected := []domain.Task{}
	for _, t := range x.tasks {
		d := baseDecision(t, x, s.now(), false)
		if d.State == "runnable" {
			if state, reason := s.inspect(ctx, t, x); state != "" {
				d.State, d.Reason = state, reason
			}
		}

		if d.State == "runnable" && s.Runners != nil {
			resolved := domain.ResolveExecution(domain.Level{Source: domain.SourceTask, Config: t.Execution}, domain.Level{Source: domain.SourceProject, Config: x.project.Execution}, domain.Level{Source: domain.SourceGlobal, Config: global})
			if _, _, e := s.Runners.Route(ctx, &t, resolved); e != nil {
				d.State = "queued"
				d.Reason = e.Error()
			}
		}
		if d.State == "runnable" {
			if slots <= 0 {
				d.State, d.Reason = "queued", "Earlier runnable tasks have priority"
			} else {
				for _, other := range selected {
					if len(t.Orchestration.ExpectedPaths) == 0 || len(other.Orchestration.ExpectedPaths) == 0 || pathsOverlap(t.Orchestration.ExpectedPaths, other.Orchestration.ExpectedPaths) {
						d.State, d.Reason = "potentially_conflicting", "Earlier runnable task has overlapping or unknown file scope"
						break
					}
				}
				if d.State == "runnable" {
					slots--
					selected = append(selected, t)
				}
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// CheckStart rechecks gates just before setup. Manager holds the shared Git
// operation lock through setup, claim and launch, including manual starts.
func (s *Scheduler) CheckStart(ctx context.Context, taskID, key string) error {
	var x scheduleSnapshot
	var task *domain.Task
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		task, e = tx.Tasks().Get(ctx, taskID)
		if e != nil {
			return e
		}
		x, e = scheduleRead(ctx, tx, task.ProjectID)
		return e
	})
	if err != nil {
		return err
	}
	if key != "" && (task.Orchestration.Key != key || !task.Orchestration.Enabled) {
		return fmt.Errorf("%w: schedule was edited", domain.ErrConflict)
	}
	d := baseDecision(*task, x, s.now(), key == "")
	if d.State != "runnable" {
		return fmt.Errorf("%w: %s: %s", domain.ErrConflict, d.State, d.Reason)
	}
	if state, reason := s.inspect(ctx, *task, x); state != "" {
		return fmt.Errorf("%w: %s: %s", domain.ErrConflict, state, reason)
	}
	return nil
}
func (s *Scheduler) RecordDispatchError(ctx context.Context, taskID, key, reason string, missed bool) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		t, e := tx.Tasks().Get(ctx, taskID)
		if e != nil {
			return e
		}
		if t.Orchestration.Key != key || t.Orchestration.RunID != "" {
			return nil
		}
		if t.Orchestration.Error == reason && t.Orchestration.Missed == missed {
			return nil
		}
		t.Orchestration.Error = reason
		t.Orchestration.Missed = missed
		t.UpdatedAt = s.now()
		if e = tx.Tasks().Update(ctx, t); e != nil {
			return e
		}
		ev := newEvent(domain.EventTaskUpdated, t)
		ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
		return em.emit(ev)
	})
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/runnerwire"
	"devboard/internal/store"
)

func integrationSourceKey(source string) string {
	hash := sha256.Sum256([]byte(source))
	return "integration:source:" + hex.EncodeToString(hash[:])
}

// IntegrationProjects omits local paths, instructions, settings and Git snapshots.
func (s *Tasks) IntegrationProjects(ctx context.Context) (integration.Projects, error) {
	out := integration.Projects{Schema: integration.Schema, Projects: []integration.Project{}}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		ps, err := tx.Projects().List(ctx)
		if err != nil {
			return err
		}
		for _, p := range ps {
			v := integration.Project{ID: p.ID, Name: p.Name, Remotes: []string{}}
			repo, err := tx.Repositories().Get(ctx, p.ID)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			if err == nil {
				for _, r := range repo.Remotes {
					if _, err := integration.RepositoryIdentity(r.URL); err == nil {
						v.Remotes = append(v.Remotes, r.URL)
					}
				}
			}
			out.Projects = append(out.Projects, v)
		}
		return nil
	})
	return out, err
}

// Import exchanges text only. Provenance is bound globally to a project/task in
// the same transaction as creation, so a lost response or changed project choice
// cannot duplicate work. Runs and local edits are always preserved.
func (s *Tasks) Import(ctx context.Context, in integration.Import) (integration.Imported, error) {
	out := integration.Imported{Schema: integration.Schema}
	if in.Schema != integration.Schema || in.SourceRef == "" || len(in.SourceRef) > 2000 {
		return out, domain.ErrInvalid
	}
	want, err := integration.RepositoryIdentity(in.Repository)
	if err != nil {
		return out, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	title, err := domain.ValidateTaskTitle(in.Title)
	if err != nil {
		return out, err
	}
	if err := domain.ValidateTaskDescription(in.Description); err != nil {
		return out, err
	}
	for _, b := range []string{in.WorkBranch, in.BaseBranch} {
		if b != "" {
			if err := domain.ValidateRefName(b); err != nil {
				return out, err
			}
		}
	}
	if len(in.SourceAliases) > 16 {
		return out, domain.ErrInvalid
	}
	sources := append([]string{in.SourceRef}, in.SourceAliases...)
	for _, source := range sources {
		if source == "" || len(source) > 2000 {
			return out, domain.ErrInvalid
		}
	}
	key := integrationSourceKey(in.SourceRef)
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		if _, err := tx.Projects().Get(ctx, in.ProjectID); err != nil {
			return err
		}
		repo, err := tx.Repositories().Get(ctx, in.ProjectID)
		if err != nil {
			return err
		}
		matched := false
		for _, r := range repo.Remotes {
			got, e := integration.RepositoryIdentity(r.URL)
			matched = matched || e == nil && got == want
		}
		if !matched {
			return fmt.Errorf("%w: selected project does not match the repository", domain.ErrConflict)
		}
		var saved integration.Imported
		err = tx.Settings().Get(ctx, key, &saved)
		if err == nil && saved.ProjectID != in.ProjectID {
			return fmt.Errorf("%w: this source already belongs to project %s", domain.ErrConflict, saved.ProjectID)
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		var task *domain.Task
		if err == nil {
			task, err = tx.Tasks().Get(ctx, saved.TaskID)
			if err != nil {
				return err
			}
		} else {
			all, err := tx.Tasks().ListByProject(ctx, in.ProjectID)
			if err != nil {
				return err
			}
			for i := range all {
				for _, source := range sources {
					if all[i].SourceRef == source {
						if task != nil && task.ID != all[i].ID {
							return fmt.Errorf("%w: provenance matches multiple tasks; select the existing task explicitly", domain.ErrConflict)
						}
						task = &all[i]
					}
				}
			}
			if task == nil {
				for _, existing := range all {
					if in.WorkBranch != "" && existing.WorkBranch == in.WorkBranch {
						return fmt.Errorf("%w: work branch already belongs to another task", domain.ErrConflict)
					}
				}
			}

		}
		text := integration.Text{Title: title, Description: in.Description, WorkBranch: in.WorkBranch, BaseBranch: in.BaseBranch}
		if task == nil {
			pos, err := tx.Tasks().MaxPosition(ctx, in.ProjectID, domain.TaskBacklog)
			if err != nil {
				return err
			}
			now := s.now()
			task = &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: in.ProjectID, Title: title, Description: in.Description, SourceRef: in.SourceRef, WorkBranch: in.WorkBranch, BaseBranch: in.BaseBranch, State: domain.TaskBacklog, Position: pos + 1, CreatedAt: now, UpdatedAt: now}
			if err := tx.Tasks().Create(ctx, task); err != nil {
				return err
			}
			e := newEvent(domain.EventTaskCreated, task)
			e.ProjectID, e.TaskID = task.ProjectID, task.ID
			if err := em.emit(e); err != nil {
				return err
			}
		} else {
			current := integration.Text{Title: task.Title, Description: task.Description, WorkBranch: task.WorkBranch, BaseBranch: task.BaseBranch}
			if current != text {
				runs, err := tx.Runs().ListByTask(ctx, task.ID)
				if err != nil {
					return err
				}
				if in.Previous == nil || current != *in.Previous || len(runs) > 0 || task.State != domain.TaskBacklog || task.ArchivedAt != nil {
					out.Conflict = true
				} else {
					task.Title, task.Description, task.WorkBranch, task.BaseBranch = title, in.Description, in.WorkBranch, in.BaseBranch
					task.UpdatedAt = s.now()
					if err := tx.Tasks().Update(ctx, task); err != nil {
						return err
					}
					e := newEvent(domain.EventTaskUpdated, task)
					e.ProjectID, e.TaskID = task.ProjectID, task.ID
					if err := em.emit(e); err != nil {
						return err
					}
				}
			}
		}
		out.ProjectID, out.TaskID = task.ProjectID, task.ID
		for _, source := range sources {
			var bound integration.Imported
			err := tx.Settings().Get(ctx, integrationSourceKey(source), &bound)
			if err == nil && (bound.ProjectID != task.ProjectID || bound.TaskID != task.ID) {
				return fmt.Errorf("%w: provenance already belongs to another task", domain.ErrConflict)
			}
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			if err := tx.Settings().Set(ctx, integrationSourceKey(source), integration.Imported{Schema: integration.Schema, ProjectID: task.ProjectID, TaskID: task.ID}, s.now()); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

func executionMetadata(r domain.Run) integration.Execution {
	e := integration.Execution{RunID: r.ID, Branch: r.Branch, HeadCommit: r.HeadCommit, HandoffAvailable: r.Handoff != nil}
	switch r.State {
	case domain.RunStarting:
		e.State = "queued"
	case domain.RunRunning:
		e.State = "running"
	case domain.RunWaitingForUser:
		e.State = "needs_input"
	case domain.RunBlocked:
		e.State = "blocked"
	case domain.RunCompleted:
		e.State = "completed"
	case domain.RunFailed:
		e.State = "failed"
	case domain.RunStopped:
		e.State = "canceled"
	}
	if r.State != domain.RunStarting {
		at := r.CreatedAt
		e.StartedAt = &at
	}
	e.CompletedAt = r.EndedAt
	if r.State.Terminal() {
		e.Outcome = e.State
		e.Summary = &integration.CompletionSummary{Outcome: e.State, HandoffAvailable: e.HandoffAvailable}
		if r.EndedAt != nil {
			elapsed := max(int64(0), r.EndedAt.Sub(r.CreatedAt).Milliseconds())
			e.Summary.ElapsedMillis = &elapsed
		}
	}
	return e
}

// IntegrationSnapshot reads task/run/cursor together. Availability is computed
// from persisted ownership and heartbeat, without exposing hostnames or agents.
func (s *Tasks) IntegrationSnapshot(ctx context.Context, pid, tid string) (integration.Snapshot, error) {
	out := integration.Snapshot{Schema: integration.Schema, Execution: integration.Execution{State: "queued"}}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		t, err := tx.Tasks().Get(ctx, tid)
		if err != nil {
			return err
		}
		if t.ProjectID != pid {
			return domain.ErrNotFound
		}
		runs, err := tx.Runs().ListByTask(ctx, tid)
		if err != nil {
			return err
		}
		var latest *domain.Run
		for i := range runs {
			if latest == nil || runs[i].CreatedAt.After(latest.CreatedAt) || runs[i].CreatedAt.Equal(latest.CreatedAt) && runs[i].Attempt > latest.Attempt {
				latest = &runs[i]
			}
		}
		if latest != nil {
			out.Execution = executionMetadata(*latest)
		} else {
			out.Execution.Branch = t.WorkBranch
		}
		runners, err := tx.Runners().List(ctx)
		if err != nil {
			return err
		}
		active, err := tx.Runs().ListActive(ctx)
		if err != nil {
			return err
		}
		for _, r := range runners {
			if r.Disabled || r.Removed {
				continue
			}
			if latest != nil && latest.RunnerID != "" && r.ID != latest.RunnerID {
				continue
			}
			allowed := r.Kind == domain.RunnerLocal
			for _, p := range r.Projects {
				allowed = allowed || p == pid
			}
			if !allowed {
				continue
			}
			online := r.Kind == domain.RunnerLocal || !r.LastSeenAt.IsZero() && s.now().Sub(r.LastSeenAt) <= runnerwire.OnlineWindow
			count := 0
			for _, a := range active {
				if a.RunnerID == r.ID || a.RunnerID == "" && r.Kind == domain.RunnerLocal {
					count++
				}
			}
			out.Execution.RunnerOnline = out.Execution.RunnerOnline || online
			out.Execution.RunnerAvailable = out.Execution.RunnerAvailable || online && count < max(1, r.Capacity)
		}
		out.Cursor, err = tx.Events().LatestSeq(ctx)
		return err
	})
	return out, err
}

// IntegrationEvents pages the existing durable log, projecting lifecycle events
// onto the reviewed metadata type. No raw payload crosses this API.
func (s *Tasks) IntegrationEvents(ctx context.Context, pid, tid string, after int64) (integration.Feed, error) {
	out := integration.Feed{Schema: integration.Schema, Cursor: after, Events: []integration.Event{}}
	if after < 0 {
		return out, domain.ErrInvalid
	}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		t, err := tx.Tasks().Get(ctx, tid)
		if err != nil {
			return err
		}
		if t.ProjectID != pid {
			return domain.ErrNotFound
		}
		latest, err := tx.Events().LatestSeq(ctx)
		if err != nil {
			return err
		}
		floor, err := tx.Events().ReplayFloor(ctx)
		if err != nil {
			return err
		}
		if after < floor || after > latest {
			out.Reset = true
			out.Cursor = latest
			return nil
		}
		es, err := tx.Events().ListAfterProject(ctx, pid, after, 500)
		if err != nil {
			return err
		}
		for _, e := range es {
			out.Cursor = e.Seq
			if e.TaskID != tid || e.Type != domain.EventRunStateChanged {
				continue
			}
			var payload struct {
				Run domain.Run `json:"run"`
			}
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				return err
			}
			if payload.Run.ID != e.RunID || payload.Run.TaskID != tid || payload.Run.ProjectID != pid {
				return domain.ErrInvalid
			}
			m := executionMetadata(payload.Run)
			if !m.Valid() {
				return domain.ErrInvalid
			}
			out.Events = append(out.Events, integration.Event{Seq: e.Seq, Execution: &m})
		}
		if len(es) < 500 {
			out.Cursor = latest
		}
		return nil
	})
	return out, err
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
)

func executionKey(id string) string { return "execution:authorization:" + id }

func executionPreview(ctx context.Context, tx store.Tx, req integration.ExecutionRequest, runtime map[string]string) (integration.ExecutionPreview, error) {
	out := integration.ExecutionPreview{Request: req, RuntimePolicy: runtime, StopBehavior: "stop/restart; no live pause"}
	for _, id := range []string{req.ExecutionID, req.ProjectID, req.TaskID, req.RunnerID} {
		if !integration.Identifier(id) {
			return out, domain.ErrInvalid
		}
	}
	if req.Fence == "" || len(req.Fence) > 4096 || req.RunnerID == "automatic" || req.AgentID == "" || req.Model == "" || req.Reasoning == "" || !domain.InteractionPolicy(req.Interaction).Valid() {
		return out, domain.ErrInvalid
	}
	cfg := domain.ExecutionConfig{Runner: req.RunnerID, Agent: req.AgentID, Model: req.Model, Reasoning: req.Reasoning, Interaction: domain.InteractionPolicy(req.Interaction)}
	if err := cfg.Validate(); err != nil {
		return out, err
	}
	task, err := tx.Tasks().Get(ctx, req.TaskID)
	if err != nil {
		return out, err
	}
	if task.ProjectID != req.ProjectID {
		return out, domain.ErrNotFound
	}
	if task.ArchivedAt != nil || task.State == domain.TaskDone {
		return out, domain.ErrConflict
	}
	repo, err := tx.Repositories().Get(ctx, req.ProjectID)
	if err != nil {
		return out, err
	}
	runner, err := tx.Runners().Get(ctx, req.RunnerID)
	if err != nil {
		return out, err
	}
	if runner.Removed || runner.Disabled {
		return out, domain.ErrConflict
	}
	allowed := runner.Kind == domain.RunnerLocal
	for _, p := range runner.Projects {
		allowed = allowed || p == req.ProjectID
	}
	if !allowed {
		return out, domain.ErrForbidden
	}
	// Remote agents' permission configuration cannot be attested by this controller.
	// Approve on their own controller instead of guessing a remote effective policy.
	if runner.Kind != domain.RunnerLocal {
		return out, fmt.Errorf("%w: approve through the selected device's own controller", domain.ErrInvalid)
	}
	repoCopy := *repo
	repoCopy.InspectedAt = time.Time{}
	repoCopy.HeadCommit = ""
	repoCopy.CurrentBranch = ""
	lv, err := loadLevels(ctx, tx, req.ProjectID)
	if err != nil {
		return out, err
	}
	// Do not hash volatile availability/task-state/version fields. Bind text, Git
	// identity, instructions/configuration and the effective runtime permissions.
	raw, err := json.Marshal(struct {
		Request    integration.ExecutionRequest
		Text       integration.Text
		Source     string
		Repository *domain.GitRepository
		Levels     Levels
		Config     domain.ExecutionConfig
		Runtime    map[string]string
	}{req, integration.Text{Title: task.Title, Description: task.Description, WorkBranch: task.WorkBranch, BaseBranch: task.BaseBranch}, task.SourceRef, &repoCopy, lv, task.Execution, runtime})
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(raw)
	out.Digest = hex.EncodeToString(sum[:])
	out.Title = task.Title
	out.Description = task.Description
	return out, nil
}
func (s *Tasks) PreviewExecution(ctx context.Context, req integration.ExecutionRequest, runtime map[string]string) (integration.ExecutionPreview, error) {
	var out integration.ExecutionPreview
	err := s.Store.View(ctx, func(tx store.Tx) error { var err error; out, err = executionPreview(ctx, tx, req, runtime); return err })
	return out, err
}

// ApproveExecution is called only through authenticated local owner interaction.
// The digest requires the exact policy/context the user was shown.
func (s *Tasks) ApproveExecution(ctx context.Context, preview integration.ExecutionPreview, expires time.Time, runtime map[string]string) (integration.ExecutionApproval, error) {
	var out integration.ExecutionApproval
	now := s.now()
	if !expires.After(now) || expires.After(now.Add(30*24*time.Hour)) {
		return out, domain.ErrInvalid
	}
	err := s.Store.Update(ctx, func(tx store.Tx) error {
		current, err := executionPreview(ctx, tx, preview.Request, runtime)
		if err != nil {
			return err
		}
		if preview.Digest != current.Digest {
			return fmt.Errorf("%w: execution context changed; review again", domain.ErrConflict)
		}
		var prior integration.ExecutionApproval
		if err := tx.Settings().Get(ctx, executionKey(preview.Request.ExecutionID), &prior); err == nil {
			return fmt.Errorf("%w: execution identifier already authorized; use a new identifier", domain.ErrConflict)
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		out = integration.ExecutionApproval{Preview: current, ExpiresAt: expires}
		return tx.Settings().Set(ctx, executionKey(preview.Request.ExecutionID), out, now)
	})
	return out, err
}
func (s *Tasks) ExecutionApproval(ctx context.Context, in integration.ExecutionDispatch) (integration.ExecutionApproval, error) {
	var out integration.ExecutionApproval
	if !integration.Identifier(in.ExecutionID) || in.Fence == "" {
		return out, domain.ErrInvalid
	}
	err := s.Store.View(ctx, func(tx store.Tx) error { return tx.Settings().Get(ctx, executionKey(in.ExecutionID), &out) })
	if err == nil && (out.Preview.Request.Fence != in.Fence || out.Revoked) {
		err = domain.ErrForbidden
	}
	if err == nil && out.RunID == "" && !s.now().Before(out.ExpiresAt) {
		err = domain.ErrForbidden
	}
	return out, err
}
func (s *Tasks) RevokeExecution(ctx context.Context, id string) error {
	if !integration.Identifier(id) {
		return domain.ErrInvalid
	}
	return s.Store.Update(ctx, func(tx store.Tx) error {
		var a integration.ExecutionApproval
		if err := tx.Settings().Get(ctx, executionKey(id), &a); err != nil {
			return err
		}
		a.Revoked = true
		return tx.Settings().Set(ctx, executionKey(id), a, s.now())
	})
}

// ClaimExecution composes with normal Run creation; approval consumption and the
// run ID commit together. No failed setup spends permission and no retry creates
// a second process after a committed claim (even if launch/acknowledgment fails).
func (s *Tasks) ClaimExecution(in integration.ExecutionDispatch, runtime map[string]string) func(context.Context, store.Tx, *domain.Run) error {
	return func(ctx context.Context, tx store.Tx, r *domain.Run) error {
		var a integration.ExecutionApproval
		if err := tx.Settings().Get(ctx, executionKey(in.ExecutionID), &a); err != nil {
			return err
		}
		if s.now().Before(a.Preview.Request.NotBefore) || !a.Preview.Request.Deadline.IsZero() && !s.now().Before(a.Preview.Request.Deadline) {
			return domain.ErrConflict
		}
		if a.Revoked || a.RunID != "" || !s.now().Before(a.ExpiresAt) || a.Preview.Request.Fence != in.Fence {
			return domain.ErrForbidden
		}
		current, err := executionPreview(ctx, tx, a.Preview.Request, runtime)
		if err != nil {
			return err
		}
		if current.Digest != a.Preview.Digest {
			return fmt.Errorf("%w: approved execution context changed", domain.ErrConflict)
		}
		req := a.Preview.Request
		model, reason := req.Model, req.Reasoning
		if model == domain.AgentDefault {
			model = ""
		}
		if reason == domain.AgentDefault {
			reason = ""
		}
		if r.TaskID != req.TaskID || r.ProjectID != req.ProjectID || r.RunnerID != req.RunnerID || r.AgentID != req.AgentID || r.Model != model || r.Reasoning != reason || string(r.Policy.Interaction) != req.Interaction {
			return domain.ErrForbidden
		}
		runners, err := tx.Runners().List(ctx)
		if err != nil {
			return err
		}
		active, err := tx.Runs().ListActive(ctx)
		if err != nil {
			return err
		}
		for _, runner := range runners {
			if runner.ID == req.RunnerID {
				count := 0
				for _, run := range active {
					if run.RunnerID == runner.ID || run.RunnerID == "" && runner.Kind == domain.RunnerLocal {
						count++
					}
				}
				if count >= max(1, runner.Capacity) {
					return fmt.Errorf("%w: selected runner is at capacity", domain.ErrConflict)
				}
			}
		}
		a.RunID = r.ID
		return tx.Settings().Set(ctx, executionKey(in.ExecutionID), a, s.now())
	}
}

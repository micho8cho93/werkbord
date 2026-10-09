package runner

import (
	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/service"
	"devboard/internal/store"
	"errors"
	"sync"
	"testing"
	"time"
)

func authorize(t *testing.T, e *env, task *domain.Task, id string, mutate func(*integration.ExecutionRequest)) integration.ExecutionDispatch {
	t.Helper()
	r, err := e.settings.RegisterRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req := integration.ExecutionRequest{ExecutionID: id, ProjectID: task.ProjectID, TaskID: task.ID, Fence: "assignment_1", RunnerID: r.ID, AgentID: "fake", Model: "default", Reasoning: "default", Interaction: "interactive"}
	if mutate != nil {
		mutate(&req)
	}
	policy, _ := e.mgr.ExecutionPolicy(req.AgentID)
	preview, err := e.tasks.PreviewExecution(ctx, req, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tasks.ApproveExecution(ctx, preview, time.Now().Add(time.Hour), policy); err != nil {
		t.Fatal(err)
	}
	return integration.ExecutionDispatch{ExecutionID: id, Fence: req.Fence}
}
func TestAuthorizedSchedulingClaimsApprovalWithRunAndRecoversDuplicates(t *testing.T) {
	e := newEnv(t)
	task := e.task("untrusted external requirement")
	in := authorize(t, e, task, "exe_once", nil)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.mgr.ScheduleAuthorized(ctx, in)
			if err != nil {
				errs <- err
			} else {
				ids <- r.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate run")
		}
		id = got
	}
	if len(e.adapter.Sessions()) != 1 {
		t.Fatal("duplicate process")
	}
	if e.adapter.Last().Req.Policy.Interaction != domain.InteractionInteractive {
		t.Fatal("policy changed")
	}
	if _, err := e.mgr.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	r, err := e.mgr.ScheduleAuthorized(ctx, in)
	if err != nil || r.ID != id || len(e.adapter.Sessions()) != 1 {
		t.Fatal("terminal approval reused", r, err)
	}
	restarted := e.newManager()
	defer restarted.Shutdown(ctx)
	r, err = restarted.ScheduleAuthorized(ctx, in)
	if err != nil || r.ID != id || len(e.adapter.Sessions()) != 1 {
		t.Fatal("restart duplicated dispatch", r, err)
	}
	if _, err := e.mgr.Start(ctx, StartInput{Authorization: &in, AgentID: "fake"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("authorization accepted a policy override", err)
	}
}
func TestAuthorizedDispatchRejectsChangedTaskAndRevokedExpiredWrongFence(t *testing.T) {
	for _, kind := range []string{"text", "revoked", "expired", "fence", "early", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			task := e.task("bound task")
			in := authorize(t, e, task, "exe_bound", func(req *integration.ExecutionRequest) {
				if kind == "early" {
					req.NotBefore = time.Now().Add(time.Hour)
				}
				if kind == "deadline" {
					req.Deadline = time.Now().Add(-time.Hour)
				}
			})
			switch kind {
			case "text":
				title := "changed instructions"
				_, err := e.tasks.Update(ctx, task.ID, service.TaskPatch{Title: &title, Version: task.Version})
				if err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := e.tasks.RevokeExecution(ctx, in.ExecutionID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				e.tasks.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
			case "fence":
				in.Fence = "assignment_2"
			}
			if _, err := e.mgr.ScheduleAuthorized(ctx, in); err == nil {
				t.Fatal("unauthorized launch")
			}
			if len(e.adapter.Sessions()) != 0 {
				t.Fatal("process launched before authorization check")
			}
		})
	}
}
func TestExecutionApprovalRequiresReviewedDigestAndCannotBeReplaced(t *testing.T) {
	e := newEnv(t)
	task := e.task("review")
	in := authorize(t, e, task, "exe_existing", nil)
	a, _ := e.tasks.ExecutionApproval(ctx, in)
	policy, _ := e.mgr.ExecutionPolicy("fake")
	if _, err := e.tasks.ApproveExecution(ctx, a.Preview, time.Now().Add(time.Hour), policy); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("approval overwritten", err)
	}
	a.Preview.Request.ExecutionID = "exe_forged"
	a.Preview.Request.Interaction = "autonomous"
	if _, err := e.tasks.ApproveExecution(ctx, a.Preview, time.Now().Add(time.Hour), policy); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("altered review digest accepted", err)
	}
}

func TestAuthorizedDispatchRechecksEffectiveRuntimePermissions(t *testing.T) {
	e := newEnv(t)
	task := e.task("permission-bound task")
	runner, err := e.settings.RegisterRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req := integration.ExecutionRequest{ExecutionID: "exe_permissions", ProjectID: task.ProjectID, TaskID: task.ID, Fence: "assignment_1", RunnerID: runner.ID, AgentID: "fake", Model: "default", Reasoning: "default", Interaction: "interactive"}
	// Approval was reviewed with a different effective runtime permission. The
	// current adapter must attest the same permissions before starting anything.
	policy, _ := e.mgr.ExecutionPolicy("fake")
	policy["sandbox"] = "previous-local-permission"
	preview, err := e.tasks.PreviewExecution(ctx, req, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tasks.ApproveExecution(ctx, preview, time.Now().Add(time.Hour), policy); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.ScheduleAuthorized(ctx, integration.ExecutionDispatch{ExecutionID: req.ExecutionID, Fence: req.Fence}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("changed runtime permission launched", err)
	}
	if len(e.adapter.Sessions()) != 0 {
		t.Fatal("process launched")
	}
}

func TestAuthorizedSchedulingWaitsForCapacityWithoutConsumingApproval(t *testing.T) {
	e := newEnv(t)
	first, second := e.task("first"), e.task("second")
	a := authorize(t, e, first, "exe_capacity_first", nil)
	b := authorize(t, e, second, "exe_capacity_second", nil)
	approval, _ := e.tasks.ExecutionApproval(ctx, a)
	if err := e.db.Update(ctx, func(tx store.Tx) error {
		runner, err := tx.Runners().Get(ctx, approval.Preview.Request.RunnerID)
		if err != nil {
			return err
		}
		runner.Capacity = 1
		return tx.Runners().Save(ctx, runner)
	}); err != nil {
		t.Fatal(err)
	}
	run, err := e.mgr.ScheduleAuthorized(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.ScheduleAuthorized(ctx, b); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("capacity ignored", err)
	}
	pending, err := e.tasks.ExecutionApproval(ctx, b)
	if err != nil || pending.RunID != "" || len(e.adapter.Sessions()) != 1 {
		t.Fatal("waiting spent approval or launched", pending, err)
	}
	if _, err := e.mgr.Stop(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.ScheduleAuthorized(ctx, b); err != nil {
		t.Fatal("capacity recovery failed", err)
	}
	if len(e.adapter.Sessions()) != 2 {
		t.Fatal("waiting execution not launched once")
	}
}

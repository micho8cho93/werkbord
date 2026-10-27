package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/runnerwire"
	"werkbord/internal/store"
)

func TestDeviceQueueScopesReplayRevocationAndLateReceipts(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	allowed := true
	d := &DeviceOperations{Deps: rt.svc.Deps, Runners: rt.svc, ActorAllowed: func(id string) bool { return allowed && id == "client_a" }}
	rt.svc.Operations = d
	sync := func(results ...devicewire.Result) runnerwire.SyncReply {
		t.Helper()
		rt.sequence++
		caps := rt.caps()
		caps.DeviceOperations = true
		in := runnerwire.Sync{Protocol: runnerwire.Protocol, RunnerID: rt.runner.ID, Sequence: rt.sequence, At: rt.now, Capabilities: caps, OperationResults: results}
		raw, _ := json.Marshal(in)
		out, err := rt.svc.Sync(ctx, raw, runnerwire.Signature(rt.key, raw))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	sync()
	req := devicewire.Request{RequestID: domain.NewID("request"), Kind: "git.status", RunnerID: rt.runner.ID, ProjectID: rt.project.ID}
	record, err := d.Submit(ctx, "client_a", req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := d.Submit(ctx, "client_a", req)
	if err != nil || again.Operation.ID != record.Operation.ID {
		t.Fatalf("replay %+v %v", again, err)
	}
	req.Kind = "git.prepare"
	req.Action = "push"
	if _, err = d.Submit(ctx, "client_a", req); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed request accepted %v", err)
	}
	if _, err = d.Get(ctx, "other_client", record.Operation.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other client read %v", err)
	}
	out := sync()
	if len(out.Operations) != 1 || out.Operations[0].ID != record.Operation.ID {
		t.Fatalf("dispatch %+v", out)
	}
	rt.now = rt.now.Add(3 * time.Minute)
	out = sync()
	if len(out.Operations) != 0 {
		t.Fatal("expired command dispatched")
	}
	got, err := d.Get(ctx, "client_a", record.Operation.ID)
	if err != nil || got.Result.State != "uncertain" {
		t.Fatalf("expiry %+v %v", got, err)
	}
	out = sync(devicewire.Result{ID: record.Operation.ID, State: "done", Message: "original result"})
	if len(out.OperationAcks) != 1 {
		t.Fatal("missing ack")
	}
	got, err = d.Get(ctx, "client_a", record.Operation.ID)
	if err != nil || got.Result.State != "done" {
		t.Fatalf("late receipt %+v %v", got, err)
	}
	allowed = false
	if _, err = d.Get(ctx, "client_a", record.Operation.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("revoked client read %v", err)
	}
}
func TestDeviceQueueRejectsWrongRunnerAndTerminalGrant(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	d := &DeviceOperations{Deps: rt.svc.Deps, ActorAllowed: func(string) bool { return true }}
	req := devicewire.Request{RequestID: domain.NewID("request"), Kind: "terminal.input", RunnerID: rt.runner.ID, ProjectID: rt.project.ID, SessionID: "native_session", Client: "stable_client_123456", Sequence: 1, Data: "hello"}
	if d.TerminalAllowed(ctx, "client", req.RunnerID, req.ProjectID, req.SessionID) {
		t.Fatal("implicit grant")
	}
	if err := d.GrantTerminal(ctx, req.RunnerID, req.ProjectID, req.SessionID, "client", true); err != nil {
		t.Fatal(err)
	}
	if !d.TerminalAllowed(ctx, "client", req.RunnerID, req.ProjectID, req.SessionID) {
		t.Fatal("grant missing")
	}
	if d.TerminalAllowed(ctx, "client", "other_runner", req.ProjectID, req.SessionID) || d.TerminalAllowed(ctx, "client", req.RunnerID, "other_project", req.SessionID) {
		t.Fatal("grant crosses target")
	}
	if err := d.GrantTerminal(ctx, req.RunnerID, req.ProjectID, req.SessionID, "client", false); err != nil {
		t.Fatal(err)
	}
	if d.TerminalAllowed(ctx, "client", req.RunnerID, req.ProjectID, req.SessionID) {
		t.Fatal("revoked grant active")
	}
	err := rt.f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, err := tx.Runners().Get(ctx, rt.runner.ID)
		if err != nil {
			return err
		}
		r.Projects = nil
		return tx.Runners().Save(ctx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Kind = "git.status"
	if _, err = d.Submit(ctx, "owner", req); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("project permission bypass %v", err)
	}
}

func TestDevicePauseRefusesActiveLocalOperationAfterQueueExpiry(t *testing.T) {
	rt := newRunnerTest(t)
	d := &DeviceOperations{Deps: rt.svc.Deps}
	d.localActive.Store(1)
	if err := d.Pause(context.Background()); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("paused while a write may still be running: %v", err)
	}
	d.localActive.Store(0)
	if err := d.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.Resume()
}

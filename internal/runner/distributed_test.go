package runner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/runnerwire"
	"devboard/internal/service"
)

func TestManagerRoutesWithoutStartingControllerProcessAndRetainsOwnership(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	deps := e.tasks.Deps
	deps.Now = func() time.Time { return now }
	distributed := &service.Runners{Deps: deps, Runs: e.runs}
	pair, err := distributed.Pair(ctx, "http://controller.test:7420", []string{e.project.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, _ := runnerwire.DecodeCode(pair.Code)
	public, key, _ := ed25519.GenerateKey(rand.Reader)
	remote, err := distributed.Join(ctx, runnerwire.Join{Secret: secret, PublicKey: base64.RawURLEncoding.EncodeToString(public), Name: "Peer"})
	if err != nil {
		t.Fatal(err)
	}
	remote.Automatic = true
	remote, err = distributed.Manage(ctx, remote.ID, *remote, false)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := runnerwire.Sync{Protocol: runnerwire.Protocol, RunnerID: remote.ID, Sequence: 1, At: now, Capabilities: domain.RunnerCapabilities{CPU: 8, Agents: []domain.Agent{{ID: "fake", Available: true}}, Repositories: []string{e.project.ID}}}
	body, _ := json.Marshal(heartbeat)
	if _, err := distributed.Sync(ctx, body, runnerwire.Signature(key, body)); err != nil {
		t.Fatal(err)
	}
	e.mgr.opt.Distributed = distributed
	task := e.task("specific runner")
	run, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake", RunnerID: remote.ID})
	if err != nil || !run.Remote || run.RunnerID != remote.ID {
		t.Fatalf("manual: %+v %v", run, err)
	}
	if len(e.adapter.Sessions()) != 0 || run.WorktreeID != "" || e.mgr.IsLive(run.ID) {
		t.Fatal("remote task ran on controller")
	}
	now = now.Add(time.Minute)
	if err := e.mgr.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake", RunnerID: remote.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: e.task("offline assignment").ID, AgentID: "fake", RunnerID: remote.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("offline manual: %v", err)
	}
	if len(e.adapter.Sessions()) != 0 {
		t.Fatal("offline assignment silently fell back")
	}
}

package service

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
	"werkbord/internal/workspace"
)

func agentOn(t *testing.T, tm *team, who Actor, id string) (workspace.AgentRef, bool) {
	t.Helper()
	b, err := tm.svc.Board(bg, who, tm.pid())
	if err != nil {
		t.Fatal(err)
	}
	r, ok := b.Agents[id]
	return r, ok
}

func TestATeammateSeesWhichAgentRunsAndThatItIsFresh(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Shared view")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	r.agent = "codex"
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	for name, who := range map[string]Actor{"the owner": tm.bo, "a teammate": tm.cy, "the workspace owner": tm.owner} {
		got, ok := agentOn(t, tm, who, k.ID)
		if !ok || got.ID != "codex" || got.State != workspace.ExecRunning || !got.Fresh || !got.ObservedAt.Equal(*r.clock) {
			t.Fatalf("%s sees %+v (%v)", name, got, ok)
		}
	}
	// An old connector, or a member who withholds the agent, shows a neutral mark with the true state.
	w := tm.working(tm.cy, "Neutral")
	r2 := tm.runnerFor(tm.cy, w, "cy's laptop")
	if err := r2.report(0, "starting"); err != nil {
		t.Fatal(err)
	}
	if got, ok := agentOn(t, tm, tm.bo, w.ID); !ok || got.ID != "" || got.State != workspace.ExecStarting || !got.Fresh {
		t.Fatalf("neutral report: %+v (%v)", got, ok)
	}
}

// Freshness is the server's word: no older than the online window, and the runner reported online.
func TestAnAgentReportIsFreshOnlyWithinTheWindowAndWithTheRunnerOnline(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Window")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	start := *r.clock
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		after time.Duration
		fresh bool
	}{{0, true}, {domain.DeviceOnlineWindow, true}, {domain.DeviceOnlineWindow + time.Millisecond, false}} {
		*r.clock = start.Add(c.after)
		got, ok := agentOn(t, tm, tm.cy, k.ID)
		if !ok || got.Fresh != c.fresh || got.State != workspace.ExecRunning {
			t.Fatalf("after %v: %+v (%v)", c.after, got, ok)
		}
	}
	// A stale report is still a running report that is not current: never "done", never "offline".
	*r.clock = start
	r.offline = true
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	if got, _ := agentOn(t, tm, tm.cy, k.ID); got.Fresh || got.State != workspace.ExecRunning {
		t.Fatalf("a runner that is not online is fresh, or its state was rewritten: %+v", got)
	}
}

func TestSwitchingAgentReplacesTheOldOneAndAnOldReplayCannotBringItBack(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Switch")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	r.agent = "codex"
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	*r.clock = r.clock.Add(time.Second)
	r.agent = "claude-code"
	if err := r.report(1, "running"); err != nil {
		t.Fatal(err)
	}
	if got, _ := agentOn(t, tm, tm.cy, k.ID); got.ID != "claude-code" {
		t.Fatalf("after the switch: %+v", got)
	}
	// Replaying the first report (an older sequence) changes nothing.
	r.seq = 0
	r.agent = "codex"
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	if got, _ := agentOn(t, tm, tm.cy, k.ID); got.ID != "claude-code" {
		t.Fatalf("a replay brought the old agent back: %+v", got)
	}
}

func TestAReportFromBeforeAReclaimNeverAppears(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Fence")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	r.agent = "codex"
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.ReleaseTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}
	if got, ok := agentOn(t, tm, tm.cy, k.ID); ok {
		t.Fatalf("a report from the earlier claim appears: %+v", got)
	}
}

func TestAnArchivedOrRevokedSourceShowsNothing(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Revoked")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	if _, ok := agentOn(t, tm, tm.cy, k.ID); !ok {
		t.Fatal("no report before revoking")
	}
	if _, err := tm.svc.RevokeDevice(bg, tm.bo, r.actor.Device.ID); err != nil {
		t.Fatal(err)
	}
	if got, ok := agentOn(t, tm, tm.cy, k.ID); ok {
		t.Fatalf("a revoked device's report shows: %+v", got)
	}
}

// What a peer may see is exactly these keys. Nothing in a run, a prompt, a path, usage or a connection leaks.
func TestWhatAPeerSeesOfAnAgentIsAnExactAllowlistWithNoPresence(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Allowlist")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	r.agent = "codex"
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	b, err := tm.svc.Board(bg, tm.cy, tm.pid())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b.Agents[k.ID])
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	var got []string
	for k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	if want := []string{"fresh", "id", "observedAt", "state"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("peer keys %v, want %v", got, want)
	}
	whole, _ := json.Marshal(b.Agents)
	for _, banned := range []string{"run_a", "runId", "runnerOnline", "prompt", "branch", "summary", "handoff", "question", "usage", "path"} {
		if strings.Contains(string(whole), banned) {
			t.Fatalf("%q reached a peer: %s", banned, whole)
		}
	}
	// Presence guard: no field of what peers get speaks of whether anyone is around.
	var walk func(reflect.Type)
	walk = func(ty reflect.Type) {
		for ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Map || ty.Kind() == reflect.Slice {
			ty = ty.Elem()
		}
		if ty.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			name := strings.ToLower(f.Name + " " + f.Tag.Get("json"))
			for _, banned := range []string{"online", "lastseen", "presence", "available"} {
				if strings.Contains(name, banned) {
					t.Fatalf("%s.%s speaks of presence", ty.Name(), f.Name)
				}
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeOf(workspace.AgentRef{}))
	walk(reflect.TypeOf(b.Agents))
}

func TestActiveIsDerivedFromTheSameProjection(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Parity")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	for i, state := range []string{"queued", "starting", "running", "needs_input", "failed", "completed"} {
		if err := r.report(i, state); err != nil {
			t.Fatal(err)
		}
		b, _ := tm.svc.Board(bg, tm.cy, tm.pid())
		live := state == "queued" || state == "starting" || state == "running" || state == "needs_input"
		if b.Active[k.ID] != (live && b.Agents[k.ID].Fresh) {
			t.Fatalf("%s: active %v but agents %+v", state, b.Active, b.Agents[k.ID])
		}
		if b.Active[k.ID] != live {
			t.Fatalf("%s: active=%v", state, b.Active[k.ID])
		}
	}
}

func TestTheOverviewQueueCarriesThePeerAgentAndAMemberOutsideTheProjectGetsNone(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Queue")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	r.agent = "codex"
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}
	o, err := tm.svc.Overview(bg, tm.cy)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, q := range o.Queue {
		if q.ID == k.ID {
			found = true
			if q.Agent == nil || q.Agent.ID != "codex" || q.Agent.State != workspace.ExecRunning || !q.Agent.Fresh {
				t.Fatalf("queue agent %+v", q.Agent)
			}
		}
	}
	if !found {
		t.Fatal("ticket missing from the queue")
	}
	// A member outside the project is refused by the board itself, and an archived project exposes no reports.
	stranger, _ := tm.world.workspace("Rival", "Rex")
	if _, err := tm.svc.Board(bg, stranger, tm.pid()); err == nil {
		t.Fatal("a non-member read the board")
	}
	p := tm.project
	p.Archived = true
	var got map[string]workspace.AgentRef
	if err := tm.svc.db.View(bg, func(tx store.Tx) error {
		var err error
		got, err = tm.svc.peerExecutions(bg, tx, tm.cy, p, []domain.Ticket{k})
		return err
	}); err != nil || len(got) != 0 {
		t.Fatalf("archived: %v %v", got, err)
	}
}

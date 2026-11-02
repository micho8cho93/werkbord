package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func failedWith(t *testing.T, f *fixture, taskID, agent string, kind domain.ProviderErrorKind, edit func(*domain.Run)) domain.Run {
	t.Helper()
	return failedFrom(t, f, taskID, agent, kind, "", edit)
}

func failedFrom(t *testing.T, f *fixture, taskID, agent string, kind domain.ProviderErrorKind, origin string, edit func(*domain.Run)) domain.Run {
	t.Helper()
	ctx := context.Background()
	run, err := f.runs.Create(ctx, NewRun{TaskID: taskID, AgentID: agent, Prompt: "work", Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	var out domain.Run
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Runs().Get(ctx, run.ID)
		if e != nil {
			return e
		}
		r.State, r.Reason = domain.RunFailed, "provider stopped"
		r.Blocker = &domain.Blocker{ProviderKind: kind, Summary: "s", Source: domain.BlockerReport}
		if edit != nil {
			edit(r)
		}
		out = *r
		return tx.Runs().Update(ctx, r)
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func fallbackOn(t *testing.T, s *Scheduler, pid string, providers ...string) {
	t.Helper()
	if _, err := s.SetFallbackSettings(context.Background(), pid, domain.FallbackSettings{Enabled: true, Providers: providers}); err != nil {
		t.Fatal(err)
	}
}

var anyAgent = func(string) bool { return true }

func TestFallbackIsOffUntilAProjectOptsIn(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	task, _ := f.tasks.Create(ctx, pid, "Build", "")
	failedWith(t, f, task.ID, "claude-code", domain.ProviderQuota, nil)
	if got, err := s.FallbackCandidates(ctx, pid, anyAgent); err != nil || len(got) != 0 {
		t.Fatal("fallback must be off by default", got, err)
	}
	cfg, _ := s.FallbackSettings(ctx, pid)
	if cfg.Enabled || cfg.MaxSwitches != 1 || cfg.DailyLimit != 3 {
		t.Fatalf("%+v", cfg)
	}
	for _, bad := range []domain.FallbackSettings{{Enabled: true}, {Enabled: true, Providers: []string{"Codex!"}}, {Providers: []string{"codex", "codex"}}, {MaxSwitches: 9, Providers: []string{"codex"}}, {DailyLimit: 99, Providers: []string{"codex"}}} {
		if _, err := s.SetFallbackSettings(ctx, pid, bad); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	fallbackOn(t, s, pid, "codex")
	got, err := s.FallbackCandidates(ctx, pid, anyAgent)
	if err != nil || len(got) != 1 || got[0].Agent != "codex" {
		t.Fatal(got, err)
	}
}

func TestFallbackOnlyForRunsStoppedByARecoverableProviderLimit(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	fallbackOn(t, s, pid, "codex")
	mk := func(title string) *domain.Task { x, _ := f.tasks.Create(ctx, pid, title, ""); return x }
	failedWith(t, f, mk("auth").ID, "claude-code", domain.ProviderAuth, nil)
	failedWith(t, f, mk("context").ID, "claude-code", domain.ProviderContext, nil)
	failedWith(t, f, mk("remote").ID, "claude-code", domain.ProviderQuota, func(r *domain.Run) { r.Remote = true })
	failedWith(t, f, mk("unknown stop").ID, "claude-code", domain.ProviderQuota, func(r *domain.Run) { r.Recovery = "intervention" })
	failedWith(t, f, mk("stale").ID, "claude-code", domain.ProviderQuota, func(r *domain.Run) { r.UpdatedAt = r.UpdatedAt.Add(-48 * time.Hour) })
	// A session left open by a provider limit is the person's for the grace period; a question is never a provider limit.
	justNow := mk("just blocked")
	failedWith(t, f, justNow.ID, "claude-code", domain.ProviderQuota, func(r *domain.Run) { r.State = domain.RunBlocked; r.Blocker.RaisedAt = s.Now() })
	asked := mk("blocked on a question")
	failedWith(t, f, asked.ID, "claude-code", domain.ProviderOther, func(r *domain.Run) { r.State = domain.RunBlocked; r.Blocker.RaisedAt = s.Now().Add(-time.Hour) })
	waiting := mk("waiting for a message")
	failedWith(t, f, waiting.ID, "claude-code", domain.ProviderQuota, func(r *domain.Run) {
		r.State, r.Waiting = domain.RunWaitingForUser, domain.WaitIdle
		r.Blocker.RaisedAt = s.Now().Add(-time.Hour)
	})
	stuck := mk("stuck on a limit")
	failedWith(t, f, stuck.ID, "claude-code", domain.ProviderQuota, func(r *domain.Run) {
		r.State = domain.RunBlocked
		r.Blocker.RaisedAt = s.Now().Add(-domain.FallbackGrace - time.Second)
	})
	done := mk("done")
	failedWith(t, f, done.ID, "claude-code", domain.ProviderQuota, nil)
	st := domain.TaskDone
	cur, _ := f.tasks.Get(ctx, done.ID)
	f.tasks.Update(ctx, done.ID, TaskPatch{Version: cur.Version, State: &st})
	good := mk("rate limited")
	failedWith(t, f, good.ID, "claude-code", domain.ProviderRateLimit, nil)
	got, err := s.FallbackCandidates(ctx, pid, anyAgent)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	ids := map[string]bool{got[0].Run.TaskID: true, got[1].Run.TaskID: true}
	if !ids[good.ID] || !ids[stuck.ID] {
		t.Fatalf("want the ended run and the session stuck past the grace period: %+v", got)
	}
}

func TestFallbackNeverReturnsToAnAgentAlreadyUsedAndRespectsUsability(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	fallbackOn(t, s, pid, "claude-code", "codex", "gemini-cli")
	task, _ := f.tasks.Create(ctx, pid, "Build", "")
	failedWith(t, f, task.ID, "claude-code", domain.ProviderQuota, nil)
	got, _ := s.FallbackCandidates(ctx, pid, func(id string) bool { return id == "gemini-cli" })
	if len(got) != 1 || got[0].Agent != "gemini-cli" {
		t.Fatalf("an agent that cannot start now is skipped: %+v", got)
	}
	if got, _ := s.FallbackCandidates(ctx, pid, func(id string) bool { return false }); len(got) != 0 {
		t.Fatal("no usable agent, no fallback")
	}
	got, _ = s.FallbackCandidates(ctx, pid, func(id string) bool { return id != "claude-code" })
	if got[0].Agent != "codex" {
		t.Fatalf("preference order: %+v", got)
	}
}

func TestFallbackClaimIsTheIdempotencyKey(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	fallbackOn(t, s, pid, "codex")
	task, _ := f.tasks.Create(ctx, pid, "Build", "")
	run := failedWith(t, f, task.ID, "claude-code", domain.ProviderQuota, nil)
	c := FallbackChoice{Run: run, Agent: "codex"}
	if err := s.ClaimFallback(ctx, pid, c); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimFallback(ctx, pid, c); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("a second claim of the same run must be refused", err)
	}
	if got, _ := s.FallbackCandidates(ctx, pid, anyAgent); len(got) != 0 {
		t.Fatal("a claimed run is not offered again, even if nothing ever started")
	}
	if err := s.ClaimFallback(ctx, pid, FallbackChoice{Run: run, Agent: "gemini-cli"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("an agent the project did not allow must be refused")
	}
	// A refusal for a passing reason reopens it; anything else closes it.
	if err := s.ResolveFallback(ctx, pid, run.ID, domain.FallbackRetry, "capacity", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FallbackCandidates(ctx, pid, anyAgent); len(got) != 1 {
		t.Fatal("a retry must be offered again")
	}
	if err := s.ClaimFallback(ctx, pid, c); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveFallback(ctx, pid, run.ID, domain.FallbackDeclined, "no verified handoff", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimFallback(ctx, pid, c); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("a declined attempt is never retried automatically", err)
	}
	l, _ := s.FallbackLedger(ctx, pid)
	if len(l.Entries) != 1 || l.Entries[0].Result != domain.FallbackDeclined || l.Entries[0].Reason == "" {
		t.Fatalf("%+v", l)
	}
}

func TestFallbackBudgetsCapSwitchesPerTaskAndPerDay(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	if _, err := s.SetFallbackSettings(ctx, pid, domain.FallbackSettings{Enabled: true, Providers: []string{"codex", "gemini-cli"}, MaxSwitches: 1, DailyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	a, _ := f.tasks.Create(ctx, pid, "A", "")
	b, _ := f.tasks.Create(ctx, pid, "B", "")
	c, _ := f.tasks.Create(ctx, pid, "C", "")
	// A was already continued once automatically, so it has used its one switch.
	first := failedWith(t, f, a.ID, "claude-code", domain.ProviderQuota, nil)
	failedFrom(t, f, a.ID, "codex", domain.ProviderQuota, domain.FallbackOrigin(first.ID), nil)
	ra := failedWith(t, f, b.ID, "claude-code", domain.ProviderQuota, nil)
	rb := failedWith(t, f, c.ID, "claude-code", domain.ProviderRateLimit, nil)
	got, _ := s.FallbackCandidates(ctx, pid, anyAgent)
	if len(got) != 2 {
		t.Fatalf("task A is out of switches: %+v", got)
	}
	if err := s.ClaimFallback(ctx, pid, FallbackChoice{Run: ra, Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimFallback(ctx, pid, FallbackChoice{Run: rb, Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
	extra := failedWith(t, f, mustTask(t, f, pid, "D").ID, "claude-code", domain.ProviderQuota, nil)
	if err := s.ClaimFallback(ctx, pid, FallbackChoice{Run: extra, Agent: "codex"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("the day's allowance is spent", err)
	}
	if got, _ := s.FallbackCandidates(ctx, pid, anyAgent); len(got) != 0 {
		t.Fatal("nothing is offered once the allowance is spent")
	}
	// A day later the allowance is back.
	later := s.Now()
	s.Now = func() time.Time { return later.Add(25 * time.Hour) }
	if err := s.ClaimFallback(ctx, pid, FallbackChoice{Run: extra, Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
}

func mustTask(t *testing.T, f *fixture, pid, title string) *domain.Task {
	t.Helper()
	task, err := f.tasks.Create(context.Background(), pid, title, "")
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestAHandoffIsVerifiedOnlyByTheControllersGitReading(t *testing.T) {
	good := func() *domain.Handoff {
		return &domain.Handoff{Summary: "Implemented the parser", FilesChanged: []string{"a.go"}, GitState: "Branch: werkbord/x; HEAD: abc; staged: 0; unstaged: 1; untracked: 0; conflicts: 0; operation: ", GeneratedAt: time.Now()}
	}
	if why := domain.HandoffUnverified(good()); why != "" {
		t.Fatal(why)
	}
	for name, edit := range map[string]func(*domain.Handoff){
		"placeholder git":  func(h *domain.Handoff) { h.GitState = "Repository evidence has not been captured." },
		"removed worktree": func(h *domain.Handoff) { h.GitState = "Worktree was removed; only the recorded handoff is available." },
		"cannot inspect":   func(h *domain.Handoff) { h.GitState = "Cannot inspect worktree: boom" },
		"conflicts": func(h *domain.Handoff) {
			h.GitState = "Branch: x; HEAD: a; staged: 0; unstaged: 0; untracked: 0; conflicts: 2; operation: "
		},
		"mid-operation": func(h *domain.Handoff) {
			h.GitState = "Branch: x; HEAD: a; staged: 0; unstaged: 0; untracked: 0; conflicts: 0; operation: rebase"
		},
		"no capture time": func(h *domain.Handoff) { h.GeneratedAt = time.Time{} },
		"nothing to resume": func(h *domain.Handoff) {
			h.FilesChanged = nil
			h.Summary = "Implementation details have not been reported."
		},
	} {
		h := good()
		edit(h)
		if domain.HandoffUnverified(h) == "" {
			t.Errorf("%s was accepted as verified", name)
		}
	}
	if domain.HandoffUnverified(nil) == "" {
		t.Error("no handoff is not verified")
	}
	// Files changed alone are evidence enough when the agent died before summarising.
	h := good()
	h.Summary = "Implementation details have not been reported."
	if why := domain.HandoffUnverified(h); why != "" {
		t.Errorf("recorded changes should be enough: %s", why)
	}
}

package service

import (
	"context"
	"errors"
	"testing"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/runnerwire"
	"devboard/internal/store"
)

func TestCompletionReviewEvidence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		commits   int
		end       domain.RunState
		moved     domain.TaskState
		scheduled bool
	}{
		{"commits", 1, domain.RunCompleted, "", false},
		{"no commits", 0, domain.RunCompleted, "", false},
		{"failed", 1, domain.RunFailed, "", false},
		{"stopped", 1, domain.RunStopped, "", false},
		{"already backlog", 1, domain.RunCompleted, domain.TaskBacklog, false},
		{"already review", 1, domain.RunCompleted, domain.TaskReview, false},
		{"already done", 1, domain.RunCompleted, domain.TaskDone, false},
		{"scheduled no commits", 0, domain.RunCompleted, "", true},
		{"scheduled commits", 1, domain.RunCompleted, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGC(t)
			// agentBranch normally finishes itself. Create the run/worktree without its
			// finishing helper so this tests the automatic transition alone.
			task, e := f.tasks.Create(f.ctx, f.project.ID, "Review evidence", "")
			if e != nil {
				t.Fatal(e)
			}
			wt, e := f.wts.Create(f.ctx, NewWorktree{ProjectID: f.project.ID, Path: f.wtRoot + "/evidence", Branch: "evidence", BaseRef: "main"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.git.AddWorktree(f.ctx, f.repo, wt.Path, wt.Branch, "main"); e != nil {
				t.Fatal(e)
			}
			r, e := f.runs.Create(f.ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "work", WorktreeID: wt.ID})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.runs.MarkStarted(f.ctx, r.ID, Started{}); e != nil {
				t.Fatal(e)
			}
			if tc.scheduled {
				if e = f.deps.Store.Update(f.ctx, func(tx store.Tx) error {
					r, e = tx.Runs().Get(f.ctx, r.ID)
					if e != nil {
						return e
					}
					r.ScheduleKey = "scheduled"
					return tx.Runs().Update(f.ctx, r)
				}); e != nil {
					t.Fatal(e)
				}
			}
			if tc.commits > 0 {
				commitIn(t, wt.Path, "change.txt", "work", "Change")
			}
			before, _ := f.tasks.Get(f.ctx, task.ID)
			if tc.moved != "" {
				before, e = f.tasks.Update(f.ctx, task.ID, TaskPatch{Version: before.Version, State: &tc.moved})
				if e != nil {
					t.Fatal(e)
				}
			}
			f.runs.Handoffs = &Handoffs{Deps: f.deps, Git: f.git}
			sub := f.bus.Subscribe(100)
			defer sub.Close()
			if _, e = f.runs.End(f.ctx, r.ID, Ended{State: tc.end}); e != nil {
				t.Fatal(e)
			}
			after, _ := f.tasks.Get(f.ctx, task.ID)
			want := before.State
			move := tc.commits > 0 && tc.end == domain.RunCompleted && before.State == domain.TaskDoing
			if move {
				want = domain.TaskReview
			}
			if after.State != want {
				t.Fatalf("state %s want %s", after.State, want)
			}
			count := 0
			for {
				select {
				case ev := <-sub.C:
					if ev.Type == domain.EventTaskUpdated {
						count++
					}
				default:
					goto drained
				}
			}
		drained:
			if move {
				if after.Version != before.Version+1 || count != 1 {
					t.Fatalf("version/event: %d %d", after.Version, count)
				}
				title := "stale"
				if _, e = f.tasks.Update(f.ctx, task.ID, TaskPatch{Version: before.Version, Title: &title}); !errors.Is(e, domain.ErrConflict) {
					t.Fatalf("stale write: %v", e)
				}
			} else if after.Version != before.Version || count != 0 {
				t.Fatal("unchanged task mutated")
			}
			if _, e = f.runs.End(f.ctx, r.ID, Ended{State: tc.end}); !errors.Is(e, domain.ErrTransition) {
				t.Fatalf("duplicate end: %v", e)
			}
			again, _ := f.tasks.Get(f.ctx, task.ID)
			if again.Version != after.Version {
				t.Fatal("duplicate move")
			}
		})
	}
}

func TestRemoteCompletionReviewEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, head string
		end        domain.RunState
		moved      domain.TaskState
	}{
		{"commits", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", domain.RunCompleted, ""},
		{"same head", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", domain.RunCompleted, ""},
		{"unknown head", "", domain.RunCompleted, ""},
		{"failed", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", domain.RunFailed, ""},
		{"stopped", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", domain.RunStopped, ""},
		{"person moved", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", domain.RunCompleted, domain.TaskBacklog},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := newRunnerTest(t)
			ctx := context.Background()
			task := rt.task(t)
			r := rt.start(t, task)
			rt.syncMust(t, []runnerwire.Report{{RunID: r.ID, Observations: []runnerwire.Observation{rt.started(r)}}})
			before, _ := rt.f.tasks.Get(ctx, task.ID)
			if tc.moved != "" {
				var e error
				before, e = rt.f.tasks.Update(ctx, task.ID, TaskPatch{Version: before.Version, State: &tc.moved})
				if e != nil {
					t.Fatal(e)
				}
			}
			res := agent.Result{State: tc.end}
			report := []runnerwire.Report{{RunID: r.ID, Observations: []runnerwire.Observation{{Seq: 2, Kind: "ended", HeadCommit: tc.head, Result: &res}}}}
			rt.syncMust(t, report)
			after, _ := rt.f.tasks.Get(ctx, task.ID)
			want := before.State
			if tc.end == domain.RunCompleted && tc.head != "" && tc.head != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" && want == domain.TaskDoing {
				want = domain.TaskReview
			}
			if after.State != want {
				t.Fatalf("state %s want %s", after.State, want)
			}
			rt.syncMust(t, report)
			again, _ := rt.f.tasks.Get(ctx, task.ID)
			if again.Version != after.Version {
				t.Fatal("duplicate report moved task")
			}
		})
	}
}

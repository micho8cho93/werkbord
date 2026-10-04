package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTaskStatesAreExactlyFour(t *testing.T) {
	want := []TaskState{"backlog", "doing", "review", "done"}
	if len(TaskStates) != len(want) {
		t.Fatalf("got %d task states, want %d", len(TaskStates), len(want))
	}
	for i, s := range want {
		if TaskStates[i] != s {
			t.Errorf("TaskStates[%d] = %q, want %q", i, TaskStates[i], s)
		}
	}
	for _, bad := range []string{"", "todo", "in_progress", "Backlog"} {
		if _, err := ParseTaskState(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseTaskState(%q) error = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestRunTransitions(t *testing.T) {
	cases := []struct {
		from, to RunState
		ok       bool
	}{
		{RunStarting, RunRunning, true},
		{RunStarting, RunWaitingForUser, false},
		{RunRunning, RunWaitingForUser, true},
		{RunWaitingForUser, RunRunning, true},
		{RunWaitingForUser, RunCompleted, true}, // finishing an idle session
		{RunStarting, RunCompleted, false},
		{RunRunning, RunCompleted, true},
		{RunRunning, RunStopped, true},
		{RunCompleted, RunRunning, false},
		{RunFailed, RunStarting, false},
		{RunStopped, RunRunning, false},
	}
	for _, c := range cases {
		if got := c.from.CanTransitionTo(c.to); got != c.ok {
			t.Errorf("%s -> %s = %v, want %v", c.from, c.to, got, c.ok)
		}
	}
}

func TestRunTransitionSetsEndedAt(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	r := &Run{ID: "run_x", State: RunRunning}
	if err := r.Transition(RunFailed, "boom", now); err != nil {
		t.Fatal(err)
	}
	if r.EndedAt == nil || !r.EndedAt.Equal(now) || r.Reason != "boom" {
		t.Fatalf("unexpected run after transition: %+v", r)
	}
	if err := r.Transition(RunRunning, "", now); !errors.Is(err, ErrTransition) {
		t.Fatalf("transition out of terminal state: err = %v, want ErrTransition", err)
	}
}

func TestValidateTaskTitle(t *testing.T) {
	if got, err := ValidateTaskTitle("  Fix it  "); err != nil || got != "Fix it" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := ValidateTaskTitle("   "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank title: err = %v", err)
	}
	if _, err := ValidateTaskTitle(strings.Repeat("a", 201)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long title: err = %v", err)
	}
}

func TestNewIDHasPrefixAndIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID(PrefixTask)
		if !strings.HasPrefix(id, "tsk_") || len(id) != len("tsk_")+16 {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestRunWaitingKinds(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	r := &Run{ID: "run_x", State: RunRunning}
	if err := r.Transition(RunWaitingForUser, "", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Transition into waiting must demand a kind: err = %v", err)
	}
	if err := r.WaitFor(WaitNone, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("WaitFor(none): err = %v", err)
	}
	if err := r.WaitFor(WaitQuestion, now); err != nil || r.State != RunWaitingForUser || r.Waiting != WaitQuestion {
		t.Fatalf("WaitFor: %+v, %v", r, err)
	}
	if err := r.Transition(RunRunning, "", now); err != nil || r.Waiting != WaitNone {
		t.Fatalf("leaving the waiting state must clear what it waited for: %+v, %v", r, err)
	}
	r.PID, r.ProcessID = 42, "start-time"
	if err := r.Transition(RunStopped, "stopped", now); err != nil || r.PID != 0 || r.ProcessID != "" {
		t.Fatalf("an ended run must not keep a process: %+v, %v", r, err)
	}
	starting := &Run{ID: "run_y", State: RunStarting}
	if err := starting.WaitFor(WaitIdle, now); !errors.Is(err, ErrTransition) {
		t.Fatalf("starting -> waiting: err = %v", err)
	}
}

func TestOutputStreams(t *testing.T) {
	for _, s := range []OutputStream{StreamAssistant, StreamTool, StreamUser, StreamSystem, StreamStderr} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if OutputStream("stdout").Valid() || OutputStream("").Valid() {
		t.Error("unknown streams must be invalid")
	}
}

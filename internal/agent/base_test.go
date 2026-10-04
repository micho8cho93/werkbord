package agent

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"devboard/internal/domain"
)

// shell launches `sh -c script` as a session whose stdout lines are collected.
func shell(t *testing.T, script string) (*Base, *lineLog) {
	t.Helper()
	log := &lineLog{}
	b := NewBase(ProcSpec{Command: "/bin/sh", Args: []string{"-c", script}, Dir: t.TempDir(), Env: os.Environ()})
	b.OnLine = func(line []byte, truncated bool) { log.add(string(line)) }
	if err := b.Launch(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = b.Stop(ctx)
		// Drain so the queue's pump goroutine ends.
		for range b.Events() {
		}
	})
	return b, log
}

type lineLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *lineLog) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }
func (l *lineLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func (l *lineLog) waitFor(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := l.get(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d lines; have %v", n, l.get())
	return nil
}

func waitResult(t *testing.T, b *Base) Result {
	t.Helper()
	ch := make(chan Result, 1)
	go func() { ch <- b.Wait() }()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end")
		return Result{}
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d is still running", pid)
}

func TestSessionRoundTripAndGracefulEnd(t *testing.T) {
	b, log := shell(t, `while read l; do echo "got:$l"; done`)
	if err := b.WriteLine([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteLine([]byte("two")); err != nil {
		t.Fatal(err)
	}
	if got := log.waitFor(t, 2); got[0] != "got:one" || got[1] != "got:two" {
		t.Fatalf("lines = %v", got)
	}
	if err := b.CloseInput(); err != nil {
		t.Fatal(err)
	}
	res := waitResult(t, b)
	if res.State != domain.RunCompleted || res.ExitCode != 0 || res.Reason != "" {
		t.Fatalf("result = %+v; closing stdin is a graceful end", res)
	}
	if err := b.WriteLine([]byte("late")); !errors.Is(err, ErrEnded) {
		t.Fatalf("write after the end: err = %v, want ErrEnded", err)
	}
	if err := b.CloseInput(); !errors.Is(err, ErrEnded) {
		t.Fatalf("close after the end: err = %v, want ErrEnded", err)
	}
	if _, open := <-b.Events(); open {
		// Events must be closed once the session has ended and been drained.
		for range b.Events() {
		}
	}
	if info := b.Process(); info.PID == 0 || info.ID == "" {
		t.Fatalf("process info = %+v; the pid and an identity must be recorded", info)
	}
}

func TestFailureReportsExitStatusAndStderr(t *testing.T) {
	b, _ := shell(t, `echo "something broke" >&2; exit 3`)
	res := waitResult(t, b)
	if res.State != domain.RunFailed || res.ExitCode != 3 {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(res.Reason, "status 3") || !strings.Contains(res.Reason, "something broke") {
		t.Fatalf("reason = %q; it should carry the exit status and the last line of stderr", res.Reason)
	}
	var sawStderr bool
	for ev := range b.Events() {
		if ev.Kind == KindOutput && ev.Stream == domain.StreamStderr && ev.Text == "something broke" {
			sawStderr = true
		}
	}
	if !sawStderr {
		t.Fatal("stderr was not delivered as output")
	}
}

func TestFailureHookOverridesGenericReason(t *testing.T) {
	b := NewBase(ProcSpec{Command: "/bin/sh", Args: []string{"-c", "exit 1"}, Env: os.Environ()})
	b.Failure = func() string { return "Invalid API key" }
	if err := b.Launch(); err != nil {
		t.Fatal(err)
	}
	res := waitResult(t, b)
	if res.Reason != "Invalid API key" {
		t.Fatalf("reason = %q; the agent's own account should win", res.Reason)
	}
	for range b.Events() {
	}
}

func TestStopKillsTheWholeProcessGroup(t *testing.T) {
	old := StopGrace
	StopGrace = 200 * time.Millisecond
	t.Cleanup(func() { StopGrace = old })

	// The shell ignores SIGTERM, and has a child of its own, as an agent with a
	// tool or dev server running would.
	b, log := shell(t, `trap '' TERM; sleep 300 & echo $!; while :; do sleep 1; done`)
	childPID, err := strconv.Atoi(log.waitFor(t, 1)[0])
	if err != nil {
		t.Fatal(err)
	}
	if !alive(childPID) {
		t.Fatal("the grandchild is not running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	res := waitResult(t, b)
	if res.State != domain.RunFailed || res.ExitCode != -1 || !strings.Contains(res.Reason, "signal") {
		t.Fatalf("result = %+v; a killed process is reported as failed by signal (the controller calls it stopped)", res)
	}
	waitGone(t, childPID)
}

func TestStopEscalatesOnlyWhenNeeded(t *testing.T) {
	old := StopGrace
	StopGrace = 5 * time.Second
	t.Cleanup(func() { StopGrace = old })
	b, log := shell(t, `echo ready; sleep 300`)
	log.waitFor(t, 1)
	start := time.Now()
	if err := b.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Stop took %v for a process that obeys SIGTERM", d)
	}
}

func TestProcessLeftoversDieWithTheAgent(t *testing.T) {
	old := drainWait
	drainWait = 300 * time.Millisecond
	t.Cleanup(func() { drainWait = old })

	// The agent exits at once, but a background job keeps its stdout open.
	b, log := shell(t, `sleep 300 & echo $!; exit 0`)
	childPID, err := strconv.Atoi(log.waitFor(t, 1)[0])
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res := waitResult(t, b)
	if res.State != domain.RunCompleted {
		t.Fatalf("result = %+v", res)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("a leftover child holding the pipe delayed the end by %v", d)
	}
	waitGone(t, childPID)
	for range b.Events() {
	}
}

func TestWriteToAgentThatStoppedReadingDoesNotHang(t *testing.T) {
	old := writeTimeout
	writeTimeout = 300 * time.Millisecond
	t.Cleanup(func() { writeTimeout = old })

	b, _ := shell(t, `sleep 300`) // never reads stdin
	big := []byte(strings.Repeat("x", 1<<20))
	done := make(chan error, 1)
	go func() { done <- b.WriteLine(big) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a 1 MiB write to a process that does not read must not succeed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WriteLine hung on a stuck agent")
	}
	// A partly written line corrupts the stream, so the pipe is now unusable.
	if err := b.WriteLine([]byte("again")); err == nil {
		t.Fatal("the stream should be closed after a partial write")
	}
}

func TestStartFailureIsReported(t *testing.T) {
	b := NewBase(ProcSpec{Command: "/definitely/not/here", Env: os.Environ()})
	if err := b.Launch(); err == nil {
		t.Fatal("expected an error for a missing executable")
	}
	if _, open := <-b.Events(); open {
		t.Fatal("events of a session that never started must be closed")
	}
}

func TestReap(t *testing.T) {
	b, _ := shell(t, `echo ready; sleep 300`)
	info := b.Process()

	if res, err := Reap(info.PID, "Thu Jan  1 00:00:00 1970", time.Second); err != nil || res != ReapForeign {
		t.Fatalf("wrong identity: %v, %v; a reused pid must never be signalled", res, err)
	}
	if res, _ := Reap(info.PID, "", time.Second); res != ReapForeign {
		t.Fatalf("no identity recorded: %v; must not signal", res)
	}
	if !alive(info.PID) {
		t.Fatal("a refused reap killed the process")
	}
	if res, err := Reap(info.PID, info.ID, 2*time.Second); err != nil || res != ReapKilled {
		t.Fatalf("matching identity: %v, %v", res, err)
	}
	waitResult(t, b)
	if res, _ := Reap(info.PID, info.ID, time.Second); res != ReapGone {
		t.Fatalf("already dead: %v", res)
	}
	if res, _ := Reap(0, "x", time.Second); res != ReapGone {
		t.Fatalf("pid 0: %v", res)
	}
	if res, _ := Reap(1, "x", time.Second); res != ReapGone {
		t.Fatalf("pid 1 must be refused: %v", res)
	}
}

func TestReadLinesTruncatesAndKeepsTheRest(t *testing.T) {
	long := strings.Repeat("a", 100)
	input := "short\n" + long + "\nlast without newline"
	var got []string
	var cut []bool
	if err := readLines(strings.NewReader(input), 10, func(line []byte, truncated bool) {
		got = append(got, string(line))
		cut = append(cut, truncated)
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"short", strings.Repeat("a", 10), "last witho"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if cut[0] || !cut[1] || !cut[2] {
		t.Fatalf("truncated flags = %v", cut)
	}
}

func TestReadLinesHandlesLinesLongerThanTheBuffer(t *testing.T) {
	line := strings.Repeat("b", 200<<10) // longer than the 64 KiB read buffer
	var got []string
	if err := readLines(strings.NewReader(line+"\r\nnext\n"), 1<<20, func(l []byte, truncated bool) {
		if truncated {
			t.Error("a line under the limit must not be truncated")
		}
		got = append(got, string(l))
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != line || got[1] != "next" {
		t.Fatalf("got %d lines, first %d bytes", len(got), len(got[0]))
	}
}

func TestEventQueueDropsOnlyOutput(t *testing.T) {
	q := newEventQueue(3)
	// Nothing consumes yet, but the pump moves up to 64+1 events into the channel,
	// so fill past that to reach the limit.
	total := 64 + 1 + 3
	for i := 0; i < total; i++ {
		q.push(Event{Kind: KindOutput, Text: "x"})
	}
	q.push(Event{Kind: KindOutput, Text: "dropped-1"})
	q.push(Event{Kind: KindOutput, Text: "dropped-2"})
	q.push(Event{Kind: KindQuestion, Question: &Question{Ref: "q"}})
	q.push(Event{Kind: KindTurnEnd})
	q.close()

	var kinds []EventKind
	var notice string
	for ev := range q.events() {
		kinds = append(kinds, ev.Kind)
		if ev.Stream == domain.StreamSystem {
			notice = ev.Text
		}
	}
	// The notice about what was dropped is queued last, when the queue closes.
	if n := len(kinds); n < 4 || kinds[n-1] != KindOutput || kinds[n-2] != KindTurnEnd || kinds[n-3] != KindQuestion {
		t.Fatalf("control events were lost or reordered: %v", kinds[len(kinds)-4:])
	}
	if !strings.Contains(notice, "dropped") {
		t.Fatalf("no notice about dropped output (events: %d)", len(kinds))
	}
	if q.push(Event{Kind: KindOutput}) {
		t.Fatal("push after close must report false")
	}
}

func TestSanitizedEnv(t *testing.T) {
	in := []string{"PATH=/bin", "HOME=/h", "GIT_DIR=/x/.git", "GIT_WORK_TREE=/x", "GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.sshCommand", "GIT_CONFIG_VALUE_0=evil", "GIT_AUTHOR_NAME=me", "ANTHROPIC_API_KEY=k"}
	got := strings.Join(SanitizedEnv(in), " ")
	for _, gone := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s survived: %s", gone, got)
		}
	}
	for _, kept := range []string{"PATH=/bin", "HOME=/h", "GIT_AUTHOR_NAME=me", "ANTHROPIC_API_KEY=k"} {
		if !strings.Contains(got, kept) {
			t.Errorf("%s was removed: %s", kept, got)
		}
	}
}

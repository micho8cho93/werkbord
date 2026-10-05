package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devboard/internal/domain"
)

// Tunables, variables so tests can shorten them.
var (
	// StopGrace is how long Stop waits after SIGTERM before SIGKILL.
	StopGrace = 5 * time.Second
	// drainWait is how long, after the process exits, its output pipes are
	// given to reach end-of-file. A process the agent started may hold them open.
	drainWait = 2 * time.Second
	// writeTimeout bounds a write to the agent's stdin.
	writeTimeout = 10 * time.Second
)

// Base implements the parts of a Session that do not depend on the agent's
// protocol: the process and its pipes, the event queue, ending and stopping,
// and the Result. An adapter embeds it, sets OnLine, then calls Launch.
//
//	b := agent.NewBase(spec)
//	s := &session{Base: b}
//	b.OnLine = s.onLine
//	if err := b.Launch(); err != nil { ... }
type Base struct {
	Spec ProcSpec
	// OnLine is called for each line the agent writes to stdout, in order, from
	// one goroutine. truncated is set when the line was longer than the limit.
	OnLine func(line []byte, truncated bool)
	// Failure, if set, is asked for the agent's own account of what went wrong
	// when the process exits unsuccessfully; it takes precedence over a generic
	// message built from the exit status and standard error.
	Failure func() string

	proc   *process
	q      *eventQueue
	stderr tail
	done   chan struct{}
	result Result
	ended  atomic.Bool

	stopOnce sync.Once
}

// NewBase returns an unlaunched Base for the process described by spec.
func NewBase(spec ProcSpec) *Base {
	return &Base{Spec: spec, q: newEventQueue(defaultQueueLimit), done: make(chan struct{})}
}

// Launch starts the process and the goroutines that serve it.
func (b *Base) Launch() error {
	p, err := startProcess(b.Spec)
	if err != nil {
		b.q.close()
		close(b.done)
		return fmt.Errorf("start %s: %w", b.Spec.Command, err)
	}
	b.proc = p

	stdoutDone, stderrDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stdoutDone)
		_ = readLines(p.stdout, maxLineBytes, func(line []byte, truncated bool) {
			if b.OnLine != nil {
				b.OnLine(line, truncated)
			}
		})
	}()
	go func() {
		defer close(stderrDone)
		_ = readLines(p.stderr, 64<<10, func(line []byte, _ bool) {
			text := strings.TrimSpace(string(line))
			if text == "" {
				return
			}
			b.stderr.add(text)
			b.Emit(Event{Kind: KindOutput, Stream: domain.StreamStderr, Text: text})
		})
	}()
	go b.supervise(stdoutDone, stderrDone)
	return nil
}

// supervise waits for the process, lets its output drain, cleans up after it
// and publishes the Result.
func (b *Base) supervise(stdoutDone, stderrDone <-chan struct{}) {
	p := b.proc
	<-p.exited
	b.ended.Store(true)
	p.killGroup() // nothing the agent started outlives it; also releases the pipes it held

	drain := time.NewTimer(drainWait)
	defer drain.Stop()
	for _, ch := range []<-chan struct{}{stdoutDone, stderrDone} {
		select {
		case <-ch:
		case <-drain.C:
			p.closeReaders()
			<-ch
		}
	}
	p.closeReaders()
	p.closeStdin()

	code, sig := p.status()
	res := Result{State: domain.RunCompleted, ExitCode: code}
	if code != 0 {
		res.State = domain.RunFailed
		res.Reason = b.failureReason(code, sig)
	}
	b.result = res
	b.q.close()
	close(b.done)
}

func (b *Base) failureReason(code int, sig string) string {
	if b.Failure != nil {
		if r := strings.TrimSpace(b.Failure()); r != "" {
			return r
		}
	}
	reason := fmt.Sprintf("exited with status %d", code)
	if sig != "" {
		reason = "killed by signal " + sig
	}
	if t := b.stderr.String(); t != "" {
		reason += ": " + lastLine(t)
	}
	return reason
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// Emit queues an event for the consumer. It never blocks.
func (b *Base) Emit(ev Event) { b.q.push(ev) }

// Say queues an output event.
func (b *Base) Say(stream domain.OutputStream, format string, args ...any) {
	b.Emit(Event{Kind: KindOutput, Stream: stream, Text: fmt.Sprintf(format, args...)})
}

// WriteLine sends one line to the agent's stdin. It returns ErrEnded if the
// process has ended or its stdin is closed.
func (b *Base) WriteLine(line []byte) error { return b.proc.writeLine(line, writeTimeout) }

// Events implements Session.
func (b *Base) Events() <-chan Event { return b.q.events() }

// Wait implements Session.
func (b *Base) Wait() Result {
	<-b.done
	return b.result
}

// ExitedWithin waits up to d for the process to end. Adapters call it right
// after delivering the first message, so a process that dies at once (an
// unsupported flag, a missing dependency) fails Start instead of looking like a
// session that ran.
func (b *Base) ExitedWithin(d time.Duration) (Result, bool) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-b.done:
		return b.result, true
	case <-t.C:
		return Result{}, false
	}
}

// Done is closed when the session has ended and its Result is available.
func (b *Base) Done() <-chan struct{} { return b.done }

// StderrTail returns the last few lines the agent wrote to standard error.
func (b *Base) StderrTail() string { return b.stderr.String() }

// Ended reports whether the process has exited.
func (b *Base) Ended() bool { return b.ended.Load() }

// Process implements Session.
func (b *Base) Process() ProcessInfo {
	if b.proc == nil {
		return ProcessInfo{}
	}
	return ProcessInfo{PID: b.proc.pid, ID: b.proc.identity}
}

// CloseInput closes the agent's stdin, which is how both supported agents are
// told to finish. It is the whole of a graceful Close for them.
func (b *Base) CloseInput() error {
	if b.Ended() {
		return ErrEnded
	}
	b.proc.closeStdin()
	return nil
}

// Stop implements Session. It returns when the process is gone or ctx is done.
func (b *Base) Stop(ctx context.Context) error {
	if b.proc == nil {
		return nil
	}
	b.stopOnce.Do(func() { go b.proc.terminate(StopGrace) })
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SanitizedEnv returns env without the variables that make Git act on a
// repository other than the one in the agent's working directory (as happens
// when the controller was started from a Git hook), and without variables that
// would confuse a nested agent. Everything else, including PATH, credentials
// and the user's own configuration, is kept: the agent runs as the user.
func SanitizedEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if domain.IsControllerSecret(kv) {
			continue
		}
		if strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		switch name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_COMMON_DIR", "GIT_PREFIX", "GIT_NAMESPACE", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_QUARANTINE_PATH",
			"GIT_IMPLICIT_WORK_TREE", "GIT_INTERNAL_SUPER_PREFIX":
			continue
		}
		out = append(out, kv)
	}
	return out
}

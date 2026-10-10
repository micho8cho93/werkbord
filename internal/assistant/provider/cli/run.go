// Package cli runs a provider's command line for one turn, line by line, and stops it, with everything it started,
// when the turn is cancelled or ends.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"devboard/internal/agent"
)

// Spec is the command to run.
type Spec struct {
	Path  string
	Args  []string
	Dir   string
	Stdin string
	// Interactive keeps the process's input open so the caller can write to it while it runs (see Writer). Stdin is
	// ignored then.
	Interactive bool
	// Env is added to the process's environment, after the controller's own secrets have been removed from it.
	Env []string
}

// Limits.
const (
	// MaxLine bounds one line of output. A longer line is dropped, not buffered without end.
	MaxLine = 4 << 20
	// stderrKeep is how much of the end of the error output is kept to explain a failure.
	stderrKeep = 4 << 10
	// stopGrace is how long a cancelled process gets to exit on its own before it is killed.
	stopGrace = 2 * time.Second
)

// Exit says how the process ended.
type Exit struct {
	Code int // -1 if it was killed by a signal
	// Stderr is the end of what it wrote to its error output.
	Stderr string
}

// Run starts the command and calls onLine for each line it writes to stdout, in order, on the calling goroutine.
// onLine returns false to stop reading and end the process (the caller has seen enough, or a rule was broken).
//
// When ctx is cancelled the process group is asked to stop, and killed if it does not. Run returns after the process
// is gone and its output has been delivered; the error is ctx.Err() if the context ended it.
func Run(ctx context.Context, s Spec, onLine func(line []byte) bool) (Exit, error) {
	s.Interactive = false
	return RunInteractive(ctx, s, nil, func(line []byte, _ *Writer) bool { return onLine(line) })
}

// Writer sends lines to a running process.
type Writer struct {
	mu     sync.Mutex
	w      io.WriteCloser
	closed bool
}

// WriteLine writes one line. It returns an error once the process's input is closed.
func (w *Writer) WriteLine(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return io.ErrClosedPipe
	}
	_, err := w.w.Write(append(append([]byte{}, b...), '\n'))
	return err
}

// Close ends the process's input, which is how a process that reads until the end of its input is told to finish.
func (w *Writer) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		_ = w.w.Close()
	}
}

// RunInteractive is Run for a process that is talked to: start is called once the process is running, and onLine is
// given a Writer with each line.
func RunInteractive(ctx context.Context, s Spec, start func(*Writer), onLine func(line []byte, w *Writer) bool) (Exit, error) {
	cmd := exec.Command(s.Path, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = append(agent.SanitizedEnv(os.Environ()), s.Env...)
	setProcAttr(cmd)
	var writer *Writer
	if s.Interactive {
		in, err := cmd.StdinPipe()
		if err != nil {
			return Exit{}, err
		}
		writer = &Writer{w: in}
	} else if s.Stdin != "" {
		cmd.Stdin = strings.NewReader(s.Stdin)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Exit{}, err
	}
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Exit{}, fmt.Errorf("start %s: %w", s.Path, err)
	}

	var mu sync.Mutex
	var hardKill *time.Timer
	done, stopped := false, false
	stop := func() {
		mu.Lock()
		defer mu.Unlock()
		if done || stopped {
			return
		}
		stopped = true
		terminate(cmd)
		hardKill = time.AfterFunc(stopGrace, func() { kill(cmd) })
	}
	watch := make(chan struct{})
	defer close(watch)
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-watch:
		}
	}()

	if writer != nil {
		defer writer.Close()
		if start != nil {
			start(writer)
		}
	}
	r := bufio.NewReaderSize(stdout, 64<<10)
	var readErr error
	for {
		line, tooLong, err := readLine(r)
		if len(line) > 0 && !tooLong {
			if !onLine(line, writer) {
				stop()
				break
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				readErr = err
			}
			break
		}
	}
	// The process may still be writing if we stopped early; drain so that Wait can return.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	mu.Lock()
	done = true // nothing is signalled once the process is gone
	if hardKill != nil {
		hardKill.Stop()
	}
	mu.Unlock()
	exit := Exit{Stderr: stderr.String()}
	if cmd.ProcessState != nil {
		exit.Code = cmd.ProcessState.ExitCode()
	}
	// The group may have left children behind that still hold the pipes; make sure none survive the turn.
	kill(cmd)
	switch {
	case ctx.Err() != nil:
		return exit, ctx.Err()
	case readErr != nil:
		return exit, readErr
	}
	_ = waitErr // a non-zero exit is reported through Exit; the caller knows what it means for its protocol
	return exit, nil
}

// readLine reads up to MaxLine bytes of the next line. A longer line is consumed whole and reported tooLong.
func readLine(r *bufio.Reader) (line []byte, tooLong bool, err error) {
	var buf bytes.Buffer
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return bytes.TrimRight(buf.Bytes(), "\r"), tooLong, err
		}
		if !tooLong {
			if buf.Len()+len(chunk) > MaxLine {
				tooLong = true
				buf.Reset()
			} else {
				buf.Write(chunk)
			}
		}
		if !isPrefix {
			return bytes.TrimRight(buf.Bytes(), "\r"), tooLong, nil
		}
	}
}

// tail keeps the last stderrKeep bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrKeep {
		t.buf = t.buf[len(t.buf)-stderrKeep:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

package agent

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcSpec describes the process behind a session.
type ProcSpec struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
}

// process is a child in its own process group, with pipes the caller owns the
// ends of. It exists so that adapters get the same guarantees: the child and
// everything it started die together, a stuck child cannot block the
// controller, and nothing is signalled that is not ours.
type process struct {
	cmd      *exec.Cmd
	stdin    *os.File
	stdout   *os.File
	stderr   *os.File
	pid      int
	identity string

	exited  chan struct{} // closed once the child has been reaped
	state   *os.ProcessState
	waitErr error

	wmu         sync.Mutex // one write at a time, so lines are never interleaved
	stdinClosed bool
	stdinBroken bool
}

func startProcess(spec ProcSpec) (*process, error) {
	if runtimeUnsupported {
		return nil, errNotUnix
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		closeAll(inR, inW)
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		closeAll(inR, inW, outR, outW)
		return nil, err
	}
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir, cmd.Env = spec.Dir, spec.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	setProcAttr(cmd)
	startErr := cmd.Start()
	// The child has its own copies now; ours would keep the pipes open.
	closeAll(inR, outW, errW)
	if startErr != nil {
		closeAll(inW, outR, errR)
		return nil, startErr
	}
	p := &process{cmd: cmd, stdin: inW, stdout: outR, stderr: errR, pid: cmd.Process.Pid, exited: make(chan struct{})}
	p.identity = processIdentity(p.pid)
	go func() {
		p.waitErr = cmd.Wait()
		p.state = cmd.ProcessState
		close(p.exited)
	}()
	return p, nil
}

func closeAll(files ...*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

// writeLine writes b and a newline to the child's stdin atomically with
// respect to other writers. It gives up at the context-less deadline: a child
// that stopped reading must not hold a request, or the controller's lock, forever.
func (p *process) writeLine(b []byte, timeout time.Duration) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if p.stdinClosed || p.stdinBroken {
		return ErrEnded
	}
	select {
	case <-p.exited:
		return ErrEnded
	default:
	}
	_ = p.stdin.SetWriteDeadline(time.Now().Add(timeout))
	buf := make([]byte, 0, len(b)+1)
	buf = append(append(buf, b...), '\n')
	n, err := p.stdin.Write(buf)
	if err != nil {
		if n > 0 {
			// A partial line would corrupt the protocol for good.
			p.stdinBroken = true
			_ = p.stdin.Close()
		}
		if isClosedPipe(err) {
			return ErrEnded
		}
		return fmt.Errorf("write to agent: %w", err)
	}
	return nil
}

func (p *process) closeStdin() {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if !p.stdinClosed {
		p.stdinClosed = true
		_ = p.stdin.Close()
	}
}

// terminate stops the child's whole process group: SIGTERM, then SIGKILL if it
// is still there after grace. It returns once the child has been reaped.
func (p *process) terminate(grace time.Duration) {
	select {
	case <-p.exited:
	default:
		signalGroup(p.pid, syscall.SIGTERM)
		select {
		case <-p.exited:
		case <-time.After(grace):
			signalGroup(p.pid, syscall.SIGKILL)
			select {
			case <-p.exited:
			case <-time.After(5 * time.Second):
			}
		}
	}
	p.killGroup()
}

// killGroup kills whatever the child left running in its group (a dev server
// the agent started, say). It is only called after the child itself is gone or
// being stopped, when the group is no longer anyone's work in progress.
func (p *process) killGroup() { signalGroup(p.pid, syscall.SIGKILL) }

// status describes how the child ended. Only valid after exited is closed.
func (p *process) status() (code int, signal string) {
	if p.state == nil {
		return -1, ""
	}
	if ws, ok := p.state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return -1, ws.Signal().String()
	}
	return p.state.ExitCode(), ""
}

// tail keeps the last few lines of a stream, for error messages.
type tail struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func (t *tail) add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.max == 0 {
		t.max = 8
	}
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, "\n")
}

// readerClosers lets the supervisor cut a pipe that a grandchild holds open.
func (p *process) closeReaders() {
	_ = p.stdout.Close()
	_ = p.stderr.Close()
}

var _ io.Reader = (*os.File)(nil)

//go:build darwin || linux

package terminal

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"devboard/internal/agent"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const supported = true

type process struct {
	cmd      *exec.Cmd
	terminal *os.File
	identity string
}

func start(path, dir string, cols, rows int) (*process, error) {
	cmd := exec.Command(path)
	cmd.Dir = dir
	for _, entry := range agent.SanitizedEnv(os.Environ()) {
		if !strings.HasPrefix(entry, "TERM=") && !strings.HasPrefix(entry, "COLORTERM=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	// Nonblocking descriptors permit bounded writes and unblock reads on Close.
	fd, err := unix.FcntlInt(f.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
		_ = f.Close()
		return nil, err
	}
	_ = f.Close()
	if err = syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
		return nil, err
	}
	f = os.NewFile(uintptr(fd), "coding-agent-pty")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b, _ := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
	return &process{cmd: cmd, terminal: f, identity: strings.Join(strings.Fields(string(b)), " ")}, nil
}
func (p *process) info() agent.ProcessInfo {
	return agent.ProcessInfo{PID: p.cmd.Process.Pid, ID: p.identity}
}
func (p *process) read(b []byte) (int, error) { return p.terminal.Read(b) }
func (p *process) write(b []byte) error {
	_ = p.terminal.SetWriteDeadline(time.Now().Add(2 * time.Second))
	for len(b) > 0 {
		n, e := p.terminal.Write(b)
		if e != nil {
			return e
		}
		b = b[n:]
	}
	return nil
}
func (p *process) resize(c, r int) error {
	return pty.Setsize(p.terminal, &pty.Winsize{Cols: uint16(c), Rows: uint16(r)})
}
func (p *process) kill()       { _ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL) }
func (p *process) wait() error { return p.cmd.Wait() }
func (p *process) close()      { _ = p.terminal.Close() }

//go:build unix

package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const runtimeUnsupported = false

// setProcAttr puts the child in a new process group, so a signal to the group
// reaches the agent and every tool it started, and Ctrl-C in a terminal running
// the controller does not reach agents behind its back.
func setProcAttr(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// signalGroup signals the process group led by pid.
func signalGroup(pid int, sig syscall.Signal) {
	if pid > 1 {
		_ = syscall.Kill(-pid, sig)
	}
}

func isClosedPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed)
}

// processIdentity returns a token that identifies the process pid: its start
// time. A later process that reuses the pid has a different one. It is empty if
// the process does not exist or cannot be examined.
func processIdentity(pid int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

// ReapResult says what Reap found.
type ReapResult int

const (
	// ReapGone: no such process, so there was nothing to do.
	ReapGone ReapResult = iota
	// ReapKilled: it was the recorded process and has been killed, with its group.
	ReapKilled
	// ReapForeign: a process with that pid exists but is not the recorded one, or
	// there was no way to tell, so it was left alone.
	ReapForeign
)

// Reap stops a process a previous controller started and did not clean up. It
// only acts if pid still has the recorded identity and leads its own process
// group, so a pid that was reused by an unrelated program is never signalled.
func Reap(pid int, identity string, grace time.Duration) (ReapResult, error) {
	if pid <= 1 {
		return ReapGone, nil
	}
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return ReapGone, nil
	}
	if identity == "" || processIdentity(pid) != identity {
		return ReapForeign, nil
	}
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
		return ReapForeign, nil
	}
	signalGroup(pid, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			signalGroup(pid, syscall.SIGKILL) // anything it left behind
			return ReapKilled, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	signalGroup(pid, syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
	return ReapKilled, nil
}

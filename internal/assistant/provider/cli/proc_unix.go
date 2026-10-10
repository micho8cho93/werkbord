//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// setProcAttr puts the child in its own process group, so that one signal reaches it and everything it starts.
func setProcAttr(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func terminate(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGTERM) }

func kill(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGKILL) }

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process != nil && cmd.Process.Pid > 1 {
		_ = syscall.Kill(-cmd.Process.Pid, sig)
	}
}

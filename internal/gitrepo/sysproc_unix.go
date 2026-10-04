//go:build unix

package gitrepo

import (
	"os/exec"
	"syscall"
)

// detach runs a command that talks to the network in its own session. It then
// has no controlling terminal, so ssh cannot stop to ask a person for a
// passphrase or to confirm a host key that nobody is there to answer (it fails
// at once instead), and on a timeout the whole process group is killed, ssh and
// credential helpers included, not only git.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

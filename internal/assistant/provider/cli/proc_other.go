//go:build !unix

package cli

import "os/exec"

func setProcAttr(*exec.Cmd) {}

func terminate(cmd *exec.Cmd) { kill(cmd) }

func kill(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

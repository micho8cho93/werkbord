//go:build !unix

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

func alive(p *os.Process) bool {
	// On Windows FindProcess opens the process, which fails if it does not exist.
	return p != nil
}

func terminate(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	return p.Kill()
}

func kill(pid int) error { return terminate(pid) }

func spawnDetached(spec Spec) (int, error) {
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Env = os.Environ()
	if spec.DataDir != "" {
		cmd.Env = append(cmd.Env, "WERKBORD_DATA_DIR="+spec.DataDir, "DEVBOARD_DATA_DIR="+spec.DataDir)
	}
	if spec.LogFile != "" {
		f, err := os.OpenFile(spec.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return 0, err
		}
		defer f.Close()
		cmd.Stdout, cmd.Stderr = f, f
	}
	// DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

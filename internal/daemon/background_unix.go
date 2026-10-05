//go:build unix

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

func alive(p *os.Process) bool { return p.Signal(syscall.Signal(0)) == nil }

func terminate(pid int) error {
	err := syscall.Kill(pid, syscall.SIGTERM)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func kill(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }

// spawnDetached starts the controller in its own session, so that closing the
// terminal it was started from does not end it.
func spawnDetached(spec Spec) (int, error) {
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Env = os.Environ()
	if spec.Path != "" {
		cmd.Env = append(cmd.Env, "PATH="+spec.Path)
	}
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }() // reap it if it exits while this process is still around (it outlives the process otherwise)
	return pid, nil
}

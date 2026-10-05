package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Background runs the controller as a detached process, for a computer with no
// service manager Werkbord can use, and for `werkbord start` before anything has
// been installed. It does not start at log-in. Its process ID is kept in the data
// directory so that stop and status find the same process.
type Background struct{ Options }

var _ Manager = (*Background)(nil)

func (b *Background) Name() string { return "background process" }

func (b *Background) pidFile() string { return filepath.Join(b.DataDir, "controller.pid") }

func (b *Background) pid() int {
	raw, err := os.ReadFile(b.pidFile())
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if n > 0 && processAlive(n) {
		return n
	}
	return 0
}

// background has no definition to write: Install records how to start it, so
// that Start works after, and says it will not start at log-in.
func (b *Background) specFile() string { return filepath.Join(b.DataDir, "controller.spec") }

func (b *Background) Install(_ context.Context, spec Spec) error {
	if b.DataDir == "" {
		return errors.New("background process: no data directory")
	}
	if err := os.MkdirAll(b.DataDir, 0o700); err != nil {
		return err
	}
	if spec.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(spec.LogFile), 0o700); err != nil {
			return err
		}
		rotateLog(spec.LogFile, LogRotateSize)
	}
	lines := append([]string{spec.Binary, spec.LogFile, spec.Path, spec.DataDir}, spec.Args...)
	return os.WriteFile(b.specFile(), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func (b *Background) loadSpec() (Spec, error) {
	raw, err := os.ReadFile(b.specFile())
	if err != nil {
		return Spec{}, ErrNotInstalled
	}
	f := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(f) < 4 {
		return Spec{}, ErrNotInstalled
	}
	return Spec{Binary: f[0], LogFile: f[1], Path: f[2], DataDir: f[3], Args: f[4:]}, nil
}

func (b *Background) Start(ctx context.Context) error {
	if b.pid() != 0 {
		return nil
	}
	spec, err := b.loadSpec()
	if err != nil {
		return err
	}
	pid, err := spawnDetached(spec)
	if err != nil {
		return fmt.Errorf("background process: start: %w", err)
	}
	return os.WriteFile(b.pidFile(), []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func (b *Background) Stop(ctx context.Context) error {
	pid := b.pid()
	if pid == 0 {
		_ = os.Remove(b.pidFile())
		return nil
	}
	if err := terminate(pid); err != nil {
		return fmt.Errorf("background process: stop: %w", err)
	}
	deadline := time.Now().Add(35 * time.Second) // the controller has 30 s to wind agents down
	for time.Now().Before(deadline) && processAlive(pid) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if processAlive(pid) {
		_ = kill(pid)
	}
	_ = os.Remove(b.pidFile())
	return nil
}

func (b *Background) Restart(ctx context.Context) error {
	if err := b.Stop(ctx); err != nil {
		return err
	}
	return b.Start(ctx)
}

func (b *Background) Uninstall(ctx context.Context) error {
	if err := b.Stop(ctx); err != nil {
		return err
	}
	_ = os.Remove(b.specFile())
	return nil
}

func (b *Background) Status(context.Context) (State, error) {
	st := State{Manager: "background process"}
	if pid := b.pid(); pid != 0 {
		st.Running, st.PID = true, pid
	}
	return st, nil
}

// signal 0 asks whether the process exists.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return alive(p)
}

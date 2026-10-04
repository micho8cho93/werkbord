package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Schtasks runs the controller as a Windows scheduled task that starts when the
// user logs on. A task needs no administrator rights and runs as the user.
type Schtasks struct{ Options }

var _ Manager = (*Schtasks)(nil)

func (s *Schtasks) Name() string { s.fill(); return "task scheduler" }

func (s *Schtasks) taskName() string { return "Devboard" }

// command is what the task runs: cmd, so that the controller's output can go to
// a log file, with the data directory in its environment.
func (s *Schtasks) command(spec Spec) string {
	inner := `"` + spec.Binary + `"`
	for _, a := range spec.Args {
		inner += " " + a
	}
	if spec.LogFile != "" {
		inner += ` >> "` + spec.LogFile + `" 2>&1`
	}
	if spec.DataDir != "" {
		inner = `set "DEVBOARD_DATA_DIR=` + spec.DataDir + `" && ` + inner
	}
	return `cmd /c "` + inner + `"`
}

func (s *Schtasks) run(ctx context.Context, what string, args ...string) (string, error) {
	out, err := s.Exec(ctx, "schtasks", args...)
	if err != nil {
		return out, errorf("task scheduler", what, out, err)
	}
	return out, nil
}

func (s *Schtasks) Install(ctx context.Context, spec Spec) error {
	s.fill()
	if spec.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(spec.LogFile), 0o700); err != nil {
			return err
		}
		rotateLog(spec.LogFile, LogRotateSize)
	}
	_, err := s.run(ctx, "install", "/Create", "/F", "/TN", s.taskName(), "/SC", "ONLOGON", "/RL", "LIMITED", "/TR", s.command(spec))
	return err
}

func (s *Schtasks) query(ctx context.Context) (string, bool) {
	out, err := s.Exec(ctx, "schtasks", "/Query", "/TN", s.taskName(), "/FO", "LIST", "/V")
	return out, err == nil
}

func (s *Schtasks) Start(ctx context.Context) error {
	s.fill()
	if _, ok := s.query(ctx); !ok {
		return ErrNotInstalled
	}
	_, err := s.run(ctx, "start", "/Run", "/TN", s.taskName())
	return err
}

func (s *Schtasks) Stop(ctx context.Context) error {
	s.fill()
	if _, ok := s.query(ctx); !ok {
		return nil
	}
	// /End ends the task's process tree; the controller is told to stop like any
	// other process, and agents it started go with it.
	_, err := s.Exec(ctx, "schtasks", "/End", "/TN", s.taskName())
	_ = err // "no running instance" is an answer, not a failure
	return nil
}

func (s *Schtasks) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
}

func (s *Schtasks) Uninstall(ctx context.Context) error {
	s.fill()
	if _, ok := s.query(ctx); !ok {
		return nil
	}
	_ = s.Stop(ctx)
	_, err := s.run(ctx, "uninstall", "/Delete", "/F", "/TN", s.taskName())
	return err
}

func (s *Schtasks) Status(ctx context.Context) (State, error) {
	s.fill()
	st := State{Manager: "task scheduler"}
	out, ok := s.query(ctx)
	if !ok {
		return st, nil
	}
	st.Installed = true
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "Status") {
			v = strings.TrimSpace(v)
			st.Running = strings.EqualFold(v, "Running")
			if !st.Running && v != "" && !strings.EqualFold(v, "Ready") {
				st.Detail = fmt.Sprintf("task scheduler says %s", v)
			}
		}
	}
	return st, nil
}

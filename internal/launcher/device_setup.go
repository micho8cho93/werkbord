package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"werkbord/internal/device"
)

type DeviceJob struct {
	Running bool   `json:"running"`
	Action  string `json:"action,omitempty"`
	Phase   string `json:"phase,omitempty"`
	Error   string `json:"error,omitempty"`
}
type DeviceStatus struct {
	Mode         string            `json:"mode"`
	Controller   string            `json:"controller,omitempty"`
	Runner       bool              `json:"runner"`
	RunnerPaired bool              `json:"runnerPaired"`
	ClientPaired bool              `json:"clientPaired"`
	Projects     []string          `json:"projects"`
	Bindings     map[string]string `json:"bindings"`
	Job          DeviceJob         `json:"job"`
}

func (l *Launcher) DeviceStatus(ctx context.Context) (DeviceStatus, error) {
	cfg, err := l.Config(ctx)
	if err != nil {
		return DeviceStatus{}, err
	}
	state, err := device.Load(cfg.DataDir)
	if err != nil {
		return DeviceStatus{}, err
	}
	out := DeviceStatus{Mode: state.Mode, Controller: state.Controller, Runner: state.Runner, Projects: []string{}, Bindings: map[string]string{}}
	if out.Mode == "" {
		out.Mode = "local"
	}
	if c, e := device.LoadCredential(cfg.DataDir); e == nil {
		out.ClientPaired = c.Controller == state.Controller || state.Controller == ""
	}
	var id struct {
		Controller string
		Projects   []string
	}
	if raw, e := os.ReadFile(filepath.Join(cfg.DataDir, "runner", "identity.json")); e == nil {
		if json.Unmarshal(raw, &id) != nil {
			return out, errors.New("runner identity cannot be read")
		}
		out.RunnerPaired = id.Controller == state.Controller || state.Controller == ""
		out.Projects = id.Projects
	}
	if raw, e := os.ReadFile(filepath.Join(cfg.DataDir, "runner", "repositories.json")); e == nil {
		if json.Unmarshal(raw, &out.Bindings) != nil {
			return out, errors.New("local clone bindings cannot be read")
		}
	}
	l.deviceMu.Lock()
	out.Job = l.deviceJob
	l.deviceMu.Unlock()
	return out, nil
}
func (l *Launcher) StartDeviceSetup(ctx context.Context, req device.SetupRequest) (DeviceJob, error) {
	if err := req.Validate(); err != nil {
		return DeviceJob{}, err
	}
	raw, _ := json.Marshal(req)
	return l.startDeviceAction(ctx, "connect", raw, req)
}
func (l *Launcher) UseLocalController(ctx context.Context) (DeviceJob, error) {
	return l.startDeviceAction(ctx, "local", nil, device.SetupRequest{})
}
func (l *Launcher) BindDeviceRepository(ctx context.Context, project, path string) (DeviceJob, error) {
	if len(project) > 120 || !strings.HasPrefix(project, "prj_") || strings.ContainsAny(project, "/\\\x00\n\r") || !filepath.IsAbs(path) {
		return DeviceJob{}, errors.New("select a project and a local clone")
	}
	raw, _ := json.Marshal(struct{ Project, Path string }{project, path})
	return l.startDeviceAction(ctx, "bind", raw, device.SetupRequest{})
}
func (l *Launcher) startDeviceAction(ctx context.Context, action string, input []byte, req device.SetupRequest) (DeviceJob, error) {
	l.deviceMu.Lock()
	defer l.deviceMu.Unlock()
	if l.deviceJob.Running {
		return l.deviceJob, errors.New("a device operation is already in progress")
	}
	// The app's lifetime context owns the subprocess. Closing a frame does not kill
	// an in-progress service transition; all launcher mutations share l.mu.
	l.deviceJob = DeviceJob{Running: true, Action: action, Phase: "working"}
	go func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		cfg, err := l.Config(runCtx)
		var output bytes.Buffer
		if err == nil {
			inst := l.FindInstall(runCtx)
			if inst.Path == "" {
				err = errors.New("open Werkbord once to install its controller program before connecting")
			} else {
				args := []string{"device", "configure"}
				if action == "local" {
					args = []string{"device", "local"}
				}
				if action == "bind" {
					args = []string{"device", "bind"}
				}
				if l.opt.RunInput != nil {
					err = l.opt.RunInput(runCtx, l.childEnv(runCtx, cfg), inst.Path, args, bytes.NewReader(input), &output)
				} else {
					err = runDeviceCommand(runCtx, l.childEnv(runCtx, cfg), inst.Path, args, bytes.NewReader(input), &output)
				}
			}
		}
		job := DeviceJob{Action: action, Phase: "done"}
		if err != nil {
			job.Phase = "failed"
			job.Error = lastLine(output.String(), err)
			for _, secret := range []string{req.Code, req.RunnerCode} {
				if secret != "" {
					job.Error = strings.ReplaceAll(job.Error, secret, "[pairing code]")
				}
			}
			if len(job.Error) > 800 {
				job.Error = "Device setup failed. Check connection status and retry with a fresh pairing code."
			}
		}
		l.deviceMu.Lock()
		l.deviceJob = job
		l.deviceMu.Unlock()
	}()
	return l.deviceJob, nil
}

type boundedOutput struct {
	io.Writer
	n int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	size := len(p)
	if b.n < 65536 {
		keep := min(len(p), 65536-b.n)
		_, _ = b.Writer.Write(p[:keep])
		b.n += keep
	}
	return size, nil
}
func runDeviceCommand(ctx context.Context, env []string, binary string, args []string, input io.Reader, output io.Writer) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = input
	bounded := &boundedOutput{Writer: output}
	cmd.Stdout = bounded
	cmd.Stderr = bounded
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("device operation failed: %w", err)
	}
	return nil
}

package shell

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"werkbord/internal/device"
	"werkbord/internal/launcher"
)

// DeviceLauncher is what guided device setup needs of the launcher. The launcher runs the installed program's own
// `device` commands, so pairing, switching and repository binding keep one implementation; this app adds a window to them.
type DeviceLauncher interface {
	DeviceStatus(ctx context.Context) (launcher.DeviceStatus, error)
	StartDeviceSetup(ctx context.Context, req device.SetupRequest) (launcher.DeviceJob, error)
	UseLocalController(ctx context.Context) (launcher.DeviceJob, error)
	BindDeviceRepository(ctx context.Context, project, path string) (launcher.DeviceJob, error)
}

func (s *Shell) deviceLauncher() (DeviceLauncher, error) {
	l, ok := s.o.Launcher.(DeviceLauncher)
	if !ok {
		return nil, errors.New("connecting to another Werkbord is unavailable in this build")
	}
	return l, nil
}

// deviceRequest is what Individual's page may supply to connect: where, a name, and one-time codes. It names no path, program
// or argument; the codes travel to the program on its standard input and are never logged or shown in a dialog.
type deviceRequest struct {
	Controller string `json:"controller"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	Runner     bool   `json:"runner"`
	RunnerCode string `json:"runnerCode"`
}

// device performs one of the page's device requests. Everything that changes a connection asks the person in a dialog
// the page cannot press; the status is read-only.
func (s *Shell) device(method string, one func(any) error, none func() error) (any, error) {
	l, err := s.deviceLauncher()
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.timeout(15 * time.Second)
	defer cancel()
	switch method {
	case "DeviceStatus":
		if err := none(); err != nil {
			return nil, err
		}
		return l.DeviceStatus(ctx)
	case "DeviceConnect":
		var in deviceRequest
		if err := one(&in); err != nil {
			return nil, err
		}
		req := device.SetupRequest{Controller: in.Controller, Name: in.Name, Code: in.Code, Runner: in.Runner, RunnerCode: in.RunnerCode}
		if err := req.Validate(); err != nil {
			return nil, err
		}
		what := "control it from this computer"
		if req.Runner {
			what = "control it from this computer, and let it run agents here"
		}
		if s.o.UI.Ask(Dialog{
			Kind:  Question,
			Title: "Connect to another Werkbord?",
			Message: fmt.Sprintf("This computer will %s. Its board and history stay on %s. Work already running here must finish first, and "+
				"this computer's own board is kept, not deleted. You can switch back in Settings.", what, req.Controller),
			Buttons: []string{"Connect", "Cancel"}, Default: "Cancel", Cancel: "Cancel",
		}) != "Connect" {
			return nil, errors.New("not connected")
		}
		job, err := l.StartDeviceSetup(s.o.Ctx, req)
		if err == nil {
			s.reloadWhenDeviceJobEnds()
		}
		return job, err
	case "DeviceLocal":
		if err := none(); err != nil {
			return nil, err
		}
		if s.o.UI.Ask(Dialog{
			Kind:  Question,
			Title: "Switch back to this computer's Werkbord?",
			Message: "Werkbord stops using the other controller and starts this computer's own again, with the board it had before. " +
				"It will not switch while agents or terminals started from the other controller are still running here; finish or stop them first. " +
				"Work on the other controller is not copied here.",
			Buttons: []string{"Switch back", "Cancel"}, Default: "Cancel", Cancel: "Cancel",
		}) != "Switch back" {
			return nil, errors.New("not switched")
		}
		job, err := l.UseLocalController(s.o.Ctx)
		if err == nil {
			s.reloadWhenDeviceJobEnds()
		}
		return job, err
	case "DeviceBind":
		var in struct {
			Project string `json:"project"`
		}
		if err := one(&in); err != nil {
			return nil, err
		}
		// The folder comes from the person's own choice in the native picker, never from the page.
		dir, err := s.ChooseDirectory()
		if err != nil {
			return nil, err
		}
		if dir == "" {
			return nil, errors.New("no folder was chosen")
		}
		dir = filepath.Clean(dir)
		if !filepath.IsAbs(dir) {
			return nil, errors.New("choose a folder on this computer")
		}
		if s.o.UI.Ask(Dialog{
			Kind:  Question,
			Title: "Use this folder for the project?",
			Message: fmt.Sprintf("Agents the other controller starts for this project will work in a worktree of the Git repository at %s on this computer. "+
				"Nothing is committed, pushed or copied.", strings.TrimSpace(dir)),
			Buttons: []string{"Use folder", "Cancel"}, Default: "Cancel", Cancel: "Cancel",
		}) != "Use folder" {
			return nil, errors.New("no folder was bound")
		}
		return l.BindDeviceRepository(s.o.Ctx, in.Project, dir)
	}
	return nil, errors.New("unknown request")
}

// reloadWhenDeviceJobEnds takes the window back to the loading screen once a connection change has finished, which connects
// to whichever controller is now selected. A failed change leaves the window as it was, with its message in the page.
func (s *Shell) reloadWhenDeviceJobEnds() {
	l, err := s.deviceLauncher()
	if err != nil {
		return
	}
	go func() {
		deadline := time.NewTimer(6 * time.Minute)
		defer deadline.Stop()
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-s.o.Ctx.Done():
				return
			case <-deadline.C:
				return
			case <-tick.C:
			}
			ctx, cancel := context.WithTimeout(s.o.Ctx, 5*time.Second)
			st, err := l.DeviceStatus(ctx)
			cancel()
			if err != nil || st.Job.Running {
				continue
			}
			if st.Job.Phase == "done" {
				s.o.UI.Reload()
			}
			return
		}
	}()
}

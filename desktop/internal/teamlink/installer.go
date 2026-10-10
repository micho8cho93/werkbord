package teamlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Exec runs a program and returns what it printed (standard output only is the answer; standard error is for a log).
type Exec func(ctx context.Context, name string, args ...string) (stdout string, err error)

// Installer drives Team's installer, which is this app's own executable run in its installer mode (internal/teaminstall). It
// runs nothing but that program, with the fixed arguments below.
type Installer struct {
	// Program is the app's own executable.
	Program string
	Run     Exec
}

// payload is the Team service the installer puts in place; it sits beside the app's other helpers. A build without it (a
// development build run from source) cannot install Team.
func payload(program string) string {
	return filepath.Join(filepath.Dir(program), "..", "Helpers", "werkbord-team")
}

// Find returns the installer program, or an error saying why this build cannot install Team.
func (i *Installer) Find() (string, error) {
	if i.Program != "" {
		if fi, err := os.Stat(i.Program); err == nil && fi.Mode().IsRegular() {
			if _, err := os.Stat(payload(i.Program)); err == nil {
				return i.Program, nil
			}
		}
	}
	return "", errors.New("this build of Werkbord does not carry Team's service (a development build); use the Werkbord app from a release to add a Team")
}

// Result is what Team's installer printed.
type Result struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Detail  string `json:"detail"`
}

func (i *Installer) run(ctx context.Context, args ...string) (Result, error) {
	exe, err := i.Find()
	if err != nil {
		return Result{}, err
	}
	out, err := i.Run(ctx, exe, args...)
	var r Result
	line := strings.TrimSpace(out)
	if j := strings.LastIndexByte(line, '\n'); j >= 0 {
		line = strings.TrimSpace(line[j+1:])
	}
	if jerr := json.Unmarshal([]byte(line), &r); jerr != nil {
		if err != nil {
			return Result{}, fmt.Errorf("Team's installer stopped: %w", err)
		}
		return Result{}, errors.New("Team's installer gave an answer this app does not understand")
	}
	if !r.OK {
		if r.Detail == "" {
			r.Detail = "the installer reported a problem"
		}
		return r, errors.New(r.Detail)
	}
	return r, nil
}

// Activate installs Team's service (or brings it up to date) so it holds no path to the person's own Werkbord, and waits
// until it answers. The caller has already shown the person what this does and been told to go ahead.
func (i *Installer) Activate(ctx context.Context) (Result, error) { return i.run(ctx, "--activate") }

// Service starts, stops or removes the installed service. action is one of start, stop, uninstall.
func (i *Installer) Service(ctx context.Context, action string) (Result, error) {
	switch action {
	case "start", "stop", "uninstall":
	default:
		return Result{}, errors.New("choose start, stop or uninstall")
	}
	return i.run(ctx, "--service", action)
}

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

// Installer finds and drives Team's own installer. It runs nothing but that program, with the fixed arguments below, and
// only a copy whose signature checks out.
type Installer struct {
	// Candidates are the places Team's app may be: inside this app, then the usual folders.
	Candidates []string
	// Verify checks a program's signature; nil means a development build that cannot (and says so).
	Verify func(ctx context.Context, path string) error
	Run    Exec
}

// DefaultCandidates lists where Werkbord Team.app can be, for an app at appBundle and a person at home.
func DefaultCandidates(appBundle, home string) []string {
	var out []string
	if appBundle != "" {
		out = append(out, filepath.Join(appBundle, "Contents", "Helpers", "Werkbord Team.app"))
	}
	out = append(out, "/Applications/Werkbord Team.app")
	if home != "" {
		out = append(out, filepath.Join(home, "Applications", "Werkbord Team.app"))
	}
	return out
}

// ExecutableName is the name of the program inside Team's app.
const ExecutableName = "Werkbord Team"

// Find returns Team's installer program, or an error saying where the person can get Team.
func (i *Installer) Find() (string, error) {
	for _, app := range i.Candidates {
		exe := filepath.Join(app, "Contents", "MacOS", ExecutableName)
		if fi, err := os.Stat(exe); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return exe, nil
		}
	}
	return "", errors.New("Werkbord Team is not installed on this Mac. Download it from the Werkbord Team release page, put it in Applications, and try again")
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
	if i.Verify != nil {
		if err := i.Verify(ctx, exe); err != nil {
			return Result{}, fmt.Errorf("Werkbord Team's installer could not be verified (%v); it was not run", err)
		}
	}
	out, err := i.Run(ctx, exe, args...)
	var r Result
	line := strings.TrimSpace(out)
	if j := strings.LastIndexByte(line, '\n'); j >= 0 {
		line = strings.TrimSpace(line[j+1:])
	}
	if jerr := json.Unmarshal([]byte(line), &r); jerr != nil {
		if err != nil {
			return Result{}, fmt.Errorf("Werkbord Team's installer stopped: %w", err)
		}
		return Result{}, errors.New("Werkbord Team's installer gave an answer this app does not understand")
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

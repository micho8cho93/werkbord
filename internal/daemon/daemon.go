// Package daemon runs the controller as a background service with whatever the
// operating system provides for that: launchd on macOS, a systemd user service
// on Linux, a scheduled task at log-on on Windows, and a plain detached process
// where none of those can be used. They all answer to one interface so that
// `devboard start`, `stop`, `restart` and `status` manage the one controller the
// service owns, and never start a second one beside it.
//
// The service runs as the user, not as root: agents run as the user, in the
// user's repositories, with the user's own sign-ins.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultLabel names the service.
const DefaultLabel = "dev.devboard.controller"

// Spec describes the service to install.
type Spec struct {
	// Binary is the absolute path of the devboard executable.
	Binary string
	// Args are its arguments, normally {"serve"}.
	Args []string
	// DataDir is where the controller keeps its state. It is passed to the service
	// as DEVBOARD_DATA_DIR so the service and the commands agree on it.
	DataDir string
	// LogFile receives the controller's standard output and error.
	LogFile string
	// Path is the PATH the service runs with. A service does not inherit the
	// shell's PATH, and the agents (claude, codex) are found through it, so it is
	// captured from where the user ran setup.
	Path string
}

// State is what the service manager says about the service.
type State struct {
	// Manager names what runs it: "launchd", "systemd", "task scheduler" or "background process".
	Manager string `json:"manager"`
	// Installed: it will start by itself at log-in. A background process is never installed.
	Installed bool `json:"installed"`
	// Running: the manager says it is running now.
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
	// Detail is the manager's own words for an odd state.
	Detail string `json:"detail,omitempty"`
}

// Manager controls the service.
type Manager interface {
	// Name is the manager's name, as in State.Manager.
	Name() string
	// Install writes the service definition so that the controller starts at
	// log-in. It does not start it, and installing again updates the definition.
	Install(ctx context.Context, spec Spec) error
	// Uninstall stops the service and removes its definition.
	Uninstall(ctx context.Context) error
	// Start starts the service; starting one that is running does nothing.
	Start(ctx context.Context) error
	// Stop stops it until the next start (or log-in, for an installed one).
	Stop(ctx context.Context) error
	// Restart stops and starts it, so it picks up a new binary or definition.
	Restart(ctx context.Context) error
	// Status says whether it is installed and running.
	Status(ctx context.Context) (State, error)
}

// Exec runs a command and returns what it printed. It is a parameter so that the
// managers can be tested without touching the system.
type Exec func(ctx context.Context, name string, args ...string) (string, error)

// RunCommand is the Exec that runs the real command.
func RunCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ErrNotInstalled is returned by Start and Restart for a service that is not installed.
var ErrNotInstalled = errors.New("the service is not installed")

// Options say where things are; the zero value is the real system.
type Options struct {
	Home string
	Exec Exec
	// Label overrides DefaultLabel.
	Label string
	// DataDir is needed by the background process manager for its pid file.
	DataDir string
	// GOOS overrides runtime.GOOS (tests).
	GOOS string
	// UID overrides the user ID (tests); launchd needs it.
	UID int
}

func (o *Options) fill() {
	if o.Home == "" {
		o.Home, _ = os.UserHomeDir()
	}
	if o.Exec == nil {
		o.Exec = RunCommand
	}
	if o.Label == "" {
		o.Label = DefaultLabel
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.UID == 0 {
		o.UID = os.Getuid()
	}
}

// Detect returns the manager for this system: the one that defines a service
// already installed, else the one setup would install. A system without a usable
// one (a container, a minimal Linux without a systemd user session) gets the
// background process manager.
func Detect(ctx context.Context, o Options) Manager {
	o.fill()
	switch o.GOOS {
	case "darwin":
		return &Launchd{Options: o}
	case "windows":
		return &Schtasks{Options: o}
	case "linux":
		if _, err := o.Exec(ctx, "systemctl", "--user", "show", "--property=Version"); err == nil {
			return &Systemd{Options: o}
		}
	}
	return &Background{Options: o}
}

// Installed returns the installable manager if the service is installed. The
// background manager never counts: it has nothing to install.
func Installed(ctx context.Context, o Options) (Manager, bool) {
	m := Detect(ctx, o)
	if _, isBG := m.(*Background); isBG {
		return m, false
	}
	st, err := m.Status(ctx)
	return m, err == nil && st.Installed
}

// ServicePATH builds the PATH a service runs with: the one setup was run with,
// then the places agents are commonly installed, each once.
func ServicePATH(current, home string) string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range filepath.SplitList(current) {
		add(p)
	}
	for _, p := range []string{
		filepath.Join(home, ".local", "bin"), filepath.Join(home, ".claude", "local"), filepath.Join(home, ".npm-global", "bin"),
		"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin",
	} {
		if runtime.GOOS != "windows" {
			add(p)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// rotateLog moves a log that has grown past max bytes aside, keeping one older
// copy, so a service that logs for years does not fill the disk.
func rotateLog(path string, max int64) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > max {
		_ = os.Rename(path, path+".1")
	}
}

// LogRotateSize is how big the controller's log may get before it is rotated at
// the next (re)start.
const LogRotateSize = 10 << 20

func errorf(manager, what string, out string, err error) error {
	out = strings.TrimSpace(out)
	if out != "" {
		return fmt.Errorf("%s: %s: %s", manager, what, out)
	}
	return fmt.Errorf("%s: %s: %w", manager, what, err)
}

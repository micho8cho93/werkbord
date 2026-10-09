package main

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"devboard/desktop/internal/shell"
	"devboard/desktop/internal/teamlink"
	"devboard/desktop/internal/workspaces"
	"devboard/internal/launcher"
)

// The window's own page, which shows every workspace. It is built from the web app's design system and components (web/, make
// web-shell) and embedded under frontend/dist/shell. Missing shell assets are a build error.
const shellPage = "shell/shell/index.html"

// workspaceParts is everything the shell is given to work with workspaces.
type workspaceParts struct {
	registry  *workspaces.Registry
	personal  *workspaces.Personal
	team      *teamlink.Link
	installer *teamlink.Installer
	invites   *shell.Invites
}

// shellState is where the shell remembers which workspace was open and where in each: its own small file, in the
// per-user configuration directory, holding no credential and no content (workspaces.State).
func shellStatePath() string {
	if p := os.Getenv("WERKBORD_DESKTOP_STATE"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "werkbord-desktop", "shell.json")
}

// appBundle is Werkbord.app, found from the running program, or "" when it is not run from one (development).
func appBundle() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
	if strings.HasSuffix(dir, ".app") {
		return dir
	}
	return ""
}

func newWorkspaceParts(l *launcher.Launcher, log *slog.Logger) (*workspaceParts, error) {
	home, _ := os.UserHomeDir()
	personal := workspaces.NewPersonal(func(ctx context.Context) (workspaces.Access, error) {
		cfg, err := l.Config(ctx)
		if err != nil {
			return workspaces.Access{}, err
		}
		tok, _ := cfg.ResolveToken(false)
		return workspaces.Access{Base: cfg.ControllerURL(), Token: tok}, nil
	})
	// Where Team's service is and where its credential for this person is can be pointed elsewhere for development and tests.
	base, keyFile := teamlink.DefaultBase, teamlink.KeyFile(home)
	if v := os.Getenv("WERKBORD_DESKTOP_TEAM_BASE"); v != "" {
		base = v
	}
	if v := os.Getenv("WERKBORD_DESKTOP_TEAM_KEY_FILE"); v != "" {
		keyFile = v
	}
	team, err := teamlink.New(base, keyFile, nil)
	if err != nil {
		return nil, err
	}
	reg := workspaces.NewRegistry(workspaces.OpenState(shellStatePath()), log, personal, team.Source)
	inst := &teamlink.Installer{Candidates: teamlink.DefaultCandidates(appBundle(), home), Verify: verifyProgram, Run: runProgram}
	return &workspaceParts{registry: reg, personal: personal, team: team, installer: inst, invites: &shell.Invites{}}, nil
}

// hasShell says whether this build carries the shell page.
func hasShell(assets fs.FS) bool {
	f, err := assets.Open("frontend/dist/" + shellPage)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// runProgram runs Team's installer and returns what it printed. Its standard error is for the app's log, not for the person.
func runProgram(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out strings.Builder
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

// verifyProgram checks the code signature of a program before it is run. On a Mac every copy of Team's app, ad hoc signed
// or Developer ID signed, carries one; a copy that was altered does not verify.
func verifyProgram(ctx context.Context, path string) error {
	if goruntime.GOOS != "darwin" {
		return nil
	}
	app := filepath.Clean(filepath.Join(filepath.Dir(path), "..", ".."))
	return exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", "--deep", app).Run()
}

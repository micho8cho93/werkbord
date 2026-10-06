// Package shell is what the Werkbord desktop app does, apart from putting a window on the
// screen: it connects to the controller, answers the few things a web page may ask of the
// app, and runs the update when its person agrees. The window itself (Wails, WebKit, the
// menu bar) is a thin layer in package main that supplies the UI below; nothing here needs
// a display, so all of it is tested without one.
//
// The exported methods of Shell are the whole of what the app offers a page. Wails makes
// every exported method of the struct it is given callable from a page served by the
// controller, so this set is deliberately small, is pinned by a test, and none of it hands a
// page anything it does not already hold: the one thing that acts (RequestUpdate) asks the
// person in a native dialog, which a page cannot click.
package shell

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"devboard/internal/config"
	"devboard/internal/launcher"
	"devboard/internal/update"
)

// Launcher is what the shell needs of the launcher (internal/launcher), which does the work.
type Launcher interface {
	Connect(ctx context.Context, report func(launcher.Step)) (launcher.Connection, error)
	Config(ctx context.Context) (config.Config, error)
	UpdateStatus(ctx context.Context, force bool) update.Status
	ApplyUpdate(ctx context.Context) (string, error)
	DiagnosticsText(ctx context.Context) string
}

// DialogKind is how a native dialog looks.
type DialogKind string

const (
	Info     DialogKind = "info"
	Question DialogKind = "question"
	Failure  DialogKind = "error"
)

// Dialog is a native alert with buttons.
type Dialog struct {
	Kind            DialogKind
	Title, Message  string
	Buttons         []string
	Default, Cancel string
}

// UI is the window system, which package main supplies.
type UI interface {
	// Ask shows a native dialog and returns the label of the button that was pressed.
	Ask(d Dialog) string
	// OpenURL opens an address in the person's default browser.
	OpenURL(url string)
	// Reveal shows a file in the Finder; OpenFile opens it in the program that opens such files.
	Reveal(path string)
	OpenFile(path string)
	// Reload takes the window back to the loading screen, which connects again.
	Reload()
	// Emit tells the loading screen something.
	Emit(event string, data any)
}

// Options are what a Shell is made of.
type Options struct {
	Launcher Launcher
	UI       UI
	// Version is the app's version; Platform is runtime.GOOS.
	Version, Platform string
	// AppLog is where the app writes its own log, mentioned in the diagnostics.
	AppLog string
	Log    *slog.Logger
	// Ctx ends when the app quits: whatever is in progress is abandoned (except an update,
	// which is never cut short half-way: the launcher sees to that).
	Ctx context.Context
}

// Shell is the app, as the loading screen, the menu and the web app see it.
type Shell struct {
	o        Options
	updating atomic.Bool
}

// New returns a shell.
func New(o Options) *Shell {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Ctx == nil {
		o.Ctx = context.Background()
	}
	return &Shell{o: o}
}

// ---- what a page may ask ----

// AppInfo says which app this is.
type AppInfo struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

// Info tells a page it is in the desktop app. The web app calls it to know whether to offer
// "Update now" and to open links in the browser.
func (s *Shell) Info() AppInfo {
	s.o.Log.Debug("page call", "method", "Info")
	return AppInfo{Version: s.o.Version, Platform: s.o.Platform}
}

// Connected is what the loading screen needs to open the web app.
type Connected struct {
	// SignInURL carries the access token in its fragment. It is for navigating to and
	// nothing else: it is not logged, shown or kept.
	SignInURL string `json:"signInUrl"`
	Version   string `json:"version"`
}

// Connect makes sure the controller is running and opens to it, reporting what it is doing as
// "progress" events. A failure is returned as it is, for the loading screen to show, and Diagnostics
// says more.
func (s *Shell) Connect() (Connected, error) {
	conn, err := s.o.Launcher.Connect(s.o.Ctx, func(step launcher.Step) {
		s.o.Log.Info("connecting", "phase", step.Phase, "text", step.Text)
		s.o.UI.Emit("progress", step)
	})
	if err != nil {
		s.o.Log.Error("could not connect", "err", err)
		return Connected{}, err
	}
	s.o.Log.Info("connected", "controller", conn.Version, "url", conn.URL)
	if conn.Notice != "" {
		s.o.Log.Warn("notice", "text", conn.Notice)
		go s.o.UI.Ask(Dialog{Kind: Info, Title: "Werkbord", Message: conn.Notice, Buttons: []string{"OK"}, Default: "OK", Cancel: "OK"})
	}
	return Connected{SignInURL: conn.SignInURL, Version: conn.Version}, nil
}

// Diagnostics is what to look at when Werkbord does not open, as text with no secret in it.
func (s *Shell) Diagnostics() string {
	text := s.o.Launcher.DiagnosticsText(s.o.Ctx)
	if s.o.AppLog != "" {
		text += fmt.Sprintf("%-20s %s\n", "App log:", s.o.AppLog)
	}
	return text
}

// ShowDiagnostics writes Diagnostics to a file only the user can read, and opens it: a report to
// read, or to attach to a bug report, without a terminal.
func (s *Shell) ShowDiagnostics() {
	dir := filepath.Dir(s.o.AppLog)
	if s.o.AppLog == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.o.Log.Warn("show diagnostics", "err", err)
		return
	}
	path := filepath.Join(dir, "diagnostics.txt")
	if err := os.WriteFile(path, []byte(s.Diagnostics()), 0o600); err != nil {
		s.o.Log.Warn("show diagnostics", "err", err)
		return
	}
	s.o.UI.OpenFile(path)
}

// OpenLogs shows the controller's log in the Finder.
func (s *Shell) OpenLogs() {
	cfg, err := s.o.Launcher.Config(s.o.Ctx)
	if err != nil {
		s.o.Log.Warn("open logs", "err", err)
		return
	}
	path := cfg.LogPath()
	if _, err := os.Stat(path); err != nil {
		path = filepath.Dir(path)
		if _, err := os.Stat(path); err != nil {
			path = cfg.DataDir
		}
	}
	s.o.UI.Reveal(path)
}

// ValidExternalURL is the address of a web page that may be opened in the browser, or an error.
// Only http and https are: a page must not be able to launch a file, a script or another
// program's handler through the app.
func ValidExternalURL(raw string) (string, error) {
	if len(raw) > 4096 {
		return "", errors.New("that address is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("%q is not a web address", truncate(raw, 60))
	}
	return u.String(), nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// OpenExternal opens a web address in the person's own browser: a link that leaves Werkbord
// (a pull request, GitHub's sign-in, Tailscale's) must not take the window away from it.
func (s *Shell) OpenExternal(raw string) error {
	u, err := ValidExternalURL(raw)
	if err != nil {
		s.o.Log.Warn("refused to open", "err", err)
		return err
	}
	s.o.Log.Info("opening in the browser", "host", hostOf(u))
	s.o.UI.OpenURL(u)
	return nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Hostname()
	}
	return ""
}

// UpdateStatus says whether a newer release exists than the installed program.
func (s *Shell) UpdateStatus(force bool) update.Status {
	return s.o.Launcher.UpdateStatus(s.o.Ctx, force)
}

// UpdateResult is how an update went, for the page that asked.
type UpdateResult struct {
	OK bool `json:"ok"`
	// Declined: the person chose Later. Nothing happened, and nothing needs saying.
	Declined bool `json:"declined,omitempty"`
	// Message is why not, or what happened.
	Message string `json:"message,omitempty"`
}

// RequestUpdate is "Update now" in the web app: it asks the person, in a native dialog the page
// cannot press, and then runs `werkbord update` for the installed program. That is the same
// update the terminal has, with its checksum, its refusal while agents are working, its
// database backup and its way back; the app adds nothing to it and removes nothing from it.
func (s *Shell) RequestUpdate() UpdateResult {
	s.o.Log.Info("the web app asked for an update")
	return s.update(false)
}

// CheckForUpdates is the menu item: it says what it found, whichever it is.
func (s *Shell) CheckForUpdates() { s.update(true) }

func (s *Shell) update(fromMenu bool) UpdateResult {
	if !s.updating.CompareAndSwap(false, true) {
		return UpdateResult{Message: "An update is already running."}
	}
	defer s.updating.Store(false)

	st := s.o.Launcher.UpdateStatus(s.o.Ctx, true)
	say := func(kind DialogKind, title, msg string) {
		if fromMenu {
			s.o.UI.Ask(Dialog{Kind: kind, Title: title, Message: msg, Buttons: []string{"OK"}, Default: "OK", Cancel: "OK"})
		}
	}
	switch {
	case st.Disabled:
		say(Info, "Updates", "Looking for updates is turned off (noUpdateCheck in config.json).")
		return UpdateResult{Message: "Looking for updates is turned off."}
	case !st.Release:
		say(Info, "Updates", fmt.Sprintf("This Werkbord (%s) was built from source, so there is no release to install. Update it from its source tree.", st.Current))
		return UpdateResult{Message: "This Werkbord was built from source."}
	case st.Error != "":
		say(Failure, "Could not look for updates", st.Error)
		return UpdateResult{Message: "Could not look for updates: " + st.Error}
	case !st.Available:
		say(Info, "Werkbord is up to date", fmt.Sprintf("You have Werkbord %s, the newest release.", strings.TrimPrefix(st.Current, "v")))
		return UpdateResult{OK: true, Message: "Werkbord is up to date."}
	}

	choice := s.o.UI.Ask(Dialog{
		Kind:  Question,
		Title: fmt.Sprintf("Update to Werkbord %s?", strings.TrimPrefix(st.Latest, "v")),
		Message: fmt.Sprintf("You have %s. Your data is backed up first, and the update will not start while coding agents are working. "+
			"The app, your browser and your phone reconnect by themselves when it is done.", strings.TrimPrefix(st.Current, "v")),
		Buttons: []string{"Update now", "Later"}, Default: "Update now", Cancel: "Later",
	})
	if choice != "Update now" {
		return UpdateResult{Declined: true}
	}
	s.o.Log.Info("updating", "from", st.Current, "to", st.Latest)
	out, err := s.o.Launcher.ApplyUpdate(s.o.Ctx)
	if err != nil {
		s.o.Log.Error("update failed", "err", err)
		say(Failure, "Werkbord was not updated", err.Error()+"\n\nNothing was changed that could not be put back.")
		return UpdateResult{Message: err.Error()}
	}
	s.o.Log.Info("updated", "to", st.Latest)
	s.o.UI.Reload()
	return UpdateResult{OK: true, Message: lastLine(out)}
}

// ---- what only the menu asks ----

// OpenInBrowser opens Werkbord, signed in, in the person's default browser: the same controller, in a tab.
func (s *Shell) OpenInBrowser() {
	cfg, err := s.o.Launcher.Config(s.o.Ctx)
	if err != nil {
		s.o.Log.Warn("open in browser", "err", err)
		return
	}
	u, err := cfg.SignInURL()
	if err != nil {
		s.o.Log.Warn("open in browser", "err", err)
		return
	}
	s.o.UI.OpenURL(u)
}

// Reload takes the window back through the loading screen: it connects again, with the token as it is now.
func (s *Shell) Reload() { s.o.UI.Reload() }

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

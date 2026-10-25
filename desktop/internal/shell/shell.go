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
//
// Updating has two parts and one story (docs/DESKTOP.md, "Updates"). The app itself (this window and the
// program copy it carries) is replaced by the Updater (Sparkle), which verifies an EdDSA signature
// before anything is installed. The program that runs the controller is replaced only by the program's own
// installer (`install-release`) or `werkbord update`, with their checksum, their refusal while agents are
// working, their database snapshot and their way back. The shell decides which of them a click on
// "Update now" means, so that one release is one question.
package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devboard/internal/config"
	"devboard/internal/launcher"
	"devboard/internal/update"
)

// Launcher is what the shell needs of the launcher (internal/launcher), which does the work.
type Launcher interface {
	Connect(ctx context.Context, report func(launcher.Step)) (launcher.Connection, error)
	// AssistantWorkspaceRequest checks a pending, controller-issued task approval for the desktop bridge.
	AssistantWorkspaceRequest(ctx context.Context, session, request string) ([]byte, error)
	Config(ctx context.Context) (config.Config, error)
	UpdateStatus(ctx context.Context, force bool) update.Status
	ApplyUpdate(ctx context.Context) (string, error)
	DiagnosticsText(ctx context.Context) string
	// PendingProgramUpdate says whether this app carries a newer program than the one installed, and AdoptBundled
	// installs it (by the program's own installer): what finishes updating the app, with no network.
	PendingProgramUpdate(ctx context.Context) (installed, bundled string, ok bool)
	AdoptBundled(ctx context.Context) (string, error)
}

// Updater replaces the app itself (Sparkle, in the real app; nothing in a development or source build). Everything it
// asks of the network is a request for a small signed feed, and only when the person asks: it has no schedule of its own.
type Updater interface {
	// Active says the updater started: this build carries it, its signing key and its feed address.
	Active() bool
	// Probe asks the feed, with no window, whether an update of the app exists.
	Probe(ctx context.Context) (Probe, error)
	// Show opens the updater's own window for the update Probe found: release notes, and Install or Later.
	Show()
	// GuardRelaunch gives the updater a way to ask whether the app may quit and be replaced now. It is not while
	// the program is being updated: the app must not be swapped under that.
	GuardRelaunch(busy func() bool)
}

// Placed is optionally implemented by an Updater that knows where the app is running. InstallProblem is why the app cannot
// replace itself there (it is running from a mounted disk image, say), in a sentence a person can act on, or "" if it can.
// Looking at the feed and the program's own update do not depend on it; replacing the app does.
type Placed interface {
	InstallProblem() string
}

// Probe is what the feed said.
type Probe struct {
	Found   bool
	Version string
}

// Alerter tells the person something happened while they are in another app: a system notification and a bounce of the Dock
// icon. Nil where the system has no such thing (or, on a Mac, where the app is not an app bundle).
type Alerter interface {
	// Status is what the system allows this app: "granted", "denied" (turned off in System Settings), "unknown" (the person has
	// not been asked yet: the first alert asks) or "unsupported".
	Status() string
	// Show posts one alert. Unless force, it does nothing while the app is in front. It reports whether the system took it.
	Show(title, body string, urgent, force bool) bool
	// OpenSettings opens the system's notification settings.
	OpenSettings()
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
	UpdateGuard func(context.Context) error
	Launcher    Launcher
	UI          UI
	// Version is the app's version; Platform is runtime.GOOS.
	Version, Platform string
	// AppLog is where the app writes its own log, mentioned in the diagnostics.
	AppLog string
	Log    *slog.Logger
	// Ctx ends when the app quits: whatever is in progress is abandoned (except an update,
	// which is never cut short half-way: the launcher sees to that).
	Ctx context.Context
	// Updater replaces the app itself. Nil in a build that cannot (development, or no signing key).
	Updater Updater
	// Alerter shows the person what happened while they are elsewhere. Nil where the system cannot.
	Alerter Alerter

	// The workspaces. Registry lists them; Team, TeamInstaller and Grants are how the app reaches Team's service, Team's own
	// installer and the person's Werkbord for the one grant a Team workspace is given. Invites holds an invitation the app
	// was opened with. ShellPage is where the window loads its own page after connecting ("" loads Individual's web app
	// directly, as the app did before there was a shell).
	Workspaces    Registry
	Team          Team
	TeamInstaller TeamInstaller
	Grants        Grants
	Invites       *Invites
	ShellPage     string
	// Premium is the person's unlock key. Nil in a test that has none.
	Premium Premium
}

// Shell is the app, as the loading screen, the menu and the web app see it.
type Shell struct {
	workspaceCalls workspaceCallCache
	o              Options
	updating       atomic.Bool

	alertMu    sync.Mutex
	alertTimes []time.Time
}

// New returns a shell.
func New(o Options) *Shell {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Ctx == nil {
		o.Ctx = context.Background()
	}
	s := &Shell{o: o}
	if o.Updater != nil {
		// A closure, not a method: Wails makes every exported method of the shell callable from a page.
		o.Updater.GuardRelaunch(func() bool {
			if s.updating.Load() {
				return true
			}
			if s.o.UpdateGuard != nil {
				ctx, cancel := context.WithTimeout(s.o.Ctx, 3*time.Second)
				defer cancel()
				return s.o.UpdateGuard(ctx) != nil
			}
			return false
		})
	}
	return s
}

// ---- what a page may ask ----

// AppInfo says which app this is.
type AppInfo struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
	// Updater: this app can replace itself. Informational, and optional for a page to read: a page from before it
	// existed ignores it, and an app from before it existed does not send it.
	Updater bool `json:"updater,omitempty"`
}

// Info tells a page it is in the desktop app. The web app calls it to know whether to offer
// "Update now" and to open links in the browser.
func (s *Shell) Info() AppInfo {
	s.o.Log.Debug("page call", "method", "Info")
	return AppInfo{Version: s.o.Version, Platform: s.o.Platform, Updater: s.o.Updater != nil && s.o.Updater.Active()}
}

// AlertStatus says whether the system lets this app show alerts: "granted", "denied", "unknown" or "unsupported". It reveals
// nothing about the person's computer beyond that one permission.
func (s *Shell) AlertStatus() string {
	if s.o.Alerter == nil {
		return "unsupported"
	}
	return s.o.Alerter.Status()
}

// alertWindow and alertMax bound what a page can have the app say: a page is the person's own Werkbord, but a flood of
// notifications is a way to harm the person's attention, so it is short, plain and slow.
const (
	alertWindow = time.Minute
	alertMax    = 12
)

// Alert shows a system notification and bounces the Dock icon, while the app is behind another one. The text is what the
// person's own Werkbord says happened (a question, a failed run); it is trimmed to a notification's size and rate limited. It
// is safe for a page to call: it opens nothing, runs nothing and reads nothing.
func (s *Shell) Alert(title, body string, urgent bool) bool {
	if s.o.Alerter == nil {
		return false
	}
	title, body = alertText(title, 80), alertText(body, 200)
	if title == "" || !s.alertAllowed(time.Now()) {
		return false
	}
	return s.o.Alerter.Show(title, body, urgent, false)
}

// TestAlert shows one now, even with the app in front, so that the person sees what an alert is like and, the first time, the
// system asks them to allow it. It is rate limited with Alert.
func (s *Shell) TestAlert() bool {
	if s.o.Alerter == nil || !s.alertAllowed(time.Now()) {
		return false
	}
	return s.o.Alerter.Show("Werkbord alerts are on", "You will hear about questions, blocked and failed work while you are in another app.", false, true)
}

// OpenAlertSettings opens the system's notification settings, where a person who turned alerts off can turn them on.
func (s *Shell) OpenAlertSettings() {
	if s.o.Alerter != nil {
		s.o.Alerter.OpenSettings()
	}
}

func (s *Shell) alertAllowed(now time.Time) bool {
	s.alertMu.Lock()
	defer s.alertMu.Unlock()
	kept := s.alertTimes[:0]
	for _, t := range s.alertTimes {
		if now.Sub(t) < alertWindow {
			kept = append(kept, t)
		}
	}
	s.alertTimes = kept
	if len(kept) >= alertMax {
		return false
	}
	s.alertTimes = append(s.alertTimes, now)
	return true
}

// alertText is plain text of at most max characters: no control characters, runs of white space made one space.
func alertText(v string, max int) string {
	var b strings.Builder
	space := false
	for _, r := range v {
		if r < ' ' || r == 0x7f || r == ' ' || r == 0xa0 {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	out := b.String()
	if rs := []rune(out); len(rs) > max {
		out = string(rs[:max-1]) + "…"
	}
	return out
}

// ChooseDirectory opens a native folder picker. It returns only the path the
// person explicitly chose, and grants no access to files or other computers.
func (s *Shell) ChooseDirectory() (string, error) {
	if picker, ok := s.o.UI.(interface{ ChooseDirectory() (string, error) }); ok {
		return picker.ChooseDirectory()
	}
	return "", errors.New("the folder picker is unavailable")
}

// ChosenFile is a file the person picked in the app's own dialog, as the page that asked for it receives it: a name and the
// content, never a path. (The page that asked is the person's own Werkbord, which asks the person again before it shows the
// content to anything.)
type ChosenFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// Limits on what one choice may carry back through the relay. The assistant accepts far less (docs/ASSISTANT.md); a file over the
// limit is reported by name, so the person is told which one and why.
const (
	maxChosenFiles = 8
	maxChosenBytes = 1 << 20
	maxChosenTotal = 4 << 20
)

// chooseFiles opens the app's file dialog and returns what the person chose. It is not callable from a page directly: the relay
// offers it to Individual's page only, and the dialog is the person's say-so for each file.
func (s *Shell) chooseFiles() ([]ChosenFile, error) {
	picker, ok := s.o.UI.(interface{ ChooseFiles() ([]string, error) })
	if !ok {
		return nil, errors.New("the file dialog is unavailable")
	}
	paths, err := picker.ChooseFiles()
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	if len(paths) > maxChosenFiles {
		return nil, fmt.Errorf("choose at most %d files at a time", maxChosenFiles)
	}
	var out []ChosenFile
	total := 0
	for _, p := range paths {
		data, err := readChosen(p)
		if err != nil {
			return nil, err
		}
		if total += len(data); total > maxChosenTotal {
			return nil, errors.New("those files are too large together")
		}
		out = append(out, ChosenFile{Name: filepath.Base(p), Data: data})
	}
	return out, nil
}

// readChosen reads one file the person chose: a regular file, not larger than the limit, however it was reached.
func readChosen(path string) ([]byte, error) {
	name := filepath.Base(path)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s could not be opened", name)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a file", name)
	}
	if st.Size() > maxChosenBytes {
		return nil, fmt.Errorf("%s is larger than %d KB", name, maxChosenBytes>>10)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxChosenBytes+1))
	if err != nil || len(data) > maxChosenBytes {
		return nil, fmt.Errorf("%s could not be read", name)
	}
	return data, nil
}

// Connected is what the loading screen needs to open the web app.
type Connected struct {
	// SignInURL carries the access token in its fragment. It is for navigating to and
	// nothing else: it is not logged, shown or kept.
	SignInURL string `json:"signInUrl"`
	Version   string `json:"version"`
	// Shell is the app's own page, which shows every workspace; the loading screen goes there when it is set.
	Shell string `json:"shell,omitempty"`
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
		// Team workspaces remain useful if the Individual runner is unavailable.
		if s.o.ShellPage != "" {
			return Connected{Shell: s.o.ShellPage}, nil
		}
		return Connected{}, err
	}
	s.o.Log.Info("connected", "controller", conn.Version, "url", conn.URL)
	if conn.Notice != "" {
		s.o.Log.Warn("notice", "text", conn.Notice)
		go s.o.UI.Ask(Dialog{Kind: Info, Title: "Werkbord", Message: conn.Notice, Buttons: []string{"OK"}, Default: "OK", Cancel: "OK"})
	}
	return Connected{SignInURL: conn.SignInURL, Version: conn.Version, Shell: s.o.ShellPage}, nil
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

	say := func(kind DialogKind, title, msg string) {
		if fromMenu {
			s.o.UI.Ask(Dialog{Kind: kind, Title: title, Message: msg, Buttons: []string{"OK"}, Default: "OK", Cancel: "OK"})
		}
	}

	if s.o.UpdateGuard != nil {
		if err := s.o.UpdateGuard(s.o.Ctx); err != nil {
			say(Info, "Update deferred", err.Error())
			return UpdateResult{Message: err.Error()}
		}
	}

	// 1. This app already carries a newer program than the one on this computer (it was updated, and the controller has
	// not moved to it yet, because agents were working): finishing that needs nothing from the internet.
	if installed, bundled, ok := s.o.Launcher.PendingProgramUpdate(s.o.Ctx); ok {
		return s.finish(installed, bundled, say)
	}

	st := s.o.Launcher.UpdateStatus(s.o.Ctx, true)
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
	}

	// 2. A newer release than this app: the app is replaced by the updater, which has its own window with the release
	// notes and its own question. That one question is the whole of the update: when the new app opens it moves the
	// controller to the program it carries (internal/launcher), or says why not yet.
	//
	// A preview is asked whatever the newest stable release is: st.Latest is /releases/latest, which never points at a preview,
	// so it says nothing about the preview feed the app was built to follow (docs/DESKTOP_RELEASE.md, "Preview update feed").
	if u := s.o.Updater; u != nil && u.Active() && update.Release(s.o.Version) && (update.Prerelease(s.o.Version) || update.Compare(s.o.Version, st.Latest) < 0) {
		p, err := u.Probe(s.o.Ctx)
		switch {
		case err != nil:
			// The feed could not be read (the release is still being published, say): the program can still be updated.
			s.o.Log.Warn("the app's update feed could not be read", "err", err)
		case p.Found:
			if why := installProblem(u); why != "" {
				// Sparkle would open its window and then fail to swap the app. Say what to do instead.
				s.o.Log.Warn("an update of the app exists, but this copy cannot replace itself where it is", "to", p.Version, "why", why)
				msg := fmt.Sprintf("A newer Werkbord (%s) is available. %s", p.Version, why)
				say(Info, "Move Werkbord to Applications to update it", msg)
				return UpdateResult{Message: msg}
			}
			s.o.Log.Info("opening the update window", "from", s.o.Version, "to", p.Version)
			u.Show()
			return UpdateResult{OK: true, Message: "The update window is open."}
		default:
			s.o.Log.Info("the release has no update of the app yet", "latest", st.Latest)
		}
	}

	// 3. Only the program is behind: the update the terminal has, unchanged.
	if !st.Available {
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

// installProblem is why u cannot replace the app where it runs, if it can say.
func installProblem(u Updater) string {
	if p, ok := u.(Placed); ok {
		return p.InstallProblem()
	}
	return ""
}

// finish installs the program the app carries over the one installed, when the person agrees: the same installer, with the
// same refusal while agents are working, the same snapshot and the same way back as `werkbord update`.
func (s *Shell) finish(installed, bundled string, say func(DialogKind, string, string)) UpdateResult {
	choice := s.o.UI.Ask(Dialog{
		Kind:  Question,
		Title: fmt.Sprintf("Finish updating to Werkbord %s?", strings.TrimPrefix(bundled, "v")),
		Message: fmt.Sprintf("This app is Werkbord %s, but the Werkbord running on this computer is still %s. Your data is backed up first, and it will not start while coding agents are working. "+
			"The app, your browser and your phone reconnect by themselves when it is done.", strings.TrimPrefix(bundled, "v"), strings.TrimPrefix(installed, "v")),
		Buttons: []string{"Update now", "Later"}, Default: "Update now", Cancel: "Later",
	})
	if choice != "Update now" {
		return UpdateResult{Declined: true}
	}
	s.o.Log.Info("finishing the update with the program in the app", "from", installed, "to", bundled)
	out, err := s.o.Launcher.AdoptBundled(s.o.Ctx)
	if err != nil {
		s.o.Log.Error("finishing the update failed", "err", err)
		say(Failure, "Werkbord was not updated", err.Error()+"\n\nNothing was changed that could not be put back.")
		return UpdateResult{Message: err.Error()}
	}
	s.o.Log.Info("updated", "to", bundled)
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

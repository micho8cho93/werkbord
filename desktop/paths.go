package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"devboard/internal/launcher"
)

// launcherOptions says where the controller's program comes from.
//
// In the app it is inside the bundle, at Werkbord.app/Contents/Helpers/werkbord, and is copied to
// ~/.local/bin the first time it is needed (never run from there: see internal/launcher). In
// development, WERKBORD_DESKTOP_CLI names a program built from this tree, which is run where it is
// and installs nothing and, unless WERKBORD_DESKTOP_SERVICE is set, no login service either.
func launcherOptions() launcher.Options {
	o := launcher.Options{Version: version}
	if cli := os.Getenv("WERKBORD_DESKTOP_CLI"); cli != "" {
		o.Bundled, o.NoInstall = cli, true
		o.NoService = os.Getenv("WERKBORD_DESKTOP_SERVICE") == ""
		return o
	}
	if exe, err := os.Executable(); err == nil {
		helper := filepath.Join(filepath.Dir(exe), "..", "Helpers", "werkbord")
		if fi, err := os.Stat(helper); err == nil && !fi.IsDir() {
			o.Bundled = filepath.Clean(helper)
		}
	}
	return o
}

// openLog opens the app's own log, where macOS keeps an app's logs (~/Library/Logs/Werkbord): it is
// small, and holds what the app did, never a token. The controller has its own log in its data directory.
func openLog() (log *slog.Logger, path string, closeLog func()) {
	home, err := os.UserHomeDir()
	if err != nil {
		return slog.New(slog.DiscardHandler), "", func() {}
	}
	dir := filepath.Join(home, "Library", "Logs", "Werkbord")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return slog.New(slog.DiscardHandler), "", func() {}
	}
	path = filepath.Join(dir, "desktop.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return slog.New(slog.DiscardHandler), "", func() {}
	}
	level := slog.LevelInfo
	if v := os.Getenv("WERKBORD_DESKTOP_LOG_LEVEL"); v != "" {
		_ = level.UnmarshalText([]byte(v)) // debug, info, warn or error; anything else leaves info
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})), path, func() { _ = f.Close() }
}

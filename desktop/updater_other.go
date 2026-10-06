//go:build !darwin

package main

import (
	"log/slog"

	"devboard/desktop/internal/shell"
)

// The app updates itself on macOS only (docs/DESKTOP.md).
type nativeUpdater struct{ shell.Updater }

func newNativeUpdater(*slog.Logger, func() bool) *nativeUpdater { return nil }
func startNative(*nativeUpdater, *shell.Shell)                  {}

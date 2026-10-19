//go:build !darwin

package main

import "devboard/desktop/internal/shell"

// Alerts are the Mac's (docs/DESKTOP.md); elsewhere the window has none, and says so.
func newAlerter() shell.Alerter { return nil }

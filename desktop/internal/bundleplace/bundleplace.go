// Package bundleplace says whether the app can replace itself where it is running.
//
// An update swaps Werkbord.app for the new one. That is impossible from the disk image the app was opened from (a
// read-only volume, /Volumes/Werkbord), and from the temporary copy macOS runs an app from when it was opened straight
// out of Downloads or a disk image (App Translocation). Sparkle would open its window, download the update, and fail at
// the last step; and restarting the app opens the same copy again. The person is told the one thing that fixes it.
package bundleplace

import "strings"

// Problem is why an app whose executable is exe cannot replace itself there, in a sentence a person can act on, or ""
// when it can. readOnly is whether the volume the app is on is mounted read-only (the caller asks the system).
// An executable that is not inside an app bundle (a development run) has no problem to report: nothing replaces it.
func Problem(exe string, readOnly bool) string {
	if !strings.Contains(exe, ".app/Contents/") {
		return ""
	}
	switch {
	case strings.Contains(exe, "/AppTranslocation/"):
		return "macOS is running this copy from a temporary location because it was opened straight from Downloads or a disk image, " +
			"and a copy there cannot be replaced. Quit Werkbord, drag it into your Applications folder (replace the old copy if asked), " +
			"and open it from there."
	case readOnly:
		return "This copy is running from a disk image, and an app cannot replace itself there. Quit Werkbord, drag it from the disk image " +
			"into your Applications folder (replace the old copy if asked), eject the disk image, and open Werkbord from Applications."
	}
	return ""
}

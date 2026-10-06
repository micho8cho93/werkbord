//go:build !updatertest

package main

// updaterTestBuild says whether this is the build scripts/test-desktop-update.sh makes (go build -tags updatertest),
// which installs a valid update without asking, so that the real updater can be run with nobody to press a button.
// scripts/build-desktop.sh --release refuses it, and the release check looks for it in the binary.
const updaterTestBuild = false

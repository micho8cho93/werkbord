package main

import (
	"os"
	"testing"
)

// The tests of the commands that predate replicated storage run a workspace in one file, as they always did, and need
// no database program. Tests of the replicated kind ask for it themselves. The commands' tests use teams larger than the
// Free plan's two people, so they run as Premium, as the browser scripts' builds do (testPremium).
func TestMain(m *testing.M) {
	os.Setenv("WERKBORD_TEAM_STORAGE", "single-file")
	testPremium = "1"
	os.Setenv("WERKBORD_TEAM_KEY_STORAGE", "file") // disposable test stores, no user's OS keychain
	os.Exit(m.Run())
}

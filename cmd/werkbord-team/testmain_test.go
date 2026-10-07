package main

import (
	"os"
	"testing"
)

// The tests of the commands that predate replicated storage run a workspace in one file, as they always did, and need
// no database program. Tests of the replicated kind ask for it themselves.
func TestMain(m *testing.M) {
	os.Setenv("WERKBORD_TEAM_STORAGE", "single-file")
	os.Exit(m.Run())
}

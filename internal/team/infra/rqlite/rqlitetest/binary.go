//go:build !windows

// Package rqlitetest runs real rqlite clusters for tests: the pinned program, supervised by the
// same supervisor Team uses, as several nodes on loopback, with a proxy in front of each node's Raft
// port so that a test can cut the network between chosen nodes and mend it again.
//
// It is for tests and nothing else; no program imports it outside a _test.go file.
package rqlitetest

import (
	"os"
	"path/filepath"
	"runtime"

	"devboard/internal/team/infra/rqlite"
)

// BinaryDir finds the pinned program. Tests do not download or build it on their own: `make rqlite`
// (scripts/fetch-rqlite.sh) puts it in .cache/rqlite, checked against the pin, and
// WERKBORD_REQUIRE_RQLITE=1 turns a missing one into a failure (CI sets it) instead of a skip, and WERKBORD_SKIP_RQLITE=1 skips
// the tests even when it is there (`make test` sets it: clusters elect leaders by timeout, and these tests are run on their own,
// one package at a time, by `make test-rqlite`).
func BinaryDir(tb TB) string {
	tb.Helper()
	if os.Getenv("WERKBORD_SKIP_RQLITE") == "1" {
		tb.Skip("WERKBORD_SKIP_RQLITE=1: the tests that start real database clusters run in `make test-rqlite`")
	}
	if _, err := rqlite.ThisPlatform(); err != nil {
		tb.Skip(err)
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..")
	dirs := []string{os.Getenv("WERKBORD_TEST_RQLITE_DIR"), filepath.Join(root, ".cache", "rqlite", rqlite.Version, runtime.GOOS+"_"+runtime.GOARCH)}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		d, _ = filepath.Abs(d)
		if _, err := os.Stat(filepath.Join(d, rqlite.BinaryName)); err == nil {
			return d
		}
	}
	if os.Getenv("WERKBORD_REQUIRE_RQLITE") == "1" {
		tb.Fatalf("the pinned rqlite %s is required (WERKBORD_REQUIRE_RQLITE=1) and is not in .cache/rqlite: run scripts/fetch-rqlite.sh", rqlite.Version)
	}
	tb.Skipf("the pinned rqlite %s is not in .cache/rqlite: run scripts/fetch-rqlite.sh to run this test", rqlite.Version)
	return ""
}

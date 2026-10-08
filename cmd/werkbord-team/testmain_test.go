package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"devboard/internal/team/license"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The tests of the commands that predate replicated storage run a workspace in one file, as they always did, and need
// no database program. Tests of the replicated kind ask for it themselves.
func TestMain(m *testing.M) {
	os.Setenv("WERKBORD_TEAM_STORAGE", "single-file")
	dir, err := os.MkdirTemp("", "team-cli-entitlement-")
	if err != nil {
		panic(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	licenseIssuer = base64.RawURLEncoding.EncodeToString(pub)
	raw, err := license.Issue(license.Claims{Schema: license.Schema, Product: "werkbord-team", Edition: license.EditionTeam, ID: "test", Customer: "CLI test", Seats: 100, IssuedAt: time.Now().Add(-time.Hour)}, key)
	if err != nil {
		panic(err)
	}
	path := filepath.Join(dir, "license.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		panic(err)
	}
	os.Setenv("WERKBORD_TEAM_LICENSE_FILE", path)
	os.Setenv("WERKBORD_TEAM_KEY_STORAGE", "file") // disposable test stores, no user's OS keychain
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoppedHostsAndAdditionalSlotsCannotBeReplacedOrRemoved(t *testing.T) {
	for _, slot := range []string{"", "slots/second-team"} {
		for _, state := range []string{"workspace", "pending", "leaving", "demoting"} {
			t.Run(filepath.Join(slot, state), func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, "data", slot, state)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := CheckReplacement(filepath.Join(root, "data")); err == nil {
					t.Fatal("unsafe stopped workspace accepted")
				}
				if err := removeEmptyService(root, filepath.Join(root, "service.plist")); err == nil {
					t.Fatal("unsafe removal accepted")
				}
				if _, err := os.Stat(dir); err != nil {
					t.Fatal("workspace was removed", err)
				}
			})
		}
	}
}

func TestRemovingTeamCapabilityPreservesPersonalAndRetainedTeamState(t *testing.T) {
	root := t.TempDir()
	team := filepath.Join(root, "team")
	plist := filepath.Join(root, "team.plist")
	retained := []string{"personal/devboard.db", "personal/token", "personal/runner.json", "team/data/identity.json", "team/data/license.json", "team/access.key", "team/owner", "team/Helpers.previous/program", "team/install-backup-one/program"}
	for _, name := range append(append([]string{}, retained...), "team/Helpers/program", "team.plist") {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeEmptyService(team, plist); err != nil {
		t.Fatal(err)
	}
	for _, name := range retained {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(b) != name {
			t.Fatalf("retained state changed: %s: %v", name, err)
		}
	}
	for _, path := range []string{plist, filepath.Join(team, "Helpers")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("program remains installed", path, err)
		}
	}
	if err := removeEmptyService(team, plist); err != nil {
		t.Fatal("retry", err)
	}
}

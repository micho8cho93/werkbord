package teaminstall

import (
	"errors"
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

func TestRemovingTeamCapabilityPreservesIndividualAndRetainedTeamState(t *testing.T) {
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

// The names are the contract with pki.NewSecureSealer: this one was read from the system keychain of a Mac whose service
// stored its workspace at the installed path.
func TestSealingServicesMatchTheService(t *testing.T) {
	got := sealingServices("/Library/Application Support/Werkbord Team/data/workspace")
	hash := "ccaba60831db566824dd63cb3af2313e911b661cb92b09afd4901693d55da09c"
	if len(got) != 2 || got[0] != "werkbord-team/"+hash+"/device" || got[1] != "werkbord-team/"+hash+"/authority" {
		t.Fatalf("keychain item names changed: %v", got)
	}
}

func recordKeychainDeletes(t *testing.T) *[]string {
	t.Helper()
	var deleted []string
	previous := deleteSealingKey
	deleteSealingKey = func(service string) error { deleted = append(deleted, service); return nil }
	t.Cleanup(func() { deleteSealingKey = previous })
	return &deleted
}

func TestRemovedServiceForgetsItsKeychainKeys(t *testing.T) {
	deleted := recordKeychainDeletes(t)
	root := t.TempDir()
	// A second Team that was removed leaves an empty slot; both slots start from no keys.
	for _, dir := range []string{"data", "data/slots/second"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeEmptyService(root, filepath.Join(root, "service.plist")); err != nil {
		t.Fatal(err)
	}
	want := append(sealingServices(filepath.Join(root, "data", "workspace")), sealingServices(filepath.Join(root, "data", "slots", "second", "workspace"))...)
	if len(*deleted) != len(want) {
		t.Fatalf("deleted %v, want %v", *deleted, want)
	}
	for i := range want {
		if (*deleted)[i] != want[i] {
			t.Fatalf("deleted %v, want %v", *deleted, want)
		}
	}
}

func TestKeysThatMaySealDataAreKept(t *testing.T) {
	for _, kept := range []string{"left-workspace-17", "retired-storage-17", ".workspace-create-abc", "pending-join"} {
		t.Run(kept, func(t *testing.T) {
			deleted := recordKeychainDeletes(t)
			data := t.TempDir()
			if err := os.MkdirAll(filepath.Join(data, kept), 0700); err != nil {
				t.Fatal(err)
			}
			if err := forgetOrphanedSealingKeys(data); err != nil {
				t.Fatal(err)
			}
			if len(*deleted) != 0 {
				t.Fatalf("forgot keys that may still seal data: %v", *deleted)
			}
		})
	}
}

func TestAKeychainThatCannotBeCleanedStopsTheInstallation(t *testing.T) {
	previous := deleteSealingKey
	deleteSealingKey = func(string) error { return errors.New("denied") }
	t.Cleanup(func() { deleteSealingKey = previous })
	if err := forgetOrphanedSealingKeys(t.TempDir()); err == nil {
		t.Fatal("a failed removal was hidden")
	}
}

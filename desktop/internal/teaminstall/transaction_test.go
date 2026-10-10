package teaminstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeInstallRecoversInterruptedSwapAndMetadata(t *testing.T) {
	for _, phase := range []string{"before_swap", "old_renamed", "new_installed", "new_metadata"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			plist := filepath.Join(root, "service.plist")
			helpers := filepath.Join(root, "Helpers")
			os.Mkdir(helpers, 0700)
			os.WriteFile(filepath.Join(helpers, "werkbord-team"), []byte("old binary"), 0700)
			os.WriteFile(plist, []byte("old service"), 0600)
			os.WriteFile(filepath.Join(root, "owner"), []byte("501"), 0600)
			os.WriteFile(filepath.Join(root, "access.key"), []byte("original key"), 0600)
			if err := beginInstall(root, plist); err != nil {
				t.Fatal(err)
			}
			if phase != "before_swap" {
				os.Rename(helpers, helpers+".previous")
			}
			if phase == "new_installed" || phase == "new_metadata" {
				os.Mkdir(helpers, 0700)
				os.WriteFile(filepath.Join(helpers, "werkbord-team"), []byte("new binary"), 0700)
			}
			if phase == "new_metadata" {
				os.WriteFile(plist, []byte("new service"), 0600)
				os.WriteFile(filepath.Join(root, "access.key"), []byte("changed key"), 0600)
			}
			if err := recoverInstall(root, plist); err != nil {
				t.Fatal(err)
			}
			for p, want := range map[string]string{plist: "old service", filepath.Join(helpers, "werkbord-team"): "old binary", filepath.Join(root, "access.key"): "original key", filepath.Join(root, "owner"): "501"} {
				b, err := os.ReadFile(p)
				if err != nil || string(b) != want {
					t.Fatalf("recovery %s: %s %v", p, b, err)
				}
			}
			if err := recoverInstall(root, plist); err != nil {
				t.Fatal("retry", err)
			}
		})
	}
}

func TestVerifiedInstallKeepsRollbackGeneration(t *testing.T) {
	root := t.TempDir()
	plist := filepath.Join(root, "service.plist")
	os.Mkdir(filepath.Join(root, "Helpers"), 0700)
	if err := beginInstall(root, plist); err != nil {
		t.Fatal(err)
	}
	os.Rename(filepath.Join(root, "Helpers"), filepath.Join(root, "Helpers.previous"))
	os.Mkdir(filepath.Join(root, "Helpers"), 0700)
	if err := finishInstall(root); err != nil {
		t.Fatal(err)
	}
	if err := recoverInstall(root, plist); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Helpers.previous")); err != nil {
		t.Fatal("previous installation deleted")
	}
	if err := beginInstall(root, plist); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "install-backup-*", "Helpers"))
	if len(matches) != 1 {
		t.Fatal("previous generation not archived")
	}
}

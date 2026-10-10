package teaminstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrReplacementDeferred is what the person is told when Team's service holds a workspace and so is neither replaced nor
// removed by an installer. It is written for the person: the installer prints it as is, and every caller shows it as is.
var ErrReplacementDeferred = errors.New("Team's service on this Mac belongs to a Team workspace, so it will not be replaced or removed automatically. Nothing was changed and it keeps running. Updating it takes coordinated administrator maintenance with a verified backup; removing it takes leaving the workspace first")

// CheckReplacement is deliberately conservative. Until a coordinated cluster maintenance
// protocol is available, native replacement never stops an enrolled/pending Team service.
// This check is repeated under administrator authority and covers every slot, including
// stopped services. No frontend status or unreachable Host can waive it.
func CheckReplacement(data string) error {
	dirs := []string{data}
	if fi, err := os.Lstat(filepath.Join(data, "slots")); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("Team slots directory is a symlink")
	}
	slots, err := os.ReadDir(filepath.Join(data, "slots"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, s := range slots {
		if s.Type()&os.ModeSymlink != 0 {
			return errors.New("a Team workspace slot is a symlink; inspect it before maintenance")
		}
		if s.IsDir() {
			dirs = append(dirs, filepath.Join(data, "slots", s.Name()))
		}
	}
	for _, dir := range dirs {
		for _, name := range []string{"workspace", "pending", "leaving", "demoting"} {
			if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
				return ErrReplacementDeferred
			}
		}
	}
	return nil
}

// Removal is limited to installed programs and the launch definition. Local
// identities, licenses, recovery generations and all product data stay in place.
func removeEmptyService(root, plist string) error {
	if err := CheckReplacement(filepath.Join(root, "data")); err != nil {
		return err
	}
	for _, path := range []string{plist, filepath.Join(root, "Helpers")} {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	// Nothing is sealed with these keys any more, and a later installation seals new data under the same names.
	return forgetOrphanedSealingKeys(filepath.Join(root, "data"))
}

// systemKeychain is where a root service keeps the keys that seal a workspace's secrets (internal/team/infra/pki).
const systemKeychain = "/Library/Keychains/System.keychain"

// sealingServices names the two keychain items that seal the workspace stored in workspaceDir. It mirrors
// pki.NewSecureSealer (this is a separate module, so it cannot be imported); TestSealingServicesMatchTheService pins it.
func sealingServices(workspaceDir string) []string {
	h := sha256.Sum256([]byte(workspaceDir))
	base := "werkbord-team/" + hex.EncodeToString(h[:])
	return []string{base + "/device", base + "/authority"}
}

// deleteSealingKey removes one item from the System keychain. An item that is already gone is not a failure.
var deleteSealingKey = func(service string) error {
	out, err := exec.Command("/usr/bin/security", "delete-generic-password", "-s", service, "-a", "sealing", systemKeychain).CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 44 { // errSecItemNotFound
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove the keychain item %s: %w: %s", service, err, bytes.TrimSpace(out))
	}
	return nil
}

// A keychain item is tied to the program that made it. When the workspace it sealed is gone (the service was removed,
// or its data deleted by hand), the next installation is a different program asking for the same item, macOS wants to
// ask the person, and a background service cannot be asked: Keychain status -25308. So a service without a workspace
// starts from no keys. Anything that may still hold sealed data (a workspace, one being created, joined, left or
// demoted, or a kept archive of one) keeps its keys: forgetting them would make that data unreadable.
var keepsSealedData = []string{"workspace", ".workspace", "pending", "leaving", "demoting", "left-workspace-", "retired-storage-"}

func forgetOrphanedSealingKeys(data string) error {
	slots := []string{data}
	entries, err := os.ReadDir(filepath.Join(data, "slots"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			slots = append(slots, filepath.Join(data, "slots", e.Name()))
		}
	}
	var failed []error
slot:
	for _, dir := range slots {
		files, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			failed = append(failed, err)
			continue
		}
		for _, f := range files {
			for _, prefix := range keepsSealedData {
				if strings.HasPrefix(f.Name(), prefix) {
					continue slot
				}
			}
		}
		for _, service := range sealingServices(filepath.Join(dir, "workspace")) {
			if err := deleteSealingKey(service); err != nil {
				failed = append(failed, err)
			}
		}
	}
	return errors.Join(failed...)
}

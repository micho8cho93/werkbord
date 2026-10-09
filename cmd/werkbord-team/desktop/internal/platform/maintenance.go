package platform

import (
	"errors"
	"os"
	"path/filepath"
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
	return nil
}

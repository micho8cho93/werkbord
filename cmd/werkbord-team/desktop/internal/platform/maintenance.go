package platform

import (
	"errors"
	"os"
	"path/filepath"
)

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
				return errors.New("Team service replacement is deferred while a workspace is enrolled, joining or leaving; preserve its backup and use coordinated administrator maintenance")
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

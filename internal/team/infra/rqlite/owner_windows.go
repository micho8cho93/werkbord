//go:build windows

package rqlite

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
	"werkbord/internal/fssecure"
)

func checkOwnedPrivate(path string, dir bool) error {
	attrs, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("rqlite: %s is a reparse point", path)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if dir && !fi.IsDir() {
		return fmt.Errorf("rqlite: %s is not a directory", path)
	}
	if !dir && !fi.Mode().IsRegular() {
		return fmt.Errorf("rqlite: %s is not a regular file", path)
	}
	if err := fssecure.VerifyWindowsPrivateACL(path, true); err != nil {
		return fmt.Errorf("rqlite: %s: %w", path, err)
	}
	return nil
}

//go:build windows

package nebula

import (
	"errors"
	"fmt"
	"os"

	"devboard/internal/fssecure"
	"golang.org/x/sys/windows"
)

// checkOwnedPrivate treats a Windows DACL as the privacy boundary. A file or
// directory is accepted only when its owner and every allow ACE belong to the
// current account, LocalSystem, or Administrators. In particular, inherited
// access for Users, Everyone, or Authenticated Users is refused.
func checkOwnedPrivate(path string, dir bool) error {
	attrs, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("nebula: %s is a reparse point", path)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if dir && !fi.IsDir() {
		return fmt.Errorf("nebula: %s is not a directory", path)
	}
	if !dir && !fi.Mode().IsRegular() {
		return fmt.Errorf("nebula: %s is not a regular file", path)
	}
	return verifyWindowsPrivateACL(path)
}

func verifyWindowsPrivateACL(path string) error {
	if err := fssecure.VerifyWindowsPrivateACL(path, true); err != nil {
		return fmt.Errorf("nebula: %s: %w", path, err)
	}
	return nil
}

// signalReload is not supported on Windows: the supervisor restarts the node instead.
func signalReload(*os.Process) error {
	return errors.New("nebula: reload by signal is not supported here")
}

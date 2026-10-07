//go:build windows

package rqlite

import "os"

// checkOwnedPrivate does nothing on Windows, where ownership is an ACL and not a mode:
// Team's installer does not support Windows, and no pinned release exists for it.
func checkOwnedPrivate(path string, dir bool) error {
	_, err := os.Lstat(path)
	return err
}

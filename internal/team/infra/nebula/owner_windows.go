//go:build windows

package nebula

import (
	"errors"
	"os"
)

// checkOwnedPrivate does nothing on Windows, where ownership is an ACL and not a mode:
// Team's installer does not support Windows yet, and no pinned release exists for it.
func checkOwnedPrivate(path string, dir bool) error {
	_, err := os.Lstat(path)
	return err
}

// signalReload is not supported on Windows: the supervisor restarts the node instead.
func signalReload(*os.Process) error {
	return errors.New("nebula: reload by signal is not supported here")
}

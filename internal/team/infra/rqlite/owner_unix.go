//go:build !windows

package rqlite

import (
	"fmt"
	"os"
	"syscall"
)

// checkOwnedPrivate refuses a path the supervisor cannot trust to be private: it must
// be owned by the user running the supervisor and not writable by anyone else. It is a
// check of the facts, not a repair: a directory someone else could write to may already
// hold something they put there.
func checkOwnedPrivate(path string, dir bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("rqlite: %s is a symbolic link; the supervisor does not follow one for something it will run or read credentials from", path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("rqlite: cannot tell who owns %s", path)
	}
	if int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("rqlite: %s is owned by user %d, not by the user running Werkbord Team (%d)", path, st.Uid, os.Geteuid())
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("rqlite: %s is writable by others (mode %o)", path, fi.Mode().Perm())
	}
	if dir && !fi.IsDir() {
		return fmt.Errorf("rqlite: %s is not a directory", path)
	}
	return nil
}

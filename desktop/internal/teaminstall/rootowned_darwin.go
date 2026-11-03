package teaminstall

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// What the administrator's authorization is given to run must be something only root could have put there. AuthorizeService copies the
// app into a private root-owned folder, checks the copy's signature and runs the copy; this is the privileged step refusing to go on
// from anywhere else. A program that runs as root from a place an ordinary program could write to is a way to be root, whatever
// checks it makes of itself.

// requireRootOwnedTree says whether the app at root, and every folder above it, can only be changed by root.
func requireRootOwnedTree(root string) error { return requireOwnedTree(root, 0, true) }

// above says whether the folders over the app are checked too (a test cannot choose its own parents).
func requireOwnedTree(root string, uid uint32, above bool) error {
	root = filepath.Clean(root)
	for dir := filepath.Dir(root); above; dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if why := unsafeEntry(fi, uid, true); why != "" {
			return fmt.Errorf("the Team app is being run from a place that is not only root's (%s %s); nothing was changed", dir, why)
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := de.Info()
		if err != nil {
			return err
		}
		// A link's own mode means nothing: what it leads to is checked as the walk reaches it (or is outside, and is not run).
		if fi.Mode()&os.ModeSymlink != 0 {
			if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != uid {
				return fmt.Errorf("the Team app is being run from a place that is not only root's (%s is not root's); nothing was changed", path)
			}
			return nil
		}
		if why := unsafeEntry(fi, uid, false); why != "" {
			return fmt.Errorf("the Team app is being run from a place that is not only root's (%s %s); nothing was changed", path, why)
		}
		return nil
	})
}

// unsafeEntry says why a file or folder could be changed by someone other than uid, or "" when it cannot. A folder above the app may be
// open to everyone when it is sticky (as /private/var/tmp is): nobody can then rename or remove what another owns.
func unsafeEntry(fi os.FileInfo, uid uint32, ancestor bool) string {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "cannot be inspected"
	}
	if st.Uid != uid {
		return "is not root's"
	}
	mode := fi.Mode()
	if mode&0o022 == 0 {
		return ""
	}
	if ancestor && fi.IsDir() && mode&os.ModeSticky != 0 {
		return ""
	}
	return "can be changed by others"
}

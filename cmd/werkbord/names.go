package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Werkbord was called Dev Board, and its command devboard. Both names work: the
// executable is installed as werkbord, with devboard beside it as another name for
// it, and an install from before the rename (an executable named devboard) gets
// werkbord beside it. Neither name ever nags.
//
// ensureCommandNames gives the executable at exe its other name, in the same
// directory, if that name is free: a relative symbolic link on Unix (so an update,
// which replaces the file, is seen under both names), and a one-line .cmd on
// Windows. It returns the path it created, or "" if there was nothing to do: an
// executable with another name (a build in bin/, a test binary), or a name that
// already exists, which is never touched.
func ensureCommandNames(exe string) (string, error) {
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	dir, base := filepath.Dir(real), filepath.Base(real)
	name := strings.TrimSuffix(base, ".exe")
	var other string
	switch name {
	case "werkbord":
		other = "devboard"
	case "devboard":
		other = "werkbord"
	default:
		return "", nil
	}
	if runtime.GOOS == "windows" {
		for _, taken := range []string{other + ".exe", other + ".cmd", other + ".bat"} {
			if _, err := os.Lstat(filepath.Join(dir, taken)); err == nil {
				return "", nil
			}
		}
		shim := filepath.Join(dir, other+".cmd")
		return shim, os.WriteFile(shim, []byte("@\"%~dp0"+base+"\" %*\r\n"), 0o755)
	}
	link := filepath.Join(dir, other)
	if _, err := os.Lstat(link); err == nil {
		return "", nil
	}
	return link, os.Symlink(base, link)
}

//go:build linux

package teaminstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func VerifyRelease(appRoot string) error {
	if releaseBuild == "" { // a development build is never accepted by privileged setup
		return nil
	}
	return verifyLinuxRelease(appRoot)
}

func RequireRelease(appRoot string) error {
	if releaseBuild == "" {
		return errors.New("this development build has no release build stamp")
	}
	return verifyLinuxRelease(appRoot)
}

func verifyLinuxRelease(appRoot string) error {
	if strings.TrimPrefix(releaseBuild, "v") != strings.TrimPrefix(nativeVersion, "v") {
		return errors.New("the app and Team installer versions do not match")
	}
	root := filepath.Clean(appRoot)
	if !filepath.IsAbs(root) {
		return errors.New("the Team app must be installed in a protected system directory")
	}
	// Every parent must be a real root-owned directory that an unprivileged user
	// cannot replace. This is what makes pkexec safe for a desktop app installed
	// from a root-owned package in /opt or /usr/lib.
	for p := root; ; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("%s is not a protected root-owned app directory", p)
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	for _, name := range []string{
		"bin/Werkbord",
		"Helpers/werkbord",
		"Helpers/werkbord-team",
		"Helpers/nebula",
		"Helpers/rqlited",
		"Resources/rqlited.build",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		fi, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("release app is incomplete (%s): %w", name, err)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("release app file %s is not protected and root owned", name)
		}
	}
	return nil
}

func releaseRoot(exe string) string {
	return filepath.Clean(filepath.Join(filepath.Dir(exe), ".."))
}

package nebula

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// ErrBinaryNotFound: no file named nebula in any of the places it may be shipped.
var ErrBinaryNotFound = errors.New("nebula: the bundled network program was not found")

// ErrBinaryMismatch: a file was found, and it is not the pinned release. It is never
// started: not "close enough", not "newer", not from the user's PATH.
var ErrBinaryMismatch = errors.New("nebula: the program found is not the release this version of Werkbord Team ships and was not started")

// BinaryName returns the file name the supervisor looks for on this platform.
func binaryFile() string {
	if runtime.GOOS == "windows" {
		return BinaryName + ".exe"
	}
	return BinaryName
}

// sha256File is the SHA-256 of a file, as lower-case hex.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sameHash(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// verified is a copy of the pinned program, checked, in a directory only the
// supervisor's user can write.
type verified struct {
	// Path is the absolute path of the private copy the supervisor starts.
	Path string
	// SHA256 is the program's hash, equal to the manifest's.
	SHA256 string
	// Version is the pinned release's version.
	Version string
}

// locate finds the program in dirs (searched in order; never PATH), verifies it
// against the manifest and installs a private copy under binDir, which it returns.
// A file that does not match the pin is an error, even if a later directory holds a
// good one: a directory that holds the wrong program is a fault to report, not to step over.
func locate(dirs []string, binDir string, art Artifact) (verified, error) {
	var found string
	for _, d := range dirs {
		if d == "" || !filepath.IsAbs(d) {
			continue
		}
		p := filepath.Join(d, binaryFile())
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if !fi.Mode().IsRegular() {
			return verified{}, fmt.Errorf("%w: %s is not a regular file", ErrBinaryMismatch, p)
		}
		found = p
		break
	}
	if found == "" {
		return verified{}, ErrBinaryNotFound
	}
	sum, err := sha256File(found)
	if err != nil {
		return verified{}, err
	}
	if !sameHash(sum, art.BinarySHA256) {
		return verified{}, fmt.Errorf("%w: %s has SHA-256 %s", ErrBinaryMismatch, found, sum)
	}
	// Run a private copy, not the file where it was found: the shipping location may
	// be writable by someone else, and what is hashed here is what must be what runs.
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return verified{}, err
	}
	if err := checkOwnedPrivate(binDir, true); err != nil {
		return verified{}, err
	}
	dst := filepath.Join(binDir, fmt.Sprintf("nebula-%s-%s", Version, sum[:12]))
	if runtime.GOOS == "windows" {
		dst += ".exe"
	}
	if have, err := sha256File(dst); err != nil || !sameHash(have, sum) {
		if err := copyFile(found, dst, 0o500); err != nil {
			return verified{}, err
		}
	}
	return verified{Path: dst, SHA256: sum, Version: Version}, nil
}

// reverify checks the private copy again, immediately before it is started.
func (v verified) reverify(art Artifact) error {
	sum, err := sha256File(v.Path)
	if err != nil {
		return err
	}
	if !sameHash(sum, art.BinarySHA256) || !sameHash(sum, v.SHA256) {
		return fmt.Errorf("%w: %s changed after it was verified", ErrBinaryMismatch, v.Path)
	}
	return checkOwnedPrivate(v.Path, false)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".nebula-*")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return err
	}
	ok = true
	return nil
}

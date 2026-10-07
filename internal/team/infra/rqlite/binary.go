package rqlite

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ErrBinaryNotFound: no file named rqlited in any of the places it may be shipped.
var ErrBinaryNotFound = errors.New("rqlite: the bundled database program was not found")

// ErrBinaryMismatch: a file was found, and it is not the pinned release. It is never
// started: not "close enough", not "newer", not from the user's PATH.
var ErrBinaryMismatch = errors.New("rqlite: the program found is not the release this version of Werkbord Team ships and was not started")

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

func sameHash(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// buildRecord is what scripts/fetch-rqlite.sh leaves beside a program it built from the
// pinned source.
type buildRecord struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	SHA256  string `json:"sha256"`
}

// verified is a copy of the pinned program, checked, in a directory only the
// supervisor's user can write.
type verified struct {
	// Path is the absolute path of the private copy the supervisor starts.
	Path string
	// SHA256 is the program's hash.
	SHA256 string
	// Pinned says whether the hash is the manifest's (true) or only the build record's.
	Pinned bool
}

// locate finds the program in dirs (searched in order; never PATH), verifies it
// against the manifest and installs a private copy under binDir. A file that does not
// match the pin is an error, even if a later directory holds a good one: a directory that
// holds the wrong program is a fault to report, not to step over.
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
	pinned, err := checkAgainstPin(found, sum, art)
	if err != nil {
		return verified{}, err
	}
	// Run a private copy, not the file where it was found: the shipping location may be
	// writable by someone else, and what is hashed here is what must be what runs.
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return verified{}, err
	}
	if err := checkOwnedPrivate(binDir, true); err != nil {
		return verified{}, err
	}
	dst := filepath.Join(binDir, fmt.Sprintf("rqlited-%s-%s", Version, sum[:12]))
	if runtime.GOOS == "windows" {
		dst += ".exe"
	}
	if have, err := sha256File(dst); err != nil || !sameHash(have, sum) {
		if err := copyFile(found, dst, 0o500); err != nil {
			return verified{}, err
		}
	}
	return verified{Path: dst, SHA256: sum, Pinned: pinned}, nil
}

func checkAgainstPin(path, sum string, art Artifact) (pinned bool, err error) {
	if art.BinarySHA256 != "" {
		if !sameHash(sum, art.BinarySHA256) {
			return false, fmt.Errorf("%w: %s has SHA-256 %s", ErrBinaryMismatch, path, sum)
		}
		return true, nil
	}
	if !art.Source {
		return false, fmt.Errorf("%w: this platform has no pinned program", ErrBinaryMismatch)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), RecordName))
	if err != nil {
		return false, fmt.Errorf("%w: %s has no build record (run scripts/fetch-rqlite.sh, which builds the pinned source)", ErrBinaryMismatch, path)
	}
	var rec buildRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.Version != Tag || rec.Commit != SourceCommit || !sameHash(sum, rec.SHA256) {
		return false, fmt.Errorf("%w: %s is not the program its build record describes, or was not built from %s", ErrBinaryMismatch, path, SourceCommit[:12])
	}
	return false, nil
}

// reverify checks the private copy again, immediately before it is started.
func (v verified) reverify(art Artifact) error {
	sum, err := sha256File(v.Path)
	if err != nil {
		return err
	}
	if !sameHash(sum, v.SHA256) || (art.BinarySHA256 != "" && !sameHash(sum, art.BinarySHA256)) {
		return fmt.Errorf("%w: %s changed after it was verified", ErrBinaryMismatch, v.Path)
	}
	return checkOwnedPrivate(v.Path, false)
}

// command builds the one kind of process this package starts: the verified rqlited, with
// arguments this package made. It is the only place in the package that names a program;
// internal/archtest holds it to one call, with a constant program name.
func command(ctx context.Context, v verified, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "rqlited", args...)
	// exec resolved the bare name through PATH; what runs is the verified private copy, and nothing else.
	cmd.Path, cmd.Err = v.Path, nil
	cmd.Env = []string{} // the node needs nothing from the environment, and gets nothing
	return cmd
}

var versionLine = regexp.MustCompile(`^rqlited (v?[0-9][0-9A-Za-z.+-]*) `)

// VerifyVersion runs the located program with -version, once, and checks it reports the
// pinned release. A release build reports "v10.5.2"; so does one built from the pinned
// source by scripts/fetch-rqlite.sh.
func verifyVersion(ctx context.Context, v verified) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := command(ctx, v, "-version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rqlite: the program would not report its version: %w", err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	m := versionLine.FindStringSubmatch(first)
	if m == nil {
		return "", fmt.Errorf("%w: unexpected -version output %q", ErrBinaryMismatch, first)
	}
	if m[1] != Tag {
		return m[1], fmt.Errorf("%w: it reports %s, this version of Werkbord Team ships %s", ErrBinaryMismatch, m[1], Tag)
	}
	return m[1], nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".rqlited-*")
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

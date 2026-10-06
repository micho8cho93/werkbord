// Package update finds, downloads, verifies and installs a newer devboard.
//
// Releases live where the installer gets them: a GitHub releases page, laid out as
//
//	<base>/latest                       redirects to <base>/tag/<tag>
//	<base>/download/<tag>/<asset>       a release asset
//
// with one archive per platform, devboard_<version>_<os>_<arch>.tar.gz (.zip on
// Windows), and a checksums.txt (sha256sum format) beside them. Nothing is
// installed unless its checksum matches, and nothing is run before that.
//
// The repository releases more than one product, so a release is named by a tag
// that says whose it is: werkbord-v1.2.3 for this program (releases before 0.8.0
// used a bare v1.2.3). The program itself reports the bare version, v1.2.3; the
// tag is only how a release is found and downloaded. /latest must stay pointed at
// this product's newest release, which is why the other products' releases are
// published with "latest" turned off (see docs/VERSIONING.md).
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBase is where releases are published.
const DefaultBase = "https://github.com/micho8cho93/werkbord/releases"

// Source is a place releases come from.
type Source struct {
	// Base is the releases URL, without a trailing slash. Empty means DefaultBase,
	// or WERKBORD_RELEASE_URL (DEVBOARD_RELEASE_URL) if set (for a mirror, or a test).
	Base string
	HTTP *http.Client
}

func (s Source) base() string {
	switch {
	case s.Base != "":
		return strings.TrimSuffix(s.Base, "/")
	case releaseURL() != "":
		return strings.TrimSuffix(releaseURL(), "/")
	}
	return DefaultBase
}

func (s Source) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// Latest returns the newest release's tag, found by following /latest: that needs
// no API and no credentials, and is not subject to API rate limits.
func (s Source) Latest(ctx context.Context) (string, error) {
	c := *s.client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base()+"/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("look for the latest release: %w", err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || loc == "" {
		return "", fmt.Errorf("look for the latest release: %s did not point at a release (HTTP %d)", s.base()+"/latest", resp.StatusCode)
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	tag := path.Base(u.Path)
	if !tagRE.MatchString(tag) {
		return "", fmt.Errorf("the latest release is called %q, which is not a version", tag)
	}
	return tag, nil
}

// Status is what a look at the releases page found, for whoever shows it: the
// controller's API (and so the web app) and the desktop app.
type Status struct {
	// Current is the version that was asked about: v1.2.3, or dev for a build from source.
	Current string `json:"current"`
	// Latest is the newest stable release, as the program reports it (v1.2.3). It is
	// empty when nothing was looked up, or the lookup failed.
	Latest string `json:"latest,omitempty"`
	// Available: Latest is newer than Current. Never true for a build from source,
	// which is not an installed release and is updated from its source tree.
	Available bool `json:"available"`
	// Release is false for a build from source: no release is installed, so there is
	// nothing to update, and the releases page is never asked.
	Release bool `json:"release"`
	// Disabled: looking for updates is turned off for this controller (noUpdateCheck).
	Disabled bool `json:"disabled,omitempty"`
	// CheckedAt is when the releases page was last asked.
	CheckedAt time.Time `json:"checkedAt,omitempty"`
	// Error says why the releases page could not be asked. It is not an error of the
	// program: being offline is normal, and nothing else depends on this.
	Error string `json:"error,omitempty"`
}

// Check asks the releases page whether a newer stable release than current exists.
// "Stable" is what /latest means: it never points at a pre-release or a draft, and
// another product's releases are published with "latest" turned off (see the
// package comment), so a Werkbord Team release can never be offered here.
//
// A build from source is not asked about at all.
func (s Source) Check(ctx context.Context, current string) Status {
	st := Status{Current: current, Release: Release(current)}
	if !st.Release {
		return st
	}
	st.CheckedAt = time.Now().UTC()
	tag, err := s.Latest(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Latest = VersionOf(tag)
	st.Available = Compare(current, st.Latest) < 0
	return st
}

// Checker answers Check from a cache, so a page that asks on every visit does not
// become a request to GitHub on every visit. A failure is kept for a shorter time
// than an answer, so being offline is retried soon and not hammered.
type Checker struct {
	Source  Source
	Current string
	// Disabled makes every answer "not looking": nothing leaves this computer.
	Disabled bool
	// TTL is how long an answer is kept (default 6 hours); FailureTTL a failure (default 15 minutes).
	TTL, FailureTTL time.Duration
	// MinRefresh is the shortest time between two lookups, even when one is forced (default 1 minute).
	MinRefresh time.Duration

	now  func() time.Time
	mu   sync.Mutex
	last Status
	at   time.Time
}

// Status returns the cached answer, asking the releases page when there is none,
// it has expired, or force is set (and the last lookup was not just now).
func (c *Checker) Status(ctx context.Context, force bool) Status {
	if c.Disabled {
		return Status{Current: c.Current, Release: Release(c.Current), Disabled: true}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	ttl := orDuration(c.TTL, 6*time.Hour)
	if c.last.Error != "" {
		ttl = orDuration(c.FailureTTL, 15*time.Minute)
	}
	age := now().Sub(c.at)
	fresh := !c.at.IsZero() && age < ttl
	if force {
		fresh = !c.at.IsZero() && age < orDuration(c.MinRefresh, time.Minute)
	}
	if fresh {
		return c.last
	}
	c.last = c.Source.Check(ctx, c.Current)
	c.at = now()
	return c.last
}

func orDuration(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// TagPrefix starts the tag of every release of this product, from FirstProductTag on.
const TagPrefix = "werkbord-"

// FirstProductTag is the first version released under a product tag; earlier
// releases are tagged with the bare version.
const FirstProductTag = "v0.8.0"

var tagRE = regexp.MustCompile(`^(?:werkbord-)?v\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$`)

// ValidTag reports whether s is a release tag of this product: werkbord-v1.2.3,
// or the bare v1.2.3 of the earliest releases. Another product's tag (such as
// werkbord-team-v1.2.3) is not one.
func ValidTag(s string) bool { return tagRE.MatchString(s) }

// VersionOf is the version a release tag names, which is what the program
// reports about itself: werkbord-v1.2.3 and v1.2.3 are both v1.2.3.
func VersionOf(tag string) string { return strings.TrimPrefix(tag, TagPrefix) }

// TagFor is the tag of the release of a version (v1.2.3 or 1.2.3). A full tag is
// returned as it is.
func TagFor(version string) string {
	if strings.HasPrefix(version, TagPrefix) {
		return version
	}
	v := version
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if Compare(v, FirstProductTag) < 0 {
		return v
	}
	return TagPrefix + v
}

// AssetName is the archive for a platform in the release with this tag.
func AssetName(tag, goos, goarch string) string { return assetNamed("werkbord", tag, goos, goarch) }

// LegacyAssetName is the same archive under the name releases had before Werkbord
// was renamed from Dev Board. Every release still publishes it, for the updaters of
// those releases; this updater reads it only from a release that has nothing else.
func LegacyAssetName(tag, goos, goarch string) string {
	return assetNamed("devboard", tag, goos, goarch)
}

func assetNamed(prefix, tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s%s", prefix, strings.TrimPrefix(VersionOf(tag), "v"), goos, goarch, ext)
}

const (
	maxArchive   = 300 << 20
	maxChecksums = 1 << 20
	maxBinary    = 300 << 20
)

func (s Source) get(ctx context.Context, rawURL string, limit int64, dst io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", rawURL, resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("%s is larger than %d bytes", rawURL, limit)
	}
	return nil
}

// Download fetches the archive for a platform into dir and verifies it against
// the release's checksums. It returns the archive's path. An archive whose
// checksum is missing from the list, or does not match, is deleted and refused.
func (s Source) Download(ctx context.Context, tag, goos, goarch, dir string) (string, error) {
	asset := AssetName(tag, goos, goarch)
	base := s.base() + "/download/" + tag + "/"
	var sums strings.Builder
	if err := s.get(ctx, base+"checksums.txt", maxChecksums, &sums); err != nil {
		return "", fmt.Errorf("download the checksums: %w", err)
	}
	want, ok := checksumFor(sums.String(), asset)
	if !ok {
		// A release from before the rename publishes only the old name.
		if old, found := checksumFor(sums.String(), LegacyAssetName(tag, goos, goarch)); found {
			asset, want, ok = LegacyAssetName(tag, goos, goarch), old, true
		}
	}
	if !ok {
		return "", fmt.Errorf("the checksums for %s do not list %s: this release has no build for %s/%s", tag, asset, goos, goarch)
	}
	dest := filepath.Join(dir, asset)
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	err = s.get(ctx, base+asset, maxArchive, io.MultiWriter(f, h))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dest)
		return "", fmt.Errorf("download %s: %w", asset, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		_ = os.Remove(dest)
		return "", fmt.Errorf("%s does not match its checksum (expected %s, got %s): not installing it", asset, want, got)
	}
	return dest, nil
}

// checksumFor finds a file's sha256 in sha256sum output.
func checksumFor(list, name string) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(list))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], true
		}
	}
	return "", false
}

// BinaryName is the executable's name inside an archive for a platform.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "werkbord.exe"
	}
	return "werkbord"
}

// legacyBinaryName is the executable's name in an archive from before the rename.
func legacyBinaryName(goos string) string {
	if goos == "windows" {
		return "devboard.exe"
	}
	return "devboard"
}

// ExtractBinary takes the werkbord executable (named devboard in a release from
// before the rename) out of an archive and writes it into
// dir. Only that one file is read, by name, and never from a path the archive
// chooses, so a hostile archive cannot write elsewhere.
func ExtractBinary(archive, goos, dir string) (string, error) {
	want := BinaryName(goos)
	dest := filepath.Join(dir, want)
	write := func(r io.Reader) error {
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, io.LimitReader(r, maxBinary+1))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil && n > maxBinary {
			err = errors.New("the executable in the archive is implausibly large")
		}
		if err != nil {
			_ = os.Remove(dest)
		}
		return err
	}
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		for _, zf := range zr.File {
			if name := path.Base(zf.Name); (name == want || name == legacyBinaryName(goos)) && !zf.FileInfo().IsDir() {
				rc, err := zf.Open()
				if err != nil {
					return "", err
				}
				defer rc.Close()
				return dest, write(rc)
			}
		}
		return "", fmt.Errorf("%s has no %s", filepath.Base(archive), want)
	}
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("%s has no %s", filepath.Base(archive), want)
		}
		if err != nil {
			return "", err
		}
		if name := path.Base(hdr.Name); hdr.Typeflag == tar.TypeReg && (name == want || name == legacyBinaryName(goos)) {
			return dest, write(tr)
		}
	}
}

// Replace puts the executable at newBin in the place of the one at current, so
// the next start runs it, and keeps the old one beside it as current + ".prev".
// On Unix a running executable can be replaced by renaming over it; on Windows it
// can only be renamed aside, so it is.
func Replace(current, newBin string) (prev string, err error) {
	dir := filepath.Dir(current)
	prev = current + ".prev"
	if _, err := os.Lstat(prev); err == nil {
		return "", fmt.Errorf("an earlier update still has a rollback executable at %s; verify or recover that installation before updating again", prev)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot inspect rollback executable %s: %w", prev, err)
	}
	staged := filepath.Join(dir, "."+filepath.Base(current)+".new")
	if err := copyFile(newBin, staged, 0o755); err != nil {
		return "", fmt.Errorf("cannot write to %s (%w): reinstall with the install script, or use sudo", dir, err)
	}
	defer os.Remove(staged)
	if runtime.GOOS == "windows" {
		if err := os.Rename(current, prev); err != nil {
			return "", err
		}
		if err := os.Rename(staged, current); err != nil {
			_ = os.Rename(prev, current)
			return "", err
		}
		return prev, nil
	}
	if err := copyFile(current, prev, 0o755); err != nil {
		return "", fmt.Errorf("keep the old version: %w", err)
	}
	if err := os.Rename(staged, current); err != nil {
		return "", err
	}
	return prev, nil
}

// Restore puts the old executable back.
func Restore(current, prev string) error {
	if runtime.GOOS == "windows" {
		_ = os.Remove(current)
	}
	return os.Rename(prev, current)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// ---- versions ----

var semverRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// Release reports whether v is a release version (v1.2.3, optionally with a
// pre-release part), as opposed to a build from source ("dev", "v1.2.3-4-gabc-dirty").
func Release(v string) bool {
	m := semverRE.FindStringSubmatch(v)
	return m != nil && !strings.Contains(v, "-dirty") && !gitDescribeRE.MatchString(v)
}

var gitDescribeRE = regexp.MustCompile(`-\d+-g[0-9a-f]+`)

// Compare orders two versions: -1, 0 or 1. A pre-release is older than its release.
// Versions that are not versions compare as equal.
func Compare(a, b string) int {
	ma, mb := semverRE.FindStringSubmatch(a), semverRE.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return 0
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch pa, pb := ma[4], mb[4]; {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	case pa < pb:
		return -1
	}
	return 1
}

// releaseURL is WERKBORD_RELEASE_URL, or the older DEVBOARD_RELEASE_URL.
func releaseURL() string {
	if v := os.Getenv("WERKBORD_RELEASE_URL"); v != "" {
		return v
	}
	return os.Getenv("DEVBOARD_RELEASE_URL")
}

package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var bg = context.Background()

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// releaseServer serves a releases page for one tag, laid out like GitHub's.
func releaseServer(t *testing.T, tag string, assets map[string][]byte, checksums string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, checksums) })
	for name, body := range assets {
		body := body
		mux.HandleFunc("/download/"+tag+"/"+name, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) })
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestLatestFollowsTheRedirect(t *testing.T) {
	ts := releaseServer(t, "v1.4.2", nil, "")
	tag, err := Source{Base: ts.URL}.Latest(bg)
	if err != nil || tag != "v1.4.2" {
		t.Fatalf("latest = %q, %v", tag, err)
	}
	// A page that is not a redirect, or that redirects somewhere that is not a version, is an error.
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	if _, err := (Source{Base: bad.URL}).Latest(bg); err == nil {
		t.Fatal("no release page was accepted")
	}
	odd := httptest.NewServer(http.RedirectHandler("/tag/not-a-version", http.StatusFound))
	defer odd.Close()
	if _, err := (Source{Base: odd.URL}).Latest(bg); err == nil || !strings.Contains(err.Error(), "not a version") {
		t.Fatalf("odd tag: %v", err)
	}
}

func TestDownloadVerifiesTheChecksum(t *testing.T) {
	archive := tarGz(t, map[string]string{"devboard": "#!/bin/sh\necho v1.0.0\n"})
	asset := AssetName("v1.0.0", "linux", "amd64")
	if asset != "devboard_1.0.0_linux_amd64.tar.gz" || AssetName("v1.0.0", "windows", "arm64") != "devboard_1.0.0_windows_arm64.zip" {
		t.Fatalf("asset names = %s", asset)
	}
	good := fmt.Sprintf("%s  %s\n%s  other.tar.gz\n", sum(archive), asset, strings.Repeat("0", 64))
	ts := releaseServer(t, "v1.0.0", map[string][]byte{asset: archive}, good)

	dir := t.TempDir()
	path, err := Source{Base: ts.URL}.Download(bg, "v1.0.0", "linux", "amd64", dir)
	if err != nil || filepath.Base(path) != asset {
		t.Fatalf("download = %q, %v", path, err)
	}

	// A tampered archive is refused and removed.
	tampered := releaseServer(t, "v1.0.0", map[string][]byte{asset: append(archive, 'x')}, good)
	dir2 := t.TempDir()
	if _, err := (Source{Base: tampered.URL}).Download(bg, "v1.0.0", "linux", "amd64", dir2); err == nil || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Fatalf("tampered: %v", err)
	}
	if entries, _ := os.ReadDir(dir2); len(entries) != 0 {
		t.Fatalf("a refused download was kept: %v", entries)
	}
	// An archive the checksums do not list is refused, and the error says the platform has no build.
	unlisted := releaseServer(t, "v1.0.0", map[string][]byte{asset: archive}, strings.Repeat("0", 64)+"  other.tar.gz\n")
	if _, err := (Source{Base: unlisted.URL}).Download(bg, "v1.0.0", "linux", "amd64", t.TempDir()); err == nil || !strings.Contains(err.Error(), "no build for linux/amd64") {
		t.Fatalf("unlisted: %v", err)
	}
	// No checksums at all: nothing is installed unchecked.
	none := httptest.NewServer(http.NotFoundHandler())
	defer none.Close()
	if _, err := (Source{Base: none.URL}).Download(bg, "v1.0.0", "linux", "amd64", t.TempDir()); err == nil {
		t.Fatal("a release without checksums was accepted")
	}
}

func TestExtractBinaryOnlyReadsTheExecutableByName(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "a.tar.gz")
	_ = os.WriteFile(arch, tarGz(t, map[string]string{"README.md": "hi", "devboard_1.0.0_linux_amd64/devboard": "BIN", "../../evil": "x"}), 0o600)
	out := t.TempDir()
	bin, err := ExtractBinary(arch, "linux", out)
	if err != nil || bin != filepath.Join(out, "devboard") {
		t.Fatalf("extract = %q, %v", bin, err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "BIN" {
		t.Fatalf("binary = %q", b)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 1 {
		t.Fatalf("extra files written: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil")); err == nil {
		t.Fatal("a path in the archive was followed")
	}
	// No executable in it.
	empty := filepath.Join(dir, "e.tar.gz")
	_ = os.WriteFile(empty, tarGz(t, map[string]string{"README.md": "hi"}), 0o600)
	if _, err := ExtractBinary(empty, "linux", t.TempDir()); err == nil {
		t.Fatal("an archive without the executable was accepted")
	}

	// Zip, for Windows.
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	w, _ := zw.Create("devboard.exe")
	_, _ = w.Write([]byte("EXE"))
	_ = zw.Close()
	zpath := filepath.Join(dir, "a.zip")
	_ = os.WriteFile(zpath, zb.Bytes(), 0o600)
	zout := t.TempDir()
	if bin, err := ExtractBinary(zpath, "windows", zout); err != nil || filepath.Base(bin) != "devboard.exe" {
		t.Fatalf("zip = %q, %v", bin, err)
	}
}

func TestReplaceKeepsTheOldBinaryForRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by the rename path")
	}
	dir := t.TempDir()
	current := filepath.Join(dir, "devboard")
	_ = os.WriteFile(current, []byte("old"), 0o755)
	fresh := filepath.Join(t.TempDir(), "devboard")
	_ = os.WriteFile(fresh, []byte("new"), 0o755)

	prev, err := Replace(current, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(current); string(b) != "new" {
		t.Fatalf("current = %q", b)
	}
	if b, _ := os.ReadFile(prev); string(b) != "old" {
		t.Fatalf("prev = %q", b)
	}
	if fi, _ := os.Stat(current); fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the new binary is not executable: %v", fi.Mode())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("staging files left behind: %v", entries)
	}
	if err := Restore(current, prev); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(current); string(b) != "old" {
		t.Fatalf("after restore = %q", b)
	}

	// A directory that cannot be written says so, and changes nothing.
	ro := t.TempDir()
	target := filepath.Join(ro, "devboard")
	_ = os.WriteFile(target, []byte("old"), 0o755)
	_ = os.Chmod(ro, 0o500)
	defer os.Chmod(ro, 0o700)
	if os.Getuid() != 0 {
		if _, err := Replace(target, fresh); err == nil || !strings.Contains(err.Error(), "sudo") {
			t.Fatalf("read-only dir: %v", err)
		}
		if b, _ := os.ReadFile(target); string(b) != "old" {
			t.Fatal("a failed replace changed the binary")
		}
	}
}

func TestVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0}, {"v1.2.3", "v1.2.4", -1}, {"v1.10.0", "v1.9.9", 1}, {"v2.0.0", "v1.99.99", 1},
		{"v1.0.0-rc1", "v1.0.0", -1}, {"v1.0.0", "v1.0.0-rc1", 1}, {"v1.0.0-rc1", "v1.0.0-rc2", -1}, {"1.2.3", "v1.2.3", 0},
		{"dev", "v1.0.0", 0},
	} {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	for v, want := range map[string]bool{"v1.2.3": true, "v1.2.3-rc1": true, "dev": false, "v0.6.0-4-g1234abc": false, "v0.6.0-dirty": false, "1.2.3": true, "": false, "abc1234": false} {
		if Release(v) != want {
			t.Errorf("Release(%q) = %v", v, !want)
		}
	}
	if !ValidTag("v1.2.3") || ValidTag("1.2.3") || ValidTag("v1.2") || ValidTag("../v1.2.3") || ValidTag("v1.2.3/x") {
		t.Error("ValidTag is wrong")
	}
}

func TestProductTagsAreFoundAndOtherProductsAreNot(t *testing.T) {
	// A release is named by its product's tag, but the program reports the bare version.
	ts := releaseServer(t, "werkbord-v0.8.0", nil, "")
	tag, err := Source{Base: ts.URL}.Latest(bg)
	if err != nil || tag != "werkbord-v0.8.0" || VersionOf(tag) != "v0.8.0" {
		t.Fatalf("latest = %q (%s), %v", tag, VersionOf(tag), err)
	}
	// Were a Team release ever to be the latest, it must be refused, never installed over this program.
	team := releaseServer(t, "werkbord-team-v0.1.0", nil, "")
	if _, err := (Source{Base: team.URL}).Latest(bg); err == nil || !strings.Contains(err.Error(), "not a version") {
		t.Fatalf("a Team release was accepted as this product's latest: %v", err)
	}
	for tag, want := range map[string]bool{
		"werkbord-v0.8.0": true, "werkbord-v1.2.3-rc1": true, "v0.7.0": true,
		"werkbord-team-v0.1.0": false, "team-v1.0.0": false, "werkbord-1.2.3": false, "werkbord-v1.2": false,
		"werkbord-werkbord-v1.2.3": false, "werkbord-v1.2.3/x": false, "../werkbord-v1.2.3": false,
	} {
		if ValidTag(tag) != want {
			t.Errorf("ValidTag(%q) = %v, want %v", tag, !want, want)
		}
	}
}

func TestTagForAndAssetNames(t *testing.T) {
	for in, want := range map[string]string{
		"v0.8.0": "werkbord-v0.8.0", "0.8.0": "werkbord-v0.8.0", "v1.4.2": "werkbord-v1.4.2",
		"werkbord-v0.9.0": "werkbord-v0.9.0",
		"v0.7.0":          "v0.7.0", // released before product tags: the bare tag is the only one that exists
	} {
		if got := TagFor(in); got != want {
			t.Errorf("TagFor(%q) = %q, want %q", in, got, want)
		}
	}
	// The archive is named by the version, whichever way the tag was spelled.
	for _, tag := range []string{"werkbord-v1.0.0", "v1.0.0"} {
		if got := AssetName(tag, "linux", "amd64"); got != "devboard_1.0.0_linux_amd64.tar.gz" {
			t.Errorf("AssetName(%q) = %q", tag, got)
		}
	}
}

//go:build !windows

package rqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A stand-in for the program: it reports a version the way rqlited does.
func standIn(t *testing.T, dir, version string) string {
	t.Helper()
	p := filepath.Join(dir, "rqlited")
	body := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"-version\" ]; then echo 'rqlited %s linux amd64 go1.26.7 sqlite3.53.4 (commit x, compiler gc)'; fi\n", version)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func privateDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "d")
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOnlyThePinnedProgramIsLocated(t *testing.T) {
	src := privateDir(t)
	p := standIn(t, src, Tag)
	sum, _ := sha256File(p)
	art := Artifact{BinarySHA256: sum}
	v, err := locate([]string{src}, privateDir(t), art)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Pinned || v.SHA256 != sum || v.Path == p {
		t.Fatalf("verified %+v: it must run a private copy, with a pinned hash", v)
	}
	// A different file is refused, even when a good one is further along.
	other := privateDir(t)
	standIn(t, other, "v9.9.9")
	if _, err := locate([]string{other, src}, privateDir(t), art); !errors.Is(err, ErrBinaryMismatch) {
		t.Fatalf("a program that is not the pinned one was located: %v", err)
	}
	// Not found.
	if _, err := locate([]string{privateDir(t)}, privateDir(t), art); !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("%v", err)
	}
	// A relative directory and the PATH are not searched.
	t.Setenv("PATH", src)
	if _, err := locate([]string{"relative"}, privateDir(t), art); !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("a relative directory was searched: %v", err)
	}
	// A symlink is not a program.
	link := privateDir(t)
	if err := os.Symlink(p, filepath.Join(link, "rqlited")); err != nil {
		t.Fatal(err)
	}
	if _, err := locate([]string{link}, privateDir(t), art); !errors.Is(err, ErrBinaryMismatch) {
		t.Fatalf("a symbolic link was run: %v", err)
	}
	// What was verified is checked again at the moment of starting: a changed copy is not run.
	if err := os.Chmod(v.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v.Path, []byte("#!/bin/sh\necho changed\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.reverify(art); !errors.Is(err, ErrBinaryMismatch) {
		t.Fatalf("a copy changed after it was verified would have been run: %v", err)
	}
}

func TestAProgramBuiltFromSourceNeedsItsBuildRecord(t *testing.T) {
	src := privateDir(t)
	p := standIn(t, src, Tag)
	sum, _ := sha256File(p)
	art := Artifact{Source: true}
	if _, err := locate([]string{src}, privateDir(t), art); !errors.Is(err, ErrBinaryMismatch) {
		t.Fatalf("a program with no build record was accepted: %v", err)
	}
	write := func(commit, hash string) {
		rec := fmt.Sprintf(`{"version":%q,"commit":%q,"sha256":%q}`, Tag, commit, hash)
		if err := os.WriteFile(filepath.Join(src, RecordName), []byte(rec), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("0000000000000000000000000000000000000000", sum)
	if _, err := locate([]string{src}, privateDir(t), art); !errors.Is(err, ErrBinaryMismatch) {
		t.Fatalf("a program built from another commit was accepted: %v", err)
	}
	write(SourceCommit, strings.Repeat("a", 64))
	if _, err := locate([]string{src}, privateDir(t), art); !errors.Is(err, ErrBinaryMismatch) {
		t.Fatalf("a program that is not the one the record describes was accepted: %v", err)
	}
	write(SourceCommit, sum)
	v, err := locate([]string{src}, privateDir(t), art)
	if err != nil {
		t.Fatal(err)
	}
	if v.Pinned {
		t.Fatal("a program accepted on its build record is not hash-pinned, and must not say it is")
	}
	// A platform with no entry at all runs nothing.
	if _, err := locate([]string{src}, privateDir(t), Artifact{}); !errors.Is(err, ErrBinaryMismatch) {
		t.Fatalf("a platform with no pin ran something: %v", err)
	}
}

func TestTheProgramMustReportThePinnedVersion(t *testing.T) {
	ctx := context.Background()
	for version, ok := range map[string]bool{Tag: true, "v10.5.1": false, "v11.0.0": false, "10": false} {
		dir := privateDir(t)
		p := standIn(t, dir, version)
		v := verified{Path: p}
		got, err := verifyVersion(ctx, v)
		if (err == nil) != ok {
			t.Errorf("reporting %q: %v (%q)", version, err, got)
		}
		if err != nil && !errors.Is(err, ErrBinaryMismatch) {
			t.Errorf("reporting %q: the error is not a mismatch: %v", version, err)
		}
	}
	// A program that prints nonsense is not accepted either.
	dir := privateDir(t)
	p := filepath.Join(dir, "rqlited")
	_ = os.WriteFile(p, []byte("#!/bin/sh\necho hello\n"), 0o755)
	if _, err := verifyVersion(ctx, verified{Path: p}); !errors.Is(err, ErrBinaryMismatch) {
		t.Fatalf("%v", err)
	}
}

func TestThePlatformsAreTheOnesTeamShips(t *testing.T) {
	want := []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"}
	if got := Platforms(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("platforms %v", got)
	}
	for _, p := range want {
		os, arch, _ := strings.Cut(p, "/")
		a, err := ArtifactFor(os, arch)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case a.Source && (a.Archive != "" || a.BinarySHA256 != ""):
			t.Errorf("%s: a source build with a published pin", p)
		case !a.Source && (len(a.ArchiveSHA256) != 64 || len(a.BinarySHA256) != 64 || !strings.Contains(a.Archive, Tag)):
			t.Errorf("%s: an incomplete pin %+v", p, a)
		}
	}
	if _, err := ArtifactFor("windows", "amd64"); err == nil {
		t.Error("Windows has a pin")
	}
	if len(SourceCommit) != 40 || Tag != "v"+Version {
		t.Error("the version or commit is malformed")
	}
	_ = runtime.GOOS
}

func TestThePinsMatchWhatTheReleasePublished(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	published, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "third_party", "rqlite", "RELEASE-v"+Version+".sha256"))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(published), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && !strings.HasPrefix(line, "#") {
			have[f[1]+" "+f[0]] = true
		}
	}
	for _, p := range Platforms() {
		goos, goarch, _ := strings.Cut(p, "/")
		a, _ := ArtifactFor(goos, goarch)
		if a.Source {
			continue
		}
		if !have[a.Archive+" "+a.ArchiveSHA256] {
			t.Errorf("%s: the archive %s with SHA-256 %s is not in what the release published", p, a.Archive, a.ArchiveSHA256)
		}
	}
	// The script that fetches the program reads the same file the supervisor does: it must find every platform it names.
	script, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "fetch-rqlite.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "internal/team/infra/rqlite/manifest.go") {
		t.Error("the fetch script does not read the manifest")
	}
}

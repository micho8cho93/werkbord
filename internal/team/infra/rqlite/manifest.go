// Package rqlite is Werkbord Team's supervisor for the second program a Team
// deployment runs: rqlite (https://rqlite.io, MIT), the replicated SQLite database
// that holds a workspace's data on its Workspace Hosts.
//
// Werkbord ships rqlite, it does not reimplement it, and it does not reimplement Raft:
// replication, leader election, membership changes and snapshots are rqlite's. What this
// package knows is which release Werkbord ships (manifest.go: a pinned version and the
// SHA-256 of every file), how to check a binary against that pin, how to build the flags
// one node needs from a typed description (and refuse any that would expose it), and how
// to start, watch, stop and ask questions of that one program. Like the Nebula supervisor
// it has no function that runs "a command": the only way to start anything is Start, which
// starts the verified, pinned rqlited with arguments this package builds itself.
// internal/archtest keeps it that way.
package rqlite

import (
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
)

// Version is the rqlite release Werkbord Team ships and supervises. Moving it is a
// reviewed change to this file (docs/TEAM_STORAGE.md, "Bumping the pinned rqlite"): the
// version, the commit, every hash below and third_party/rqlite/ change together. A binary
// that does not match is refused; "latest" is never fetched at runtime.
const Version = "10.5.2"

// Tag is the release's tag and the version string the program reports.
const Tag = "v" + Version

// SourceCommit is the commit the release was built from (and the tag points at). A
// platform with no published binary is built from exactly this commit.
const SourceCommit = "a73dd2e63acb72080f5eb20881a03bb06a464f89"

// distributionBinarySHA256 is set by the macOS packager after it verifies the pinned source build,
// combines architectures and signs it. The service then pins that exact signed artifact, not a mutable build record.
// Empty on source builds and on platforms with an upstream binary. It never comes from runtime configuration.
var distributionBinarySHA256 string

// ReleaseURL is where the pinned release is published. It is used only by
// scripts/fetch-rqlite.sh at build time, never by the running server (which has no code
// that downloads anything), and every download is checked against the hashes below before
// anything is unpacked.
const ReleaseURL = "https://github.com/rqlite/rqlite/releases/download/" + Tag + "/"

// Artifact pins one platform's release.
type Artifact struct {
	// Archive is the release asset's name; ArchiveSHA256 its SHA-256, as published in the
	// release and checked again when pinned. Both are empty for a platform with no
	// published binary.
	Archive       string
	ArchiveSHA256 string
	// BinarySHA256 is the SHA-256 of the rqlited program inside the archive; it is what
	// the supervisor checks, immediately before every start. Empty for a platform whose
	// program is built from SourceCommit (Source): a trusted build embeds its exact hash
	// in distributionBinarySHA256 before the service may execute it.
	BinarySHA256 string
	// Source marks a platform for which the project publishes no binary: the program is
	// built, by scripts/fetch-rqlite.sh, from the pinned source at SourceCommit with the
	// same version flags the project's own release build uses. A build is not reproducible
	// across toolchains. A build pins the source and embeds its resulting executable hash;
	// runtime checks that hash before asking for the version. An adjacent build record is
	// diagnostic data and cannot authorize an executable.
	Source bool
}

// artifacts are the platforms Team's installer supports. Windows is not here: Team's
// installer does not support it.
var artifacts = map[string]Artifact{
	"linux/amd64":  {Archive: "rqlite-v10.5.2-linux-amd64.tar.gz", ArchiveSHA256: "1a04a7c2c542e8e1c1347bd31af0fa9ef7043febb3c4d1806d36bc1702c6fa0f", BinarySHA256: "7c52a50e1ae99debcf5ce59fe0410c2e939aa5ec30b1ebb8e736ea6d9273d426"},
	"linux/arm64":  {Archive: "rqlite-v10.5.2-linux-arm64.tar.gz", ArchiveSHA256: "44b4bd7ca39720dee3b916f9ef59da117c07ddc008704ac25e56385e56951b8a", BinarySHA256: "84b089fcfd5c1f38564d816cd9c619f0538d9bd60cb03ee47009531827895573"},
	"darwin/amd64": {Source: true},
	"darwin/arm64": {Source: true},
}

// ErrUnsupportedPlatform is returned where no pinned rqlite exists for the platform.
type ErrUnsupportedPlatform struct{ OS, Arch string }

func (e ErrUnsupportedPlatform) Error() string {
	return fmt.Sprintf("no pinned rqlite release for %s/%s (supported: %s)", e.OS, e.Arch, strings.Join(Platforms(), ", "))
}

// ArtifactFor returns the pinned release for a platform.
func ArtifactFor(goos, goarch string) (Artifact, error) {
	a, ok := artifacts[goos+"/"+goarch]
	if !ok {
		return Artifact{}, ErrUnsupportedPlatform{goos, goarch}
	}
	if goos == "darwin" && distributionBinarySHA256 != "" {
		b, err := hex.DecodeString(distributionBinarySHA256)
		if err != nil || len(b) != 32 {
			return Artifact{}, fmt.Errorf("invalid distribution database pin")
		}
		a.BinarySHA256 = distributionBinarySHA256
	}
	return a, nil
}

// ThisPlatform returns the pinned release for the platform the program runs on.
func ThisPlatform() (Artifact, error) { return ArtifactFor(runtime.GOOS, runtime.GOARCH) }

// Platforms lists the supported platforms, sorted.
func Platforms() []string {
	out := make([]string, 0, len(artifacts))
	for k := range artifacts {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// BinaryName is the file the supervisor looks for.
const BinaryName = "rqlited"

// RecordName is the build record beside a program built from source.
const RecordName = "rqlited.build"

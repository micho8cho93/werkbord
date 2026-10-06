// Package nebula is Werkbord Team's supervisor for the one network program a Team
// deployment runs: Nebula (https://github.com/slackhq/nebula, MIT), the private
// network a customer's workspace creates and owns.
//
// Werkbord ships Nebula, it does not reimplement it. What this package knows is
// which release it ships (manifest.go: a pinned version and the SHA-256 of every
// file), how to check a binary against that pin, how to render the configuration a
// node needs from a typed description, and how to start, watch and stop that one
// program. It deliberately has no function that runs "a command": the only way to
// start anything is StartNebula, which starts the verified, pinned Nebula binary
// with arguments this package builds itself. internal/archtest keeps it that way.
package nebula

import (
	"fmt"
	"runtime"
	"strings"
)

// Version is the Nebula release Werkbord Team ships and supervises. Moving it is a
// reviewed change to this file (see docs/TEAM_NETWORK.md, "Bumping the pinned
// Nebula"): the version, every hash below, and third_party/nebula/ change together.
// A binary that does not match is refused; "latest" is never fetched at runtime.
const Version = "1.11.2"

// ReleaseURL is where the pinned release is published. It is used only by
// scripts/fetch-nebula.sh at build time, never by the running server (which has no
// code that downloads anything), and every download is checked against the hashes
// below before anything is unpacked.
const ReleaseURL = "https://github.com/slackhq/nebula/releases/download/v" + Version + "/"

// Artifact pins one platform's release.
type Artifact struct {
	// Archive is the release asset's name, and ArchiveSHA256 its SHA-256, as
	// published in the release's SHASUM256.txt and checked again when pinned.
	Archive       string
	ArchiveSHA256 string
	// BinarySHA256 is the SHA-256 of the `nebula` program inside the archive. It is
	// what the supervisor checks, immediately before every start.
	BinarySHA256 string
}

// artifacts are the platforms Team's installer supports (macOS and Linux, on
// amd64 and arm64). Windows is not here: Nebula needs the third-party Wintun
// driver there, whose licence has terms this project has not taken on, and
// Team's installer does not support Windows.
var artifacts = map[string]Artifact{
	// One universal binary serves both Mac architectures.
	"darwin/amd64": {Archive: "nebula-darwin.zip", ArchiveSHA256: "ae23ebd29e570d72f28d71e608261d454fb87b1a19b4ca21c2aa26bb4698da88", BinarySHA256: "b2eec15124d83774e5831baeabfca6283f90c950143e21334ed2522c793e9038"},
	"darwin/arm64": {Archive: "nebula-darwin.zip", ArchiveSHA256: "ae23ebd29e570d72f28d71e608261d454fb87b1a19b4ca21c2aa26bb4698da88", BinarySHA256: "b2eec15124d83774e5831baeabfca6283f90c950143e21334ed2522c793e9038"},
	"linux/amd64":  {Archive: "nebula-linux-amd64.tar.gz", ArchiveSHA256: "6140d33f2ec21ce7f6b655b5bc820e93a684d97e51d0ddcf907324b5b28aac1e", BinarySHA256: "9cda43821d200211fbc264f2ac4df48973c6efde2ad0984be769699a55c2f0d5"},
	"linux/arm64":  {Archive: "nebula-linux-arm64.tar.gz", ArchiveSHA256: "85d10e7bc2d121193c1392a1a919172ded7c413f46e602138281cfa9fa1b0231", BinarySHA256: "e59c1cbaeaec58936ef4b94265d3b0827a504e98575133094940b2e63e009c38"},
}

// ErrUnsupportedPlatform is returned where no pinned Nebula exists for the platform.
type ErrUnsupportedPlatform struct{ OS, Arch string }

func (e ErrUnsupportedPlatform) Error() string {
	return fmt.Sprintf("no pinned Nebula release for %s/%s (supported: %s)", e.OS, e.Arch, strings.Join(Platforms(), ", "))
}

// ArtifactFor returns the pinned release for a platform.
func ArtifactFor(goos, goarch string) (Artifact, error) {
	a, ok := artifacts[goos+"/"+goarch]
	if !ok {
		return Artifact{}, ErrUnsupportedPlatform{goos, goarch}
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
	// Small and fixed: an insertion sort keeps this file free of imports it does not need.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// BinaryName is the file the supervisor looks for.
const BinaryName = "nebula"

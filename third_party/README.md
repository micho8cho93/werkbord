# Third-party material that ships with Werkbord Team

| Directory | What | Why it is here |
| --- | --- | --- |
| `nebula/` | The licence of [Nebula](https://github.com/slackhq/nebula) (MIT), the licences of every Go module statically linked into the Nebula release binary Team ships (`THIRD_PARTY_LICENSES.txt`, generated), and the checksum file that release itself published (`SHASUM256-v<version>.txt`). | Team's release archives for macOS and Linux carry the unmodified Nebula binary and these licences, as their terms require; the checksum file is what a test compares the pins in the supervisor's `manifest.go` against. Nebula has no NOTICE file. |
| `go/` | The Go licence. | Every Go program, Nebula's included, has the Go runtime in it. |

To change the pinned Nebula release, follow "Bumping the pinned Nebula" in [docs/TEAM_NETWORK.md](../docs/TEAM_NETWORK.md): the
version and hashes in `manifest.go`, this directory (`scripts/gen-nebula-notices.sh <version>`, the new checksum file and
`LICENSE`), `go.mod`, and the tests move together.

Nothing here is downloaded when Team runs. The Nebula binary itself is not in the repository: `scripts/fetch-nebula.sh` gets
it into `.cache/nebula` and refuses anything that does not match the pin.

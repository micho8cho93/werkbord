# Installing and updating Team

For ordinary use, install the unified Werkbord desktop app, which carries Team's native installer and service for macOS, Linux and Windows. It activates only when explicitly requested. Individual remains free, and each workspace retains backend-authoritative offline licensing. See [unified distribution](ARCHITECTURE.md#distribution-licensing-and-updates).

Werkbord is one release with one version. The app is signed and notarized like everything else in it, and the Team service refuses to install unless the app was signed by the release's Apple Developer team. Joined devices and Workspace Hosts defer native service replacement until administrator maintenance; installing a newer app does not move their databases. Until the host is updated, a window newer than it works with what the host knows: a screen the host has no route for (Calendar, Analytics, MCP clients, comments and dates all came after Team 3.x) says "needs a newer Workspace Host" with the host's version, in place of the host's own "no such endpoint", and the rest of the window keeps working.

Team requires no Tailscale account. Customers operate their own hosts, remote-access connectivity, backups and runners. Production admission is recorded in [TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md); do not treat an ad-hoc preview as a signed release.

Use the app and its platform-native Team installer described in [TEAM_DESKTOP.md](TEAM_DESKTOP.md). Verify the macOS Developer ID/notarization, Linux root-owned package path, or Windows Authenticode signature before granting the installer system privileges. The macOS root service uses System Keychain; Linux and Windows use protected passphrase files. Test the actual signed service through updates before deployment.

The public unified desktop release does not include standalone Team Host CLI archives. The CLI and offline archive tool remain available for source builds and controlled operator deployments. For an explicitly authorized private archive, obtain a **reviewed local** `scripts/install-team.sh` and the vendor's release verification PEM from a trusted independent channel. Do not pipe an unverified website script into a shell; a replaced script can remove every signature check. Provision the public key separately from release downloads:

```sh
WERKBORD_TEAM_RELEASE_PUBLIC_KEY_FILE=/trusted/werkbord-team-release.pub \
WERKBORD_TEAM_VERSION=werkbord-v4.0.0-preview.1 \
  sh scripts/install-team.sh
```

The installer authenticates `checksums-team.txt.sig` with Ed25519, binds it to Team's archives and the release tag (`werkbord-vX.Y.Z`), checks the requested archive, and checks its executable's version before installing anything. OpenSSL with Ed25519 `pkeyutl -rawin` support is required. A missing trust key/signature fails closed. A checksum downloaded beside an archive is insufficient. The archive includes the pinned Nebula/rqlite programs and third-party notices; runtime fetches neither. macOS rqlite must be built on a Mac and hash-stamped into Team; a Darwin archive made without that sidecar is not a production Workspace Host distribution.

If a customer requires a separately managed CLI deployment, its archive must be built and signed on the reviewed offline release workstation:

```sh
LICENSE_ISSUER_PUBLIC_KEY=<license-public-key-in-raw-url-base64> \
WERKBORD_TEAM_RELEASE_SIGNING_KEY_FILE=/secure/separate-release-key.pem \
  make dist PRODUCT=werkbord-team
```

Do not attach those standalone archives to the public unified desktop release. Distribute them only through the reviewed customer-controlled channel with `checksums-team.txt` and `checksums-team.txt.sig`. `BUNDLE_NEBULA=no` is only an unsigned archive-format fixture, not a production distribution. Production vendor license/release verification keys still require maintainer provisioning; this repository does not silently trust a generated test key.

For a CLI workspace, configure its signed license and secure storage, then create it:

```sh
export WERKBORD_TEAM_LICENSE_FILE=/customer/licenses/team.json
# Linux / unsupported OS storage: owner-only, high-entropy passphrase file
# outside the workspace data/backups. macOS normally uses Keychain.
export WERKBORD_TEAM_PKI_PASSPHRASE_FILE=/customer/secure/team-unlock
werkbord-team workspace create --name Acme --owner Ada --endpoint team.example.org
werkbord-team serve
```

Nebula and administrator enrollment approval are defaults. `--network=false --storage=single-file` is explicit loopback evaluation. `--addr 0.0.0.0:7430` is rejected in production: Team binds its separate remote API to Nebula. Allow only the documented public enrollment/UDP endpoints. Add two additional Workspace Hosts for quorum tolerance.

Upgrade all hosts in a planned maintenance window when changing schemas or remote protocol. Older hosts refuse an unknown database schema; older remote bearer clients receive 401 and must re-enroll/use the current device-signing client. If an old CLI host lacks the sealed member identity, re-enroll that host rather than guessing an identity from unauthenticated remote data. Team has no silent Tailscale/Nebula dual mode.

The CLI installer stages the new executable and publishes it after sidecars. An interrupted multi-file replacement may leave the old executable with a new sidecar; the runtime rejects a mismatched hash rather than executing it. Running supervisors use private verified copies. Stop the service, rerun the exact signed installer, verify the whole bundle and restart. This is fail-closed recovery, not an atomic multi-file update. See the gate for tested interruption points.

## Transition from adjacent key files

Team 3.x supports `WERKBORD_TEAM_KEY_STORAGE=file` only as explicit evaluation/transition compatibility with 2.x vaults. It never falls back from failed OS storage. Existing ciphertext cannot be read by a newly created unrelated Keychain key. There is no automatic vault rewrapping tool in this release.

Keep the old host stopped and retain an encrypted offline recovery copy. Start with the explicit old key mode only to maintain access during a controlled migration, then enroll a fresh, securely stored replacement host and remove the old one via quorum-aware membership changes. Re-enroll ordinary devices into new securely stored installations while clearing the old local trust/approval state. Keep the authority unlock material apart from database backups. The next major removes adjacent-key/legacy license transition support. If authority material was exposed, rebuild a new workspace/network instead of treating storage migration as key rotation.

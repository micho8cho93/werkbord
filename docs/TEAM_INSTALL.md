# Installing and updating Team

For ordinary use, install the one Werkbord desktop app. **New team or join…** opens setup; installing the privileged Team service
is explicit and needs administrator authorisation on macOS. Personal needs no Team service. Free includes teams of two;
teams of 3 or more need Premium for every member, checked offline by the host. Linux packages are opt-in after acceptance and
Windows publication is deferred. See [plans](PLANS.md) and [distribution](ARCHITECTURE.md#distribution-plans-and-updates).

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
WERKBORD_TEAM_RELEASE_SIGNING_KEY_FILE=/secure/separate-release-key.pem \
  make dist PRODUCT=werkbord-team
```

Do not attach those standalone archives to the public unified desktop release. Distribute them only through the reviewed customer-controlled channel with `checksums-team.txt` and `checksums-team.txt.sig`. `BUNDLE_NEBULA=no` is only an unsigned archive-format fixture, not a production distribution. The production release verification key still requires maintainer provisioning; this repository does not silently trust a generated test key.

For a CLI workspace, configure its secure storage, then create it (a team of up to two people is free; see [PLANS.md](PLANS.md)):

```sh
# Linux / unsupported OS storage: owner-only, high-entropy passphrase file
# outside the workspace data/backups. macOS normally uses Keychain.
export WERKBORD_TEAM_PKI_PASSPHRASE_FILE=/customer/secure/team-unlock
werkbord-team workspace create --name Acme --owner Ada --endpoint team.example.org
werkbord-team serve
```

Nebula and administrator enrollment approval are defaults. `--network=false --storage=single-file` is explicit loopback evaluation. `--addr 0.0.0.0:7430` is rejected in production: Team binds its separate remote API to Nebula. Allow only the documented public enrollment/UDP endpoints. Add two additional Workspace Hosts for quorum tolerance.

Upgrade all hosts in a planned maintenance window when changing schemas or remote protocol. Older hosts refuse an unknown database schema; older remote bearer clients receive 401 and must re-enroll/use the current device-signing client. If an old CLI host lacks the sealed member identity, re-enroll that host rather than guessing an identity from unauthenticated remote data. Team has no silent Tailscale/Nebula dual mode.

The CLI installer stages the new executable and publishes it after sidecars. An interrupted multi-file replacement may leave the old executable with a new sidecar; the runtime rejects a mismatched hash rather than executing it. Running supervisors use private verified copies. Stop the service, rerun the exact signed installer, verify the whole bundle and restart. This is fail-closed recovery, not an atomic multi-file update. See the gate for tested interruption points.

## Maintenance: updating a service that holds a workspace

The ordinary update refuses a service that holds a workspace and never weakens that. The supported way to bring such a service to the app's version is maintenance, macOS only in this release. Run the release-signed app's executable with `--maintain`:

```sh
/Applications/Werkbord.app/Contents/MacOS/Werkbord --maintain
```

It needs the same release signing as an installation (a development or ad-hoc build is refused) and the same macOS administrator authorization, and it must be run by the account that owns the installation. It refuses while a workspace on the computer is being created, joined, left or demoted, and when the running service cannot show an open workspace. In order:

1. The new programs are staged and the staged `werkbord-team` must report this app's version. The service has not been touched.
2. A computer that only joined a team (a member, not a Workspace Host) keeps no copy of the workspace's database: the hosts hold it, and a backup asked of such a computer is made on a host's disk, never in this one's backup directory. For such a workspace steps 3 and 4 are skipped and the programs are simply replaced (the note and the replacement). Only a workspace that plainly has none of the places a host keeps its data (`storage.json`, `storage/`, `team.db`, looked for in the workspace's `workspace/` directory and beside it) is treated this way; when in doubt, it is backed up.
3. If the service has no backup directory, `--backup-dir <Werkbord Team>/backups` is added to its launch definition (nothing else in it changes) and the same programs are restarted; if they do not come back with the workspace open, the original definition is restored.
4. The running service is asked, as this device, to back up the workspace. The backup must match its recorded hash and be opened by the NEW `werkbord-team storage verify-backup`. Otherwise maintenance stops with the old service running.
5. A note of the versions and the backup is written to `<Werkbord Team>/maintenance/`, and a hand-off note, `upgrade-handoff-<workspace>.json`, beside the backup in the backup directory. The new service reads the hand-off (see [The service protects its own data](#the-service-protects-its-own-data)) and does not make a second backup.
6. The service stops and only its programs are replaced (the old ones stay as `Helpers.previous`). The data directory, workspace, System-keychain items, access key, owner record, network configuration and hosting state are never written. The service must come back at the new version with the workspace still open and, if it was connected before, connected.
7. If it does not, the previous programs are restored and started again. A schema migration the new service already made cannot be undone by this; if the previous programs then refuse the database, restore the backup named in the error as described in [TEAM_BACKUP_RECOVERY.md](TEAM_BACKUP_RECOVERY.md).

A keychain item is tied to the program that created it, so a new program signed differently may be unable to open the workspace. Step 6 detects that and rolls back rather than leaving the workspace sealed. Keep the backups directory; maintenance never deletes anything from it.

## The service protects its own data

Maintenance is one way a new version arrives. macOS launching the service from inside the app, or a person running `werkbord-team serve`, is another, and nothing there runs `--maintain`. So the service does not depend on how it was installed: **when it starts and finds its data last opened by an older version, it takes a backup before it opens the database.** For each workspace that keeps a database on this computer, in order:

1. It takes a backup into the backup directory (`--backup-dir` / `WERKBORD_TEAM_BACKUP_DIR`; the desktop service's `backups/` beside `workspace/` by default; `backups/` in the data directory for a `serve` given none). A cluster's backup is the one `werkbord-team storage backup` takes; a single-file workspace is copied with SQLite's consistent copy.
2. It verifies the backup the way `storage verify-backup` does: the hash against the record beside it, SQLite's integrity, and a scratch copy brought to this version's schema, which proves this version can open it. A backup that fails is removed.
3. It records both versions in `data-guard.json` in the workspace directory: the version that opened the data before, this version, and the backup (`dataProtection` in the device state shows them).
4. Only then does it open the database, which is what migrates it.

If the backup or its verification fails (the disk is full, the backup directory cannot be used, this version cannot read what the older one wrote), the service does **not** open the database, and nothing in it is read for writing, migrated or changed. The window reads why from `GET /api/device/v1/state`: `dataProtection` has `state: "refused"`, the `reason`, `dataUntouched: true` and the version still on record, and `error` carries the same reason. A refusal is not retried more than every five minutes within one process, so a failing service does not fill the disk with backups; starting the service again tries at once. Fix the cause and start it again.

What counts as "older": the recorded version is a release or preview below this one, or is not a version that can be ordered (a build made from source between releases, or data that no version recorded, such as a workspace from before this existed). The same version again, and a newer one (a downgrade; the database refuses a schema it does not know), take no backup. A brand-new workspace, and a host that has just joined a cluster and holds no copy yet, have nothing older to protect and only record the version. A computer that only joined a team (no `storage.json`, `storage/` or `team.db`) has no database here, so nothing is backed up and nothing is recorded.

**Maintenance and the service do not do the work twice.** After maintenance has taken and verified its backup, it leaves `upgrade-handoff-<workspace>.json` in the backup directory: the workspace, the two versions, the backup's name and its hash. The new service accepts that backup, without making another, only if the note is for its workspace and version, matches the version on record, and the backup file still has that hash; it then deletes the note. Anything else, and it takes its own backup.

The service reads and copies its own workspace's database; it starts no process and holds no Git, GitHub or agent credential. Backups the service takes are ordinary backups and follow the same retention as scheduled ones; maintenance's retained copy under `<Werkbord Team>/backups/maintenance-*` is never pruned.

A cluster's database node may have no leader for a moment after it starts. When there is something to protect, the service waits up to 20 seconds for one, so that the backup and the migration happen together instead of waiting for the next start; a cluster that stays without a leader is opened read-only on this host's copy as before, which migrates nothing, and the data is protected on the next start that can write.

## Transition from adjacent key files

Team 3.x supports `WERKBORD_TEAM_KEY_STORAGE=file` only as explicit evaluation/transition compatibility with 2.x vaults. It never falls back from failed OS storage. Existing ciphertext cannot be read by a newly created unrelated Keychain key. There is no automatic vault rewrapping tool in this release.

Keep the old host stopped and retain an encrypted offline recovery copy. Start with the explicit old key mode only to maintain access during a controlled migration, then enroll a fresh, securely stored replacement host and remove the old one via quorum-aware membership changes. Re-enroll ordinary devices into new securely stored installations while clearing the old local trust/approval state. Keep the authority unlock material apart from database backups. The next major removes adjacent-key transition support. If authority material was exposed, rebuild a new workspace/network instead of treating storage migration as key rotation.

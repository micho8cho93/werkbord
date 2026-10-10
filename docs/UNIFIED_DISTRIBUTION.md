# Unified distribution (Phase 4)

Individual 1.9.0-preview.1 / Team 3.8.0 implement one primary macOS installer.
The preview suffix remains until production distribution acceptance. The shell
tracks Individual's version; Team retains its own executable, version and tag.
Both CLI installers and historical `devboard`/`DEVBOARD_*` behavior remain.

The bundle contains the shell, the Personal helper, and an inert nested
`Werkbord Team.app` with Team's own native installer, backend, pinned sidecars,
license verification and offline release proof. It installs no privileged service on launch.
Personal works for free without a license, Team infrastructure or vendor login.
**Add a Team** uses an explicit native dialog and administrator authorization to
activate Team. Each workspace has a separate enrolled identity and signed license;
up to sixteen teams are supported by the existing isolated slots.

Seats remain live members of a workspace, including its owner. Additional devices
enroll under that member and consume no additional seats. The Team backend checks
signatures and seats transactionally; frontend controls never unlock authority.
Renewal remains owner-authorized, offline and possible after runtime expiration.
Support expiry does not disable the runtime. See [TEAM_LICENSE.md](TEAM_LICENSE.md).

## Compatibility

The machine-readable matrix is `desktop/build/compatibility.json`, copied into the
bundle and checked against the reviewed source. Component versions are read from
the actual helpers and written to `Resources/components.txt`; the shell exposes
them in Workspaces and devices and Help → Diagnostics.

| Component | Supported contract |
| --- | --- |
| Shell | 1.10.x, versioned with Individual; Individual + isolated Team frames |
| Personal | Bundled 1.6–1.x; unified summary v1 and `execution-local-v1` |
| Team | 3.7–3.x; device API v1, Team API v1, multi-workspace listing v1 |
| Synchronization | `integration-v1` and `execution-v1`; exact-context approvals/fencing unchanged |
| Storage | Separate Personal SQLite and Team SQLite/rqlite; never merged |

Bundling rejects unknown component majors; clients reject unknown protocol schemas. Old Team installations that
cannot serve a signed enrolled-device listing need administrator migration before
the unified shell can verify them; their existing console/CLI stays available.

## Building and signing

`make desktop-package` builds an ad hoc development DMG with both components, building Team's installer from this tree.
`make test-unified-installer` mounts it and checks versions, checksums, signatures, compatibility and that a development build
is refused as a release. These tests never install a service or use the user's production licenses.

A production build is the one release workflow (`.github/workflows/release.yml`, [DESKTOP_RELEASE.md](DESKTOP_RELEASE.md)). It
builds Team's installer itself from the same commit and version (`scripts/build-team-desktop.sh --nested`), signs and
notarizes it with the same Developer ID as the app, and checks it with `scripts/check-team-desktop.sh --distribution` and the
installer's own `--verify-release`, which tests that it was signed by the release's Apple Developer team. Individual's existing
Sparkle signatures and Apple release checks remain intact. There is no second release, no Team release key in CI, and no
downloaded Team payload to authenticate: the older offline manifest (`werkbord-team/desktop-release/v1`, `TEAM_RELEASE_PUBLIC_KEY`,
`TEAM_OFFLINE_MANIFEST`) existed only because Team was released separately and fetched, and is gone.

Team's command-line archives (for a Workspace Host without the Mac app) are still signed OFFLINE with a release key that is not
in CI, now against the one release tag: `make dist PRODUCT=werkbord-team` on the release workstation writes
`checksums-team.txt` and `checksums-team.txt.sig`, which are attached to the release ([TEAM_INSTALL.md](TEAM_INSTALL.md)).

## Updates and removal

Personal replacement uses its existing checksummed/signed trust paths, active-run
and runner-journal refusal, database snapshot, executable backup and restart
verification. Sparkle now also rechecks current safety at relaunch. Updates defer
while local agent work is active or any enrolled Team slot requires maintenance.

Team native replacement authenticates/stages first, then publishes a durable
`install-transaction.json` marker that fences device mutations. It refuses every
enrolled, pending or transitioning workspace, including additional slots and stopped
services. For an empty service, it saves the old service definition/access metadata,
swaps helpers atomically, probes the new service, and commits only after health.
Failures restore the old helpers and metadata; interruption recovery retains failed
generations and previous helpers. Joined Host updates require coordinated
administrator maintenance; no rolling-cluster updater is claimed by this phase.
Before it asks for an administrator's password, the installer asks the running
service (with the person's own credential) whether it holds a workspace, and says
so in plain words if it does; the root-run check over the data on disk still decides
and cannot be waived by that answer. The shell words the same request as an update
when the installed service is older, and shows the installer's reply unchanged.

Stopping/removing Team never removes Personal data. Service removal requires safely
leaving every slot, and retains local identities, signed licenses, archives and
owner metadata. Old app bundles and backups are not automatically cleaned up.
See [PHASE4_READINESS.md](PHASE4_READINESS.md).

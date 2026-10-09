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
| Legacy Personal | 1.0–1.x basic health/projects connection for adoption; upgrade before using newer unified APIs |
| Team | 3.7–3.x; device API v1, Team API v1, multi-workspace listing v1 |
| Synchronization | `integration-v1` and `execution-v1`; exact-context approvals/fencing unchanged |
| Storage | Separate Personal SQLite and Team SQLite/rqlite; never merged |

Bundling rejects unknown component majors; clients reject unknown protocol schemas. Old Team installations that
cannot serve a signed enrolled-device listing need administrator migration before
the unified shell can verify them; their existing console/CLI stays available.

## Building and signing

`make desktop-package` builds an ad hoc development DMG with both components.
`make test-unified-installer` mounts it and checks versions, checksums, signatures,
compatibility and refusal to authorize a development payload as an offline release.
These tests never install a service or use the user's production licenses.

A production build takes `TEAM_DESKTOP_APP` pointing to a reviewed, independently
signed/notarized Team bundle. It checks Team's offline manifest, Apple signature,
notarization ticket and Gatekeeper before copying the bundle without re-signing it.
The builder and fresh release checker also verify the bytes with the separately
provisioned `TEAM_RELEASE_PUBLIC_KEY`, using the repository verifier rather than
trusting a key or verification result supplied by the downloaded bundle.
Individual's existing Sparkle signatures and Apple release checks remain intact.
There is no private Team release key in the online Individual workflow.

The Team workflow produces **review candidates only**, retained as workflow
artifacts. Finalize on the authorized release workstation:

1. Review the candidate helper bytes and public `team-desktop.manifest.json`.
2. Sign that input with the **separate release key** on the offline workstation:
   `go run ./cmd/werkbord-team/vendor desktop-release --input manifest.json --out manifest.json.sig --tag werkbord-team-v3.8.0 < /secure/release-key.pem`.
3. Build with `TEAM_RELEASE_PUBLIC_KEY` (raw URL-base64 public key),
   `LICENSE_ISSUER_PUBLIC_KEY`, `TEAM_REVIEWED_HELPERS` (the exact candidate Helpers
   plus its `rqlited.build`), and `TEAM_OFFLINE_MANIFEST` (the signed manifest path).
   Run `make team-desktop-release` with the existing Developer ID/notary credentials.
   The builder verifies the exact copied bytes before distribution.
4. Verify the final artifact on a fresh Mac, keep its release draft until reviewed,
   and upload only with explicit release authorization. Follow RELEASE_SEQUENCE.md.
5. The Individual signing workflow fetches that compatible Team release through
   `prepare-unified-team.sh`. It refuses missing/invalid proof. Its feed is still
   published only after fresh-runner release verification.

The signed payload is `werkbord-team/desktop-release/v1`, NUL, the Team tag, NUL,
then the exact JSON manifest bytes. The manifest binds all five installable helper/
database-record paths. Apple signs the native verifier and outer resources. CLI
release signatures use their existing separate domain; neither they nor an
Individual update signature can authorize a desktop Team payload. Test keys are
generated at runtime in disposable tests, never shipped as customer trust anchors.

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
See [MIGRATION.md](MIGRATION.md) and [PHASE4_READINESS.md](PHASE4_READINESS.md).

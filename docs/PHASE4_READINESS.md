# Phase 4 release-readiness report

Assessment date: 2026-10-09. Target builds: Individual **1.9.0-preview.1** and
Team **3.8.0**. Phases 1–3 were checked before implementation with `make check`
and the real unified browser suite. This is a tested distribution implementation,
**not a production signing or installation acceptance claim**.

## Architecture and installation

One primary macOS DMG carries the free Personal controller and an inert, separately
versioned Team app. Personal starts through its established launcher; opening the
free installation does not install or start privileged Team infrastructure. Explicit
Team activation uses the native installer and normal macOS administrator dialog.
Workspace licenses, member seats, renewal and expiration stay authoritative on the
Team backend. More devices for one member do not consume more seats. Each enrolled
team retains its own identity, network and database.

Legacy migration adopts the existing services and paths in place. Private SQLite
snapshots include WAL data; hashed backups and durable markers permit verification,
retry and rollback without overwriting work created since adoption. External agent/
Git credentials, repositories, worktrees and root-owned Team storage are retained,
not relocated. Adoption snapshots are not Host disaster-recovery backups. Nothing
automatically deletes old applications or state.

Updates retain Individual's update trust and add current agent/runner safety checks.
Team has an independent offline release manifest/signature, checked against a public
key provisioned separately from the downloaded artifact. No private vendor key is
packaged or present in online release jobs. Empty Team service replacement has a
durable rollback journal and mutation fence. Any enrolled or transitioning Team
slot defers replacement, including a stopped Host. See [UNIFIED_DISTRIBUTION.md](UNIFIED_DISTRIBUTION.md)
for the compatibility matrix and offline finalization procedure.

## 1. Verified automated tests

Run on this macOS Apple Silicon workstation against the implementation. Tests use
disposable databases, services, signing keys and browser fixtures; they do not alter
the user's actual installed Team service or production licenses.

| Command / suite | Verified result |
| --- | --- |
| `make check` | Both products' Go tests/vet, architectural boundary checks, web checks/build, 235 web unit tests across 25 files, workflow and release validation fixtures pass |
| `make verify-isolation` | Individual builds, vets, tests (33 packages), migrates and runs with Team source removed; neutral desktop shell also passes |
| `make desktop-test team-desktop-test desktop-check team-desktop-check` | Native modules test/vet and desktop compilation pass |
| Desktop migration and shell `go test -race` | Race checks pass, including verification callbacks that consult migration status |
| `make test-install test-install-team` | Eight Individual installer cases and nine Team cases pass; legacy Devboard upgrade, data/token preservation, interruption, forged signatures, wrong tags, missing trust anchor and product separation |
| `make test-unified-desktop-browser test-team-desktop-browser` | Real browser/backend flows pass: creation/join, multiple teams, permissions, local agent execution, background survival, restart and isolated leave; adoption/retry/rollback and component diagnostics also tested |
| `make test-rqlite` | Pinned real database clusters pass infrastructure, replicated-store, server and service suites, including failover, partitions and checked backups |
| Real rqlite service suite after the new seat test | A full two-person license permits another device for the existing person and refuses a third person |
| `make test-nebula test-team-signed-release` | Real pinned network and independently signed Team CLI distribution tests pass; native licensed create/serve and sidecar rejection before execution |
| `make test-desktop-sign` | 50 development/signature checks pass; no additional real native window opened while the user's app was running |
| `make test-desktop-update` | 18 real Sparkle checks pass: accepted update, tamper, wrong keys/signatures, unsigned payload, downgrade/replay, disabled checks, active-agent deferral and resumed update |
| `ARCH=universal make desktop-package` and `scripts/test-unified-installer.sh` | Universal development DMG builds/mounts; checksum, Applications shortcut, actual component versions, nested code signatures and refusal of unsigned offline release authorization pass |
| Native offline proof and vendor verification tests | Runtime-generated ephemeral keys cover valid proof, different product/CLI domains, wrong tag/key, missing signature and tampered payload; vendor verification needs only the independently trusted public key |
| Native replacement/removal tests | Every workspace slot and transition blocks replacement/removal; interrupted swaps recover old helpers/metadata, keep generations and retry; removing an empty Team service preserves Personal data and retained Team licenses/identities |
| Backend cross-builds | Individual and Team compile for Darwin, Linux and Windows on amd64 and arm64; this does not claim native desktop installation on those platforms |

Existing backend license tests cover atomic member insertion/seat limits, expiration,
owner-authorized renewal after expiry, replicated offline restart, support expiration
semantics and containment without granting new authority. Migration fixtures cover
Individual-only, Team-only, both and Devboard, separate WAL SQLite snapshots,
credentials/config/identity/license retention, interrupted preparation/probes,
corruption rejection and work created after rollback. Root-owned Host state and
Keychain access are preserved by leaving them in place; they were not tested through
a privileged production installation.

Browser desktop and phone screenshots were visually inspected for the migration
controls. These are automated fixtures, not manual installer acceptance.

## 2. Verified manual installation tests

**None in this task.** No production installation, administrator lifecycle operation,
fresh-user migration or manual license ceremony was performed on the user's machine.
The mounted development DMG and browser/native updater fixtures above are automated
tests. Genuine installation acceptance still needs fresh Intel and Apple Silicon Macs:

| Manual acceptance | Required observation |
| --- | --- |
| Clean free installation with Team/vendor networks unavailable | Personal tasks/projects work; no license/login/admin dialog or Team service required |
| Licensed Team create/join and two-team membership | Offline activation, same-person additional-device seats, expiration/renewal and independent team identities |
| Legacy Individual, Team, both and Devboard | Actual credentials, runner ownership, tasks, schedules, repositories/worktrees, licenses and Host configuration survive adoption/restart |
| Interrupted migration/native empty-service replacement | Durable retry, retained generations and correct rollback across process/power interruption |
| Active agents and enrolled Workspace Host | Update defers without stopping work or changing cluster authority; empty-service maintenance resumes safely |
| Invalid offline/Apple/update signatures | Installation/replacement refuses each failed trust layer |
| Team removal after checked leave | Personal and retained Team data remain, and free functionality continues |

## 3. External signing and notarization checks

The local universal bundle is **ad hoc signed**. Genuine Developer ID signatures,
secure Apple timestamps, stapled notarization tickets, Gatekeeper quarantine/offline
acceptance and a production Sparkle signature have not been verified for these new
builds. Fixture tests of those mechanisms do not satisfy production acceptance.

Provision/review the existing Apple and update credentials through the protected
release mechanisms. Separately provision the public Team release trust anchor, review
the exact signed helper manifest on the authorized offline workstation, sign it with
the separate vendor release key and finalize/notarize Team before the unified bundle.
Then run the real fresh-runner release verifier and the manual matrix above. Keep
incomplete Team releases as drafts and follow [RELEASE_SEQUENCE.md](RELEASE_SEQUENCE.md).
No private signing credential was invented, no production license was altered, and
no tag was pushed or release published by this task.

## 4. Remaining blockers and limitations

1. **Production distribution is blocked** on genuine offline release authorization,
   Apple signing/notarization, production update-signature validation and the fresh-Mac
   manual acceptance matrix. Individual keeps its preview version until acceptance.
2. Enrolled Team services/Hosts conservatively refuse native replacement and desktop
   updates. Coordinated administrator maintenance is required; this phase does not
   provide a rolling cluster updater or automatically downgrade replicated storage.
3. Legacy Team versions without the compatible multi-workspace device listing can be
   detected and retained but cannot complete automatic shell verification. Upgrade
   them through the existing trusted administrator path with checked Host backups;
   historical Team installations need genuine manual acceptance before release.
4. Universal Intel code compiles and its signature is inspected, but it was not
   installed/run on physical Intel hardware here. The primary desktop installer is
   macOS 13+; existing platform-specific CLI distributions remain separate.

The implementation is committed with local annotated tags
`werkbord-v1.9.0-preview.1` and `werkbord-team-v3.8.0`, both pointing to the same
implementation/version commit and verified by `make verify-tag` for each product.

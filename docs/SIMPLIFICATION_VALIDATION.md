# Simplification validation

26 October 2026. Work continues from `5651946` on `refactor/ui-team-simplification`.

## Implementation

- `2932cdd`: normalise a fresh network authority to certificate timestamp precision, keep node validity within
  its authority and reserve overlay test ports until their nodes start. This fixes the enrollment failures seen
  in CI and a local overlay port collision. The deterministic precision regression passes; the affected
  enrollment tests passed 20 repetitions under `-race`, and the complete overlay suite passed five repetitions.
- `8b02876` (4A): one shared source for design tokens, segmented controls, section tabs and timeline layout.
  Personal and Team retain their own data, status vocabulary, routes and permission checks. The console still
  embeds without a Node build. `make check` and the real Team desktop browser suite passed.
- `8c296d4` (4B): one-host setup, invitation/device/backup next steps, accurate automatic-backup status and
  optional hosting under Advanced. The first scheduled backup is exercised without requesting a manual one;
  its checked result reaches the page without a reload. `make check`, Team desktop and unified desktop browser
  suites passed. Offline hosts, lost quorum, failed backups and unsafe host removal remain protected.

The unified browser suite also now checks process survival independently of navigation: a workspace heading
opens Workspace, so the test reopens the ticket before looking for its run controls. Failure diagnostics include
the visible frame state. No application navigation was changed to satisfy that assertion.

The planning gate exposed a remaining sixth People project tab. `c5a9fd9` moves project membership and invitations
into Project settings, preserving existing People deep links. Both frontends now show Overview, Board, Calendar,
Git and Runs. Chromium/WebKit planning interactions and the standalone Team navigation/deep-link suite pass.
`7e27daa` restores narrow-window touch targets after the shared-control extraction: rendered Team compact
segments are at least 44px, and Personal's phone component retains its existing 40px minimum.

## Historical release data

The published `werkbord-v4.24.6` stable and `werkbord-v4.83.5-preview.1` preview controller archives for macOS ARM64
were downloaded from this repository's releases and checked against their published SHA-256 manifests before
execution. Each ran in an isolated directory with private networking, the relay, update checks and Salie disabled.

Verified archive SHA-256 values:

- `werkbord_4.24.6_darwin_arm64.tar.gz`: `2a135ea330ba6bbac9f415124a0f7d81f25fcb5f1709aa4c3f81ae25bd22d221`
- `werkbord_4.83.5-preview.1_darwin_arm64.tar.gz`: `0b757f1696f3dff10849ef67c88faa0d8a6bfa355dbbe443650c94be9b574cee`

Both passed `scripts/test-release-data-import.py --manual-layout`: a project and task created by the released
executable retain their identifiers, repository path, description, Review state, version counters and owner token
when opened and edited by the current controller. Invalid credentials remain refused, and the original database
hash stays unchanged.

This is an **explicit data import**, consistent with Phase 2 removing legacy path discovery. It does not claim
automatic migration of the old default data directory, database filename or OS service. To import an older
installation, stop its controller, retain an untouched backup of its data directory, and copy the data into a
separate directory. Use SQLite's consistent backup facility to write its database as `werkbord.db`, retain its
`token` and configuration, review any configured absolute paths, then start the current controller with
`serve --data-dir <copied-directory>`. Do not copy a live database without its committed WAL data. Agent sign-ins,
phone pairing and native service registration need their own checks; they were not inferred from task retention.

Reproduce with checksum-verified executables for the current machine:

```sh
python3 scripts/test-release-data-import.py \
  --previous /path/to/released/werkbord --current /path/to/current/werkbord \
  --label <release-version> --manual-layout
```

## Size comparison

Counts use the original Phase 0 scope and commands, checked against `1de2973`. The final column includes this
report and the reusable release-data check.

| Measure | Phase 0 | Before 4A (`5651946`) | After 4A/4B |
| --- | ---: | ---: | ---: |
| Tracked files | 1,828 | 1,834 | 1,842 |
| Go lines excluding tests | 128,834 | 128,380 | 128,399 |
| Go test lines | 96,275 | 96,236 | 96,361 |
| `web/src` Svelte/TypeScript/JavaScript lines | 59,404 | 58,652 | 58,308 |
| Script files | 115 | 115 | 116 |
| Documentation files | 81 | 83 | 85 |

The shared library, declarations, regression tests and reports increase the file count. Frontend line reductions
include moving common code into `internal/uipresentation`, which is outside the original `web/src` count. The
maintenance improvement is that palettes, controls and timeline calculations each have one shared implementation;
these counts should not be read as a measurement of binary size or resource use.
Phase 0 recorded 263 local tags, and this checkout still has 263; validation creates no release tags.

## Beta gate

The automated beta gate passed on macOS ARM64. Tests used disposable services, repositories, keys, data
directories and local release feeds.

| Gate | Result |
| --- | --- |
| `make check` | Passed: Go/web tests, lint, desktop unit tests, workflows, release checks and Premium console |
| Team and unified desktop browser suites | Passed: actual device services, first scheduled verified backup, enrollment approval, runner grants and retained execution |
| Full Individual/Team browser suite | Passed: real controller/API/data flows, phone enrollment/scope/revocation, comments, handoff and Git workflow |
| Planning browser suite | Passed in Chromium and WebKit after correcting project navigation; dates, moves, resizing and contextual edits |
| Plan, onboarding, assistant, recommendations and Personal browser suites | Passed |
| Focused control and phone connector navigation suites | Passed, including rendered touch heights in both products |
| Both installers and signed Team payload tests | Passed, including data/token retention and rejection before execution |
| Desktop signing and updater tests | Passed: 56 signing checks and 18 updater checks, including hardened ARM64/Intel execution and deferred updates |
| `make test-rqlite` | Passed: actual clusters, failover, partitions, backups, restore, migration, server/CLI and service suites |
| `make test-nebula` | Passed with trusted source-build pins |
| `WERKBORD_SKIP_RQLITE=1 go test -race -timeout 30m ./...` | Passed; cluster tests run separately above |
| Final console and architecture checks | Passed after the final UI refinements |
| Both command executables, `CGO_ENABLED=0` | All 12 builds passed: darwin, linux and windows, each ARM64 and AMD64 |
| Historical release data imports | Both actual published release executables passed the explicit-layout check above |

The implementation and this report are committed locally on `refactor/ui-team-simplification`. No push,
version bump, release tag or published artifact is part of this validation. Hosted CI has not been triggered
for these local commits.

Automated browser runs do not establish real-iPhone relinking, customer network reachability, production Developer
ID notarization or runtime acceptance on Windows/Linux. Those remain distinct physical or release-environment checks.

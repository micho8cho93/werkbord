# Beta hardening implementation receipt

Engineering validation date: 2026-10-25. Production admission: **BLOCKED**.

The engineering checks below ran with local, uncommitted changes. The starting checkout was
`7068dbb95bf85a6a9fece1ad2b227d93ce1ea56b`, version `4.88.3-preview.1`.
The implementation version is `4.89.0-preview.1`: release authentication,
candidate review and optional Linux preview distribution add functionality and
therefore use a unified minor increment. This receipt does not grant publication
approval. The owner has since authorized the unified commit, local tag
verification and pushing a `codex/beta-hardening` branch plus candidate tag for
testing. Operational changes and remaining acceptance are recorded in
[PRODUCTION_ACCEPTANCE.md](PRODUCTION_ACCEPTANCE.md). Publication remains separately
gated on human review and the offline signature of exact candidate bytes.

## Implemented engineering changes

| Package | Implemented | Still required |
| --- | --- | --- |
| T1 | Resolved Go/npm inventories, source and per-binary/slice SBOMs, collected notices, advisory results, tool versions and source identity; current distribution/custody policy | Complete redistribution/license review, including unavailable notices; freeze a committed candidate |
| T2 | Go 1.27.3, x/net 0.60.0, Wails 2.17.0 and Vite 8.3.5; matching build-tool pins/lockfiles; patched Wrangler/Undici development tree | Fresh release-run evidence for the eventual committed candidate |
| T3 | rqlite 10.6.5 source commit and Windows provenance pins; fresh Unix builds with patched toolchains; patched Nebula 1.11.2 source builds; embedded executable hashes; backup-disclosure and SQLite attachment regression tests | Native Linux package/binary evidence for both architectures; owner review of distribution inventory |
| T4 | Required personal workspace and Salie session browser suites; restored stale browser expectations; fixed phone multiplexed Salie events with a completion/revocation regression | Authenticated provider smoke tests and independent relay/security review |
| T5 | Shared Ed25519 tag-bound checksum verification before controller extraction/execution; independent PEM requirement; signed exact-byte candidate/evidence archive; offline signing and protected review job; preserved Apple/Sparkle verification and legacy Team signing | Provision the independent release trust anchor; configure GitHub reviewer/tag protections; owner signing/review and previous installed-preview update acceptance |
| T6 | Reconciled hosted signing custody; read-only deployed RLS/function-grant/advisor inspection | Owner MFA, operator denial/recovery, independently retrieved encrypted key/register backups, empty-destination restoration and issuance continuity acceptance |
| T7 | Universal Mac package rebuilt locally, source-built helper signatures/pins checked, dependency notices included | Developer ID/notarization acceptance of the exact candidate, independent clean-Mac lifecycle and installed Sparkle update tests; invitations not sent |
| T8 | Preview Debian version ordering, native amd64/arm64 build jobs, authenticated public package checks, manual desktop update policy, Windows exclusion | Linux is disabled by default; Ubuntu 24.04/Debian 13 install/start/restart/upgrade/recovery/removal acceptance on both architectures before enabling it |
| T9 | Explicit pending acceptance ledger and exact-version stable gate | Independent hosts/networks, real TUN/relay tests, replacement-host recovery/fencing and independent review |
| T10 | Opt-in local content-free health/resource observer; concrete memory revision capacity guidance and journal-retention documentation | Fixed-workload resource baselines, actual 72-hour soak and seven-day invited observation; heap/goroutine/queue/WAL diagnostics and measured fixes |

## Observed local verification

- `make check` passed, including architecture boundaries, Go/web tests, native
  desktop module tests and release workflow/notarization fixtures. Web tests:
  98 files, 1,026 passing tests. The five candidate/version-order security tests
  pass separately as well.
- Native desktop compilation/checks passed. Controller and Team compile for
  darwin, Linux and Windows on amd64 and arm64; this does not establish Windows
  packaging or OS trust.
- Real pinned Nebula and rqlite tests passed, including replicated-store/server
  cluster tests, CLI cluster tests, backup/query security regressions and local
  migration/recovery fixtures. These use this development machine and do not
  establish independent failure domains.
- Targeted race tests passed for updater/signature verification, application
  operations, terminals, connections, relay envelopes and Team request proofs.
- Controller installer fixtures: eight checks passed. Legacy-compatible Team
  signed installer fixtures: ten checks passed. No production installation was
  changed.
- Personal workspace renderer, Salie creation/edit/deletion confirmation,
  navigation, phone, lifecycle recovery, icons, productivity and desktop-shell
  browser flows passed on disposable databases. The real Salie session browser
  suite passed. Fixtures use scripted providers; no live provider acceptance is
  inferred.
- The final universal Mac `.app`, DMG and Sparkle ZIP were built **ad hoc** for
  local inspection. Both architectures of all five Go executables were scanned.
  `output/hardening-final-audit/audit.json` reports 25 successful source,
  native-desktop, binary-slice/build-info and npm audit checks, with no failing
  check. Source identity is explicitly marked dirty; this is not a frozen release.
- The local observer's one-sample fixture completed with healthy HTTP status and
  production acceptance still pending. This is not soak evidence.
- Workflow policy checks and `git diff --check` passed. Cloudflare bundling was
  tested using deploy **dry-run** only; no deployment occurred.

The phone fix admits only the existing sessions subscription body to Salie's
multiplexed event route. It adds no mutation capability. The regression verifies
completion delivery, stream closure on revocation and denial of reconnection.

## Release handoff

Follow [BETA_READINESS.md](BETA_READINESS.md) and
[RELEASE_ACCEPTANCE.json](RELEASE_ACCEPTANCE.json). No acceptance status was
changed to accepted. Complete the owner-authorized unified commit and local
`make tag` / `make verify-tag` process, then obtain fresh CI/candidate evidence
from that commit. Display sequence `0.22.0-preview.1` is reserved for candidate
validation; it does not grant publication approval. Configure and independently distribute the release key
before shipping builds that depend on it; an unset key deliberately refuses new
controller downloads/updates.

A valid license from an issuer trusted by the installed build enables its Team
workspace, subject to dates, seats, membership and project permissions. License
import is a runtime authorization boundary. Production admission remains a
separate operational/release decision. Initial Team evaluation stays disposable.

All hardware, custody, recovery, hosting and soak requirements above remain open;
they cannot be replaced by simulated tests or this development Mac.

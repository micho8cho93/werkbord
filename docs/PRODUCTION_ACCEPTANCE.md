# Production acceptance progress — 2026-10-25

Production admission remains **BLOCKED**. This receipt records actual operational
checks following the local hardening implementation. It does not approve a new
release. Candidate `4.89.0-preview.1` was committed, tagged, verified and pushed
for testing at `7474b76afa77c86f2d309fc6eea2f0055cf8dc13`. Its release run
`40211454930` passed repository checks and native network/database cluster tests,
then stopped on stale Salie browser assertions before signing. The tag is
unchanged. A patch candidate will carry the browser corrections; the published
Mac baseline tested below is `4.83.5-preview.1`.

The corrected patch candidate is `4.89.1-preview.1`. Its browser checks read the
active theme, active conversation's cache and current pending-request popup,
while retaining exact approval/duplicate-execution and shell-message checks.
The standalone screen and shell suites both passed locally. The combined
`make test-assistant-browser` target also passed after the version increment;
workflow policy checks and all five release-hardening tests passed.

## Release controls configured and read back

- Created the GitHub `release-approval` environment with the existing owner
  reviewer, `micho8cho93`. Required review is enabled; administrator bypass is
  disabled. Deployments are restricted to **tags** matching `werkbord-v*`.
- Disabled administrator bypass on the existing `desktop-release` environment,
  preserving its reviewer and existing branch/tag policies. Apple and Sparkle
  secrets remain in that environment; they were not extracted.
- Created active ruleset **Werkbord immutable release tags** (ID `25825890`).
  Updates/deletions of `refs/tags/werkbord-v*` are blocked, with no bypass actors.
  The separately named preview-feed alias does not match that pattern.
- Configured the repository's public `TEAM_LICENSE_ISSUER_PUBLIC_KEY` variable
  to the issuer independently matched against the deployed register and local
  primary key fingerprint. No private licensing key was copied to GitHub.
- The owner created the separate release-manifest key using the tested offline
  command and private Terminal passphrases. Its encrypted PKCS8 primary is
  owner-only; the Ed25519 signing challenge passed. The public PEM/metadata match,
  and `RELEASE_PUBLIC_KEY` was configured and read back from GitHub. Fingerprint:
  `94a8a0b18379967e1ae69f0f1bf5453918b9bb01cd94c5074a431b6ff64659d9`.
  Its disposable command-test key was never configured as a product trust anchor.
  Independent recovery custody, human review and an actual offline manifest
  signature remain prerequisites to production admission/publication.

Evidence directory: `output/production-acceptance-20261025/`. Environment and
ruleset JSON files contain configuration read back from GitHub after mutation.
No workflow job was approved, no source/tag was pushed, and no release was
published as part of these configuration changes.

## Published Mac baseline

The actual public bytes for `werkbord-v4.83.5-preview.1` were downloaded and checked
on this development Mac. DMG SHA-256:
`4ce1a49d558d4e6ab7231bb78a0398fbe933853f1d1a8deb0a0d67510b0395eb`.

The DMG and temporarily copied app passed signature verification, stapled
notarization-ticket validation and Gatekeeper assessment after quarantine was
applied. The app reports Developer ID team `4G64UBU652`; the main executable,
controller and Team service all contain arm64 and x86_64 slices. App build
metadata is `4.83.5-preview.1`, and its Sparkle public key matches the source
checkout's committed key. No app/helper executable was launched or installed.
The temporary mount was detached.

`scripts/verify-appcast.sh werkbord-v4.83.5-preview.1`, using the explicit live
preview feed, also passed: archive size **110,657,300 bytes**, signed appcast,
archive signature and exact public preview-feed comparison. This establishes a
previous published update baseline, not acceptance of the proposed new binary.

Retained receipts: `mac-published-baseline.json`, `mac-baseline-apple.txt` and
`mac-baseline-sparkle.txt` in the evidence directory.

**Pending:** freeze the corrected source, build/sign/notarize its exact artifacts,
verify them, and accept installation and baseline-to-candidate Sparkle updating
on clean hardware. Only this development Mac is currently available, as confirmed
by the owner. Local VMs cannot satisfy independent hardware acceptance.

## Live hosted licensing

The owner signed into the existing hosted console and completed MFA privately.
Using that actual session, a disposable, short-lived three-seat license was
prepared, signed by the hosted function, recorded and downloaded through Chrome.
The installed product's actual Go license verifier accepted the downloaded bytes
offline and rejected altered seat claims and expiry. No credential or MFA code
was copied from the browser.

An exact retry of request reference
`acceptance-20261025-4.89.0-preview.1-issue` returned the same license ID and hash.
The deployed register contains one issue operation for that reference. A real
seat expansion issued revision two with **four seats**, under the same
entitlement, retaining runtime expiry **2026-10-26T23:59:59Z**. Cancelling a further
unsubmitted expansion draft left the register unchanged. Cancellation of a
server-reserved operation was **not** tested live.

The disposable entitlement `8fa5cc02-cfda-4df4-9bef-310ee6ad8e88` was then closed
through the normal operator UI. Both original signed revisions and all history
remain archived. Both downloaded files still pass offline verification under
their original terms after closure, as designed. No customer delivery or payment
was recorded. The pre-existing active entitlement was unchanged.

| Artifact | SHA-256 |
| --- | --- |
| Three-seat revision 1 (`lic_7f38b250e41c483c89b5b1237dfff818`) | `c740413bb1acc2ad60d3c864936f8656f96b4649812e9db10bef06459f26b046` |
| Four-seat revision 2 (`lic_29341f0c054940dc8e017603e43f9a42`) | `93b87e50383b2679fd2ecc0ecf9a1466f0820c93ad1b5cd347959de7f9e7a435` |

Separate deployed-database probes accepted an eligible fixture operator and
denied `aal1`, a disabled fixture operator and a deleted fixture session. All
fixture writes rolled back; these are database guard tests rather than genuine
Auth sign-in tests. No real operator was disabled or deleted. Actual hosted HTTP
requests without authentication or with an invalid bearer were rejected. The
function uses HTTP 400 with explicit authentication errors for those failures.

The local licensing primary's derived public fingerprint matches the deployed
issuer:
`b66484e825f6148e826f46e48f7134d7d00bb2ea139a700182dca1ef5ddd502b`.
Its file mode was `0600`; an encrypted primary copy did not exist at initial
inspection. The owner subsequently ran `protect-key` privately and confirmed
completion; the resulting encrypted primary exists with owner-only permissions.
Independent retrieval/key-check remains pending. A real encrypted register backup was
downloaded (`werkbord-register-2026-10-25T16-06-42-656Z.json.age`, 16,388 bytes),
with SHA-256 `fcf73e158ed80b24af21be325a3ea29770ae0ac5f2d49a29211fd749c5711f7e`.
Its standard age header was verified; private decryption/content verification
remains pending. A download on this same Mac is staging, not evidence of
independent retrieval. Computer-use access to Terminal was rejected by the tool
as a safety restriction; the owner must run the private-passphrase commands.

Retained receipts include `live-licensing-lifecycle.json`,
`live-revisions-offline-test.txt`, `licensing-endpoint-denial.json`,
`licensing-primary-metadata.json` and `licensing-closed.png`.

The encrypted licensing/release keys, encrypted register, public trust metadata
and frozen recovery source were gathered into the private archive
`Werkbord Recovery Kit 2026-10-25.tar.gz` on the owner's Desktop. Its SHA-256 is
`c90a16986769169278cc02344727fec02cef0e16efdb5fc4d9a6ed60657bc01a`.
The owner intends to email/upload this archive personally and retrieve a fresh
copy from Google Drive. No independent upload/retrieval has been verified.
Passphrases and plaintext private keys are excluded from this archive.

The existing licensing organization's recovery-project quote was $0/month. The
owner authorized that isolated project, but Supabase rejected creation because
the account already has its two active free projects. No project was created,
no charge was incurred, and no existing project was paused or changed. Local
Docker/Postgres services are unavailable, so an empty recovery destination
remains required before the live restore drill.

**Pending:** independent copies, key/register checks on
retrieved independent copies, actual restoration into an empty recovery
destination, hosting/account recovery continuity, and live prepared-operation
cancellation/disablement acceptance where not already evidenced. Never restore
over the active register to manufacture a recovery pass.

## Independent Team and real-user phase

Only this development Mac is available. Independent host failover, partition and
quorum-loss tests, actual TUN isolation/forced-relay probes and replacement-host
restore/fencing remain unrun. They require the independently failing Workspace
Hosts and usable Connectivity Hosts described in `TEAM_SECURITY_GATE.md`, plus
a disposable replacement host and independently recovered authority material.
The existing local cluster tests remain local engineering evidence only.

Independent security review, the fixed-workload 72-hour soak and seven-day
invited-user observation are assigned to the future real-user phase at the
owner's request. They remain production prerequisites. None of the pending
requirements in `RELEASE_ACCEPTANCE.json` was marked accepted by this work.

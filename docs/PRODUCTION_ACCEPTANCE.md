# Production acceptance: one product, Free and Premium

Production admission remains **BLOCKED**. Personal and Team are project kinds; Free and Premium are plans in one build
and one release. A Premium key is plan proof, never a release signature or production approval.

The [acceptance ledger](RELEASE_ACCEPTANCE.json) is not bound to an accepted build and every independent requirement
remains pending. Before stable publication, freeze the candidate version and commit, retain its signed manifests and
exact artifact hashes, and attach evidence for those bytes. Local tests, development DMGs and earlier previews do not
establish acceptance of the candidate.

## Required evidence

| Requirement | Acceptance |
| --- | --- |
| Key custody | Independently recover release, Apple, Sparkle, Premium issuer and customer workspace-authority material. Verify actual public trust anchors, encrypted backups, retrieval and signing/recovery procedures. These are separate responsibilities. See [Premium operations](PREMIUM_OPERATIONS.md) and [desktop releases](DESKTOP_RELEASE.md). |
| Dependencies | Review source and every shipping OS/architecture artifact, resolve findings or document reviewed exceptions, and retain scans and pinned-build provenance. |
| Clean Mac lifecycle | Verify signatures, notarization, install, optional Team setup, update, interrupted-update recovery, restart and uninstall on clean hardware. Test root-service boot before login, locked/unlocked Keychain, app identity changes and authority recovery without adjacent-key fallback. |
| Clean Linux lifecycle | Before publishing Linux packages, accept install/start/restart/upgrade/removal on the target distributions and architectures. Physical Linux acceptance is additionally required for production support claims. |
| Independent Team network | Use independently failing Workspace Hosts and Connectivity Hosts to test leader/follower loss, partitions, quorum loss/restoration, stale-host return, actual TUN isolation and forced-relay connectivity. Verify acknowledged writes and hashes from survivors. |
| Replacement-host recovery | Restore a customer-encrypted backup using independently recovered authority material. Fence old hosts, compare fingerprints, expire replay windows, reapply revocations and establish the data-loss boundary. See [host replacement](TEAM_HOST_REPLACEMENT.md). |
| Soak | Complete the fixed-workload 72-hour soak and seven-day invited-user observation. |
| Independent security review | Obtain an independent review of the exact candidate and its deployment/recovery assumptions. |
| Unified platform | Exercise the current project selector, Work, Calendar, Agents, Analytics, Salie, create/join, ticket handoff and Free/Premium transitions. See the [functional matrix](unified-platform/ACCEPTANCE.md). |

The earlier hosted per-workspace licensing system is retired. Its receipts do not validate Premium issuance or current
key recovery. Current vendor operations are documented in [PREMIUM_OPERATIONS.md](PREMIUM_OPERATIONS.md).

## Admission procedure

Keep each ledger requirement pending until its evidence exists. Bind accepted evidence to the exact candidate version;
Linux publication additionally requires its lifecycle requirement. Obtain the owner's release approval through the
protected release process. A commit, local test pass or preview signature does not clear production admission.

See [beta distribution](BETA_READINESS.md) for evaluation scope and
[the Team security gate](TEAM_SECURITY_GATE.md) for operational security acceptance.

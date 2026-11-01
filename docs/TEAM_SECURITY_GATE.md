# Team production security and reliability gate

Production admission: **BLOCKED**. The [acceptance ledger](RELEASE_ACCEPTANCE.json) retains pending requirements;
[production acceptance](PRODUCTION_ACCEPTANCE.md) describes the evidence needed for the exact release candidate.
Mac previews are for evaluation with disposable Team workspaces and customer-controlled backups. Linux distribution
requires its own acceptance; Windows downloads remain deferred. See [beta distribution](BETA_READINESS.md).

## Architecture and trust

The current architecture is documented in [STRUCTURE.md](STRUCTURE.md), the threat model in
[TEAM_SECURITY.md](TEAM_SECURITY.md), and deployment in [TEAM_INSTALL.md](TEAM_INSTALL.md).

- Team coordinates members, projects and tickets. Execution and Git/provider credentials remain on each person's runner.
- Customer-owned Nebula provides private connectivity; pinned TLS enrolment and device-signed API proofs establish
  application identity. Network membership alone grants no API permission.
- Workspace Hosts replicate coordination records through rqlite. Quorum loss refuses writes and remote authorization.
  Raft covers crash failures, not malicious workspace authorities or a corrupted authoritative majority.
- Locally pinned sender keys, replay checks and local approvals guard the semantic runner bridge. A signature does not
  replace permission or approval checks. Revocation prevents new authorization without silently stopping accepted work.
- Free/Premium enforcement is per person and verified offline. See [PLANS.md](PLANS.md); retired workspace licences
  and their issuer receipts are not current runtime requirements.

## Required security acceptance

1. Verify independently provisioned release trust anchors and signing custody, including Apple and Sparkle. Accept
   Premium issuer recovery and customer authority recovery separately. Publish no fixture issuer or development signature.
2. Build the reviewed candidate, require Developer ID/notarization for Mac, and retain exact artifact hashes and build
   provenance. Review protection of Apple CI signing credentials against release-infrastructure compromise.
3. Scan source and every shipping OS/architecture artifact. Review the pinned Nebula and rqlite dependencies and their
   advisories; resolve findings or retain an explicit reviewed exception with supporting tests. Earlier scan receipts
   cannot establish the status of newly built bytes.
4. Complete clean-machine install/create/join, upgrade and interrupted-upgrade recovery. On Mac, test service boot before
   login, locked/unlocked Keychain, identity changes after updating, restart, uninstall safety and authority recovery.
   No adjacent-key fallback is acceptable. Linux needs its separate lifecycle acceptance before publication.
5. Use three independently failing Workspace Hosts and at least two usable Connectivity Hosts. Kill a leader/follower,
   partition 1/2, lose quorum, restore it and return a stale host. Check successful acknowledged writes and hashes from
   every survivor. Exercise forced relay-only NAT connectivity, relay/lighthouse loss and alternative-path recovery;
   test authorization and lateral probes on actual TUN interfaces.
6. Restore an externally encrypted customer backup onto a replacement host with separate authority unlock material.
   Demonstrate stopped/fenced old hosts, correct fingerprints, expired replay windows, reapplied revocations and the
   expected data-loss boundary. A local scratch restore does not satisfy independent recovery acceptance.
7. Complete independent security review, soak and invited-user observation from
   [PRODUCTION_ACCEPTANCE.md](PRODUCTION_ACCEPTANCE.md). Bind evidence to the candidate in the ledger.

## Limits and recovery

There is no automatic quorum shrink or merge of independently writable histories. A compromised Workspace Host is a
workspace-authority incident; in-place compromised-root/CA/database-credential rotation is not supported. Recovery needs
separately protected authority material. Keychain access does not isolate root or the same authorised service UID.

Operator procedures: [backup and recovery](TEAM_BACKUP_RECOVERY.md), [host replacement](TEAM_HOST_REPLACEMENT.md),
[disaster recovery](TEAM_DISASTER_RECOVERY.md) and [execution coordination](EXECUTION_COORDINATION.md).
No local check, commit or tag grants permission to publish or deploy.

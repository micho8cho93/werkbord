# Platform acceptance

One build, one `cmd/werkbord/VERSION`, one `werkbord-v*` release. Personal and Team are project kinds; Free and Premium
are plans. This checklist describes current source contracts and outstanding acceptance, not production approval.
The exact candidate needs its own evidence and a matching version in [RELEASE_ACCEPTANCE.json](../RELEASE_ACCEPTANCE.json).
**Production admission remains blocked.**

## Functional matrix

| Area | Current behaviour | Verification / acceptance action |
| --- | --- | --- |
| One product | The app, controller and Team service share one version; there is no edition build or separate Team release | `make check`, `make tag`, `make verify-tag`; architecture and release workflow tests |
| Plans | Personal and teams of two are Free. Teams of 3 or more need Premium for every member; devices do not add people | Entitlement and Team plan/enrolment tests; `make test-plan-browser`; repeat admission races on the exact candidate |
| Read-only | Members without proof in a team of 3+ can read; coordination writes return `premium_required`. Safety operations remain available; returning to two restores access | Exercise add/remove, unlock, reconnect and restart on the candidate |
| Navigation | The desktop project dropdown scopes Work, Calendar, Agents, Analytics and Salie; All Projects includes accessible workspaces only | `make test-project-context-browser`; verify offline/revoked contexts and narrow layouts. See [navigation](../UNIFIED_NAVIGATION.md) |
| Work | Personal and Team use Now, Next and Later queues, completed-work disclosures and structured plans | `make test-work-browser`, `make test-work-order-browser`; verify dependencies, ordering, claims, permissions and plan editing |
| Create/join | Explicit Team setup, administrator-approved enrolment and runner connection. Personal works without Team | Clean-Mac install, cancellation, pending approval and restart remain independent acceptance |
| Ticket handoff | Held tickets become local tasks only after runner connection; each run retains local approval | Connector/integration tests plus separate member devices |
| Project-transfer APIs | Native transfer contracts remain; the former Move project dialog is removed | Retained integration/service/desktop tests. Any future caller must retain exact review and native confirmation; see [contract](PROJECT_MOVES.md) |
| Salie | Permanent destination, scoped context and confirmed mutations. The approved Team `create_ticket` bridge remains available | Assistant/desktop tests. The previously proposed Team read-only restriction is not implemented; do not claim Team reads are the only supported operation |
| Sessions | Agents opens the existing run/session identity; closing a view does not stop execution | `make test-assistant-browser`; verify follow-ups, approvals, recovery and navigation continuity |

Local automated tests use disposable data and keys. They do not upgrade installed hosts, exercise payment, establish
provider accuracy or prove independent-machine acceptance. Premium is not on sale until its purchase flow is configured.

## Exact-candidate production gate

1. Freeze the source commit and candidate version; retain signed manifests and artifact hashes.
2. Verify the app and both executables report that version; both plans use the same bytes.
3. On clean hardware, accept Personal without Team activation, explicit Team setup, update, restart, uninstall and recovery.
4. With independent members/hosts, accept the two-person limit, signed proof at admission, per-member read-only state,
   containment operations and restoration of access when the team returns to two.
5. Exercise the functional matrix, including cancellation, interrupted responses and retry recovery. Retained native
   project-transfer tests must prove no partial boards, overwritten edits, lost history or unintended runs. Credentials,
   files, execution settings and agent output must not cross that boundary.
6. Resolve and accept the proposed Salie Team read-only policy before claiming that restriction. Current confirmed
   Team writes remain subject to the existing permissions and approval checks.
7. Complete independent networking/failover, authority recovery, dependency/security review and observation periods from
   [PRODUCTION_ACCEPTANCE.md](../PRODUCTION_ACCEPTANCE.md) and [TEAM_SECURITY_GATE.md](../TEAM_SECURITY_GATE.md).
8. Bind the ledger to that exact version, mark only evidenced requirements accepted, and obtain the owner's release approval.

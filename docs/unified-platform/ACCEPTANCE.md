# Unified platform acceptance

Source target: **5.4.1-preview.1**. One build, one `cmd/werkbord/VERSION`, one `werkbord-v*` release.
Personal and Team are project kinds; Free and Premium are plans. This document is a checklist and a record of scope, not
production approval. The exact stable candidate must have its own evidence and a matching version in
[RELEASE_ACCEPTANCE.json](../RELEASE_ACCEPTANCE.json). **Production admission remains blocked.**

## Implementation and engineering checks

| Area | Implemented behaviour | Engineering evidence / acceptance action |
| --- | --- | --- |
| One product | The app, controller and Team service have one version; no edition build or separate Team release | `make check`, `make tag`, `make verify-tag`; architecture and release workflow tests |
| Plans | Personal and teams of two are Free. Extra devices do not add people. Teams of 3 or more need Premium for every member | `internal/entitlement`, Team service plan/enrolment tests; `make test-plan-browser` for the real console with throwaway keys |
| Admission | The host verifies signed per-person proof; concurrent membership changes cannot bypass the cap | Team service and enrolment tests; repeat invitation/approval races on the exact candidate |
| Read-only | A member without proof in a team of 3+ can read; coordination writes return HTTP 402 `premium_required`. Safety/infrastructure operations remain available. Returning to two restores access | Team plan API and service tests; exercise adding/removing people, unlocking, reconnecting and restart on the exact candidate |
| Navigation | One sidebar groups Personal and each team. Home, Calendar, Reviews and Analytics span workspaces; ⌘K goes to a place | Shell navigation/aggregation tests; desktop acceptance must inspect each scope and inaccessible-team behaviour |
| Create/join | One dialog; administrator-approved enrolment and an explicit runner-connect choice. Personal does not require Team | Shell setup tests and browser fixtures; clean-Mac installation, cancellation, pending approval and restart remain independent acceptance |
| Team ticket handoff | Held tickets become local tasks only after a runner connection; each run retains local approval | Existing connector/integration tests; verify with separate member devices |
| Project move | Exact text review and native confirmation; atomic retry-safe destination import, conditional source archive | Project-transfer tests in integration, both services and the desktop shell; desktop/narrow-screen review inspection in stage 5a. Repeat both directions and lost-response recovery on the exact candidate |
| Salie entry points | The permanent Salie destination opens the assistant | Shell/frame tests; verify the sidebar destination, waiting count and keyboard access |
| Salie Team policy | **Pending stage 5:** Personal write / Team read-only is not yet enforced; the existing approved `create_ticket` bridge remains | Do not accept this row until the bridge is restricted and tests prove Team reads still work while writes are refused |
| Home polish | **Pending stage 5:** aggregation exists, but the remaining planned polish is not signed off | Record the actual remaining review and its evidence before accepting the complete platform |
| Website and terminology | Personal / Team, Home, Free / Premium; no runtime Team licence, separate product or purchase CTA before Premium is on sale | Website copy, demo and SEO checks in the `werkbord-site` checkout; local review does not publish the site |

Local automated tests use disposable data and keys. They do not operate or upgrade the owner's installed Team host, touch the
retired hosted licensing register, exercise payment, or establish independent-machine acceptance. A scripted website demo is
illustrative evidence, not a runtime or native-authorisation test.

Website companion source for this stage: local `werkbord-site` commit `5e9dc37`. Its 28 SEO/copy checks passed, and the plan
disclosure, Settings → Plan, Personal/Team navigation and guided-demo chapters were inspected in a browser at desktop and
phone widths. The waitlist transport was stubbed during inspection. This is local engineering evidence; the site was not pushed
or published and no Premium purchase or runtime unlock was tested by the simulator.

## Exact-candidate production gate

Keep every requirement pending until its evidence exists. The release candidate tool requires `unifiedPlatform` alongside key
custody, dependency review, clean-Mac lifecycle, independent Team networking, replacement-host recovery, soak and independent
security review. Publishing Linux additionally requires clean-Linux lifecycle acceptance. A completed docs stage cannot clear
these gates, and a preview signature cannot substitute for operational evidence.

1. Freeze the source commit and candidate version; retain the signed manifest and exact artifact hashes.
2. Verify the one app and both executables report that version; both plans use the same bytes.
3. On clean hardware, accept Personal without Team activation, explicit Team setup, update, restart, uninstall and recovery.
4. With independent members/hosts, accept the two-person limit, signed proof at admission, per-member read-only state,
   containment operations and restoration of access when the team returns to two.
5. Exercise navigation, create/join, ticket handoff and project moves in both directions. Cancel a move; edit after review;
   interrupt import and archive responses; retry; verify no partial boards, overwritten edits, lost history or started runs.
   Confirm credentials, files, execution settings and agent output do not cross the project-transfer boundary.
6. Complete the pending Salie Team read-only policy and Home review, then attach their acceptance evidence.
7. Complete independent networking/failover, authority recovery, dependency/security review and observation periods from
   [PRODUCTION_ACCEPTANCE.md](../PRODUCTION_ACCEPTANCE.md) and [TEAM_SECURITY_GATE.md](../TEAM_SECURITY_GATE.md).
8. Bind the ledger to that exact version, mark only evidenced requirements accepted, and obtain the owner's release approval.

No push, publication, release promotion, production approval or live-service change is part of stage 6.

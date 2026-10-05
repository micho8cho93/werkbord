# V0–V2 stabilization audit

Audited from `a9e2757` on 2026-10-05. Scope: existing workflows and recovery,
without new product capabilities or architectural replacement.

## Architecture and boundaries

| Component | Responsibility and integration contract |
| --- | --- |
| Individual controller / CLI / PWA | `cmd/devboard`, `internal/controller`, API → services → SQLite; owns projects, tasks, runs, schedules and durable events. The Svelte browser reloads authoritative state after reconnect. CLI service commands use the same authenticated API. |
| Local execution | `internal/runner` claims tasks transactionally, prepares a recorded Git worktree, launches Codex/Claude adapters, persists questions, usage and terminal results. Scheduler rechecks gates at dispatch and requires explicit rearm after preparation failure. |
| Remote execution | `internal/remote`, `runnerwire`, controller runner service: signed requests, durable acceptance before launch, renewable lease, idempotent observations and commands. Runner machines retain their own repositories and credentials. |
| Git / GitHub | `gitrepo` inspects and changes local repositories; `github` uses the developer's CLI credentials. Git services serialize writes with execution, validate inspected heads and preserve uncertain/uncommitted work. Completion never automatically merges. |
| Team | `cmd/werkbord-team`, `internal/team`: separate database, configuration, token authentication, invites, atomic claims, optimistic ticket edits, sync revision, review and reported Git facts. Never executes agents or Git and never contacts member machines. |
| Developer handoff client | Team CLI runs on the member's machine, imports generic source/branch/base context into loopback Individual, and reports metadata through the member's credentials. Re-import is idempotent and retains local edits. |
| Shared plumbing | Only `sqlitekit`, `httpkit`, `logging`: WAL/FULL durability, single writer/read snapshots, migrations/backups, JSON decoding/errors/security headers and logging. Architecture tests enforce both dependency directions. |
| Install / release / recovery | Separate installers, VERSION files, product tags and archives. Individual updater checks checksums/version, snapshots SQLite and retains the old executable. Team uses its own release feed and is never marked Individual's latest. |

## State transitions and audit findings

- Task execution: scheduled/ready → starting → running ↔ waiting/blocked →
  completed/stopped/failed. Local crash recovery records interruption; worktrees
  remain available for retry. Terminal run state and board Done are separate.
- Remote journal: accepted → acknowledged/ready → preparing → launching → active
  → ended. Launch/crash ambiguity, missing journals and restored controller state
  become uncertain; owner inspection/resolution or revocation is required. Lease
  loss stops execution but does not silently release controller ownership.
- Team: Backlog/Available → atomic claim/In Progress → Review → Done. Stale edits
  return conflict, completion requires permitted review and merge evidence when a
  PR exists. A delayed report cannot reopen the same merged PR.
- **A — update health verification:** the previous release retained `.prev` until
  runner restart returned, but restart acceptance did not prove runner sync or
  the running executable's version. A runner dying before status inspection could
  be mistaken for one intentionally stopped. Repeated updates could overwrite an
  unresolved rollback executable. Installer upgrades replaced the executable
  directly and bypassed those recovery checks. Fix and regressions are required.
- **Verified already fixed:** post-adapter-launch lease/disabled/project guards;
  Team history clearing absent tab/project/ticket; snapshotting backup metadata
  before sorting and rejecting `--latest` plus a path; retaining `.prev` on runner
  restart errors. Existing regressions will be rerun, with uncertainty coverage
  extended where needed.
- **B — repeatable integration validation:** existing desktop/mobile browser and
  real-Git handoff scripts are manual. Automate disposable setup and cleanup and
  include the checks in CI; extend the bridge through execution and review.
- **A — published Team installer discovery:** live release validation found that
  scanning the entire Atom feed for a tag can select historical changelog links,
  including comparison suffixes, instead of a release. Select stable Team release
  links only, and cover misleading descriptions and prereleases.

Authentication/invites, request bounds, repository paths, process ownership,
runner authorization, question delivery, usage merging, token rotation, event
retention and diagnostics were inspected alongside their regression suites.
Controller credentials are filtered from agent environments; Team transports
metadata only. No demonstrated security blocker was found in those paths.

## Implemented fixes and release notes

**Individual 0.9.2 (PATCH)**

- Update remembers required runner state before mutation, stops it before the
  controller/database snapshot, and waits for a fresh signed sync with matching
  version/identity/controller and advancing sequence. Controller version,
  authenticated project access and SQLite integrity are verified before `.prev`
  removal. Failure retains rollback evidence; controller failure restores the
  binary and snapshot after stopping the failed service. A later update refuses
  to overwrite an unresolved `.prev`.
- Shell/PowerShell installer upgrades invoke the downloaded release's recovery
  implementation against the installed executable, rather than replacing it
  before verification. Active-work refusal and intentional startup failure are
  covered through this shared path. Clean installation keeps its setup flow.
- Runner status now exposes the last successful sync without credentials. Tests
  reject missing/stale/wrong-version/wrong-identity/future health evidence and
  cover runner startup failure/crash and populated-installation rollback.
- Storage-denied sign-in retains a page-only credential. Malformed pairing
  fragments cannot crash startup or erase a working credential. Unit and browser
  regressions exercise both paths.
- Post-launch journal quarantine is now covered alongside lease expiry, runner
  disablement, project removal and valid renewal. The stopped session's process
  evidence stays durable and uncertainty is not silently released.

**Team 2.1.3 (PATCH; includes 2.1.2 validation)**

- Installer discovery selects stable Team release links, ignoring historical tags
  in release descriptions, comparison URLs and prerelease entries. An Atom feed
  without an actual Team release fails explicitly. Installer URLs use the current
  repository name.
- Release validation now carries review-state restart/reopen coverage: credentials,
  project roles, branch/commit/merged-PR evidence and old sync cursors survive;
  the reviewer can finish the restored ticket and clients reconcile afterward.
- The integrated release check crosses both actual HTTP APIs and the
  developer-owned handoff CLI, then executes a subprocess in a real worktree,
  answers a question, commits, reports metadata, reviews through another member,
  merges locally and reaches Done. A stale report cannot reopen the PR. A failed
  process can be retried in its preserved workspace.

`make test-browser` builds disposable servers, creates its own repository and
private fixture credentials, runs first-run/desktop/mobile/concurrent-edit/
reconnect checks and the execution/review bridge, and shuts everything down.
Playwright is pinned in the web development dependencies. CI runs this suite and
race tests on Linux, with screenshots saved on failure. No personal installation
or external agent account is used.

## Validation evidence

| Validation | Result and workflows |
| --- | --- |
| `make check` | Passed: all Go packages, 183 web tests, gofmt, shell syntax, vet, zero Svelte errors/warnings, ESLint and both product builds. |
| `go test -race ./...` | Passed, including concurrent starts/claims/edits/answers, event/storage synchronization and runner lifecycle. Targeted changed-package checks were repeated after final edits. |
| `make verify-isolation` | Passed: Individual builds, vets, tests, runs and migrates with all Team code removed. A separate temporary copy containing only Team and the three shared packages also built, vetted, tested, ran and migrated successfully; architecture tests prohibit execution dependencies. |
| `make test-install test-install-team` | Passed: 7 Individual and 6 Team installer checks, including checksum/version refusal, product filtering, stable feed selection, clean setup, upgrade preserving project/token state and restart. |
| Both product release builds | Passed: Darwin amd64/arm64, Linux amd64/arm64, Windows amd64/arm64, including archive creation and checksums. Windows service/PowerShell execution was not tested on this Mac. |
| Disposable browser/integration suite | Passed in Chrome: first-run setup and empty-project continuation; remote-only Codex eligibility/models; local storage denied; Team token rotation, Back/Forward and absent URL parameters; stale-edit conflict with retained drafts/focus; disconnect/reconnect; Board/review agreement; 1440 px desktop and 390 px mobile without overflow or JS errors; actual process/Git/Team review flow above. |
| Existing integration/recovery suites | Passed: real subprocess failure/process-tree stop, local controller crash/restart, signed runner disconnect/lease expiry/reconnect and journal loss, duplicate-start fencing, scheduler recovery, questions and usage, real Git fetch/divergence/missing/foreign worktrees/dirty-work safeguards, Team multi-member claims/edits, database migration and restore, intentional update failure. |

The remote lease, navigation, backup-selection and rollback-retention fixes in
0.9.1/2.1.1 were verified before extension; they were not redundantly rewritten.
No schema or runner protocol change is needed. The Impeccable code detector found
no issues in the changed browser credential module; screenshots were reviewed for
workflow clarity and layout. This is a functional UX pass, not a WCAG certification.

## Foundation decision

**YES** — the existing V0–V2 foundation is sufficiently stable to begin V3.
No demonstrated category-A blocker remains. Release/update discovery is verified
against the product tags, published archives and independent installer feeds.

## Known limits and V3 deferrals

- Unknown process ownership deliberately requires owner recovery; no automatic
  retry can safely prove arbitrary external processes have stopped.
- Team's bridge is developer-owned and foreground; remote Git work must first be
  published/fetched. Automatic background reporting remains future scope.
- Coding agents run as the OS user. Environment filtering is not an OS sandbox,
  and user-authored prompts/commits/output may contain secrets.
- Team has no automatic updater or Windows installer; TLS is provided by a proxy.
- Stopped-controller upgrades retain rollback evidence until manual health verification.
  A runner reconnect failure preserves the new controller plus old binary/DB snapshot;
  automatic database rollback after remote sync could lose ownership history.
- Real provider/GitHub/Tailscale accounts and native Windows/Linux service managers
  require release/operational testing; deterministic fixtures exercise failure
  contracts without those accounts. Cosmetic redesign and new capabilities are
  deferred.

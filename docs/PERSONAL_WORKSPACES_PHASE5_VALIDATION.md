# Personal Generative Workspaces — Phase 5 implementation and validation

Implemented 23 October 2026 on top of the existing, uncommitted Phases 1–4 work.
Local changes were preserved. No repository commit, tag or push was performed.
The pre-existing working-tree version remains `4.72.0-preview.1`.

## Delivered behaviour

- Shared management controls in the desktop shell and local workspace screen:
  rename, approved icon selection, Salie customisation, display locations,
  definition history and reviewed deletion. The shell also offers direct removal
  from its current navigation context. Zero locations are valid.
- Two explicit deletion choices. Both require confirmation; data deletion names
  collections/counts and protects every shared or unowned collection. A preview
  fingerprint binds approval to live records, schemas, ownership and dependencies.
  UI and Salie invoke the same transactional service. Failed or stale changes
  preserve the workspace and collection integrity.
- A Personal data library with hidden-workspace reopening, retained collection
  inspection and recovery into a new editable workspace using the same record IDs.
  Salie can discover and reuse retained schemas without copying or replacing data.
- Definition rollback creates a fresh revision and preserves current records and
  schemas. Existing SQLite snapshots, migration backups and restore preserve the
  personal tables, reference protection, ownership and history.
- Core tasks/projects/Git, Team data and shared memory have no deletion path in
  this feature. Team reads remain permission checked and read-only.

## Changed-file summary

| Area | Principal files |
| --- | --- |
| Lifecycle model and storage | `internal/domain/personal_lifecycle.go`, `internal/store/personal.go`, `internal/store/sqlite/personal.go`, migration `0024_personal_workspace_lifecycle.sql` |
| Protected operations | `internal/service/personal_lifecycle.go`, `internal/service/personal_proposal.go`, `internal/api/personal.go`, `internal/appops/personal_lifecycle.go`, `internal/appops/personal.go` |
| Salie and boundary enforcement | `internal/assistant/protocol.go`, `internal/archtest/assistant_test.go`, `web/src/lib/assistant/ConfirmCard.svelte`, `web/src/lib/assistant/words.ts` |
| Management and recovery UI | `web/src/lib/personal/{WorkspaceManagement,PersonalLibrary,DeletionPreview,WorkspaceProposalPreview}.svelte`, `api.ts`, `web/src/routes/PersonalWorkspace.svelte` |
| Desktop navigation and transport | `web/src/shell/{GeneratedWorkspace,Shell,Sidebar}.svelte`, `model.svelte.ts`, `desktop/internal/workspaces/personal.go` |
| Verification | Personal domain/service/API/SQLite/appops/assistant tests, desktop transport tests, shell model test, `scripts/browser-personal-lifecycle.cjs`, extended personal assistant/navigation browser suites, scripted provider fixture and `Makefile` |
| Documentation | `docs/PERSONAL_WORKSPACES.md`, `docs/ASSISTANT.md`, `docs/STRUCTURE.md`, this report |

These are Phase 5 additions/edits; the repository also contains the preserved
Phases 1–4 local changes, so the full Git diff includes earlier work.

## Executed checks

All commands below completed successfully:

- `make check`: all root Go suites (with the target's normal
  `WERKBORD_SKIP_RQLITE=1`), Go vet/build, formatting and shell syntax checks,
  frontend checks/tests/builds, desktop logic tests, installer/notarisation and
  workflow-script checks, licensing/relay checks. This run reported 815 frontend
  tests; the subsequent final frontend run includes the added library test (816).
- `make desktop-check`: desktop internal vet/tests and both macOS native-shell
  vet configurations, including the updater-test configuration.
- `go test ./internal/archtest -count=1`: fresh architectural-boundary checks.
- `cd desktop && go test ./internal/shell ./internal/workspaces -count=1`:
  fresh shell and finite native transport tests.
- `go test ./internal/api ./internal/appops ./internal/service ./internal/store/sqlite ./internal/assistant ./internal/controller ./internal/domain -run 'Personal|SaliePersonal' -count=1`:
  personal functionality, isolation, concurrency/revisions, deletion, recovery,
  SQLite backup/restore/migration and Salie engine tests.
- Supplemental `go test ./internal/store/sqlite -run Personal -count=1` and
  `go test ./internal/appops ./internal/assistant -run 'PersonalDeletion|SaliePersonalWorkspaceDeletion' -count=1`
  after the final migration assertions and deletion outcome-message changes.
- `npm --prefix web run check`: zero errors and warnings;
  `npm --prefix web run lint`: passed;
  final `npm --prefix web test`: **82 files, 816 tests passed**.
- `make test-personal-browser`: production frontend and shell builds, followed
  by all four personal browser suites, passed using disposable databases.
- `node scripts/browser-shell-assistant.cjs`: existing shell/Salie regressions,
  frame preservation, Team task confirmation and saved-workspace handoff passed.
- `git diff --check`: passed. The Impeccable mechanical detector returned no
  findings. Desktop/mobile deletion and recovery screenshots were inspected.

Earlier test attempts exposed an icon-selector accessible-name issue and browser
fixture naming assumptions; both were corrected before the successful runs.

## Requested end-to-end scenarios

| # | Scenario | Successfully executed evidence |
| --- | --- | --- |
| 1 | Create through Salie | `browser-personal-assistant.cjs`: real engine/API/SQLite and scripted provider, creation preview and confirmation |
| 2 | Choose name/icon | Lifecycle browser management test; navigation browser checks persisted name, icon and revision |
| 3 | Save/reopen | Personal and Salie browser suites, including model change and reload |
| 4 | Direct personal-record edits | Personal browser CRUD, completion, hidden-field preservation and revision conflicts |
| 5 | Modify UI through Salie | Salie browser preview/approval for urgent tasks and reordered progress with exact record identity preserved |
| 6 | Universal/Individual/Team visibility | Navigation browser opens identical workspace/record IDs in all three contexts |
| 7 | Restart and persistence | Actual disposable controller process restart and SQLite reopen in personal/lifecycle browser suites |
| 8 | Remove one location | Navigation browser invokes **Remove from this location**, returns to the retained Team frame, then restores the association |
| 9 | Workspace-only deletion and retained recovery | Lifecycle browser confirms deletion, inspects retained records, creates a replacement workspace and verifies unchanged record IDs/values |
| 10 | Workspace plus exclusive data | Lifecycle browser and Salie browser confirm explicit data deletion and verify missing workspace/collection/records |
| 11 | Protect shared collections | Lifecycle browser and API/service tests protect shared data; tests also invalidate an earlier approval when a new dependency appears |
| 12 | Inaccessible/disconnected Team | Navigation browser tests revoked, stale and offline observations; native shell tests check membership/association and finite read authority |
| 13 | Existing sidebar behaviour | Navigation browser asserts exact core ordering, icons, active state, truncation and retained Individual/Team frames; shell assistant browser verifies existing assistant/confirmation behaviour |

Additional failure evidence: an injected collection deletion failure rolls back
already deleted records and workspace/history/reference state; record edits after
a deletion preview invalidate confirmation; backup restore preserves ownership,
history and reference restrictions; migration backfill refuses to invent an owner
for ambiguous shared collections.

## Limits and recommendations

No failing executed check remains. Production builds report the existing Vite
large-chunk advisory. Browser Salie tests use a scripted provider, and desktop
browser tests use a bridge/Team stand-in; they do not validate live model output,
a real disconnected Team network or an installed packaged-app restart. Rqlite
cluster tests are excluded by the repository's normal `make check` setting.

Deletion changes the live Individual database. Older backups retain their prior
snapshots until normal rotation/removal. Live history of a deleted interface is
removed; immediate recovery preserves records in a newly created interface,
while restoring the exact deleted definition requires a prior database backup.
History lists the latest 100 revisions; collection screens have a 10,000-item
limit. Board/calendar rendering, destructive schema migrations, arbitrary external
bindings, ownership transfer, deleted-definition trash and selective backup
restore remain deferred. Consider paged collection/history browsing, clearer
backup-retention controls, bundle splitting and a packaged-app/live-provider smoke
test for a later release. Sharing, multi-user collaboration and AG-UI were not
introduced.

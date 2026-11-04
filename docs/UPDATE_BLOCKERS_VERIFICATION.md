# Shared window/web verification

Historical report for commit `ff7ffff`. The subsequent approved Analytics/Feed migration is described in [ANALYTICS_FEED_MIGRATION.md](ANALYTICS_FEED_MIGRATION.md).

Branch: `feature/clear-update-blockers`. Starting commit: `7b7d26d`. Local work only; no version change, tag or push. Raw test output is retained under `output/verification/` (ignored build artifacts).

## Requested checks

| Item | Result | Evidence / change |
| --- | --- | --- |
| 1. Web catch-up | Verified, passing | `browser-catch-up.cjs`, `make test-catch-up-browser`, CI. A first visit is saved by actual page exit; a human task is completed through the API while away. The next visit shows **0 pull requests merged · 1 item completed**. Salie's composer receives the completion evidence, with no message POST. Remote-device projection hides the action. A throwing localStorage getter leaves Overview working without page errors. Positive merge counts remain covered by the existing recap unit tests; this browser fixture does not perform a GitHub merge. |
| 2. Shared attention | Verified, passing | `browser-attention.cjs`, `make test-attention-browser`, CI. On each side a real fixture process asks a question. Answering Yes through Questions and workspace notices reaches that run, records its “Committed fixture work” output, removes the question and updates Waiting for you. The window uses the built shell, its native-call stand-in and the real Personal API/runner. |
| 3. Window gear | Verified, passing | Extended `browser-project-context.cjs`; `make test-project-settings-gear-browser`, CI. All Projects has no gear; selecting Personal project one and pressing the gear navigates its workspace frame to `#/p/one/defaults`. Returning to All Projects removes it. |
| 4. Calendar/Analytics | Fixed, passing | Extended `browser-calendar.cjs` / existing Calendar target and CI. Month, Week, List, empty `?project=` scope, dark at 1440/900px, and both current Analytics views are checked. Screenshots were inspected. Week's fixed 1050px grid hid days inside the 900px page; the shared grid now fits all seven columns and wraps event text. An internal scroll-width assertion catches this, beyond document overflow. Analytics captures wait for loaded data. Inspected screenshots are retained in `output/verification/screenshots/`. |
| 5. Unified desktop flake | Reproduced and fixed | Five saved `make test-unified-desktop-browser` runs: 1 pass, 2 pass, 3 pass, 4 fail, 5 pass. Run 4: `AssertionError: settled question closes its editor` at line 327, `1 !== 0`. The backend had persisted the answer before the relay response/Svelte render removed the editor. The test now waits for detachment after verifying the runner's answer. An additional full run passes (`unified-fixed.log`). The earlier empty-output symptom did not recur; the synchronous command helper now includes command, status, signal and spawn/timeout error so that failure cannot be blank. |
| 6. Help harness | Fixed, passing | The exact `SEED_PROJECTS must point to this pass's projects.json` assertion reproduced on `723af90^` (`facf4ed`) in a detached worktree, before browser/network activity. No dependency symlink or web build was used there; the worktree was removed afterwards. `make test-help-browser` now seeds disposable projects and passes their JSON through the harness. Updated stale Help labels, Jump to selection, settled Work address and phone Control Deck Help navigation. Added CI coverage. |
| 7. Planning / project-actions-map | Reproduced; not repaired | See exact failures below. Both scripts retain their original behavior. Investigating the new Calendar address exposed another obsolete drag assumption, so a partial migration was reverted. Completing either suite requires choosing/updating the replacement navigation and lifecycle flow rather than masking its first timeout. |
| 8. Parity blind spots | Fixed, passing | Before set comparison, duplicate visible h1 names now cause a parity error, including across frame documents. Added pure regression tests. Also compare the sidebar and outer main-page panel's x/y/width/height, allowing 2px rounding. The equivalent outer panels are used because framed content's inner main is not the same region. Repeated light/dark/900px passes show no geometry drift. |
| 9. Settings navigation | Fixed, passing | `lib/settings/SettingsGroups.svelte` owns group headings, advanced groups, item buttons and selection styling. Both hosts supply groups, selected ID and callback. The window's owner folding/back controls and each host's narrow chooser stay in their wrappers. The window's group/item design is preserved. Updated Settings browser tests for buttons; window Settings suites and parity pass. |
| 10. Calendar task entry | Restored, passing | Shared Calendar accepts a task ID; `#/calendar?project=<id>&task=<id>` anchors its week and outlines the entry. A task's Calendar relationship includes its ID. Browser test confirms Week selection and highlighted entry. Normal visits keep their remembered anchor. |
| 11. Retired URLs | Fixed, passing | The router settles both old project Calendar and Timeline links to `#/p/<id>/work` on initial read and hash changes. Browser test checks both URLs in succession, including the same parsed Work state, which a component-only effect missed. |

No entries in `scripts/parity-allowed.json` were added, changed or removed. Settings still has exactly its four Mac-only allowances.

## Failures present at the start / historical attribution

- **`scripts/browser-help.cjs`**: `AssertionError [ERR_ASSERTION]: SEED_PROJECTS must point to this pass's projects.json` at line 9. Reproduced on the parent worktree as well as starting HEAD; fixed here.
- **`scripts/browser-planning.cjs`**: `locator.waitFor: Timeout 10000ms exceeded`, waiting for `[data-plan-item="<task id>"].first()` at line 46 after visiting the retired `#/p/<id>/calendar`. `Alerts []`, `Rows []`. Parent `facf4ed` already lists Calendar in `RETIRED_SECTIONS`, and Timeline already aliases Calendar, so this obsolete assumption predates the named commits. Those commits add global Calendar and composite item IDs, so a future suite migration must use both. A diagnostic migration reached `Timed out: Individual work calendar drag` at line 48; its old drag coordinates assume an item's left edge, whereas the new month item can span several days. That partial migration was reverted. No claim of a full parent browser run is made for planning.
- **`scripts/browser-project-actions-map.cjs`**: `locator.click: Timeout 10000ms exceeded`, waiting for the framed `main` button **Delete project** at line 83. The project has been restored active before the next loop iteration. Both parent and current `ProjectHistory.svelte` only offer Delete after Archive; active projects offer Archive. The subsequent test also expects an agent list on the retired project Overview. The named commits do not create the active-project deletion mismatch. No claim of a full parent browser run is made for this suite.

Additional verification failures found during this pass:

- **`scripts/test-unified-desktop-browser.cjs`**: timing assertion described in item 5, fixed. It is a harness wait issue around persisted-answer versus rendered-editor state; no production answering code was changed.
- **`scripts/browser-assistant.cjs`**: strict-mode violation at line 686, `.file[data-state="ready"]` resolved to two elements. The captured screenshot shows the outgoing translucent chip during the session-content transition. Wait for one ready chip before using the strict locator; the whole suite then passes. No attachment/store behavior was changed.

## Verification commands

- `cd web && npm run check && npm run lint && npx vitest run`: passing; 129 files, 1948 tests, zero Svelte errors/warnings.
- `node --test scripts/parity-checks.test.cjs`: passing; also included in `make test`.
- `PARITY_STRICT=1 make test-parity`: passing, zero unallowed differences in light, dark and 900px. Read `output/parity/report.md`, `dark/report.md`, `narrow/report.md`; geometry is included.
- Full `node scripts/test-browser.cjs`: passing (`full-browser.log`).
- Window suites: browser-assistant (after transition wait), browser-shell-agents, browser-shell-assistant, browser-project-context, browser-work-overview, browser-calendar, browser-settings-sections and unified desktop: passing.
- New focused catch-up, attention, Help and gear suites: passing.
- `make check`: passing end to end twice (`make-check.log`, `make-check-final.log`).

The outstanding product decisions and estimated work are in [UPDATE_BLOCKERS_DECISIONS.md](UPDATE_BLOCKERS_DECISIONS.md). Analytics and Feed were left unchanged. The user chose to leave both Analytics and Feed unchanged pending review.

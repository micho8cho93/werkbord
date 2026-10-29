# Product simplification — Phase 1 removal report

Implemented on 2026-10-29 against `32a8389`. The working tree was clean before this work.
No version, release tag, installed service or user database was changed.

## Removed systems and dependency map

| System | Removed dependency chain | Retained consumers |
| --- | --- | --- |
| Generative Workspaces | Hub/sidebar/palette and generated routes → workspace renderer and views → typed collection runtime/editor → native workspace bridge and owner/phone APIs → Personal service/app operations → domain schemas and SQLite repositories | Ordinary Personal/Team workspaces, project routing, native workspace frames and Team assistant bridge |
| UI generation capabilities | Salie workspace prompt/context → workspace proposal/confirmation cards → feature operations, permissions, record tools, preview and lifecycle services | Salie conversations, streaming, confirmations for retained operations, provider orchestration, catch-up, attachments, dictation and CLI sessions |
| Data Library | Library navigation → collection management/editor → personal collection/record API and storage | Individual/team memory, knowledge search, analytics and ordinary task/project storage |
| Timeline presentation | Desktop route and Calendar alternate → shared Gantt layout/geometry/styles/gestures; mobile Timeline mode; Team Gantt renderer and static assets | Calendar month/week views, date moves/keyboard edits, task dependencies, planned versus scheduled dates, backend validation, scheduler and runner dispatch |
| Document editor | Generated writing views → ProseMirror editor and Markdown adapters → editor-only dependencies and license notice | Existing Markdown rendering and Salie rich replies |

The feature-specific browser fixtures, seed script, build target, workflow references, tests and current documentation were removed or updated. Mixed suites retain their unrelated assertions. No replacement feature or navigation redesign was introduced.

## Reuse and persistence

`internal/uipresentation/static/planning.js` now contains only shared calendar date arithmetic, planned spans and safe label colours. Svelte imports it through `planning-dates.ts`; Team loads the same static utility. Calendar retains its existing day/minute gestures and task editing. Gantt grid/edge gesture branches and styling are deleted.

Migration `0032_remove_generated_workspaces.sql` drops all seven generated-workspace/collection tables and their associated schema objects. It removes retired pending assistant actions and clears only generated-workspace Control Deck slots, preserving the nine-position layout, ordinary navigation/workflow slots, their targets and unrelated settings. Historical numbered migrations remain contiguous; a fresh database runs through the cleanup. The upgrade regression test initializes through migration 31 with obsolete data and verifies cleanup, preserved existing project/settings/buttons and subsequent project writes.

## Dependencies removed

Nine direct npm dependencies were removed, with the lockfile updated:

- `prosemirror-commands`
- `prosemirror-history`
- `prosemirror-inputrules`
- `prosemirror-keymap`
- `prosemirror-markdown`
- `prosemirror-model`
- `prosemirror-schema-list`
- `prosemirror-state`
- `prosemirror-view`

Editor-only transitive packages (`orderedmap`, `prosemirror-transform`, `rope-sequence`, `w3c-keyname`) and redundant nested Markdown dependencies are removed from the lockfile. OpenUI, Zod, the existing Markdown renderer and xterm remain because Salie and CLI sessions use them. No Go module dependency changed.

## Validation

Passed:

- `make check`: all Go packages, architecture/trust boundaries, frontend tests (96 files, 961 tests), Svelte checking (zero errors/warnings), ESLint, Go vet, desktop module tests, release/workflow/entitlement checks, backend and frontend builds. Its standard rqlite cluster exclusion remains in effect.
- `scripts/build-desktop.sh`: complete ad-hoc signed macOS app, including controller, Team helper, infrastructure helpers and updater validation. Output: `dist/desktop/Werkbord.app`.
- Fresh SQLite initialization tests and the populated pre-removal upgrade regression; generated-workspace API paths return 404 across methods. Team tests verify both old Timeline JavaScript asset paths return 404.
- Planning browser suite in Chromium and WebKit: Calendar date moves, contextual editing, planned/scheduled semantics, dependency validation, agent start/refusal and shell calendar editing.
- Real unified desktop browser suite: multiple teams, permissions, native runner grants, task/calendar editing, shared schedule rescheduling, workspace/frame persistence, restart and Team membership lifecycle.
- Salie browser suites: streaming, forms, confirmations, recovery, attachments, dictation, Team operations, independent conversation tabs/drafts/provider selections and reload persistence.
- CLI/phone browser suite: native terminal interaction, reconnect, phone enrollment, per-session grants/revocation, navigation and month/week scheduling.
- Mobile Control Deck suites: workflow configuration, persistence, execution/progress, onboarding, touch/keyboard controls and phone-size layouts in both themes.
- Controls and workspace UX browser suites: Personal/Team calendar controls, task review, live Analytics, project routing, settings and shared shell navigation.
- Final search for retired components, APIs, operations, native methods, imports, feature flags, dependencies and active documentation; `git diff --check`.

Browser artifacts are in ignored `output/playwright/phase1-*` directories. Provider responses and phone/browser connections use disposable local fixtures; this does not certify live provider credentials, a physical phone, distribution signing/notarization or all rqlite failure modes. Existing unit/service tests cover Individual and Team memory, isolation, freshness, scheduling and recovery.

## Remaining technical debt and deliberate boundaries

- `/timeline` data-validation endpoints and `Timeline`/`TimelineWarning` data names remain in planning APIs. Calendar and dependency tooling still consume these results; they contain no Gantt renderer. Renaming those contracts is separate cleanup.
- Old UI Timeline links resolve to Calendar. Generated-workspace links no longer select a generated view; ordinary unknown-route handling applies.
- Numbered historical SQL migrations and archived documentation retain historical feature names. Existing audit/chat history is not rewritten. Pending retired mutation actions are removed so they cannot be approved after upgrade.
- Salie's inline OpenUI `TimelineBlock` is a static conversational snapshot and remains alongside its other rich replies. It is independent of the removed workspace engine and standalone scheduling presentation.
- Kanban, Product Map, Personal/Team navigation structure, floating Salie placement and session identity/storage remain for later phases. Removing them here would expand the approved scope.
- The frontend still emits its existing large-chunk warning. This phase adds no bundling framework or unrelated optimization.

## File inventory

Paths below include all tracked deletions/modifications and new source/report files in this change. Generated build/browser output is ignored.

### Deleted (119)

- `desktop/internal/shell/personal.go`
- `desktop/internal/shell/personal_test.go`
- `desktop/internal/workspaces/personal.go`
- `desktop/internal/workspaces/personal_test.go`
- `docs/PERSONAL_WORKSPACES.md`
- `docs/PERSONAL_WORKSPACE_ENGINE.md`
- `internal/api/personal.go`
- `internal/api/personal_lifecycle_test.go`
- `internal/api/personal_runtime.go`
- `internal/api/personal_runtime_test.go`
- `internal/api/personal_test.go`
- `internal/appops/personal.go`
- `internal/appops/personal_lifecycle.go`
- `internal/appops/personal_productivity_test.go`
- `internal/appops/personal_test.go`
- `internal/assistant/personal_apps_test.go`
- `internal/assistant/personal_test.go`
- `internal/assistant/testdata/personal_apps.json`
- `internal/controller/personal_test.go`
- `internal/domain/personal.go`
- `internal/domain/personal_capabilities.go`
- `internal/domain/personal_capabilities_test.go`
- `internal/domain/personal_icon.go`
- `internal/domain/personal_icon_catalog.json`
- `internal/domain/personal_icon_test.go`
- `internal/domain/personal_interactive_test.go`
- `internal/domain/personal_lifecycle.go`
- `internal/domain/personal_productivity_test.go`
- `internal/domain/personal_proposal.go`
- `internal/domain/personal_test.go`
- `internal/service/personal.go`
- `internal/service/personal_contexts.go`
- `internal/service/personal_contexts_test.go`
- `internal/service/personal_icons.go`
- `internal/service/personal_icons_test.go`
- `internal/service/personal_lifecycle.go`
- `internal/service/personal_lifecycle_test.go`
- `internal/service/personal_proposal.go`
- `internal/service/personal_runtime.go`
- `internal/service/personal_runtime_test.go`
- `internal/service/personal_test.go`
- `internal/store/personal.go`
- `internal/store/sqlite/personal.go`
- `internal/store/sqlite/personal_icons.go`
- `internal/store/sqlite/personal_icons_test.go`
- `internal/store/sqlite/personal_long_text_test.go`
- `internal/store/sqlite/personal_test.go`
- `internal/team/console/static/timeline.js`
- `internal/uipresentation/static/timeline.d.ts`
- `internal/uipresentation/static/timeline.js`
- `scripts/browser-fixture/personal.go`
- `scripts/browser-personal-assistant.cjs`
- `scripts/browser-personal-engine.cjs`
- `scripts/browser-personal-icons.cjs`
- `scripts/browser-personal-lifecycle.cjs`
- `scripts/browser-personal-navigation.cjs`
- `scripts/browser-personal-phone.cjs`
- `scripts/browser-personal-productivity-shell.cjs`
- `scripts/browser-personal-productivity.cjs`
- `scripts/browser-personal.cjs`
- `scripts/seed-demo-workspaces.py`
- `web/public/licenses/EDITOR.txt`
- `web/src/lib/mobile/TimelineView.svelte`
- `web/src/lib/personal/ActionBar.svelte`
- `web/src/lib/personal/BindingBlock.svelte`
- `web/src/lib/personal/CollectionBlock.svelte`
- `web/src/lib/personal/DeletionPreview.svelte`
- `web/src/lib/personal/PersonalLibrary.svelte`
- `web/src/lib/personal/RecordEditor.svelte`
- `web/src/lib/personal/ShortcutBlock.svelte`
- `web/src/lib/personal/WorkspaceIcon.svelte`
- `web/src/lib/personal/WorkspaceManagement.svelte`
- `web/src/lib/personal/WorkspaceProposalPreview.svelte`
- `web/src/lib/personal/WorkspaceRenderer.svelte`
- `web/src/lib/personal/api.ts`
- `web/src/lib/personal/conversation.ts`
- `web/src/lib/personal/editor/RichEditor.svelte`
- `web/src/lib/personal/editor/editor.ts`
- `web/src/lib/personal/editor/markdown.test.ts`
- `web/src/lib/personal/editor/markdown.ts`
- `web/src/lib/personal/editor/size.ts`
- `web/src/lib/personal/example.ts`
- `web/src/lib/personal/icon-components.ts`
- `web/src/lib/personal/icons.test.ts`
- `web/src/lib/personal/icons.ts`
- `web/src/lib/personal/library.ts`
- `web/src/lib/personal/personal.test.ts`
- `web/src/lib/personal/runtime.test.ts`
- `web/src/lib/personal/runtime.ts`
- `web/src/lib/personal/schema.ts`
- `web/src/lib/personal/session.svelte.ts`
- `web/src/lib/personal/session.test.ts`
- `web/src/lib/personal/shortcuts.ts`
- `web/src/lib/personal/views/BoardView.svelte`
- `web/src/lib/personal/views/CalendarView.svelte`
- `web/src/lib/personal/views/DocEditor.svelte`
- `web/src/lib/personal/views/DocumentView.svelte`
- `web/src/lib/personal/views/FilesView.svelte`
- `web/src/lib/personal/views/PagesView.svelte`
- `web/src/lib/personal/views/ProjectionView.svelte`
- `web/src/lib/personal/views/PropertyBar.svelte`
- `web/src/lib/personal/views/SnippetsView.svelte`
- `web/src/lib/personal/views/calendar-grid.test.ts`
- `web/src/lib/personal/views/calendar-grid.ts`
- `web/src/lib/personal/views/draft.svelte.ts`
- `web/src/lib/personal/views/draft.test.ts`
- `web/src/lib/personal/views/files.test.ts`
- `web/src/lib/personal/views/files.ts`
- `web/src/lib/personal/views/pages.test.ts`
- `web/src/lib/personal/views/pages.ts`
- `web/src/lib/personal/views/shared.test.ts`
- `web/src/lib/personal/views/shared.ts`
- `web/src/lib/timeline.test.ts`
- `web/src/lib/timeline.ts`
- `web/src/routes/PersonalWorkspace.svelte`
- `web/src/routes/Timeline.svelte`
- `web/src/shell/GeneratedWorkspace.svelte`
- `web/src/shell/PersonalTeamProjects.svelte`
- `web/src/shell/personal.ts`

### Modified (131)

- `.github/workflows/release.yml`
- `DESIGN.md`
- `Makefile`
- `desktop/internal/shell/shell_test.go`
- `docs/ARCHITECTURE.md`
- `docs/ASSISTANT.md`
- `docs/BETA_READINESS.md`
- `docs/DEMO_DATA.md`
- `docs/MCP.md`
- `docs/MOBILE.md`
- `docs/ORCHESTRATION.md`
- `docs/PHONE.md`
- `docs/PLANNING.md`
- `docs/SIMPLIFICATION.md`
- `docs/STRUCTURE.md`
- `docs/TEAM.md`
- `docs/UI_UX_DRY_AND_COPY_REVIEW.md`
- `internal/api/assistant.go`
- `internal/api/clients.go`
- `internal/api/phone.go`
- `internal/api/phone_test.go`
- `internal/api/server.go`
- `internal/appops/appops.go`
- `internal/appops/operations.go`
- `internal/archtest/assistant_test.go`
- `internal/archtest/uipresentation_test.go`
- `internal/assistant/engine.go`
- `internal/assistant/protocol.go`
- `internal/assistant/turn.go`
- `internal/controller/assistant.go`
- `internal/controller/assistant_test.go`
- `internal/controller/controller.go`
- `internal/device/gateway.go`
- `internal/domain/label.go`
- `internal/domain/project.go`
- `internal/domain/task.go`
- `internal/httpkit/httpkit.go`
- `internal/httpkit/httpkit_test.go`
- `internal/mcpplan/plan.go`
- `internal/planning/analyze.go`
- `internal/runner/manager.go`
- `internal/runner/orchestration_test.go`
- `internal/runner/scheduler.go`
- `internal/service/deck.go`
- `internal/service/deck_test.go`
- `internal/service/tasks.go`
- `internal/store/sqlite/sqlite.go`
- `internal/store/store.go`
- `internal/team/console/console_test.go`
- `internal/team/console/static/console.css`
- `internal/team/console/static/console.js`
- `internal/team/console/static/desktop.js`
- `internal/team/console/static/index.html`
- `internal/team/service/planning.go`
- `internal/uipresentation/static/planning-drag.js`
- `internal/uipresentation/static/planning.css`
- `scripts/browser-controls.cjs`
- `scripts/browser-fixture/assistant.go`
- `scripts/browser-fixture/main.go`
- `scripts/browser-help.cjs`
- `scripts/browser-individual.cjs`
- `scripts/browser-navigation-workstream-4.cjs`
- `scripts/browser-phone.cjs`
- `scripts/browser-planning.cjs`
- `scripts/browser-salie-sessions.cjs`
- `scripts/browser-salie-terminals.cjs`
- `scripts/browser-shell-assistant.cjs`
- `scripts/browser-workspace-ux.cjs`
- `scripts/measure-legibility.cjs`
- `scripts/test-unified-desktop-browser.cjs`
- `scripts/test-workflows.sh`
- `web/package-lock.json`
- `web/package.json`
- `web/src/App.svelte`
- `web/src/lib/AssistantGlyph.svelte`
- `web/src/lib/DependencyPicker.svelte`
- `web/src/lib/PlanningCalendar.svelte`
- `web/src/lib/Segmented.svelte`
- `web/src/lib/TaskPlanFields.svelte`
- `web/src/lib/app-icons.test.ts`
- `web/src/lib/app-icons.ts`
- `web/src/lib/assistant/ConfirmCard.svelte`
- `web/src/lib/assistant/api.ts`
- `web/src/lib/assistant/sessions.svelte.ts`
- `web/src/lib/assistant/store.svelte.ts`
- `web/src/lib/assistant/words.ts`
- `web/src/lib/board/TaskCard.svelte`
- `web/src/lib/calendar.ts`
- `web/src/lib/embed.ts`
- `web/src/lib/help.test.ts`
- `web/src/lib/help.ts`
- `web/src/lib/labels.ts`
- `web/src/lib/location.test.ts`
- `web/src/lib/location.ts`
- `web/src/lib/mobile/CalRow.svelte`
- `web/src/lib/mobile/Deck.svelte`
- `web/src/lib/mobile/DeckLibrary.svelte`
- `web/src/lib/mobile/Keycap.svelte`
- `web/src/lib/mobile/MonthView.svelte`
- `web/src/lib/mobile/Schedule.svelte`
- `web/src/lib/mobile/WeekView.svelte`
- `web/src/lib/mobile/WorkRow.svelte`
- `web/src/lib/mobile/calendar.ts`
- `web/src/lib/mobile/calstate.svelte.ts`
- `web/src/lib/mobile/deck.test.ts`
- `web/src/lib/mobile/deck.ts`
- `web/src/lib/mobile/layout.test.ts`
- `web/src/lib/mobile/layout.ts`
- `web/src/lib/mobile/work.ts`
- `web/src/lib/planning.ts`
- `web/src/lib/planningBridge.ts`
- `web/src/lib/shell/ProjectHeader.svelte`
- `web/src/lib/state.svelte.ts`
- `web/src/routes/Calendar.svelte`
- `web/src/routes/ProjectSettings.svelte`
- `web/src/routes/Scheduling.svelte`
- `web/src/routes/TaskPanel.svelte`
- `web/src/routes/WorkOverview.svelte`
- `web/src/shell/AssistantFrame.svelte`
- `web/src/shell/Calendar.svelte`
- `web/src/shell/FrameHost.svelte`
- `web/src/shell/Hub.svelte`
- `web/src/shell/Shell.svelte`
- `web/src/shell/Sidebar.svelte`
- `web/src/shell/frames.test.ts`
- `web/src/shell/frames.ts`
- `web/src/shell/model.svelte.ts`
- `web/src/shell/model.test.ts`
- `web/src/shell/nav.test.ts`
- `web/src/shell/nav.ts`
- `web/src/shell/types.ts`

### Added (8)

- `docs/SIMPLIFICATION_PHASE1.md`
- `internal/api/obsolete_systems_test.go`
- `internal/store/sqlite/migrations/0032_remove_generated_workspaces.sql`
- `internal/store/sqlite/obsolete_systems_test.go`
- `internal/uipresentation/static/planning.d.ts`
- `internal/uipresentation/static/planning.js`
- `web/src/lib/planning-dates.test.ts`
- `web/src/lib/planning-dates.ts`


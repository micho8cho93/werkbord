# Phase 8 — UI simplification and beta hardening

30 October 2026. Ordinary implementation; no version bump, tag, push or installed-service change.
This report covers the changes in this phase and distinguishes earlier removals from new deletions.

## Features removed

The earlier simplification phases removed Kanban presentation, standalone Timeline/Gantt, Generative
Workspaces, Data Library, floating Salie and separate Personal/Team navigation from the desktop shell.
Phase 8 removes the unreferenced `MoveProject.svelte` dialog and unused `activeKey`/`shellPlace` sidebar
adapters, after repository-wide consumer searches. The native project-transfer contract and its tests
remain: removing a dialog does not establish that all transfer contracts can be retired safely.
The old drag/placement/compact-card assertions are removed from browser tests; the permission, draft,
confirmation, archive, recovery and session assertions are preserved against the current controls.
Old Board and Timeline links remain compatibility aliases to Work and Calendar.

## Features consolidated

The desktop app has one native project dropdown: All Projects, Personal projects and Team projects.
Work, Calendar, Agents, Analytics and Salie are the five primary destinations, with Settings separate.
Project identity is the existing workspace/project tuple; All Projects creates no persisted project.
The same selection scopes summaries, Analytics and subsequent Salie turns without granting authority.
Git, Product Map, reviews, run history, memory and administration remain contextual tools.

Work, Personal, Team and plan section controls share `internal/uipresentation/static/work-layout.css`:
24px headings, consistent padding, search fields, section tabs, focus and responsive controls.
The 224px sidebar becomes a labelled 64px icon rail at narrow widths. Destinations and section controls
have 44px targets. Narrow search uses a full row. Existing themes, Geist, pixel mark and status colours stay.
The previously disconnected catch-up component is restored in All Projects → Work. Its attention action
opens and focuses the questions/notices disclosure. Salie explanation refreshes the underlying evidence.

## Architecture simplified

Presentation is shared; product services, permission checks, execution and coordination remain separate.
There is no new state store, UI framework, backend service or schema. The existing catch-up cursor store
now has a reachable consumer. The command palette retains workspace-owned detail routes.
Salie's Team success links now generate the canonical Work route instead of the Board alias.
Team Work restores the `with-ticket` wrapper: an open ticket stays within the viewport rather than
falling below a long queue. The queue exposes its existing accessible label as a region.

Validation exposed concurrent infrastructure builds sharing `nebula.new`. Each build now stages privately
and serialises publication per target. The regression runs eight concurrent publishers and verifies a
matching binary, module files and provenance, executable permissions and staging cleanup. Pinned source
commit verification and fresh builds are preserved.

## Dependencies eliminated and audit decisions

No package dependency was removed in Phase 8. Every direct runtime dependency has a retained consumer:
Lucide icons, OpenUI replies, xterm sessions, Markdown and Zod validation. `go mod tidy -diff` is clean.
Removing any of these to reduce a count would remove retained functionality. No new dependency is added.

| Audit area | Evidence and action |
| --- | --- |
| Dead components | Svelte filename/consumer inventory found MoveProject and CatchUp; delete the unused dialog, reconnect documented catch-up. |
| Obsolete routes/APIs | Retired generated-workspace APIs are already removed; tests retain 404/compatibility coverage. Native transfer remains pending a separate contract audit. |
| Duplicate UI | Extract Work chrome from shell, Personal and Team; retain product-specific task/action adapters. |
| Redundant state | Remove unused sidebar route classifiers; retain project context, session identity, transcript and recovery stores with live consumers. |
| Database structures | Migration 0032 already removes generated-workspace/collection tables. Preserve numbered migration history and live memory, planning and session storage. No destructive migration is justified. |
| Outdated tests | Migrate former sidebar sections, floating drag/compact cards, Board selectors and completed-card assumptions; keep the underlying behavioural checks. |
| Documentation | Update PRODUCT, DESIGN, architecture/structure, navigation, developer setup and active unified-platform plans. Preserve dated historical reports. |
| Stale flags/preferences | No active floating-placement flag remains. Keep startup removal of old localStorage preferences so upgrades discard them. Runtime agent/phone grants are permissions, not stale feature flags. |

## Validation results

All browser/service fixtures use disposable data. Final verification, 30 October 2026, on the committed tree:

- `make check`: passed (Go, lint, desktop, workflows, premium console, web build). A run with `WERKBORD_ADDR` and
  `WERKBORD_DATA_DIR` exported failed `internal/config` and `internal/launcher`; those tests assert default
  precedence, pass with a clean environment, and are not a regression.
- Browser and install suites, each run alone and passing: project-context, Work, planning, assistant, phone,
  onboarding, recommendations, held-runs, plan, motion, workspace UX, the broad browser run, Team desktop (rqlite),
  unified desktop (rqlite) and fresh install; plus agent sessions, shell Agents, shell sign-in and memory settings.
  `browser-memory-settings` must run through `scripts/test-browser.cjs`, which refuses a non-disposable origin.
- `make test-project-context-browser`: passed; selector ownership/composite IDs, All Projects, persistence,
  offline/revoked context, scoped Analytics, Salie context, plans/catch-up, narrow layout and runtime errors.
- Work acceptance: passed; final shared plan-control confirmation pending.
- Assistant, shell Agents, shell Salie and sign-in recovery: passed individually after stale assertions were migrated.
- Planning: passed in Chromium/WebKit; planned dates, execution rescheduling, recovery preferences and All Projects edits.
- Team browser: passed after restoring ticket viewport placement and adapting completed-work disclosures.
- Development installer: passed; real ARM64 app/DMG, checksum, mounted bundle, unified versions, ad-hoc
  signatures, updater configuration and development-build release refusal. This does not install a service.
- `go mod tidy -diff`: passed, no dependency graph change.
- Design detector: no findings. Independent finish review: **ship**, scoped to current desktop/narrow
  shell evidence and inspected source; both subsequent ticket-viewport and shared-plan-tab corrections reviewed resolved. Not a certification of every screen, theme or assistive technology.

## Remaining technical debt

- The controller PWA and standalone Team administration console keep their service-specific navigation;
  the one cross-service selector belongs to the desktop shell. Mobile retains the Control Deck.
- Legacy `/timeline` validation names and wire lifecycle states remain consumed by Calendar and orchestration.
  Salie's inline Timeline snapshot is a conversational component, separate from the removed Gantt UI.
- Native project-transfer APIs remain without the removed dialog. Audit programmatic/native consumers before removal.
- The Personal bundle still reports the existing large-chunk and ineffective dynamic-import warnings.
  Splitting the session singleton requires preserving circular-navigation and session identity semantics.
- Historical documents and migration files intentionally retain earlier vocabulary.

## Known limitations and beta-release blockers

Local fixture success does not certify live GitHub/provider credentials, a physical phone, independent
multi-machine runner/TUN/NAT connectivity, clean-machine service installation, boot/Keychain behaviour,
real customer backup replacement, or a production updater rollout. Those need their corresponding
external environments and release artifacts. No live credentials, installed data or services were changed.

The generated DMG is development/ad-hoc signed, not a distributable Developer ID/notarised release.
No automated check is red. The final beta gate requires the physical acceptance above to be completed. Existing trust-anchor, signing-custody, artifact/advisory and independent-host
requirements in [TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md#8-production-blockers-and-required-acceptance)
remain release gates; this UI phase does not waive them. See also [PRODUCTION_ACCEPTANCE.md](PRODUCTION_ACCEPTANCE.md).

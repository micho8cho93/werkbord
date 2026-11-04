# Analytics detail and one Work overview

The user approved this implementation after reviewing the Analytics and Feed decision notes. The earlier decision to leave both unchanged applied to the verification pass in `ff7ffff`; this change supersedes it.

## Analytics

Both the Mac window and browser draw the same three views from `lib/analytics/AnalyticsPage.svelte`:

- **Work:** creation, moves to Done, durations, progress and categories for the selected project context and period.
- **Runs and usage:** Personal execution rows filtered by provider and runner, including unrecorded runners; separate token and API-cost measures; outcomes and provider/runner comparisons; reporting coverage, model details and a keyboard-scrollable breakdown.
- **Outcomes:** accepted/rejected assessments, moves to Done, normal session ends, controller-verified merges and completion evidence. Each remains a separate signal. Missing evidence stays unknown.

Execution filters run over the report's existing rows, so neither native Analytics permissions nor its read signature needed expansion. Project and runner labels are supplied by each host. The filters do not change Work or Outcomes totals. Daily usage trends are omitted while a provider or runner filter is active because the report does not provide daily measurements split by those dimensions; an explanation is shown instead of presenting unfiltered daily data as filtered evidence. Team-only contexts explicitly lack Personal execution/outcome evidence.

## Feed migration

Overview is now the single attention destination. The former Feed tab and its unused UI components have been removed. Old `#/control?section=feed` links settle to Overview while retaining other query parameters.

| Former Feed capability | Destination |
| --- | --- |
| Kind, runner and agent filters | Questions and workspace notices; selected filters remain visible after their last matching item is settled |
| Team tickets awaiting a local repository | A prominent shared section above Overview's decisions; local-folder linking and GitHub cloning remain explicit user actions |
| Blocker choices and failed-run retries | Buttons on the relevant shared attention item |
| Risk explanation and recommended next step | Shared workspace notices, linking to Git |
| Expanded live run activity | Running now, in an expandable recent-activity section; the task still opens the complete session |
| Schedule readiness and waiting reasons | Calendar's Agent schedules section, reached from Overview; planned dates remain separate from agent starts |
| Inline alert preference | Alerts settings shortcut to each host's existing preferences |

The controller adds bounded display evidence to its neutral workspace summary: local run/runner context, pending repository imports and readiness reasons. The shared contract validates these additions, limits their size and refuses Personal execution context from coordination workspaces. No Team execution capability or credential was added.

The window sends explicit actions through Personal's existing authenticated frame. The frame re-reads current controller records: retry requires the same latest failed run, a blocker choice must still be offered, and repository linking/cloning uses the current pending item rather than a projected address. The controller remains the authority. Lost or refused responses are shown without automatically repeating a mutation.

`parity-allowed.json` has **two removals**: the temporary web Feed entries for `work` and `project-work`. No allowances were added. The four Settings allowances are unchanged.

## Verification

Raw logs and inspected screenshots are retained under `output/verification/`.

- Web type checking and lint: zero errors/warnings. Vitest: 130 files, 1,953 tests passing.
- Go workspace/service projection tests and architecture boundaries pass, including bounded context, coordination-only rejection, blocker choices, activity and readiness.
- Analytics browser test covers real API evidence, filtering, cost/token choice, keyboard access, completion semantics and screenshots at 1440px, 900px and 390px.
- Attention browser test covers both the built window and web: actual question answers, ongoing activity, failed-run retry, blocker choice, filters, real Git repository linking, incoming work, Alerts settings and retired Feed URLs.
- Calendar browser test covers schedule readiness separately from planned dates, existing edit/drag behavior, scoped empty projects, task anchoring and dark/narrow layouts.
- Window shell-agents, shell-assistant, project-context, work-overview, settings-sections and catch-up suites pass. The unified desktop suite passes, including real Team handoff, repository linking, answers and calendar edits.
- The full `node scripts/test-browser.cjs` harness passes. Its first attempt hit the unrelated existing 300ms **Ctrl-U reaches the native PTY** assertion in `browser-salie-terminals.cjs`; the isolated suite and full retry both pass. No terminal implementation or test was changed for this task.
- Strict parity covers Work, all three Analytics views, Calendar and the other existing screens in light/dark and at 900px, with zero unallowed differences. Reviews retains its documented existing skip.
- `make check` passes end to end on the final sources (`migration-check-final.log`), including strict light/dark/900px parity and all three Analytics views.

The historical `browser-planning` and `browser-project-actions-map` failures documented in the prior verification report were outside this migration and were not masked or removed.

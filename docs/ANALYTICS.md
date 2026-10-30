# Operational analytics

**Analytics** remains a primary sidebar destination and one of the Control Deck’s six default actions.
Choose a project or **All Projects** using the window’s project context, then the last 7/30/90 days.
The browser view also filters execution evidence by agent provider and runner.
Team has a separate **Analytics** section describing coordination for projects the signed-in member may read.
Neither view ranks people or measures customers, marketing, attendance or productivity of employees.

The view is a read-only snapshot. Its timestamp is the observation time, not evidence that an agent was active then.
A selected period ends at that snapshot; the start is included. Stored timestamps have millisecond precision, so records
stamped in the same millisecond as the snapshot are included and the next stored millisecond is excluded. Charts use UTC
calendar days. The first and last displayed day can be partial days.

Both views open with created/completed counts, average and median duration, and the trend chart. Execution, usage,
completion evidence and breakdown are disclosures in Individual; coordination details and work categories are disclosures
in Team. Qualifiers stay with the figures: partial history, unknown values, reported versus estimated costs, and recorded
versus verified outcomes.

## Usage trends and coverage

The existing analytics report includes daily usage totals for runs grouped by their **start day in UTC**.
These are the latest cumulative measurements for each start-day cohort, not evidence of daily spend.
The browser and desktop reuse the same trend component; the browser hides this trend when a runner filter
is selected because the daily report is scoped by project/provider, not runner. The detailed execution breakdown
continues to respect that runner filter. Missing measurements remain unknown, while measured zero remains zero.
Provider-reported API cost and estimated API equivalents remain separate, with reporting counts for each.
The desktop’s model breakdown combines matching provider/configured-model rows across the selected project context;
a configured model does not prove which model the provider resolved. Team contributes coordination metrics only.

## Individual metric definitions

| Metric | Definition / evidence |
| --- | --- |
| Tasks created | Persisted task `created_at` in the period, including archived tasks. |
| Moved to Done | Unique tasks with a recorded transition from a known non-Done state to Done in the period. Task-created/updated events establish the preceding state; edits while already Done do not count. Reopening and completing again counts the task once per selected period, on its first completion in that period. |
| Average / median task duration | Elapsed seconds from task creation to the first counted move to Done in the period. Includes backlog, waiting and review time; it is not CPU time or hands-on work time. Only non-negative intervals with known creation dates contribute; sample count is shown. Even-sized medians average the two middle values. No samples is unknown, not zero. |
| Currently blocked / stopped | Latest recorded run of each non-archived task outside Done, with state `blocked` / `stopped`, regardless of its start date. A previous failed/stopped attempt superseded by a newer run does not count. Waiting for a person is a separate outcome, not a policy blocker. Recorded state does not establish physical process liveness, especially on an offline runner. |
| Execution attempts | Distinct persisted runs started in the period. A session with multiple turns is one attempt. |
| Retries | Runs in that cohort with recorded `attempt > 1`. This does not count internal provider retries, reconnects or individual turns. Continuations may be additional numbered attempts. |
| Completion outcomes | Current state of runs in the start cohort: normal exit (`completed`), failed, stopped, blocked, waiting, starting or running. The counts add up to attempts; they are observations at the snapshot time. |
| Agent-reported task completion | **Not recorded.** Existing events do not preserve a separate agent claim that its task is complete. |
| Normal session ends | Distinct runs with controller-emitted `agent.completed` events in the period, regardless of when the run started. This records a normally ended session, including a person-finished idle session. It is not an agent claim about task completion and does not validate a summary, tests or the work. Repeated events for the same run count once. |
| Independently verified task completion | **Not recorded.** The existing data has no independent task-correctness or test verifier. A successful exit, handoff saying tests passed, Done column, accepted assessment, or reported PR merge cannot fill this metric. |
| Human accepted / rejected | Latest explicit run assessment decision recorded in the period, per run, derived from changes in `run.state_changed` usage acceptance. Repeated cumulative usage snapshots do not create new decisions. A later clearing of the assessment removes it from the period's accepted/rejected tally. Prior decisions are not dated from `updated_at`; this timestamp changes for unrelated reasons. |
| Controller-verified merges | Distinct runs with successful (`ok`, outcome `done`) task-bound `git.merged` events in the period. The Git operation checks target containment after merging. This verifies that Git effect only, not correctness or tests, and is independent of Done and acceptance. Unbound merges cannot be attributed to a task/run and are excluded. |
| Provider and model usage | Attempts grouped by exact project, recorded runner ID, agent runtime ID and configured model string. Empty runner IDs stay “Not recorded”; no present-day local runner is assigned retrospectively. Empty models mean an unrecorded agent default; configured aliases do not establish the resolved model, backend or a model switch during a session. Groups are ordered by identifiers, not performance. |
| Input / output / cached tokens | Sum each reported field independently from the latest persisted cumulative usage on runs started in the period. Zero is a known value; absent fields remain unknown. Each field shows the number of contributing runs. Cached tokens are separate and never added to input/output totals. `partial` reports are counted explicitly and totals can be lower bounds. |
| Reported API cost | Sum valid `actual_api` USD amounts with a recorded source, with coverage count. This records the source's claim, not a reconciliation against a bill. |
| Estimated API equivalent | Separately sum valid `estimated_api_equivalent` USD amounts with a recorded source and basis, with coverage count. No new price table, token-to-price conversion or subscription allocation is introduced. An estimate is not the person's subscription cost or invoice. |
| Detected usage-limit notices | Existing system-stream adapter notices: Claude Code's exact `Rate limit: rejected`; Codex's exact `The turn failed: usage limit reached`, `rate limit exceeded` or `rate limit reached` (case-insensitive reason). Counts notices in the period for known, correctly scoped runs, including older runs. Assistant/user prose, generic errors, allowed warnings and other runtimes are excluded. “None detected” does not mean no limits occurred or quota remains. Detailed output retention can remove notices after 30 days. |
| Work categories | Current user-defined task labels, with created and Done counts. A task with several labels appears in each; category totals are not additive. Tasks without labels appear as Unlabelled. Labels are not inferred from prompts, titles or people. Historical label assignments are not reconstructed. |

**Provider** in this view means the **agent runtime** (for example, Codex or Claude Code), not a presumed billing backend.
An upstream provider for OpenCode or a cloud-backed runtime is unknown unless it is represented in existing evidence;
this view never assigns one from a model name. A provider filter selects runs by their exact agent ID. For task throughput
and categories, it includes tasks with any recorded run from that runtime, including an earlier run; it does not attribute
creation or a board decision to the agent. Current blocked/stopped counts require the latest run to match the filter.

Usage and cost are **lifetime counters for the runs started in the period**, observed through the snapshot. They are not
spend within calendar days, and usage from runs started before the period is excluded. Limit notices, normal session
completion events, verified merges and assessment decisions instead use their own event timestamps; their counts can exceed
the start cohort.
The runner/provider/model breakdown may therefore contain a row with no new attempts but an older run's limit or assessment
in the period. Each event is joined to its exact run, task and project before attribution. Provider-internal retry attempts,
remaining allowance, reset times, subscription tier and subscription charges are unavailable here.

## Team coordination metric definitions

Team stores coordination, not the execution engine's telemetry. It reads existing ticket creation dates and
`ticket.completed` activity, including activity retained after reopening or archiving a ticket.

- **Tickets created:** stored ticket creation dates in the period, archived tickets included.
- **Completed tickets:** unique tickets with a recorded completion activity in the period, counted on the first such
  decision in that period. Reopening does not erase the original completion activity.
- **Average / median duration:** creation to that first counted completion; includes all waiting, with sample count and
  the same unknown/median rules as Individual.
- **Human completion decisions:** completion workflow activity recorded by an authorised reviewer or project owner.
  Multiple completion decisions for a reopened ticket remain separate decisions while throughput counts it once.
  This is an authenticated workflow decision, not proof of correctness or cryptographic proof that a human used the token.
- **Work categories:** current workspace labels attached to the visible tickets, including Unlabelled; multi-label counts
  are not additive.

Agent reports, verified task correctness, blocked/stopped execution, attempts, retries, runner/model/provider usage, tokens,
cost and provider limits are **unavailable in Team analytics**. Team does not reach into members' machines or import
Individual's execution packages to obtain them. A reported PR merge is not independent verification. Provider filtering
would have no supported meaning in this coordination view and is not offered.

## Coverage, permissions and query cost

Individual uses one consistent `Store.View` with compact projections of existing tasks, runs and lifecycle events.
It excludes prompts, task text, handoffs and transcripts. It projects only the recognised system notices needed for
limit detection. Each input is capped at 20,000 records, with an extra row detecting truncation; reads use small indexes
that skip transcripts. Lifecycle events establish historical transitions and assessment timestamps. No per-run query,
materialised aggregation, analytics table, background collector or analytics write is added.

When a task's preceding state is absent, a Done update is excluded rather than guessed from its modification date.
A bounded read or a recorded event-retention floor produces a partial-history notice. The retention notice is conservative:
removed transcript events may not affect every metric. Lifecycle evidence for terminal runs can be removed after a year;
detailed terminal output after 30 days. Legacy/missing events and missing reports cannot be reconstructed. Persisted task/run
counts remain available when their event evidence is absent. Deleted projects/records and older rows outside the cap are
not reconstructed. “All projects” means the controller/workspace serving this view, not all workspaces in the desktop shell.

Both Individual endpoints (`GET /api/analytics`, `GET /api/projects/{pid}/analytics`) require the controller's full
credential; narrow local-program tokens do not gain analytics access. Query parameters are `project`, `days` (7/30/90,
default 30), and `provider` (exact agent ID). A project path cannot be overridden with another project query parameter.
Team endpoints (`GET /api/team/v1/analytics`, `GET /api/team/v1/projects/{id}/analytics`) use `project` and `days`.
They recheck the authenticated session and project access in the read transaction, require both ticket and activity view
permissions, and scope every database read to the workspace and authorised project IDs. Hidden projects are reported as
not found. Workspace project visibility alone does not grant ticket/activity metrics.

Team caps the authorised project set at 200 and each compact ticket/completion input at 20,000 records, and announces
truncation. It uses two scoped projection queries plus existing permission/label reads, with no ticket text or member
identities returned. Its UI keeps the last requested snapshot through ordinary live board updates, invalidates it on
project-access changes, and applies pending project/time filters on **Apply / refresh**. Individual refreshes only on
**Apply / refresh**.
Neither view repeatedly recalculates on streaming agent output. HTTP responses use `Cache-Control: no-store`.

## Validation

`make check` runs aggregation, projection, range-validation and permission tests, including source-specific limit notices,
repeated completion/usage snapshots, known-zero versus missing measurements, average/median calculations, project filtering,
read-only behaviour, hidden Team projects and cross-workspace isolation. `make test-browser` also runs
`scripts/browser-analytics.cjs` with disposable controllers, storage and credentials, verifying the real API/database → UI
flow, filters, completion evidence and desktop/mobile rendering. A focused run after `make web web-embed` is:

```sh
BROWSER_ANALYTICS_ONLY=1 node scripts/test-browser.cjs
```

No provider credentials, live usage-limit endpoint or subscription account is queried by analytics tests.

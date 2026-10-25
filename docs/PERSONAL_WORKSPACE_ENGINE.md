# Composable personal workspace engine

## Audit and implementation plan

The existing path is domain → Personal service → PersonalRepo → the controller's
SQLite database. Strict owner HTTP routes and a finite desktop bridge expose it.
Salie uses record-free proposals, current revisions, atomic approval and durable
history. The Svelte renderer compiles a validated JSON tree to a separate OpenUI
registry. Shared collections already update multiple views together. Existing
forms, filters, typed sorting, calendar, Kanban, rich documents, nested pages,
snippets and local file previews can all be reused. The sidebar already places
personal workspaces beneath Analytics in Individual and connected Team contexts.
Team context IDs are display metadata; Team projects use the shell's separate
permission-checked read bridge. No Team service changes are needed.

The missing primitives are calculated numeric fields, summary aggregation, named
atomic record actions, conditions, durable record rules, rendered Individual read
bindings, and timeline/relationship projections. Extend schema version 1
additively. Old definitions remain valid, records keep their IDs and schemas
cannot destructively evolve. No new runtime dependency or generated code.

Implementation order:

1. Numeric constraints and controls, bounded arithmetic formulas, form and metric
   views. Reuse existing records, filters, sorts, date fields and calendar.
2. View-scoped literal CRUD steps, selection and conditions. Execute all steps in
   one transaction guarded by workspace, collection and record revisions. Always
   review the exact action before execution. Generated controls cannot approve
   Salie proposals or mutate native projects/tasks.
3. Single-record status rules and scheduled rules with persisted occurrence
   watermarks. Reuse the controller's existing scheduler maintenance lifecycle.
   Resolve declared Individual projects/tasks through existing owner storage,
   rechecking references on reads. Contextual assistance opens an editable Salie
   draft; generated text is never treated as approval.
4. Read-only countdown, timeline and hierarchical relationship projections over
   those same records. Keep native tokens, responsive layouts and existing pages.

Validation includes domain grammar and formula failures, atomic rollback and
stale revisions, schedule idempotency, permission/transport boundaries, browser
interactions, durable reopen, and all five requested use cases through the real
Salie engine with a scripted provider (no model credentials needed).

## Shipped phases

### 1. Flexible data and inputs

`PersonalField` adds `min`, `max`, numeric `input` (`slider` or `rating`) and an
optional `formula`. Existing text, long text, URL, boolean, number, select,
multiselect, date and datetime fields remain unchanged. A rating is an integral
bounded scoring control. Datetime entry now uses the existing local-time helpers
and the native datetime picker. The existing record editor also renders a `form`
view, which resets its draft after a successful creation or cancellation.

Formula fields are optional numbers, read-only in editors. Their grammar is a
bounded tree: `{op: "field", field: "revenue"}`, `{op: "literal", value: 0.5}`,
or `{op: "multiply", args: [...]}`. The registry includes add, subtract,
multiply, divide, min and max. At most 8 levels, 64 expanded nodes and 8 operands
per expression; subtract and divide take two. References must name numeric fields
in the same collection. Cycles, unknown operators and non-finite literals fail
validation. Missing operands, division by zero and overflow produce an absent
value, displayed as unavailable. There is no string expression parser or eval.

The server recomputes derived values on writes and reads, so changing a formula
does not depend on rewriting saved records. The browser uses the same arithmetic
semantics for previews and displays. Shared collection changes update all views.
Numeric bounds may relax during schema evolution, but tightening bounds or
switching between stored and calculated fields requires a migration and is
refused. Old version 1 schemas still parse without conversion or new SQL tables.

A metric view binds `{metric: {operation, field?}}`: count needs no field;
sum/average/min/max require a numeric field. Aggregates use the complete loaded
collection after saved filters, including calculated fields. Missing values are
excluded; an empty numeric aggregate is unavailable. Existing typed filtering,
sorting, calendar and board behaviour is reused.

### 2. Actions and conditions

Views optionally declare `actions: [{id, name, when?, steps}]`. Each step has an
allowlisted personal-record `operation`, a literal typed `values` map and optional
`clear` keys. Only fields bound to that view may be written, and the view must
explicitly permit the operation. Formula fields cannot be assigned or cleared.
Creation steps must provide required values. No supplied URL, API name, SQL,
script, model tool call or native application mutation is interpreted.

The native action bar selects up to 100 records, with a searchable bounded list,
and displays exact steps before confirmation. The action endpoint accepts only
workspace/schema/record revisions, selected record IDs and confirmation. It
reloads the saved definition and schemas, checks filters/conditions, then performs
up to eight steps for all selected records in one existing SQLite transaction.
An invalid later step, conflict or storage failure rolls back writes and events.
A create-only action requires no selection. Mixed actions create once per
selected target. Results refresh every view of that collection. Failed actions
preserve the selection and explain how to reload before retrying.

`when` conditions use typed equality and all must match. `visibleWhen` hides a
view unless at least one record matches; it is presentation, never permission.
Normal direct record CRUD continues to use the existing owner endpoints. Generated
action reviews cannot confirm Salie proposals, and vice versa.

### 3. Automation, internal data and contextual AI

Collections may declare up to 16 `rules: [{id, when?, set, schedule?}]`.
Unscheduled rules run once in declaration order on that record's write, then
formulas recompute. No recursive trigger dispatch or cross-record side effects.
A rule can update only declared non-calculated fields using validated literals.

A schedule binds `dateField`, optional datetime `lastRunField` and `everyDays`
(0 means one-shot; otherwise 1–3650 UTC days). The controller's existing runner
scheduler invokes personal maintenance once per minute, independently of browser
or agent activity. Each tick processes a bounded keyset page of collections and
records. The latest due occurrence updates its watermark and record together,
then applies ordinary status rules in the same transaction. Missed occurrences
coalesce to the latest occurrence. Restart or retry cannot replay a completed
occurrence. A failed transaction leaves its cursor and watermark unchanged.
These are in-workspace reminders/status updates, with no OS, email or push
notification delivery. Open workspaces refresh records once a minute; intervening
saves and open drafts remain protected from an in-flight refresh.

`binding` layout nodes now resolve declared `individual.project` and
`individual.tasks` references. The owner service fetches only the declared local
project, rechecking existence on every read. Counts use current stored tasks;
a task binding displays up to 100 task titles/states with full-project counts.
A missing project fails only that component with a recovery message. Refresh is
explicit, and proposal previews explain that reads resolve after saving. Salie
can add or revise these declarations through the existing reviewed proposal path.
No destination in display metadata grants read authority. The existing desktop
Team projects bridge remains separate and retains its membership checks.

A contextual action has `prompt` and `steps: []`. Clicking opens an editable Salie
draft for the exact workspace; it never sends a message automatically. The draft
and prompt travel only through the existing trusted desktop assistant frame, or
the local Individual assistant. Generated prompt text cannot approve operations.
`read_personal_records` exposes at most 20 records from a declared view, applies
its saved filters and returns only its bound fields. Its result is capped, and
larger fields are explicitly omitted. `read_personal_record_field` reads full text
in bounded Unicode chunks. Both retain the existing personal-read restriction:
owner assistant only, never project-scoped principals or MCP. The existing model
handles summaries, generation, classification, extraction and recommendations;
all returned saved content remains an escaped, untrusted operation result.

The desktop owner's finite personal transport permits only the new action POST
and declared binding GET shapes. Existing phone grants are unchanged; those two
new routes are not part of an enrolled phone's grant. Core project/task writes
continue through Werkbord's existing native screens and Salie's protected tools.

### 4. Visual composition

The same records also project into `countdown` and `timeline` views through a
`dateField`; timelines optionally bind `endDateField`. Dates without times use
UTC days; datetimes retain their instant. Timelines expose actual date labels
alongside proportional bars. A `hierarchy` view binds a text `parentField` holding
record IDs, reusing the nested-page relationship convention. Iterative traversal
shows each record once, even with missing parents, self references, cycles or very
deep graphs. Branches expand/collapse and indentation is bounded. Existing grid,
stack, cards, shortcut layouts, documents and pages compose these projections.
No new chart/diagram library or third-party embed is introduced.

## Verification and limits

Run `make check` for repository checks, frontend tests/lint/build, desktop checks
and architecture boundaries. Targeted regression tests cover arithmetic/cycles,
missing results, numeric schema compatibility, atomic action rollback/replay,
owner/transport boundaries, bounded AI reads, reminder occurrence deduplication
across service restart and actual scheduler dispatch without a frontend.

`go test ./internal/assistant -run TestSalieComposableAppsCreateInteractReopenEditDelete -v`
uses the real Salie engine and a scripted provider to create, approve, interact
with, reopen, revise and delete all five tools. The launch case connects an
existing native task. Fixtures live in
`internal/assistant/testdata/personal_apps.json`; their values are illustrative.
This is a deterministic integration test, not evidence of live model quality.

`node scripts/browser-personal-engine.cjs` uses disposable SQLite and the same
scripted provider for all five creations through the real Salie UI. It checks
approval, direct CRM entry, action review, Kanban/metric updates, scoring edits,
form/calendar/doc rendering, all light/dark desktop/phone-width layouts, controller
restart, unchanged saved record IDs/values and reviewed deletion. Screenshots go
to ignored `output/playwright/personal-engine/`. Existing personal lifecycle and
assistant browser scripts remain regression checks for the earlier contracts.

This release deliberately provides a smaller set of composable primitives.
Relationships remain within a collection; there is no general cross-collection
join, free-form flowchart/mind-map editor, arbitrary custom field plugin, file
upload store or external connector registry. Existing local PDF/image previews
and document editing are reused. Schedules update personal records only; generated
workspaces do not launch agents or make native task mutations automatically.
Existing native shortcuts/actions remain the path to those capabilities. AI work
requires the user to send the editable draft through a configured provider.

Validation outcome for 4.80.0-preview.1: `make check` passed (902 frontend tests,
Go/desktop tests and vet, architecture checks, release-script tests and builds).
The five-workspace browser suite passed, including persistence after controller
restart. The visual review inspected all 20 settled light/dark desktop/mobile
captures and returned `ship`; its documentation handoff required no design-system
changes. Focused runtime tests also verify that an unavailable/deleted native
project makes its read component unavailable without blocking personal actions.

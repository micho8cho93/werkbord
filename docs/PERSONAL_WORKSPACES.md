# Personal Generative Workspaces

The current implementation includes Phases 1–5. The Phase 5 lifecycle contract
below supersedes historical phase-specific restrictions described later in this
file. Personal workspaces remain private, local Individual data in every display
context.

Personal workspaces are private, durable data owned by the current Individual
controller. The foundational storage model is described first, followed by
historical implementation phases and the current Phase 5 lifecycle contract.

## Ownership and storage

The path is `internal/api → internal/service.Personal → store.PersonalRepo →
internal/store/sqlite`, using the existing controller database, transactions and
migration/backup machinery. Migration `0022_personal_workspaces.sql` adds four
fixed tables: `personal_workspaces`, `personal_collections`, `personal_records`,
and `personal_workspace_collections`. No user input becomes a SQL identifier or
query; no table is created per workspace. The reference table enforces collection
existence and protects referenced collections from deletion.

Workspace and collection specifications are bounded JSON documents. Records are
JSON objects of typed values in a generic table indexed by `(collection_id, id)`.
Controller-assigned IDs (`pws_`, `pcl_`, `prec_`, followed by 16 lowercase base32
characters), positive `revision`, `createdAt` and `updatedAt` accompany each entity.
Times are UTC RFC3339 in the API and Unix milliseconds in SQLite. Revisions start
at 1. Every saved workspace definition also has a revision-history entry; collection and record revisions remain optimistic concurrency counters.

A workspace contains `name`, approved `icon`, `uiSchemaVersion: 1`, `definition`,
`visibility`, `collectionIds`, and `readBindings`. Visibility is an object with
`universal`, `individual`, and `teamWorkspaceIds`. Zero or more locations may be selected. A workspace with no locations remains accessible in the Data library. Location identifiers are bounded opaque strings; the shell's
`team:<slot>` identifiers are accepted alongside workspace IDs. They are never
resolved by the controller, checked for membership, or used as destinations.
They grant neither sharing nor read access to a Team. Phase 2 must resolve the
appropriate identity for display through the desktop's trusted workspace registry.

Several workspaces, and several views in one workspace, can use the same
collection. Migration 0024 adds local collection ownership provenance and change
markers in `personal_collection_state`. Workspace-only deletion preserves all
collections. Reviewed data deletion can remove exclusively owned collections;
shared or unowned collections are protected. Direct collection deletion still
refuses collections with records or workspace references. See Phase 5 below.

All tables participate in the existing whole-database snapshots, pre-upgrade
backups, inspection, and restore-and-migrate behavior. Personal changes append
invalidation events in their transaction and publish after commit. Those events
contain only the entity ID and revision, never definitions, values, or names.

## Version 1 schemas

A collection is `{name, schemaVersion: 1, fields: [...]}`. Each field has a stable
`key`, display `name`, `type`, optional `required` (default false), and selection
`options` where applicable. Types are `text`, `number`, `boolean`, `date`
(`YYYY-MM-DD`), `datetime` (RFC3339), `select`, and `multiselect` (unique strings).
Only declared keys and correctly typed, non-null values are accepted. Required
means present; an empty text value is permitted. Optional values are omitted.
There are no implicit coercions or defaults. Text is plain data and must be
rendered as escaped text in Phase 2.

Schema replacements may rename labels, reorder fields, relax required fields,
add choices and append optional fields. They cannot remove or retype fields,
remove choices, or strengthen required constraints. This keeps all saved records
and view bindings valid without an unbounded scan or silent data conversion.
Destructive schema evolution needs a separate, explicit migration design.

`definition` is `{views: [...], layout: {...}}`. A view has `id`, `name`, `kind`,
`collectionId` and nonempty `fields` referencing declared keys. Kinds:

| Kind | Additional binding |
| --- | --- |
| `table`, `list` | none |
| `board` | `groupBy`, a select field |
| `calendar` | `dateField`, a date or datetime field |
| `checklist` | `checkedField`, a boolean field |

Layout nodes form a closed tree:

| `type` | Allowed properties beyond `type` |
| --- | --- |
| `stack` | nonempty `children` |
| `grid` | `columns` (2–4), nonempty `children` |
| `heading`, `text` | nonempty plain `text` |
| `view` | `viewId`, referencing one declared view |
| `binding` | `bindingId`, referencing one declared read binding |

No arbitrary properties, expressions, JavaScript, CSS, HTML nodes, SVG, URLs,
SQL, actions or executable components exist in this grammar. Markup-like content
inside text is inert text. `icon` must be one of the approved subset of
`web/src/lib/Icon.svelte`; a parity test prevents references to missing icons.

Read bindings are `{id, kind, projectId}`. The initial finite registry permits
only `individual.project` and `individual.tasks`: read-only references to
existing local resources outside the personal collection model. Their project
must exist when saving the definition. Phase 1 stores and validates declarations;
it implements no data resolver. URLs, remote APIs, credentials, arbitrary
connection tools and write bindings are rejected. Future external providers
require explicit server-side authorization and a reviewed finite binding kind;
a saved declaration must never become authority by itself. A resolver must
recheck existence and permissions at read time and handle deleted resources.

The persistent grammar is intentionally distinct from ephemeral conversational
OpenUI programs. Phase 2 must map this validated grammar to approved components;
it must not evaluate a saved definition as conversation source. No assistant or
MCP operation can create or modify these entities in Phase 1.

## HTTP contract

Base: `/api/personal/v1`. All routes require the controller's own bearer token,
`AuthRequired`, a loopback caller, and explicit `Options.Personal` wiring. Tokenless
mode cannot use them. Private-network wiring sets this service to nil. Scoped
local-access, MCP and phone credentials receive no new routes or permissions.
Cross-origin writes retain the existing origin checks. Query-string credentials
are never accepted. The desktop's credential and frame boundaries are unchanged.

| Resource | Collection endpoint | Entity endpoint |
| --- | --- | --- |
| Workspaces | `/workspaces` | `/workspaces/{id}` |
| Collections | `/collections` | `/collections/{id}` |
| Records | `/collections/{cid}/records` | `/collections/{cid}/records/{id}` |

- `POST` to a collection endpoint creates and returns the entity (201).
  Workspace/collection bodies are their specification. A record body is `{values}`.
  Client IDs, timestamps and revisions are not accepted on create.
- `GET` on an entity returns it (200). Record IDs are checked within the exact
  collection in the path; a valid record in another collection returns 404.
- `PUT` fully replaces mutable data and returns the entity with incremented
  revision (200). Bodies are `{revision, workspace: <spec>}`,
  `{revision, collection: <spec>}`, and `{revision, values}` respectively.
- `DELETE` on collection/record entities takes `{revision}` and returns 204.
  Workspace deletion instead requires the reviewed request described in Phase 5
  and returns its deletion result (200). Stale revisions return 409; missing or
  nonpositive revisions return 400. There is no silent last-writer-wins behaviour.
- `GET` on a collection endpoint accepts only `limit` (1–100; default 50) and
  `after` (an ID of the matching entity type), ordered lexically by stable ID.
  Returns `{items, nextAfter?}`. Use `nextAfter` as the next `after`; a full final
  page can produce one extra empty page. This is keyset traversal, not a frozen
  snapshot across requests. No user SQL, offsets, filtering or arbitrary sorting.
- `GET /capabilities` exposes schema versions, approved icons and read-binding
  kinds, and body/document/page limits. It requires the same owner authorization.

Requests must be a single UTF-8 JSON object with `Content-Type: application/json`.
Unknown fields, duplicate keys (including case variants), trailing values,
malformed queries, invalid IDs and invalid layouts return 400. The usual error
envelope is `{error: {code, message}}`. Missing credentials return 401, rejected
scope/location 403, missing entities 404, unavailable listener 503, and wrong
content type 415. Request bodies over 64 KiB return 413.

Other limits: 32 KiB per specification/value object; 120 bytes per name/option;
64 fields per collection; 100 choices per select; 16,000 bytes per text value;
32 collection references and views per workspace; 16 read bindings; 64 display
locations; layout depth 8, 128 total nodes, and 2,000 bytes per text node. Queries
are parameterized and indexed; list responses contain at most 100 bounded items.
There is no global storage quota or record-retention policy in Phase 1.

## Verification

`personal_test.go` in domain, store/sqlite, service and api covers closed layout
validation, icon registry parity, types and schema evolution, CRUD, stale and
concurrent updates, failed-write rollback, collection scoping, restart durability,
whole-database snapshot restore, upgrade from the previous schema, backup restore
and migration, malformed/oversized HTTP requests, bounded pagination, controller
isolation, unchanged existing tasks/projects, and owner/local-access/phone trust
boundaries. Existing application and architecture suites remain applicable.

## Phase 2 — saved interactive screens

Open `/#/personal` on the authenticated local Individual controller and choose
**Save to-do example**. This saves a personal collection and a complete workspace
definition without asking a model. Bookmark the resulting
`/#/personal?id=pws_…` URL. It loads the same saved definition and SQLite records
on subsequent visits and after restarting the controller. There is deliberately
no sidebar entry or Salie creation workflow in this phase. Nothing is installed
or seeded until the person chooses to save the example.

`web/src/lib/personal/WorkspaceRenderer.svelte` is the reusable renderer, supplied
with a loaded `WorkspaceSession`. It uses the installed `@openuidev/svelte-lang`
Renderer and Werkbord's existing Text, Heading, Card, Container, Progress and
BarChart Svelte components. It has a separate finite component registry and
context. Saved JSON is strictly validated, then compiled to OpenUI with quoted
strings and internal node identifiers. Saved OpenUI source, arbitrary component
props, scripts, actions, URLs, HTML and API requests are never interpreted.
The assistant registry, parser, snapshot stamps and conversation/confirmation
flow retain their existing contract. The shared container additionally supports
four columns and correctly stacks every column variant at narrow widths.

Phase 2 extends schema version 1 additively:

- A `card` layout node has a plain `text` title and nonempty `children`.
- Views may declare `operations: ["create", "update", "delete"]`, with no
  duplicates. Omitted/empty means read-only. These operations only address
  personal records in that view's declared collection and fields (plus its
  boolean `checkedField`). Create views must include every required field.
- `progress` and `status` views bind a boolean `checkedField`; `chart` binds
  `groupBy` to a select field with at most 20 options. These views are read-only.
  All calculations use loaded personal records, never fabricated observations.
- Editable `table`, `list` and `checklist` views support text, number, boolean,
  date, datetime, select and multiselect collection fields. Priority uses a
  select field with declared choices. The example supplies Low, Normal and High.

Collection views call only fixed, ID-validated Phase 1 record endpoints through
an allowlisted dispatcher. They never contact an LLM or run assistant operations.
Writes carry revisions. Full record replacements merge edits with stored values,
so hidden fields survive layout changes; clearing an optional field removes that
key explicitly. Successful writes update every view of the collection together.
Conflicts and failed requests preserve the draft and explain how to reload;
there is no automatic retry of an uncertain mutation. Deletion has an explicit
inline confirmation. Search, typed table sorting and field filters are local
presentation state, never data replacements.

Collections load through all bounded API pages, with a defensive screen limit of
10,000 records per collection; larger collections show a recovery message rather
than silently presenting partial totals. Stored Phase 1 board/calendar views and
external `binding` layout nodes have no Phase 2 renderer/resolver: attempting to
render them produces the safe unsupported-definition screen. Read declarations
never grant additional access. Malformed/unknown nodes, fields, operations,
versions and references also fail safely before mounting components. Reload
recovers after repairing the saved definition; existing records are untouched.
Visibility remains display metadata. Identity-based sidebar integration remains
out of scope, including resolving Team display associations.

Verification: `npm --prefix web run check`, `npm --prefix web run lint`,
`npm --prefix web test`, and the personal domain/API/service/SQLite/controller
and architecture Go suites. `node scripts/browser-personal.cjs` builds the
repository's disposable browser fixture, starts Vite, and runs real OpenUI/API/
SQLite flows: saved interface loading, direct CRUD and completion, shared-view
updates, layout replacement preserving hidden fields, stale-write draft recovery,
sorting/filtering, light/dark at desktop/390px, controller restart, and recovery
from invalid components. It asserts no model requests or browser exceptions and
writes screenshots under `output/playwright/personal/` (ignored by Git).


## Phase 3: conversational creation and revision

Implemented on top of the APIs and renderer above. See
[Personal generative workspaces in Salie](ASSISTANT.md#personal-generative-workspaces--phase-3)
for the operations, permissions, confirmation preview and rollback workflow.
`0023_personal_workspace_history.sql` adds definition history (including a backfill
of current saved definitions) and durable creation deduplication. The Phase 1
statement about no history describes that phase only.

**Create with Salie** starts a conversation from the personal workspace screen;
**Edit with Salie** includes the exact saved workspace ID in the editable message.
The model must retrieve SQLite state, then propose a record-free change. Creation
saves empty collections and the workspace in one transaction; edits use workspace
and collection revisions and never replace records. Supported generated views
match the Phase 2 renderer: table, list, checklist, progress, status and chart.
Board/calendar and external read bindings remain unsupported by the renderer and
cannot be proposed. Saved equality filters add `{field, value}` entries to views;
all filters must match a correctly typed existing value. Missing values never
match a filter. Filters do not alter records.

Display selections are saved and reviewed as the existing visibility metadata:
Universal, Individual and opaque selected Team context IDs can be combined and
changed by an approved proposal. Identity-based sidebar integration is unchanged.
No Team service, execution, Git or agent permissions are added.

Demonstrate both flows without external model credentials:

```sh
go test ./internal/assistant -run TestSaliePersonalWorkspaceConversationCreateReopenModify -v
node scripts/browser-personal-assistant.cjs
```

The browser flow creates and approves a workspace through Salie, enters a real
task, changes models, reopens the saved workspace, previews an urgent-task section
with progress moved to the top, approves, then verifies the exact same record ID,
revision and values. It also checks duplicate requests and reload persistence.
Desktop/mobile captures are in `output/playwright/personal-assistant/`.

## Phase 4 — desktop navigation

The desktop window lists each visible personal workspace once, keyed by its saved
`pws_` ID. Universal appends entries after Calendar; Individual appends them after
Analytics (including the existing project shortcuts and Add project); Team appends
them after Analytics. The rows reuse `.nav`, 16px registered icons, single-line
ellipsis, full-name tooltips and accessible labels in both sidebar modes. A quiet
**Create with Salie** row follows the saved entries. Footer controls keep their
existing positions. There is no new navigation category.

Opening an entry renders the Phase 2 `WorkspaceRenderer` in a shell-managed page.
Neither the Individual frame nor a Team frame is navigated or unmounted. Their
routes, in-progress input and navigation state survive. Salie's approved saved
workspace links also open this page. All contexts use the exact same controller
workspace, revision, collection IDs and records; visibility creates no copy.

The header provides **Customize with Salie** and a management overflow containing
name/icon/location settings, reload and deletion. Saving metadata replaces only
the workspace specification with its current revision; conflicts preserve edits
and require a reload. Deletion explicitly keeps the collection records. The
location controls support Universal, Individual and any combination of known
Team IDs, including retaining an unavailable saved association. Salie can change
these same fields through its existing reviewed proposal.

Customization carries the saved workspace ID **and revision** as structured
`personalEditing` context, separate from the editable message. The controller
validates that identity through its owner-only saved-workspace operation before
starting a turn, rejects stale revisions, and adds the verified identity to the
provider's trusted context. Saved names and content remain untrusted. Confirmation
and optimistic revisions remain required for generated design changes.

The shell's finite `PersonalWorkspaceCall` delegates to the existing Individual
transport: only bounded `/api/personal/v1` resource paths, methods and bodies are
accepted, using the controller's current loopback address and owner credential.
There are no redirects, proxies, arbitrary URLs or credentials in generated UI.
The ordinary frame relay cannot call this method or `PersonalTeamProjects`.

For each assigned Team, a restrained read-only Team projects disclosure reads
through `PersonalTeamProjects`. The native side rereads the saved association,
checks the desktop registry's current membership/connection, and invokes only the
existing authenticated, permission-filtered `list_projects` operation. The page
receives project IDs/names and an observation time. It never receives Team URLs,
tokens or a callable Team operation registry. Every refresh clears prior rows;
unavailable/revoked reads show recovery, and snapshots older than 30 seconds are
labelled stale and hidden. No Team frontend, storage, collaboration or write
operation is changed. Arbitrary saved external binding nodes remain unsupported.

Reproduce the three-context demonstration with disposable controller/SQLite data:

```sh
npm --prefix web run build
npm --prefix web run build:shell
node --experimental-strip-types scripts/browser-personal-navigation.cjs
```

It opens one saved workspace in Universal, Individual and `team:main`, edits the
same record and reopens it in every location, checks exact ordering and `.nav`
accessibility/truncation, retains both live frames and their unfinished state,
checks the structured Salie handoff, exercises revoked/offline/stale Team states,
and changes name/icon without touching records. Captures are written to
`output/playwright/personal-navigation/`. The browser uses the repository's native
bridge stand-in; desktop Go tests separately cover the real transport and denied
frame/Team authority. Engine tests cover stale and malformed editing context.


## Phase 5 — management, deletion and recovery

### Architecture and supported surfaces

Both the desktop shell and the local Individual workspace screen use
`WorkspaceManagement.svelte`. It provides name/icon changes, display locations,
definition history and the two deletion choices. Desktop additionally offers
**Remove from this location**, which changes only the current association.
Universal and Individual are independent booleans; Team associations are opaque
registry IDs. Removing the last location is valid and never removes a definition,
collection or record. The **Data library** is always available in the
shell, and on the local personal-workspace screen, even after the last generated
workspace has been deleted. It lists saved interfaces including hidden ones,
collection record counts and workspace usage, read-only record inspection, and
**Use in new workspace**. Recovery creates a fixed editable table bound to the
same collection and record IDs, with Universal and Individual selected.

The supported generated UI remains: escaped text/headings; stack, grid and card
containers; editable tables, lists and checklists; read-only progress, status and
selection-count charts; typed record forms; equality filters, local search and
table sorting. Approved icons reuse the existing icon registry. Board/calendar
view schemas can be stored, but the current renderer refuses them. Arbitrary
HTML, JavaScript, CSS, URLs, tool calls and generated approval controls are not
supported. Team projects are a separate fixed read-only disclosure, not a writable
generated component.

### Ownership, retention and deletion

Ownership is local collection provenance, not navigation visibility. The first
workspace to attach an unowned collection claims it. The migration backfills
ownership only for collections with exactly one existing reference; ambiguous
shared collections remain unowned. Other workspaces may reference an owned
collection without becoming its owner. If its owner is deleted while the
collection is retained, ownership becomes null. No Team context owns these rows.

**Delete workspace only** removes the definition, its saved definition history,
creation-deduplication entry and all navigation entries. All collection schemas
and records remain. The result explicitly directs the person to the library.
A future workspace can reuse those exact collections, including through Salie's
`list_personal_collections` and a new `propose_personal_workspace` proposal with
the exact collection ID, current revision and unchanged schema. Reuse never
replaces existing values or inserts sample records.

**Delete workspace and its data** first computes a server-side preview. A
collection is eligible only when this workspace owns it and it has exactly one
live workspace reference. The preview names collections, counts personal records,
shows dependent workspaces (first 20 when there are more), and marks each
collection as Delete or Keep. Shared and unowned collections are always kept,
even if the requested mode is delete-data. Both UI choices require explicit
confirmation. Only personal tables are reachable by these deletion operations;
core board tasks, projects, Git information, Team databases and shared memory
have no deletion path here.

The controller hashes the reviewed workspace revision, mode, ownership, collection
revisions, record counts, monotonic record/reference change generations and usage
summary. The UI submits that fingerprint with confirmation. In one existing
SQLite write transaction the service recomputes and compares the plan, deletes
the definition, deletes eligible records and collections, and appends invalidation
events. Any intervening record insert/update/delete, schema edit, workspace edit
or dependency change invalidates the review (409). Any failed write rolls back
the definition, history, records, ownership and reference rows together. The
repository also refuses record purges while any live workspace references the
collection. No automatic retries occur after uncertain network outcomes.

### Salie and security boundaries

`propose_delete_personal_workspace` is an owner-only personal proposal using the
existing `personal:propose` permission and mandatory human confirmation gate.
`keep_data` supports “Delete my weekly planner but keep the tasks”; `delete_data`
supports “Delete the content calendar and all of its associated personal records”.
An unspecified mode must be clarified. The model identifies/disambiguates the
saved workspace and retrieves its current definition before proposing deletion.
The fixed `DeletionPreview.svelte` independently retrieves the authoritative
preview, verifies its fingerprint and disables Confirm until it matches. It
lists all affected collections even when the conversational summary is clipped.
Execution calls the same `DeleteWorkspaceReviewed` service as the UI and checks
the fingerprint again. Natural-language text, generated screens and stored text
cannot approve an action. The engine tests use scripted providers, not live LLMs.

Personal routes require the loopback controller's explicit owner bearer
credential and auth-enabled listener, with one reviewed exception: an enrolled
phone, on the routes listed in “Phone and the Control Deck” below. Scoped
local-access and MCP callers receive no new authority. The shell transport allows only finite resource and
lifecycle paths/methods; generated components and ordinary frames cannot invoke
it. It never accepts arbitrary HTTP destinations. No Team package or shared
execution behaviour was added. Team display associations grant no membership or
write authority. The native read bridge rechecks saved association, live desktop
membership/connection and the existing Team read permission. Disconnected,
inaccessible, revoked and stale observations display explicit unavailable states;
personal records continue to work locally.

### Recovery and backups

Every definition replacement is retained by the existing history triggers.
**Definition history** lists the latest 100 revisions, loads a selected definition
for review, and restores it as a new revision using the current workspace revision.
Rollback validates its collection references and UI grammar in the same
transaction. It never rolls collection schemas or personal records backwards;
a missing collection causes a safe failure rather than a broken saved reference.
Collection evolution remains additive; failed edits leave valid records and
bindings intact. Records contain typed scalar/list values, not record-reference
foreign keys, so deleting a record cannot leave a dangling record binding.
Collection foreign keys restrict deletion while a live definition references it.

All personal tables, lifecycle metadata, definition history and triggers use the
existing Individual SQLite file. Existing whole-database snapshots, pre-upgrade
backups and restore-and-migrate include them; no second backup system is added.
Tests restore ownership, counts, reference protection and definition history from
a real SQLite snapshot and exercise schema migration/backups. Live deletion does
not erase older backups: those retain their snapshots until normal retention or
explicit backup removal. Deleting a definition removes its live history; restoring
that exact deleted interface requires a prior full backup. Preserved collections
can be recovered immediately without restoring a backup. SQLite secure deletion
is enabled by the existing database opener; no forensic-erasure guarantee is made.

Additional owner endpoints under `/api/personal/v1`:

| Method | Path | Contract |
| --- | --- | --- |
| POST | `/workspaces/{id}/deletion-preview` | `{revision, mode}` → authoritative deletion plan |
| DELETE | `/workspaces/{id}` | `{revision, mode, fingerprint, confirmed:true}` → applied plan |
| GET | `/collections/{id}/usage` | Ownership, record count, change generation, live workspace usage |
| GET | `/workspaces/{id}/history` | Latest 100 definition revision metadata entries |
| GET | `/workspaces/{id}/revisions/{revision}` | Historical definition and current collection schemas |
| POST | `/workspaces/{id}/rollback` | `{revision, targetRevision}` → new validated workspace revision |

### Validation and deferred work

`make check`, `make desktop-check`, the personal Go suites and browser scripts
exercise the existing boundaries and full renderer/API/SQLite flows. Run
`make test-personal-browser` for the four personal browser suites (create/edit,
Salie, desktop navigation and lifecycle recovery). Fixtures use disposable local
SQLite data, Chromium and a scripted model; desktop browser fixtures stand in for
the native bridge and Team UI. Native Go tests separately verify the real finite
transport and access checks. These tests do not establish live model quality,
real Team-network connectivity or a packaged-app GUI restart. Screenshots are
written under ignored `output/playwright/personal*` directories.

Deferred: sharing, multi-user collaboration, AG-UI, destructive schema migrations,
board/calendar rendering, arbitrary external bindings, ownership-transfer UI,
per-collection backup restore, deleted-definition trash and history paging beyond
the newest 100 revisions. The library and renderer have defensive 10,000-item
screen limits. The existing frontend build may warn about bundle chunk sizes;
code splitting is outside this phase.

## Phone and the Control Deck

There is one copy of every generated workspace and its records: the controller's database. The computer and a phone
are two views of it, so a workspace made on either one is the other's with no step in between.

### What a phone can do

An enrolled phone (docs/PHONE.md) opens generated workspaces and works with their **records**: it reads workspaces and
collections, and adds, changes and deletes records, exactly as a view's operations allow. Its reviewed routes are the
`/api/personal/v1` entries in `internal/api/phone.go` (listed again in the test that fails when the list changes). It
cannot create, rename, replace or delete a workspace or a collection, read definition history, roll back, preview a
deletion or use the data library's usage view. Those stay at the computer. Creating or editing a workspace from a phone
goes through Salie and its fixed confirmation card, as on the computer; the Assistant is already available to phones.

The service is wired on the private network's listener (which the relay also uses) with `PersonalPhonesOnly`: only an
enrolled phone, on those routes, is served personal data there. The controller's own token still never opens it from the
network. On a phone the page has Back, no workspace-management menu and no data library, and a table is read as
labelled cards instead of a wide table. A tokenless controller still serves no personal data.

### The Control Deck

A generated workspace can be a Control Deck button. Its `button` is the workspace's ID (`pws_` and 16 base32
characters), validated as a navigation button: it has no execution settings, and the controller does not check that the
workspace still exists, because it can be deleted at the computer. A button whose workspace is gone says “Removed
workspace” and can be removed in Edit Deck. Each phone has its own deck.

Buttons are added from the deck's Add button library (“Your workspaces”) or from a workspace's page (“Add to Control
Deck”). Separately, the deck lists **every** generated workspace under “Workspaces” and has **Create with Salie**, so a
workspace is on a phone without being placed on a button. Pinning is a shortcut, not a requirement.

### Staying in step

Workspace, collection and record changes emit `personal.*` invalidation events (an ID and a revision, never content).
The app subscribes to them: the phone's deck list and an open workspace page reload when either device changes
something, and the open page does not read over a save that is still going. The desktop shell polls its list every few
seconds. A deleted workspace leaves both lists on the next change.

### Where Create with Salie is offered

Not in the Universal sidebar (a workspace is made for Individual or a Team context, not for everything). In Individual
and Team contexts it is the first entry of the generated group, with the workspaces listed beneath it.

Verified by `scripts/browser-personal-phone.cjs` (Chromium and WebKit, phone-sized touch browser, real enrolled-phone
credential): deck listing, live updates both ways, record work, Back, no owner-only controls, card tables, deck button,
refusals, deletion at the computer, and a workspace made with Salie on the phone.

## Workspace icon identity

Generated interfaces use dedicated 5×5 glyphs, separate from navigation and action
icons. Salie requests `icon: "auto"`; the controller assigns an unused
`workspace-XXXXXXX` token before the fixed approval preview. Editing preserves the
saved token unless a new icon is requested. Confirmation rechecks availability
in the write transaction and rejects a stale colliding proposal rather than
changing the approved icon. A partial unique SQLite index also enforces this.

The sidebar, header, approval preview, phone deck and workspace settings share
one safe renderer. Its token defines only 25 occupied/unoccupied cells; it cannot
carry SVG, markup, URLs or executable content. Older built-in icon values remain
readable as stable dedicated glyphs, and new assignments avoid those glyphs too.
